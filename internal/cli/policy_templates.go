package cli

// policyTemplate returns the YAML content for the named policy template.
// Supported templates: deny-all, allow-http, allow-llm, allow-mcp.
func policyTemplate(name string) (string, bool) {
	t, ok := policyTemplates[name]
	return t, ok
}

// policyTemplateNames returns the ordered list of supported template names
// for interactive selection.
func policyTemplateNames() []string {
	return []string{"deny-all", "allow-http", "allow-llm", "allow-mcp"}
}

var policyTemplates = map[string]string{
	"deny-all": `version: "1.0"
agent:
  name: ""
  description: ""
egress: []
credentials: []
mcp_servers: []
hooks: []
ingress: []
`,
	"allow-http": `version: "1.0"
agent:
  name: ""
  description: ""
egress:
  - domain: "*"
    ports:
      - 443
    allow_wildcard: true
credentials: []
mcp_servers: []
hooks: []
ingress: []
`,
	"allow-llm": `version: "1.0"
agent:
  name: ""
  description: ""
egress:
  - domain: api.openai.com
    ports:
      - 443
  - domain: api.anthropic.com
    ports:
      - 443
  - domain: api.x.ai
    ports:
      - 443
credentials:
  - id: openai-api-key
    type: header
    header: Authorization
    value: "${cred:openai-api-key}"
mcp_servers: []
hooks: []
ingress: []
`,
	"allow-mcp": `version: "1.0"
agent:
  name: ""
  description: ""
egress: []
credentials: []
mcp_servers:
  - name: default-mcp
    url: http://localhost:3000
    transport: http
hooks: []
ingress: []
`,
}