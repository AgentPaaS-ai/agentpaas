package daemon

import (
	"context"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// TestInitValidateRoundTrip verifies that a project scaffolded by
// init_project + InitPolicy passes validation without any manual fixes.
// Regression test for B16-LC02/UC01: scaffolded policy was invalid.
func TestInitValidateRoundTrip(t *testing.T) {
	projectDir := t.TempDir()

	// Step 1: scaffold the agent project (equivalent to `agent init
	// --noninteractive --runtime python`).
	if err := pack.InitScaffold(projectDir, pack.RuntimePython); err != nil {
		t.Fatalf("InitScaffold: %v", err)
	}
	if err := pack.InitPolicy(projectDir); err != nil {
		t.Fatalf("InitPolicy: %v", err)
	}

	// Step 2: validate the project — must be ready with zero issues.
	resp, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject() error = %v", err)
	}
	if !resp.GetReady() {
		t.Fatalf("scaffolded project failed validation; issues = %v", resp.GetIssues())
	}
	if len(resp.GetIssues()) != 0 {
		t.Fatalf("expected zero issues, got %d: %v", len(resp.GetIssues()), resp.GetIssues())
	}
}

// TestPolicyInitThenValidate verifies that each policy_init template
// produces a policy that passes validation when combined with a valid
// agent.yaml.
func TestPolicyInitThenValidate(t *testing.T) {
	templates := []string{"deny-all", "allow-http", "allow-llm", "allow-mcp"}
	for _, tmpl := range templates {
		t.Run(tmpl, func(t *testing.T) {
			projectDir := t.TempDir()

			// Write agent.yaml directly for the test (minimal valid).
			writeOperatorTestFile(t, projectDir, "agent.yaml",
				`version: "1.0"
runtime: python
name: test-agent
`)

			// Write policy.yaml from the template content.
			content := policyTemplateContent(t, tmpl)
			writeOperatorTestFile(t, projectDir, "policy.yaml", content)

			resp, err := (&controlServer{}).ValidateAgentProject(
				context.Background(),
				&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
			)
			if err != nil {
				t.Fatalf("ValidateAgentProject() error = %v", err)
			}
			if !resp.GetReady() {
				t.Fatalf("template %s failed validation; issues = %v",
					tmpl, resp.GetIssues())
			}
		})
	}
}

// policyTemplateContent returns the YAML content for a named template.
// Mirrors the templates in internal/cli/policy_templates.go.
func policyTemplateContent(t *testing.T, name string) string {
	t.Helper()
	templates := map[string]string{
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
	content, ok := templates[name]
	if !ok {
		t.Fatalf("unknown template: %s", name)
	}
	return content
}
