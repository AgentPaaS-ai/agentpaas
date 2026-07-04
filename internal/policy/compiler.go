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
	Name      string                `yaml:"name,omitempty"`
	Hostnames []string              `yaml:"hostnames,omitempty"`
	Matches   []gatewayRouteMatch   `yaml:"matches,omitempty"`
	Policies  *gatewayRoutePolicies `yaml:"policies,omitempty"`
	Backends  []gatewayBackend      `yaml:"backends,omitempty"`
}

type gatewayRouteMatch struct {
	Method string `yaml:"method,omitempty"`
}

type gatewayRoutePolicies struct {
	DirectResponse *gatewayDirectResponse `yaml:"directResponse,omitempty"`
}

type gatewayDirectResponse struct {
	Status int    `yaml:"status"`
	Body   string `yaml:"body,omitempty"`
}

type gatewayBackend struct {
	Host    *string            `yaml:"host,omitempty"`
	Dynamic *struct{}          `yaml:"dynamic,omitempty"`
	MCP     *gatewayMCPBackend `yaml:"mcp,omitempty"`
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
	Connect *connectConfig `yaml:"connect,omitempty"`
}

type connectConfig struct {
	Mode string `yaml:"mode"`
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
		FrontendPolicies: &gatewayFrontendPolicies{
			Connect: &connectConfig{Mode: "route"},
		},
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
		if e.Domain == "" {
			continue
		}
		// Skip wildcard domains without explicit AllowWildcard (defense-in-depth).
		if isWildcardDomainBlocked(e) {
			continue
		}
		domainSet[strings.ToLower(e.Domain)] = struct{}{}
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

	// Egress bind: hostname-based routing with dynamic (DFP) backends.
	egressRoutes := buildEgressRoutes(p)
	if len(egressRoutes) > 1 { // >1 because denied route is always added
		binds = append(binds, gatewayBind{
			Port: 7799, // egress proxy port
			Listeners: []gatewayListener{
				{
					Protocol: "HTTP",
					Routes:   egressRoutes,
				},
			},
		})
	}

	// MCP bind: one per MCP server.
	mcpBinds := buildMCPBinds(p)
	binds = append(binds, mcpBinds...)

	return binds
}

// isWildcardDomainBlocked returns true if the egress rule has a wildcard domain
// but AllowWildcard is not explicitly set to true. This is defense-in-depth:
// validation should catch these, but the compiler must also enforce it.
func isWildcardDomainBlocked(e EgressRule) bool {
	if !strings.Contains(e.Domain, "*") {
		return false
	}
	return e.AllowWildcard == nil || !*e.AllowWildcard
}

func buildEgressRoutes(p *Policy) []gatewayRoute {
	var routes []gatewayRoute
	seen := make(map[string]bool)

	for _, e := range p.Egress {
		if e.Domain == "" {
			continue
		}
		// Skip wildcard domains without explicit AllowWildcard (defense-in-depth).
		if isWildcardDomainBlocked(e) {
			continue
		}
		key := strings.ToLower(e.Domain)
		if seen[key] {
			continue
		}
		seen[key] = true

		routeName := "egress-" + sanitizeRouteName(key)
		routes = append(routes, gatewayRoute{
			Name:      routeName,
			Hostnames: []string{e.Domain},
			Backends: []gatewayBackend{
				{Dynamic: &struct{}{}},
			},
		})
	}

	// Catch-all denied route (must be last).
	routes = append(routes, gatewayRoute{
		Name: "denied",
		Policies: &gatewayRoutePolicies{
			DirectResponse: &gatewayDirectResponse{
				Status: 403,
				Body:   "egress denied: domain not in allowlist",
			},
		},
	})

	return routes
}

func sanitizeRouteName(domain string) string {
	result := strings.NewReplacer(".", "-", "*", "wildcard", "/", "-", ":", "-").Replace(domain)
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
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

// ptr returns a pointer to the given string value.
func ptr(s string) *string {
	return &s
}
