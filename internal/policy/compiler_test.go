package policy

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCompileGatewayConfig_EmptyPolicy(t *testing.T) {
	p := &Policy{}
	got, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig(empty policy) returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("CompileGatewayConfig(empty policy) returned empty output")
	}
	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, string(got))
	}
	t.Logf("Empty policy output:\n%s", string(got))
}

func TestCompileGatewayConfig_SamplePolicy(t *testing.T) {
	p := samplePolicy()
	got, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig(sample) returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("CompileGatewayConfig(sample) returned empty output")
	}
	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, string(got))
	}
	t.Logf("Sample policy output:\n%s", string(got))
}

func TestCompileGatewayConfig_ContainsMCPBackends(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Should contain the MCP server name.
	if !containsStr(string(got), "filesystem-readonly") {
		t.Errorf("expected MCP server 'filesystem-readonly' in output, got:\n%s", string(got))
	}
	// Should contain stdio config (the command).
	if !containsStr(string(got), "agentpaas-mcp-filesystem") {
		t.Errorf("expected MCP command 'agentpaas-mcp-filesystem' in output, got:\n%s", string(got))
	}
}

func TestCompileGatewayConfig_ContainsEgressDomains(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Should contain allowed domains.
	for _, domain := range []string{"api.openai.com", "api.stripe.com", "hooks.slack.com"} {
		if !containsStr(string(got), domain) {
			t.Errorf("expected domain %q in compiled output, got:\n%s", domain, string(got))
		}
	}
}

func TestCompileGatewayConfig_NoSecretValues(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Must NOT contain any credential secret values (by-id only).
	// Credential IDs and values are emitted via CompileCredentialRules, not in the gateway config.
	for _, secret := range []string{"OPENAI_API_KEY", "STRIPE_RO_KEY", "LEGACY_TOOL_TOKEN",
		"Bearer sk-prod-123", "Bearer sk-test-456"} {
		if containsStr(string(got), secret) {
			t.Errorf("secret value %q MUST NOT appear in compiled gateway config", secret)
		}
	}
}

func TestCompileDNSAllowList_Sample(t *testing.T) {
	p := samplePolicy()
	got, err := CompileDNSAllowList(p)
	if err != nil {
		t.Fatalf("CompileDNSAllowList returned error: %v", err)
	}
	// Should include all egress domains.
	for _, dom := range []string{"api.openai.com", "api.stripe.com", "hooks.slack.com"} {
		if !containsStr(string(got), dom) {
			t.Errorf("expected domain %q in allow-list, got:\n%s", dom, string(got))
		}
	}
	// Should not include MCP server names or credential IDs.
	if containsStr(string(got), "filesystem-readonly") {
		t.Error("DNS allow-list should not contain MCP server names")
	}
	if containsStr(string(got), "openai-prod") {
		t.Error("DNS allow-list should not contain credential IDs")
	}
}

func TestCompileDNSAllowList_Empty(t *testing.T) {
	p := &Policy{}
	got, _ := CompileDNSAllowList(p)
	if len(got) > 0 {
		t.Errorf("empty policy should produce empty allow-list, got:\n%s", string(got))
	}
}

func TestCompileCredentialRules_Sample(t *testing.T) {
	p := samplePolicy()
	got, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("CompileCredentialRules(sample) returned empty output")
	}
	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, string(got))
	}
	// Must contain credential IDs but NOT secret values.
	for _, id := range []string{"openai-prod", "stripe-readonly", "legacy-tool-token"} {
		if !containsStr(string(got), id) {
			t.Errorf("expected credential id %q in credential rules", id)
		}
	}
	for _, secret := range []string{"OPENAI_API_KEY", "STRIPE_RO_KEY"} {
		if containsStr(string(got), secret) {
			t.Errorf("secret value %q MUST NOT appear in credential rules (by-id only)", secret)
		}
	}
}

// containsStr is a simple substring check.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) &&
			(s[:len(substr)] == substr ||
				len(s) > len(substr) &&
					(searchInString(s, substr) >= 0)))
}

// searchInString returns the index of substr in s, or -1 if not found.
func searchInString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// samplePolicy returns a policy matching the PRD §2.9 example.
func samplePolicy() *Policy {
	return &Policy{
		Version: "1",
		Agent: AgentConfig{
			Name: "test-agent",
		},
		Egress: []EgressRule{
			{Domain: "api.openai.com", Ports: []int{443}, Credential: "openai-prod"},
			{Domain: "api.stripe.com", Ports: []int{443}, Credential: "stripe-readonly"},
			{Domain: "hooks.slack.com", Ports: []int{443}},
		},
		Credentials: []Credential{
			{ID: "openai-prod", Type: "header", Header: "Authorization", Value: "Bearer sk-prod-123"},
			{ID: "stripe-readonly", Type: "header", Header: "Authorization", Value: "Bearer sk-test-456"},
			{ID: "legacy-tool-token", Type: "direct_lease", Mode: "file", Reason: "legacy SDK"},
		},
		MCPServers: []MCPServer{
			{
				Name:      "filesystem-readonly",
				Transport: "stdio",
				Command:   "agentpaas-mcp-filesystem",
				Args:      []string{"--root", "./data", "--readonly"},
			},
		},
		Hooks: []Hook{
			{Name: "egress_denied", URL: "http://127.0.0.1:9999/security-alert"},
		},
		Ingress: []IngressRule{
			{Path: "/", Port: 7718},
		},
	}
}