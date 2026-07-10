package policy

import (
	"fmt"
	"net/url"
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
	DNS     *dnsConfig      `yaml:"dns,omitempty"`
	Tracing *gatewayTracing `yaml:"tracing,omitempty"`
}

type gatewayTracing struct {
	OTLPEndpoint string `yaml:"otlpEndpoint,omitempty"`
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
	Name       string                `yaml:"name,omitempty"`
	Hostnames  []string              `yaml:"hostnames,omitempty"`
	Matches    []gatewayRouteMatch   `yaml:"matches,omitempty"`
	Credential string                `yaml:"credential,omitempty"`
	Policies   *gatewayRoutePolicies `yaml:"policies,omitempty"`
	Backends   []gatewayBackend      `yaml:"backends,omitempty"`
}

type gatewayRouteMatch struct {
	Method string `yaml:"method,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

type gatewayRoutePolicies struct {
	DirectResponse *gatewayDirectResponse   `yaml:"directResponse,omitempty"`
	LocalRateLimit []gatewayLocalRateLimit  `yaml:"localRateLimit,omitempty"`
	JWT            *gatewayJWTAuth          `yaml:"jwt,omitempty"`
	APIKey         *gatewayAPIKeyAuth       `yaml:"apiKey,omitempty"`
	BackendOAuth   *backendOAuthConfig      `yaml:"backendOAuth,omitempty"`
	Guardrails     *gatewayGuardrails       `yaml:"guardrails,omitempty"`
	Timeout        *gatewayTimeout          `yaml:"timeout,omitempty"`
	Retry          *gatewayRetry            `yaml:"retry,omitempty"`
	Transformation *gatewayTransformation   `yaml:"transformation,omitempty"`
}

// gatewayTransformation represents a request/response transformation policy
// applied on a gateway route.
type gatewayTransformation struct {
	Request  *gatewayRequestTransform  `yaml:"request,omitempty"`
	Response *gatewayResponseTransform `yaml:"response,omitempty"`
}

// gatewayRequestTransform defines request-level transformations for the gateway.
type gatewayRequestTransform struct {
	InjectHeaders      map[string]string `yaml:"injectHeaders,omitempty"`
	InjectSystemPrompt string            `yaml:"injectSystemPrompt,omitempty"`
}

// gatewayResponseTransform defines response-level transformations for the gateway.
type gatewayResponseTransform struct {
	RemoveHeaders []string `yaml:"removeHeaders,omitempty"`
}

type gatewayDirectResponse struct {
	Status int    `yaml:"status"`
	Body   string `yaml:"body,omitempty"`
}

// gatewayLocalRateLimit represents an agentgateway localRateLimit policy.
// Used for request-based and token-based rate limiting on routes.
type gatewayLocalRateLimit struct {
	MaxTokens      int    `yaml:"maxTokens"`
	TokensPerFill  int    `yaml:"tokensPerFill"`
	FillInterval   string `yaml:"fillInterval"`
	Type           string `yaml:"type,omitempty"` // "requests" or "tokens"
}

// gatewayJWTAuth represents a JWT validation policy on a gateway route.
type gatewayJWTAuth struct {
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`
	JWKSURL  string `yaml:"jwksUrl"`
}

// gatewayAPIKeyAuth represents an API key validation policy on a gateway route.
type gatewayAPIKeyAuth struct {
	Header     string `yaml:"header"`
	Credential string `yaml:"credential"`
}

// gatewayTimeout represents a timeout policy on a gateway route.
type gatewayTimeout struct {
	RequestTimeout string `yaml:"requestTimeout"`
}

// gatewayRetry represents a retry policy on a gateway route.
type gatewayRetry struct {
	Attempts int    `yaml:"attempts"`
	Backoff  string `yaml:"backoff"`
	Codes    []int  `yaml:"codes,omitempty"`
}

// gatewayGuardrails represents guardrails policy on a gateway route.
type gatewayGuardrails struct {
	Request  []gatewayGuardrail `yaml:"request,omitempty"`
	Response []gatewayGuardrail `yaml:"response,omitempty"`
}

// gatewayGuardrail is a union type for a single guardrail entry.
type gatewayGuardrail struct {
	Regex            *gatewayRegexGuardrail     `yaml:"regex,omitempty"`
	OpenAIModeration *gatewayOpenAIModeration   `yaml:"openAIModeration,omitempty"`
	Webhook          *gatewayWebhookGuardrail   `yaml:"webhook,omitempty"`
}

// gatewayRegexGuardrail represents a regex-based guardrail.
type gatewayRegexGuardrail struct {
	Action string             `yaml:"action"`
	Rules  []gatewayRegexRule `yaml:"rules"`
}

// gatewayRegexRule represents a single regex rule.
type gatewayRegexRule struct {
	Pattern string `yaml:"pattern,omitempty"`
	Builtin string `yaml:"builtin,omitempty"`
}

// gatewayOpenAIModeration represents an OpenAI moderation guardrail.
type gatewayOpenAIModeration struct {
	Model string `yaml:"model"`
}

// gatewayWebhookGuardrail represents a webhook-based guardrail.
type gatewayWebhookGuardrail struct {
	Target gatewayWebhookTarget `yaml:"target"`
}

// gatewayWebhookTarget defines the webhook target host.
type gatewayWebhookTarget struct {
	Host string `yaml:"host"`
}

// backendOAuthConfig carries OAuth token refresh configuration for a route's backend.
type backendOAuthConfig struct {
	TokenEndpoint          string `yaml:"tokenEndpoint"`
	ClientID               string `yaml:"clientId"`
	RefreshTokenCredential string `yaml:"refreshTokenCredential"`
	Header                 string `yaml:"header,omitempty"`
}

type gatewayBackend struct {
	Host     *string                 `yaml:"host,omitempty"`
	Dynamic  *struct{}               `yaml:"dynamic,omitempty"`
	MCP      *gatewayMCPBackend      `yaml:"mcp,omitempty"`
	Policies *gatewayBackendPolicies `yaml:"policies,omitempty"`
}

// gatewayBackendPolicies holds per-backend policies (e.g. backendTLS).
type gatewayBackendPolicies struct {
	BackendTLS *gatewayBackendTLS `yaml:"backendTLS,omitempty"`
}

// gatewayBackendTLS enables HTTPS when connecting to the upstream host backend.
// Empty struct serializes as: backendTLS: {}
type gatewayBackendTLS struct{}

type gatewayMCPBackend struct {
	Targets []gatewayMCPTarget `yaml:"targets,omitempty"`
}

type gatewayMCPTarget struct {
	Name         string             `yaml:"name"`
	Stdio        *gatewayStdio      `yaml:"stdio,omitempty"`
	MCP          *gatewayMCPHost    `yaml:"mcp,omitempty"`
	AllowedTools []string           `yaml:"allowedTools,omitempty"`
	DeniedTools  []string           `yaml:"deniedTools,omitempty"`
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
	OAuth  *OAuthCredentialRule `yaml:"oauth,omitempty"`
}

// OAuthCredentialRule carries the OAuth metadata needed by the gateway
// to obtain and refresh OAuth tokens at runtime.
type OAuthCredentialRule struct {
	TokenEndpoint         string `yaml:"tokenEndpoint"`
	ClientID              string `yaml:"clientId"`
	RefreshTokenCredential string `yaml:"refreshTokenCredential"`
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

	// Add tracing config when observability is configured.
	if p.Observability != nil && p.Observability.OTelEndpoint != "" {
		cfg.Config.Tracing = &gatewayTracing{
			OTLPEndpoint: p.Observability.OTelEndpoint,
		}
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
		case "oauth":
			rule.Header = ""
			rule.Value = ""
			rule.OAuth = &OAuthCredentialRule{
				TokenEndpoint:          c.TokenEndpoint,
				ClientID:               c.ClientID,
				RefreshTokenCredential: c.RefreshTokenCredential,
			}
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
							Policies: buildIngressAuthPolicies(p),
							Backends: backends,
						},
					},
				},
			},
		})
	}

	// Egress bind: hostname-based routing with static host backends + backendTLS.
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

		// Build method matches: one per declared method.
		var matches []gatewayRouteMatch
		for _, method := range e.Methods {
			matches = append(matches, gatewayRouteMatch{Method: method})
		}

		// LLM provider locking: add path restrictions for LLM provider domains.
		matches = applyLLMProviderLock(p, key, matches)

		policies := buildRoutePolicies(p, key, e.Credential)
		// Add per-route timeout and retry policies.
		if e.Timeout != "" {
			if policies == nil {
				policies = &gatewayRoutePolicies{}
			}
			policies.Timeout = &gatewayTimeout{RequestTimeout: e.Timeout}
		}
		if e.Retry != nil {
			if policies == nil {
				policies = &gatewayRoutePolicies{}
			}
			policies.Retry = &gatewayRetry{
				Attempts: e.Retry.MaxAttempts,
				Backoff:  e.Retry.Backoff,
			}
		}

		// Static host backend (gateway-native HTTP routing). Prefer port 443
		// when listed; otherwise use the first declared port or default 443.
		// Port 443 uses backendTLS so the gateway dials HTTPS upstream; other
		// ports (e.g. 80) stay plain HTTP.
		port := 443
		if len(e.Ports) > 0 {
			port = e.Ports[0]
			for _, ep := range e.Ports {
				if ep == 443 {
					port = 443
					break
				}
			}
		}
		host := fmt.Sprintf("%s:%d", e.Domain, port)
		backend := gatewayBackend{Host: ptr(host)}
		if port == 443 {
			backend.Policies = &gatewayBackendPolicies{
				BackendTLS: &gatewayBackendTLS{},
			}
		}

		routes = append(routes, gatewayRoute{
			Name:       routeName,
			Hostnames:  []string{e.Domain},
			Matches:    matches,
			Credential: e.Credential,
			Policies:   policies,
			Backends:   []gatewayBackend{backend},
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

// llmProviderDomains is the set of egress domains that are LLM provider
// endpoints. Rate limit and budget policies are applied to routes matching
// these domains.
var llmProviderDomains = map[string]bool{
	"openrouter.ai":                  true,
	"api.openai.com":                 true,
	"api.anthropic.com":              true,
	"api.x.ai":                       true,
	"inference-api.nousresearch.com": true,
}

// findCredByID returns the Credential with the given ID, or nil if not found.
func findCredByID(p *Policy, id string) *Credential {
	for i := range p.Credentials {
		if p.Credentials[i].ID == id {
			return &p.Credentials[i]
		}
	}
	return nil
}

// buildRoutePolicies returns gateway route policies combining LLM rate limit
// policies and OAuth backend configuration when the credential is type=oauth.
func buildRoutePolicies(p *Policy, domain string, credID string) *gatewayRoutePolicies {
	llm := buildLLMRoutePolicies(p, domain)

	// Check for OAuth credential.
	if credID != "" {
		cred := findCredByID(p, credID)
		if cred != nil && cred.Type == "oauth" {
			header := cred.Header
			if header == "" {
				header = "Authorization"
			}
			oauthCfg := &backendOAuthConfig{
				TokenEndpoint:          cred.TokenEndpoint,
				ClientID:               cred.ClientID,
				RefreshTokenCredential: cred.RefreshTokenCredential,
				Header:                 header,
			}
			if llm == nil {
				return &gatewayRoutePolicies{BackendOAuth: oauthCfg}
			}
			llm.BackendOAuth = oauthCfg
			return llm
		}
	}

	return llm
}

// buildLLMRoutePolicies returns gateway route policies (localRateLimit) for
// an egress route if it matches a known LLM provider domain and the policy
// defines LLM budget or rate limit settings. Returns nil if the route is not
// an LLM route or no LLM governance fields are set.
func buildLLMRoutePolicies(p *Policy, domain string) *gatewayRoutePolicies {
	if p == nil {
		return nil
	}
	if !llmProviderDomains[domain] {
		return nil
	}

	var limits []gatewayLocalRateLimit

	// Token rate limit (tokens per minute).
	if p.LLMRateLimit != nil && p.LLMRateLimit.TokensPerMinute > 0 {
		limits = append(limits, gatewayLocalRateLimit{
			MaxTokens:     p.LLMRateLimit.TokensPerMinute,
			TokensPerFill: p.LLMRateLimit.TokensPerMinute,
			FillInterval:  "1m",
			Type:          "tokens",
		})
	}

	// Request rate limit (requests per minute).
	if p.LLMRateLimit != nil && p.LLMRateLimit.RequestsPerMinute > 0 {
		limits = append(limits, gatewayLocalRateLimit{
			MaxTokens:     p.LLMRateLimit.RequestsPerMinute,
			TokensPerFill: p.LLMRateLimit.RequestsPerMinute,
			FillInterval:  "1m",
			Type:          "requests",
		})
	}

	// Per-request token budget.
	if p.LLMBudget != nil && p.LLMBudget.MaxTokensPerRequest > 0 {
		limits = append(limits, gatewayLocalRateLimit{
			MaxTokens:     p.LLMBudget.MaxTokensPerRequest,
			TokensPerFill: p.LLMBudget.MaxTokensPerRequest,
			FillInterval:  "1m",
			Type:          "tokens",
		})
	}

	if len(limits) == 0 && len(p.Guardrails) == 0 && p.Transformations == nil {
		return nil
	}
	policies := &gatewayRoutePolicies{
		LocalRateLimit: limits,
	}
	if len(p.Guardrails) > 0 {
		policies.Guardrails = buildGatewayGuardrails(p)
	}
	if p.Transformations != nil {
		policies.Transformation = buildGatewayTransformation(p)
	}
	return policies
}

// buildGatewayGuardrails converts policy Guardrails into the gateway guardrails
// policy struct. Guardrails are applied to both request and response directions.
func buildGatewayGuardrails(p *Policy) *gatewayGuardrails {
	var req, resp []gatewayGuardrail
	for _, g := range p.Guardrails {
		gr := gatewayGuardrail{}
		switch g.Type {
		case "regex":
			gr.Regex = &gatewayRegexGuardrail{
				Action: g.Action,
				Rules: []gatewayRegexRule{
					{Pattern: g.Pattern},
				},
			}
		case "moderation":
			gr.OpenAIModeration = &gatewayOpenAIModeration{
				Model: "omni-moderation-latest",
			}
		case "webhook":
			u, err := url.Parse(g.URL)
			host := g.URL
			if err == nil {
				host = u.Host
			}
			gr.Webhook = &gatewayWebhookGuardrail{
				Target: gatewayWebhookTarget{Host: host},
			}
		}
		req = append(req, gr)
		resp = append(resp, gr)
	}
	return &gatewayGuardrails{
		Request:  req,
		Response: resp,
	}
}

// buildGatewayTransformation converts a policy Transformation into the gateway
// transformation policy struct.
func buildGatewayTransformation(p *Policy) *gatewayTransformation {
	if p.Transformations == nil {
		return nil
	}
	gt := &gatewayTransformation{}
	if p.Transformations.Request != nil {
		gt.Request = &gatewayRequestTransform{
			InjectHeaders:      p.Transformations.Request.InjectHeaders,
			InjectSystemPrompt: p.Transformations.Request.InjectSystemPrompt,
		}
	}
	if p.Transformations.Response != nil {
		gt.Response = &gatewayResponseTransform{
			RemoveHeaders: p.Transformations.Response.RemoveHeaders,
		}
	}
	return gt
}

// applyLLMProviderLock adds path-based route matches for LLM provider domains
// when llm_provider_lock is configured. For each allowed endpoint matching the
// given domain, it adds or augments route matches with the endpoint's path.
// This provides defense-in-depth beyond hostname-based egress rules.
func applyLLMProviderLock(p *Policy, domain string, existing []gatewayRouteMatch) []gatewayRouteMatch {
	if p.LLMProviderLock == nil || len(p.LLMProviderLock.AllowedEndpoints) == 0 {
		return existing
	}
	if !llmProviderDomains[domain] {
		return existing
	}

	// Collect unique paths from allowed endpoints that match this domain.
	pathSet := make(map[string]bool)
	for _, endpoint := range p.LLMProviderLock.AllowedEndpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			continue
		}
		host := u.Host
		if host == "" {
			host = u.Hostname()
		}
		if host != domain && u.Hostname() != domain {
			continue
		}
		if u.Path == "" {
			continue // skip endpoints without a path component
		}
		pathSet[u.Path] = true
	}

	if len(pathSet) == 0 {
		return existing
	}

	// Build path-restricted matches. Combine with existing method matches
	// when present, otherwise create standalone path matches.
	var lockedMatches []gatewayRouteMatch
	for path := range pathSet {
		if len(existing) > 0 {
			for _, m := range existing {
				lockedMatches = append(lockedMatches, gatewayRouteMatch{
					Method: m.Method,
					Path:   path,
				})
			}
		} else {
			lockedMatches = append(lockedMatches, gatewayRouteMatch{Path: path})
		}
	}
	return lockedMatches
}

// buildIngressAuthPolicies returns gateway route policies for ingress auth.
// When ingress_auth is configured with type=jwt, adds a JWT validation policy.
// When type=api_key, adds an API key validation policy.
// Returns nil if no ingress_auth is configured.
func buildIngressAuthPolicies(p *Policy) *gatewayRoutePolicies {
	if p.IngressAuth == nil {
		return nil
	}

	switch p.IngressAuth.Type {
	case "jwt":
		if p.IngressAuth.JWT == nil {
			return nil
		}
		return &gatewayRoutePolicies{
			JWT: &gatewayJWTAuth{
				Issuer:   p.IngressAuth.JWT.Issuer,
				Audience: p.IngressAuth.JWT.Audience,
				JWKSURL:  p.IngressAuth.JWT.JWKSURL,
			},
		}
	case "api_key":
		if p.IngressAuth.APIKey == nil {
			return nil
		}
		return &gatewayRoutePolicies{
			APIKey: &gatewayAPIKeyAuth{
				Header:     p.IngressAuth.APIKey.Header,
				Credential: p.IngressAuth.APIKey.Credential,
			},
		}
	}
	return nil
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
		Name:         m.Name,
		AllowedTools: m.AllowedTools,
		DeniedTools:  m.DeniedTools,
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
