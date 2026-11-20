package pack

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMCPFeedbackFixtures_Validate verifies that the S1c MCP feedback
// service and client YAML structures pass pack validation.
func TestMCPFeedbackFixtures_Validate(t *testing.T) {
	t.Run("mcp-feedback-service-yaml", func(t *testing.T) {
		// agent.yaml content from test/e2e/fixtures/mcp-feedback-service/agent.yaml
		dir := t.TempDir()
		writeTestFile(t, dir, "agent.yaml", `name: mcp-feedback-service
version: 0.1.0
runtime: python3.12
entry: main.py
kind: mcp_service
mcp_service:
  transport: streamable_http
  tools:
    - lookup_feedback
  max_concurrency: 4
`)
		agent, err := LoadAgentYAML(dir)
		if err != nil {
			t.Fatalf("LoadAgentYAML: %v", err)
		}
		if agent.Kind != "mcp_service" {
			t.Fatalf("Kind = %q, want mcp_service", agent.Kind)
		}
		if err := ValidateMCPServiceConfig(agent); err != nil {
			t.Fatalf("ValidateMCPServiceConfig: %v", err)
		}

		// Verify the parsed MCPService fields.
		if agent.MCPService.Transport != "streamable_http" {
			t.Fatalf("Transport = %q, want streamable_http", agent.MCPService.Transport)
		}
		if len(agent.MCPService.Tools) != 1 || agent.MCPService.Tools[0] != "lookup_feedback" {
			t.Fatalf("Tools = %v, want [lookup_feedback]", agent.MCPService.Tools)
		}
		if agent.MCPService.MaxConcurrency != 4 {
			t.Fatalf("MaxConcurrency = %d, want 4", agent.MCPService.MaxConcurrency)
		}
	})

	t.Run("mcp-feedback-client-yaml", func(t *testing.T) {
		// agent.yaml content from test/e2e/fixtures/mcp-feedback-client/agent.yaml
		dir := t.TempDir()
		writeTestFile(t, dir, "agent.yaml", `name: mcp-feedback-client
version: 0.1.0
runtime: python3.12
entry: main.py
`)
		agent, err := LoadAgentYAML(dir)
		if err != nil {
			t.Fatalf("LoadAgentYAML: %v", err)
		}
		if agent.Kind != "" {
			t.Fatalf("Kind = %q, want empty (worker)", agent.Kind)
		}

		// workflow.yaml content from test/e2e/fixtures/mcp-feedback-client/workflow.yaml
		wfYAML := `kind: standalone
services:
  - service_id: feedback
    package_name: mcp-feedback-service
    package_version: "0.1.0"
    bundle_digest: sha256:placeholder
    allowed_tools:
      - lookup_feedback
`
		var wf WorkflowYAML
		if err := yaml.Unmarshal([]byte(wfYAML), &wf); err != nil {
			t.Fatalf("Unmarshal workflow: %v", err)
		}
		if errs := ValidateWorkflowYAML(&wf); len(errs) > 0 {
			t.Fatalf("ValidateWorkflowYAML: %v", errs)
		}
		if wf.Kind != "standalone" {
			t.Fatalf("workflow kind = %q, want standalone", wf.Kind)
		}
		if len(wf.Services) != 1 {
			t.Fatalf("services count = %d, want 1", len(wf.Services))
		}
		svc := wf.Services[0]
		if svc.ServiceID != "feedback" {
			t.Fatalf("service_id = %q, want feedback", svc.ServiceID)
		}
		if svc.PackageName != "mcp-feedback-service" {
			t.Fatalf("package_name = %q, want mcp-feedback-service", svc.PackageName)
		}
		if len(svc.AllowedTools) != 1 || svc.AllowedTools[0] != "lookup_feedback" {
			t.Fatalf("allowed_tools = %v, want [lookup_feedback]", svc.AllowedTools)
		}

		// Validate workflow service declaration against agent kind.
		if errs := ValidateWorkflowServiceDeclaration(&wf, agent.Kind); len(errs) > 0 {
			t.Fatalf("ValidateWorkflowServiceDeclaration: %v", errs)
		}
	})
}
