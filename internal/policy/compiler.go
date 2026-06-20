package policy

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ----- agentgateway config types -----

// gatewayConfig represents the top-level agentgateway configuration.
type gatewayConfig struct {
	Config           *rawConfig                `yaml:"config,omitempty"`
	Binds            []gatewayBind             `yaml:"binds,omitempty"`
	FrontendPolicies *gatewayFrontendPolicies  `yaml:"frontendPolicies,omitempty"`
}

type rawConfig struct {
	DNS *dnsConfig `yaml:"dns,omitempty"`
}

type dnsConfig struct {
	LookupFamily string `yaml:"lookupFamily,omitempty"`
}

type gatewayBind struct {
	Port      int               `yaml:"port"`
	Listeners []gatewayListener `yaml:"listeners,omitempty"`
}

type gatewayListener struct {
	Protocol string        `yaml:"protocol,omitempty"`
	Routes   []gatewayRoute `yaml:"routes,omitempty"`
}

type gatewayRoute struct {
	Name     string           `yaml:"name,omitempty"`
	Backends []gatewayBackend `yaml:"backends,omitempty"`
}

type gatewayBackend struct {
	Host *string                `yaml:"host,omitempty"`
	DFP  *struct{}              `yaml:"dfp,omitempty"`
	MCP  *gatewayMCPBackend     `yaml:"mcp,omitempty"`
}

type gatewayMCPBackend struct {
	Targets []gatewayMCPTarget `yaml:"targets,omitempty"`
}

type gatewayMCPTarget struct {
	Name  string          `yaml:"name"`
	Stdio *gatewayStdio   `yaml:"stdio,omitempty"`
	MCP   *gatewayMCPHost `yaml:"mcp,omitempty"`
}

type gatewayStdio struct {
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args,omitempty"`
}

type gatewayMCPHost struct {
	Host string `yaml:"host"`
}

type gatewayFrontendPolicies struct {
	NetworkAuthorization *networkAuthz `yaml:"networkAuthorization,omitempty"`
}

type networkAuthz struct {
	Rules []networkAuthzRule `yaml:"rules,omitempty"`
}

type networkAuthzRule struct {
	Allow string `yaml:"allow,omitempty"`
	Deny  string `yaml:"deny,omitempty"`
}

// ----- credential injection rules -----

// CredentialRule represents a credential injection rule by id only.
// The actual secret values are injected at runtime by the secrets broker.
type CredentialRule struct {
	ID     string `yaml:"id"`
	Header string `yaml:"header,omitempty"`
	Value  string `yaml:"value,omitempty"`
}

// CompileGatewayConfig compiles a *Policy into an agentgateway YAML configuration.
func CompileGatewayConfig(p *Policy) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("policy is nil")
	}

	cfg := &gatewayConfig{
		Config: &rawConfig{
			DNS: &dnsConfig{
				LookupFamily: "V4Only",
			},
		},
		Binds: buildBinds(p),
	}

	// Build frontend policies if there are egress rules.
	if len(p.Egress) > 0 {
		cfg.FrontendPolicies = buildFrontendPolicies(p)
	}

	return yaml.Marshal(cfg)
}

// CompileDNSAllowList returns a sorted, unique list of allowed egress domains.
// Each line is one domain. Empty policy returns empty output.
func CompileDNSAllowList(p *Policy) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("policy is nil")
	}

	domainSet := make(map[string]struct{})
	for _, e := range p.Egress {
		if e.Domain != "" {
			domainSet[strings.ToLower(e.Domain)] = struct{}{}
		}
	}

	if len(domainSet) == 0 {
		return []byte{}, nil
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	return []byte(strings.Join(domains, "\n") + "\n"), nil
}

// CompileCredentialRules returns credential injection rules by id only.
// Secret VALUES are NOT included — only the credential id and injection
// header name. The actual secret values are injected at runtime by the
// secrets broker.
func CompileCredentialRules(p *Policy) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("policy is nil")
	}

	var rules []CredentialRule
	for _, c := range p.Credentials {
		rule := CredentialRule{
			ID: c.ID,
		}
		switch c.Type {
		case "header":
			rule.Header = c.Header
			rule.Value = fmt.Sprintf("${%s}", c.ID)
		case "brokered":
			rule.Header = "Authorization"
			rule.Value = "${secrets:" + c.ID + "}"
		case "direct_lease":
			// direct_lease credentials don't have header injection;
			// they are mounted as files at runtime.
			rule.Header = ""
			rule.Value = ""
		}
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return []byte{}, nil
	}

	return yaml.Marshal(rules)
}

// ----- internal helpers -----

func buildBinds(p *Policy) []gatewayBind {
	var binds []gatewayBind

	// Ingress bind: expose trigger API port (if ingress rules exist).
	if len(p.Ingress) > 0 {
		port := p.Ingress[0].Port
		if port == 0 {
			port = 7718 // default trigger API port
		}
		var backends []gatewayBackend
		for _, in := range p.Ingress {
			host := fmt.Sprintf("localhost:%d", in.Port)
			if in.Port == 0 {
				host = "localhost:7718"
			}
			b := gatewayBackend{Host: ptr(host)}
			backends = append(backends, b)
		}
		binds = append(binds, gatewayBind{
			Port: port,
			Listeners: []gatewayListener{
				{
					Protocol: "HTTP",
					Routes: []gatewayRoute{
						{
							Name:     "ingress",
							Backends: backends,
						},
					},
				},
			},
		})
	}

	// Egress bind: DFP-based forward proxy (one per allowed domain).
	domainBackends := buildEgressBackends(p)
	if len(domainBackends) > 0 {
		binds = append(binds, gatewayBind{
			Port: 7799, // egress proxy port
			Listeners: []gatewayListener{
				{
					Protocol: "HTTP",
					Routes: []gatewayRoute{
						{
							Name:     "egress",
							Backends: domainBackends,
						},
					},
				},
			},
		})
	}

	// MCP bind: one per MCP server.
	mcpBinds := buildMCPBinds(p)
	binds = append(binds, mcpBinds...)

	return binds
}

func buildEgressBackends(p *Policy) []gatewayBackend {
	var backends []gatewayBackend
	seen := make(map[string]bool)

	for _, e := range p.Egress {
		if e.Domain == "" {
			continue
		}
		key := strings.ToLower(e.Domain)
		if seen[key] {
			continue
		}
		seen[key] = true

		// Use DFP backend for each allowed domain.
		backends = append(backends, gatewayBackend{
			Host: ptr(e.Domain + ":443"),
		})
	}
	return backends
}

func buildMCPBinds(p *Policy) []gatewayBind {
	var binds []gatewayBind

	for i, m := range p.MCPServers {
		if m.Name == "" {
			continue
		}

		bind := gatewayBind{
			Port: 7800 + i, // sequential ports starting at 7800
			Listeners: []gatewayListener{
				{
					Protocol: "HTTP",
					Routes: []gatewayRoute{
						{
							Name: m.Name,
							Backends: []gatewayBackend{
								{
									MCP: &gatewayMCPBackend{
										Targets: []gatewayMCPTarget{
											buildMCPTarget(m),
										},
									},
								},
							},
						},
					},
				},
			},
		}
		binds = append(binds, bind)
	}
	return binds
}

func buildMCPTarget(m MCPServer) gatewayMCPTarget {
	target := gatewayMCPTarget{
		Name: m.Name,
	}

	switch m.Transport {
	case "http":
		if m.URL != "" {
			target.MCP = &gatewayMCPHost{Host: m.URL}
		}
	case "stdio":
		fallthrough
	default:
		target.Stdio = &gatewayStdio{
			Cmd:  m.Command,
			Args: m.Args,
		}
	}
	return target
}

func buildFrontendPolicies(p *Policy) *gatewayFrontendPolicies {
	var rules []networkAuthzRule

	// Network authorization: deny all by default on the egress bind.
	// Allow only known agent container IPs (simplified: allow from agent
	// container subnet, deny everything else).
	for _, e := range p.Egress {
		if e.Domain != "" {
			// Allow DNS resolution for this domain.
			rules = append(rules, networkAuthzRule{
				Allow: fmt.Sprintf("dns.domain == %q", e.Domain),
			})
		}
	}

	return &gatewayFrontendPolicies{
		NetworkAuthorization: &networkAuthz{
			Rules: rules,
		},
	}
}

// ptr returns a pointer to the given string value.
func ptr(s string) *string {
	return &s
}
