package policy

// Policy represents the canonical agent policy configuration.
// Unknown fields in the YAML are rejected via strict decoding.
type Policy struct {
	Version     string        `yaml:"version"`
	Agent       AgentConfig   `yaml:"agent"`
	Egress      []EgressRule  `yaml:"egress"`
	Credentials []Credential  `yaml:"credentials"`
	MCPServers  []MCPServer   `yaml:"mcp_servers"`
	Hooks       []Hook        `yaml:"hooks"`
	Ingress     []IngressRule `yaml:"ingress"`
}

// AgentConfig describes the agent identity.
type AgentConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// EgressRule defines an outbound network access rule.
type EgressRule struct {
	Domain        string `yaml:"domain"`
	CIDR          string `yaml:"cidr"`
	Ports         []int  `yaml:"ports"`
	AllowWildcard *bool  `yaml:"allow_wildcard"`
	AllowPrivate  *bool  `yaml:"allow_private"`
}

// Credential defines a credential source for the agent.
type Credential struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	Header  string `yaml:"header"`
	Value   string `yaml:"value"`
	Service string `yaml:"service"`
	Path    string `yaml:"path"`
}

// MCPServer defines an MCP (Model Context Protocol) server endpoint.
type MCPServer struct {
	Name    string            `yaml:"name"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
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