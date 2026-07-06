package policy

// Policy represents the canonical agent policy configuration.
// Unknown fields in the YAML are rejected via strict decoding.
type Policy struct {
	Version         string           `yaml:"version"`
	Agent           AgentConfig      `yaml:"agent"`
	Egress          []EgressRule     `yaml:"egress"`
	Credentials     []Credential     `yaml:"credentials"`
	MCPServers      []MCPServer      `yaml:"mcp_servers"`
	Hooks           []Hook           `yaml:"hooks"`
	Ingress         []IngressRule    `yaml:"ingress"`
	LLMBudget       *LLMBudget       `yaml:"llm_budget,omitempty"`
	LLMRateLimit    *LLMRateLimit    `yaml:"llm_rate_limit,omitempty"`
	LLMProviderLock *LLMProviderLock `yaml:"llm_provider_lock,omitempty"`
	IngressAuth     *IngressAuth     `yaml:"ingress_auth,omitempty"`
	Guardrails      []Guardrail      `yaml:"guardrails,omitempty"`
	Transformations *Transformation  `yaml:"transformations,omitempty"`
}

// Transformation defines request/response transformations applied by the gateway.
// Request transforms inject headers or system prompts before the LLM sees the request.
// Response transforms strip headers from LLM responses before they reach the agent.
type Transformation struct {
	Request  *RequestTransform  `yaml:"request,omitempty"`
	Response *ResponseTransform `yaml:"response,omitempty"`
}

// RequestTransform defines request-level transformations.
type RequestTransform struct {
	InjectHeaders      map[string]string `yaml:"inject_headers,omitempty"`
	InjectSystemPrompt string            `yaml:"inject_system_prompt,omitempty"`
}

// ResponseTransform defines response-level transformations.
type ResponseTransform struct {
	RemoveHeaders []string `yaml:"remove_headers,omitempty"`
}

// LLMBudget defines per-invoke and per-request token budget limits.
// The gateway enforces these via budget limit policies on the LLM route.
type LLMBudget struct {
	MaxTokens           int `yaml:"max_tokens"`              // total tokens per invoke
	MaxTokensPerRequest int `yaml:"max_tokens_per_request"` // per-LLM-call limit
}

// LLMRateLimit defines rate limiting for LLM calls.
// The gateway enforces these via localRateLimit policies on the LLM route.
type LLMRateLimit struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	TokensPerMinute   int `yaml:"tokens_per_minute"`
}

// LLMProviderLock restricts LLM egress to specific provider endpoint URLs.
// When set, the compiler adds path-based route matches to LLM provider domain
// routes, ensuring calls are restricted to the exact API endpoints listed.
// This is defense-in-depth beyond hostname-based egress rules.
type LLMProviderLock struct {
	AllowedEndpoints []string `yaml:"allowed_endpoints"`
}

// IngressAuth defines authentication for incoming trigger requests.
type IngressAuth struct {
	Type   string      `yaml:"type"`            // "jwt" or "api_key"
	JWT    *JWTAuth    `yaml:"jwt,omitempty"`    // JWT auth config
	APIKey *APIKeyAuth `yaml:"api_key,omitempty"` // API key auth config
}

// JWTAuth defines JWT validation parameters.
type JWTAuth struct {
	Issuer   string `yaml:"issuer"`
	Audience string `yaml:"audience"`
	JWKSURL  string `yaml:"jwks_url"`
}

// APIKeyAuth defines API key validation parameters.
type APIKeyAuth struct {
	Header     string `yaml:"header"`     // HTTP header name (e.g. X-API-Key)
	Credential string `yaml:"credential"` // Keychain secret name
}

// AgentConfig describes the agent identity.
type AgentConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// EgressRule defines an outbound network access rule.
type EgressRule struct {
	Domain        string      `yaml:"domain"`
	CIDR          string      `yaml:"cidr"`
	Ports         []int       `yaml:"ports"`
	Methods       []string    `yaml:"methods"`
	AllowWildcard *bool       `yaml:"allow_wildcard"`
	AllowPrivate  *bool       `yaml:"allow_private"`
	Credential    string      `yaml:"credential"`
	MCPServerID   string      `yaml:"mcp_server_id"` // if set, this rule applies to MCP server egress
	Timeout       string      `yaml:"timeout,omitempty"`
	Retry         *RetryConfig `yaml:"retry,omitempty"`
}

// RetryConfig defines retry behavior for failed upstream requests.
type RetryConfig struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`     // "exponential", "linear", or "fixed"
	MaxBackoff  string `yaml:"max_backoff"` // max backoff duration
}

// Credential defines a credential source for the agent.
// Type may be "header", "brokered", "oauth", or "direct_lease".
type Credential struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Header  string `yaml:"header"`
	Value   string `yaml:"value"`
	Service string `yaml:"service"`
	Path    string `yaml:"path"`
	// Mode is required for direct-lease credentials: "file" or "env".
	Mode string `yaml:"mode"`
	// Reason is required for direct-lease credentials.
	Reason string `yaml:"reason"`
	// OAuth fields (type: oauth).
	TokenEndpoint          string `yaml:"token_endpoint,omitempty"`
	ClientID               string `yaml:"client_id,omitempty"`
	RefreshTokenCredential string `yaml:"refresh_token_credential,omitempty"`
}

// MCPServer defines an MCP (Model Context Protocol) server endpoint.
type MCPServer struct {
	Name          string            `yaml:"name"`
	URL           string            `yaml:"url"`
	Headers       map[string]string `yaml:"headers"`
	Transport     string            `yaml:"transport"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Endpoint      string            `yaml:"endpoint"`
	AllowedTools  []string          `yaml:"allowed_tools"`
	Env           map[string]string `yaml:"env"`
	AuthMode      string            `yaml:"auth_mode"`
	EgressBinding string            `yaml:"egress_binding"`
}

// Hook defines an outbound webhook destination.
type Hook struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Secret string `yaml:"secret"`
}

// IngressRule defines an inbound webhook listener.
type IngressRule struct {
	Path string `yaml:"path"`
	Port int    `yaml:"port"`
}

// Guardrail defines a content filtering rule for LLM prompts and responses.
type Guardrail struct {
	Type       string `yaml:"type"`
	Pattern    string `yaml:"pattern,omitempty"`
	Action     string `yaml:"action,omitempty"`
	Provider   string `yaml:"provider,omitempty"`
	Credential string `yaml:"credential,omitempty"`
	URL        string `yaml:"url,omitempty"`
}