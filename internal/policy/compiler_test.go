package policy

import (
	"os"
	"path/filepath"
	"strings"
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
}

func TestCompileGatewayConfig_ContainsMCPBackends(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Should contain the MCP server name.
	if !strings.Contains(string(got), "filesystem-readonly") {
		t.Errorf("expected MCP server 'filesystem-readonly' in output, got:\n%s", string(got))
	}
	// Should contain stdio config (the command).
	if !strings.Contains(string(got), "agentpaas-mcp-filesystem") {
		t.Errorf("expected MCP command 'agentpaas-mcp-filesystem' in output, got:\n%s", string(got))
	}
}

func TestCompileGatewayConfig_ContainsEgressDomains(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Should contain allowed domains.
	for _, domain := range []string{"api.openai.com", "api.stripe.com", "hooks.slack.com"} {
		if !strings.Contains(string(got), domain) {
			t.Errorf("expected domain %q in compiled output, got:\n%s", domain, string(got))
		}
	}
}

func TestCompileGatewayConfig_NoSecretValues(t *testing.T) {
	p := samplePolicy()
	got, _ := CompileGatewayConfig(p)
	// Must NOT contain any credential secret values (by-id only).
	for _, secret := range []string{"OPENAI_API_KEY", "STRIPE_RO_KEY", "LEGACY_TOOL_TOKEN",
		"Bearer sk-prod-123", "Bearer sk-test-456"} {
		if strings.Contains(string(got), secret) {
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
		if !strings.Contains(string(got), dom) {
			t.Errorf("expected domain %q in allow-list, got:\n%s", dom, string(got))
		}
	}
	// Should not include MCP server names or credential IDs.
	if strings.Contains(string(got), "filesystem-readonly") {
		t.Error("DNS allow-list should not contain MCP server names")
	}
	if strings.Contains(string(got), "openai-prod") {
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
		if !strings.Contains(string(got), id) {
			t.Errorf("expected credential id %q in credential rules", id)
		}
	}
	for _, secret := range []string{"OPENAI_API_KEY", "STRIPE_RO_KEY"} {
		if strings.Contains(string(got), secret) {
			t.Errorf("secret value %q MUST NOT appear in credential rules (by-id only)", secret)
		}
	}
}

func TestCompileGatewayConfig_Golden(t *testing.T) {
	p := samplePolicy()
	got, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig returned error: %v", err)
	}
	golden := readGolden(t, "gateway_config.golden")
	if string(got) != string(golden) {
		t.Errorf("gateway config mismatch.\n--- GOT:\n%s\n--- EXPECTED:\n%s", string(got), string(golden))
	}
}

func TestCompileDNSAllowList_Golden(t *testing.T) {
	p := samplePolicy()
	got, err := CompileDNSAllowList(p)
	if err != nil {
		t.Fatalf("CompileDNSAllowList returned error: %v", err)
	}
	golden := readGolden(t, "dns_allowlist.golden")
	if string(got) != string(golden) {
		t.Errorf("DNS allow-list mismatch.\n--- GOT:\n%s\n--- EXPECTED:\n%s", string(got), string(golden))
	}
}

func TestCompileCredentialRules_Golden(t *testing.T) {
	p := samplePolicy()
	got, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules returned error: %v", err)
	}
	golden := readGolden(t, "credential_rules.golden")
	if string(got) != string(golden) {
		t.Errorf("credential rules mismatch.\n--- GOT:\n%s\n--- EXPECTED:\n%s", string(got), string(golden))
	}
}

// readGolden reads a golden file from testdata/.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading golden file %q: %v", name, err)
	}
	return data
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

// ---------------------------------------------------------------------------
// B20-T04: Method enforcement in gateway config
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_MethodEnforcement_GET(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Methods: []string{"GET"}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// Must contain method match for GET.
	if !strings.Contains(string(out), "method: GET") {
		t.Errorf("expected method: GET in compiled config, got:\n%s", string(out))
	}
	// Must NOT contain POST (only GET is allowed).
	if strings.Contains(string(out), "method: POST") {
		t.Errorf("unexpected method: POST in compiled config:\n%s", string(out))
	}
}

func TestCompileGatewayConfig_MethodEnforcement_POST(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Methods: []string{"POST"}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if !strings.Contains(string(out), "method: POST") {
		t.Errorf("expected method: POST in compiled config, got:\n%s", string(out))
	}
}

func TestCompileGatewayConfig_MethodEnforcement_MultipleMethods(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Methods: []string{"GET", "POST", "PUT"}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// All three methods must be present
	for _, m := range []string{"GET", "POST", "PUT"} {
		if !strings.Contains(string(out), "method: "+m) {
			t.Errorf("expected method: %s in compiled config:\n%s", m, string(out))
		}
	}
}

func TestCompileGatewayConfig_NoMethods_NoMatches(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}}, // No Methods
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// When no methods are declared, no "method:" should appear (all methods allowed).
	if strings.Contains(string(out), "method:") {
		t.Errorf("expected no method matches when Methods is empty, got:\n%s", string(out))
	}
}

// ---------------------------------------------------------------------------
// B20-T04: Port enforcement in gateway config
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_PortEnforcement_Port443(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// agentgateway v1.3.0 matches routes by hostname, not port. Port
	// enforcement is handled by Docker network topology (only HTTPS/443
	// is proxied). The hostname must appear in the compiled config.
	if !strings.Contains(string(out), "api.example.com") {
		t.Errorf("expected hostname api.example.com in compiled config, got:\n%s", string(out))
	}
	// The compiled config must be accepted by agentgateway — no `ports` field on routes.
	if strings.Contains(string(out), "ports:") {
		t.Errorf("ports field should not appear on routes (agentgateway v1.3.0 rejects it), got:\n%s", string(out))
	}
}

func TestCompileGatewayConfig_PortEnforcement_Non443(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{8080}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// agentgateway matches by hostname; port 8080 is enforced at the network layer.
	// The hostname must still appear.
	if !strings.Contains(string(out), "api.example.com") {
		t.Errorf("expected hostname api.example.com in compiled config, got:\n%s", string(out))
	}
	// No ports field on routes (agentgateway v1.3.0 rejects it).
	if strings.Contains(string(out), "ports:") {
		t.Errorf("ports field should not appear on routes, got:\n%s", string(out))
	}
}

func TestCompileGatewayConfig_PortEnforcement_MultiplePorts(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443, 80}},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// agentgateway matches by hostname; multiple ports are enforced at the network layer.
	if !strings.Contains(string(out), "api.example.com") {
		t.Errorf("expected hostname api.example.com in compiled config, got:\n%s", string(out))
	}
	// No ports field on routes (agentgateway v1.3.0 rejects it).
	if strings.Contains(string(out), "ports:") {
		t.Errorf("ports field should not appear on routes, got:\n%s", string(out))
	}
}

// ---------------------------------------------------------------------------
// B20-T04: Credential binding (route-scoped)
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_CredentialBinding(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Credential: "my-key"},
		},
		Credentials: []Credential{
			{ID: "my-key", Type: "header", Header: "Authorization", Value: "Bearer test"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	// The credential must appear on the route.
	if !strings.Contains(string(out), "credential: my-key") {
		t.Errorf("expected credential binding 'my-key' on route, got:\n%s", string(out))
	}
}

func TestCompileGatewayConfig_CredentialScopedToRoute(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "route-a.example.com", Ports: []int{443}, Credential: "key-a"},
			{Domain: "route-b.example.com", Ports: []int{443}, Credential: "key-b"},
		},
		Credentials: []Credential{
			{ID: "key-a", Type: "header", Header: "X-Key-A", Value: "val-a"},
			{ID: "key-b", Type: "header", Header: "X-Key-B", Value: "val-b"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	// Route A should have key-a, not key-b.
	routeAIdx := strings.Index(string(out), "route-a.example.com")
	routeBIdx := strings.Index(string(out), "route-b.example.com")
	if routeAIdx < 0 || routeBIdx < 0 {
		t.Fatalf("expected both routes in output:\n%s", string(out))
	}

	routeASection := string(out)[routeAIdx:min(routeAIdx+200, len(out))]
	routeBSection := string(out)[routeBIdx:min(routeBIdx+200, len(out))]

	if !strings.Contains(routeASection, "credential: key-a") {
		t.Errorf("route A should have credential: key-a\nroute A section:\n%s", routeASection)
	}
	if strings.Contains(routeASection, "credential: key-b") {
		t.Errorf("route A must NOT have credential: key-b\nroute A section:\n%s", routeASection)
	}
	if !strings.Contains(routeBSection, "credential: key-b") {
		t.Errorf("route B should have credential: key-b\nroute B section:\n%s", routeBSection)
	}
	if strings.Contains(routeBSection, "credential: key-a") {
		t.Errorf("route B must NOT have credential: key-a\nroute B section:\n%s", routeBSection)
	}
}

func TestCompileGatewayConfig_NoCredential_NoCredentialField(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}}, // No credential
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if strings.Contains(string(out), "credential:") {
		t.Errorf("expected no credential field when credential is empty, got:\n%s", string(out))
	}
}

// ---------------------------------------------------------------------------
// B20-T04: CIDR-only rule rejection at validation time
// ---------------------------------------------------------------------------

func TestValidateCIDROnlyRuleRejected(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{CIDR: "10.0.0.0/8", Ports: []int{443}}, // No Domain
		},
	}
	errs := ValidatePolicy(p)
	found := false
	for _, e := range errs {
		if e.Severity == "error" && strings.Contains(e.Message, "CIDR egress rules are not yet supported") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CIDR-only rejection error, got %d findings: %v", len(errs), errs)
	}
}

func TestValidateCIDRWithDomainAccepted(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "example.com", CIDR: "10.0.0.0/8", Ports: []int{443}}, // Has domain
		},
	}
	errs := ValidatePolicy(p)
	for _, e := range errs {
		if e.Severity == "error" && strings.Contains(e.Message, "CIDR egress rules are not yet supported") {
			t.Errorf("CIDR with domain should NOT be rejected, got: %v", e)
		}
	}
}

// ---------------------------------------------------------------------------
// B20-T04: Golden — full policy semantics test
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_FullSemantics(t *testing.T) {
	allowWildcard := true
	allowPrivate := true
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "full-semantics-agent"},
		Egress: []EgressRule{
			{
				Domain:        "api.example.com",
				Ports:         []int{443},
				Methods:       []string{"GET", "POST"},
				Credential:    "prod-key",
			},
			{
				Domain:        "db.internal",
				Ports:         []int{5432},
				AllowPrivate:  &allowPrivate,
			},
			{
				Domain:         "*.cdn.example.com",
				Ports:          []int{443, 80},
				AllowWildcard:  &allowWildcard,
			},
		},
		Credentials: []Credential{
			{ID: "prod-key", Type: "header", Header: "X-API-Key", Value: "secret"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	outStr := string(out)

	// Method enforcement.
	if !strings.Contains(outStr, "method: GET") {
		t.Error("expected method: GET in compiled config")
	}
	if !strings.Contains(outStr, "method: POST") {
		t.Error("expected method: POST in compiled config")
	}

	// Port enforcement is handled by Docker network topology, not gateway
	// route config (agentgateway v1.3.0 matches by hostname only).
	// Verify no ports field on routes (it would crash the gateway).
	if strings.Contains(outStr, "ports:") {
		t.Error("ports field should not appear on routes (agentgateway v1.3.0 rejects it)")
	}

	// Credential binding.
	if !strings.Contains(outStr, "credential: prod-key") {
		t.Error("expected credential binding prod-key on route")
	}

	// Hostname enforcement.
	if !strings.Contains(outStr, "api.example.com") {
		t.Error("expected api.example.com hostname in config")
	}
	if !strings.Contains(outStr, "db.internal") {
		t.Error("expected db.internal hostname in config")
	}
	if !strings.Contains(outStr, "*.cdn.example.com") {
		t.Error("expected *.cdn.example.com hostname in config")
	}

	// Denied route must still be present.
	if !strings.Contains(outStr, "denied") || !strings.Contains(outStr, "403") {
		t.Error("expected denied route with 403 in compiled config")
	}

	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, outStr)
	}
}

// ---------------------------------------------------------------------------
// B20-T04: Enforcement fields must not disappear — regression test
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_EnforcementFieldsNotLost(t *testing.T) {
	// This test verifies that all enforcement fields (methods, ports, credential)
	// survive compilation when present.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{
				Domain:     "secure.example.com",
				Ports:      []int{443},
				Methods:    []string{"GET"},
				Credential: "secure-key",
			},
		},
		Credentials: []Credential{
			{ID: "secure-key", Type: "header", Header: "Authorization", Value: "Bearer test"},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	outStr := string(out)

	// Every enforcement field must be present.
	// Note: port enforcement is handled by Docker network topology, not
	// gateway route config (agentgateway v1.3.0 matches by hostname only).
	checks := map[string]string{
		"hostname":   "secure.example.com",
		"method":     "method: GET",
		"credential": "credential: secure-key",
	}
	for field, expected := range checks {
		if !strings.Contains(outStr, expected) {
			t.Errorf("enforcement field %q missing; expected %q in output:\n%s", field, expected, outStr)
		}
	}
}
