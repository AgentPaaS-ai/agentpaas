package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Adversary Test Suite for B4-T04 (agentgateway compiler)
//
// These tests attempt to BREAK the security claims:
//   1. Compiled gateway config NEVER contains raw secret values
//   2. Credential injection rules emit IDs only, never secret values
//   3. DNS allow-list contains egress domains only
//   4. Empty policy produces valid deny-all config
//   5. Compiler output is deterministic (golden file tests)
//   6. Vendored agentgateway binary has verified checksum
// ---------------------------------------------------------------------------

// TestAdversary_NoSecretValuesInGatewayConfig_Variants checks that NO raw
// credential secret values leak into the gateway config for any policy variant.
// Tests multiple credential types and special characters.
func TestAdversary_NoSecretValuesInGatewayConfig_Variants(t *testing.T) {
	secrets := []string{
		"sk-prod-123",
		"Bearer sk-prod-123",
		"sk-test-456",
		"OPENAI_API_KEY",
		"STRIPE_RO_KEY",
		"LEGACY_TOOL_TOKEN",
		// Special characters that could break YAML or leak via string interpolation
		"secret-with-dashes",
		"secret_with_underscores",
		"secret.with.dots",
		"secret\nwith\nnewlines",
		`secret"with"quotes`,
		"secret${with}braces",
		"secret:with:colons",
		"secret#with#hashes",
		`secret\with\backslashes`,
	}

	payloads := []*Policy{
		// Variant 1: Standard header credential
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{ID: "my-creds", Type: "header", Header: "X-API-Key", Value: "sk-prod-123"},
			},
		},
		// Variant 2: Brokered credential
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{ID: "my-brokered", Type: "brokered", Service: "vault", Path: "/secret/api"},
			},
		},
		// Variant 3: Direct lease credential
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{ID: "my-lease", Type: "direct_lease", Mode: "file", Reason: "testing"},
			},
		},
		// Variant 4: Mixed credential types
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{ID: "alpha", Type: "header", Header: "Authorization", Value: "Bearer sk-prod-123"},
				{ID: "beta", Type: "brokered", Service: "vault"},
				{ID: "gamma", Type: "direct_lease", Mode: "file", Reason: "legacy"},
			},
		},
		// Variant 5: Credentials with special characters in Value
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{ID: "specials", Type: "header", Header: "X-Key", Value: "secret\nwith\nnewlines"},
			},
		},
		// Variant 6: Combo with ingress/egress/MCP
		{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.example.com", Ports: []int{443}}},
			Credentials: []Credential{
				{ID: "egress-creds", Type: "header", Header: "Authorization", Value: "Bearer sk-prod-123"},
			},
			MCPServers: []MCPServer{
				{Name: "test-mcp", Transport: "http", URL: "https://mcp.example.com"},
			},
			Ingress: []IngressRule{{Path: "/", Port: 7718}},
		},
	}

	for i, p := range payloads {
		got, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("payload %d: CompileGatewayConfig returned error: %v", i, err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(got), secret) {
				t.Errorf("payload %d: secret value %q MUST NOT appear in compiled gateway config (found in output)", i, secret)
				t.Logf("output:\n%s", string(got))
			}
		}
	}
}

// TestAdversary_NoSecretValuesInCredentialRules tests that CompileCredentialRules
// emits only credential IDs (in Value placeholders like ${id}) and never the raw
// secret values. This checks that the Value field in the output contains only the
// ID-based template, not the actual credential value.
func TestAdversary_NoSecretValuesInCredentialRules(t *testing.T) {
	payloads := []struct {
		name     string
		policy   *Policy
		secrets  []string // raw values that MUST NOT appear
		mustHave []string // ID-based patterns that MUST appear
	}{
		{
			name: "header type",
			policy: &Policy{
				Version: "1",
				Agent:   AgentConfig{Name: "test"},
				Credentials: []Credential{
					{ID: "my-key", Type: "header", Header: "X-Key", Value: "super-secret-raw-value"},
				},
			},
			// Only match the exact raw value string, not common YAML keys or ID fragments
			secrets:  []string{"super-secret-raw-value"},
			mustHave: []string{"my-key", "${my-key}"},
		},
		{
			name: "brokered type",
			policy: &Policy{
				Version: "1",
				Agent:   AgentConfig{Name: "test"},
				Credentials: []Credential{
					{ID: "vault-token", Type: "brokered", Service: "hashicorp", Path: "/secrets/db"},
				},
			},
			secrets:  []string{"hashicorp", "super-secret"},
			mustHave: []string{"vault-token", "${secrets:vault-token}"},
		},
		{
			name: "direct_lease type",
			policy: &Policy{
				Version: "1",
				Agent:   AgentConfig{Name: "test"},
				Credentials: []Credential{
					{ID: "file-lease", Type: "direct_lease", Mode: "file", Reason: "legacy SDK", Value: "should-not-appear"},
				},
			},
			// Only match the exact raw value string; "file" and "lease" are ID fragments, not leaks
			secrets:  []string{"should-not-appear"},
			mustHave: []string{"file-lease"},
		},
		{
			name: "mixed types with special chars",
			policy: &Policy{
				Version: "1",
				Agent:   AgentConfig{Name: "test"},
				Credentials: []Credential{
					{ID: "special-header", Type: "header", Header: "X-API-Key", Value: "api_key_12345!@#$%^&*()"},
					{ID: "env-lease", Type: "direct_lease", Mode: "env", Reason: "CI/CD", Value: "shhh-dont-tell"},
				},
			},
			secrets:  []string{"api_key_12345!@#$%^&*()", "shhh-dont-tell"},
			mustHave: []string{"special-header", "env-lease", "${special-header}"},
		},
	}

	for _, tc := range payloads {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CompileCredentialRules(tc.policy)
			if err != nil {
				t.Fatalf("CompileCredentialRules returned error: %v", err)
			}
			for _, secret := range tc.secrets {
				if strings.Contains(string(got), secret) {
					t.Errorf("raw value %q MUST NOT appear in credential rules output", secret)
					t.Logf("output:\n%s", string(got))
				}
			}
			for _, must := range tc.mustHave {
				if !strings.Contains(string(got), must) {
					t.Errorf("expected pattern %q to appear in credential rules, got:\n%s", must, string(got))
				}
			}
		})
	}
}

// TestAdversary_DNSAllowListOnlyEgressDomains verifies the DNS allow-list
// contains ONLY egress domain names and never includes:
//   - MCP server names
//   - Credential IDs
//   - Ingress paths
//   - Hook names/URLs
//   - Agent names
func TestAdversary_DNSAllowListOnlyEgressDomains(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "my-cool-agent"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}},
			{Domain: "data.example.org", Ports: []int{443}},
		},
		Credentials: []Credential{
			{ID: "my-credential-id", Type: "header", Header: "Authorization", Value: "secret-value"},
			{ID: "another-cred", Type: "brokered"},
		},
		MCPServers: []MCPServer{
			{Name: "my-mcp-server", Transport: "stdio", Command: "mcp-tool"},
			{Name: "another-mcp", Transport: "http", URL: "https://mcp.example.com"},
		},
		Hooks: []Hook{
			{Name: "alert-hook", URL: "http://hooks.example.com/alert"},
		},
		Ingress: []IngressRule{
			{Path: "/webhook", Port: 7718},
		},
	}

	got, err := CompileDNSAllowList(p)
	if err != nil {
		t.Fatalf("CompileDNSAllowList returned error: %v", err)
	}

	// Must contain egress domains
	for _, dom := range []string{"api.example.com", "data.example.org"} {
		if !strings.Contains(string(got), dom) {
			t.Errorf("expected egress domain %q in DNS allow-list, got:\n%s", dom, string(got))
		}
	}

	// Must NOT contain non-egress identifiers
	nonDomains := []string{
		"my-cool-agent",      // Agent name
		"my-credential-id",   // Credential ID
		"another-cred",       // Credential ID
		"my-mcp-server",      // MCP server name
		"another-mcp",        // MCP server name
		"alert-hook",         // Hook name
		"hooks.example.com",  // Hook URL domain (not an egress rule domain)
		"/webhook",           // Ingress path
		"secret-value",       // Credential value
	}
	for _, non := range nonDomains {
		if strings.Contains(string(got), non) {
			t.Errorf("DNS allow-list MUST NOT contain %q (not an egress domain), got:\n%s", non, string(got))
		}
	}
}

// TestAdversary_EmptyPolicyDenyAll verifies that an empty policy produces
// a valid deny-all config: no allow rules, no backends, no credentials,
// no MCP servers, no egress, no ingress.
func TestAdversary_EmptyPolicyDenyAll(t *testing.T) {
	t.Run("gateway config", func(t *testing.T) {
		p := &Policy{}
		got, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig(empty) returned error: %v", err)
		}

		// Must be valid YAML
		var decoded any
		if err := yaml.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("empty policy output is not valid YAML: %v\n%s", err, string(got))
		}

		output := string(got)

		// Must NOT contain any backends or routes
		if strings.Contains(output, "backends:") {
			t.Errorf("empty policy gateway config must not contain any backends:\n%s", output)
		}
		if strings.Contains(output, "networkAuthorization") {
			t.Errorf("empty policy gateway config must not contain networkAuthorization:\n%s", output)
		}
		if strings.Contains(output, "mcp:") {
			t.Errorf("empty policy gateway config must not contain MCP backends:\n%s", output)
		}
		if strings.Contains(output, "egress") {
			t.Errorf("empty policy gateway config must not contain egress routes:\n%s", output)
		}
		if strings.Contains(output, "ingress") {
			t.Errorf("empty policy gateway config must not contain ingress routes:\n%s", output)
		}

		// Must contain only the minimal config structure (DNS and no binds)
		t.Logf("Empty policy output:\n%s", output)
	})

	t.Run("dns allow-list", func(t *testing.T) {
		p := &Policy{}
		got, err := CompileDNSAllowList(p)
		if err != nil {
			t.Fatalf("CompileDNSAllowList(empty) returned error: %v", err)
		}
		if len(got) > 0 {
			t.Errorf("empty policy DNS allow-list should be empty, got: %q", string(got))
		}
	})

	t.Run("credential rules", func(t *testing.T) {
		p := &Policy{}
		got, err := CompileCredentialRules(p)
		if err != nil {
			t.Fatalf("CompileCredentialRules(empty) returned error: %v", err)
		}
		if len(got) > 0 {
			t.Errorf("empty policy credential rules should be empty, got: %q", string(got))
		}
	})
}

// TestAdversary_NilPolicyDoesNotPanic verifies that passing nil for the Policy
// argument to all three compile functions returns an error (not a nil pointer panic).
func TestAdversary_NilPolicyDoesNotPanic(t *testing.T) {
	t.Run("CompileGatewayConfig", func(t *testing.T) {
		got, err := CompileGatewayConfig(nil)
		if err == nil {
			t.Error("CompileGatewayConfig(nil) should return error, got nil and output:", string(got))
		}
	})

	t.Run("CompileDNSAllowList", func(t *testing.T) {
		got, err := CompileDNSAllowList(nil)
		if err == nil {
			t.Error("CompileDNSAllowList(nil) should return error, got nil and output:", string(got))
		}
	})

	t.Run("CompileCredentialRules", func(t *testing.T) {
		got, err := CompileCredentialRules(nil)
		if err == nil {
			t.Error("CompileCredentialRules(nil) should return error, got nil and output:", string(got))
		}
	})
}

// TestAdversary_NilFieldsDoNotPanic verifies that nil slices and nil pointer
// fields within the Policy struct don't cause panics when compiling.
func TestAdversary_NilFieldsDoNotPanic(t *testing.T) {
	t.Run("nil Egress slice", func(t *testing.T) {
		p := &Policy{
			Version:     "1",
			Agent:       AgentConfig{Name: "test"},
			Egress:      nil,
			Credentials: nil,
			MCPServers:  nil,
			Hooks:       nil,
			Ingress:     nil,
		}
		// Neither should panic
		_, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig with nil fields: %v", err)
		}
		_, err = CompileDNSAllowList(p)
		if err != nil {
			t.Fatalf("CompileDNSAllowList with nil fields: %v", err)
		}
		_, err = CompileCredentialRules(p)
		if err != nil {
			t.Fatalf("CompileCredentialRules with nil fields: %v", err)
		}
	})

	t.Run("nil AllowWildcard pointer", func(t *testing.T) {
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress: []EgressRule{
				{Domain: "*.example.com", AllowWildcard: nil},
			},
		}
		_, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig with nil AllowWildcard: %v", err)
		}
		_, err = CompileDNSAllowList(p)
		if err != nil {
			t.Fatalf("CompileDNSAllowList with nil AllowWildcard: %v", err)
		}
	})

	t.Run("nil AllowPrivate pointer", func(t *testing.T) {
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress: []EgressRule{
				{Domain: "10.0.0.1", AllowPrivate: nil},
			},
		}
		_, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig with nil AllowPrivate: %v", err)
		}
	})

	t.Run("empty MCPServer with nil args/headers", func(t *testing.T) {
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			MCPServers: []MCPServer{
				{Name: "server1", Transport: "stdio", Command: "cmd", Args: nil, Headers: nil, Env: nil},
				{Name: "", Transport: "http", URL: "https://example.com/mcp"}, // empty name should be skipped gracefully
			},
		}
		_, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig with nil MCP args: %v", err)
		}
	})
}

// TestAdversary_SpecialCharsInCredentialsNotLeaked tests that credential
// values containing special YAML characters (quotes, colons, newlines, braces,
// etc.) do NOT appear in any compiled output.
func TestAdversary_SpecialCharsInCredentialsNotLeaked(t *testing.T) {
	specialValues := []string{
		"value with\nnewline",
		"quoted \"value\" here",
		"${template-like}",
		"colon:separated",
		"#hashcomment",
		`back\slash`,
		"tab\tcharacter",
		"multi\nline\nvalue",
	}

	for _, val := range specialValues {
		t.Run("special_value", func(t *testing.T) {
			p := &Policy{
				Version: "1",
				Agent:   AgentConfig{Name: "test"},
				Credentials: []Credential{
					{ID: "test-cred", Type: "header", Header: "X-Key", Value: val},
				},
				Egress: []EgressRule{
					{Domain: "api.example.com", Ports: []int{443}, Credential: "test-cred"},
				},
			}

			// Check gateway config
			gc, err := CompileGatewayConfig(p)
			if err != nil {
				t.Fatalf("CompileGatewayConfig error: %v", err)
			}
			if strings.Contains(string(gc), val) {
				t.Errorf("CompileGatewayConfig LEAKS special value %q", val)
				t.Logf("output:\n%s", string(gc))
			}

			// Check credential rules
			cr, err := CompileCredentialRules(p)
			if err != nil {
				t.Fatalf("CompileCredentialRules error: %v", err)
			}
			if strings.Contains(string(cr), val) {
				t.Errorf("CompileCredentialRules LEAKS special value %q", val)
				t.Logf("output:\n%s", string(cr))
			}

			// Check DNS allow list
			dns, err := CompileDNSAllowList(p)
			if err != nil {
				t.Fatalf("CompileDNSAllowList error: %v", err)
			}
			if strings.Contains(string(dns), val) {
				t.Errorf("CompileDNSAllowList LEAKS special value %q", val)
				t.Logf("output:\n%s", string(dns))
			}
		})
	}
}

// TestAdversary_WildcardDomainsWithoutAllowWildcard verifies that domains
// containing wildcards (like "*.example.com") are NOT compiled into the
// gateway config or DNS allow-list when AllowWildcard is nil or false.
//
// BREAK: The current compiler does NOT check AllowWildcard at all.
// If this test fails (wildcard domains appear), it means a policy with
// Domain: "*.example.com" and AllowWildcard: nil/false will produce an
// egress backend for the wildcard domain — a security bypass.
func TestAdversary_WildcardDomainsWithoutAllowWildcard(t *testing.T) {
	t.Run("wildcard domain with nil AllowWildcard", func(t *testing.T) {
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress: []EgressRule{
				{Domain: "*.example.com", Ports: []int{443}, AllowWildcard: nil},
				{Domain: "api.example.com", Ports: []int{443}},
			},
		}
		got, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig error: %v", err)
		}

		output := string(got)
		// A wildcard domain like *.example.com should NOT appear without
		// AllowWildcard == true. If it does appear, the compiler is not
		// enforcing allow_wildcard — a security vulnerability.
		if strings.Contains(output, "*.example.com") {
			t.Errorf("BREAK: Wildcard domain '*.example.com' appears in compiled config without AllowWildcard=true.\n"+
				"The compiler does not check AllowWildcard, allowing wildcard domains that should be denied.\noutput:\n%s", output)
		}

		// DNS allow-list should also not contain wildcard domains.
		dns, err := CompileDNSAllowList(p)
		if err != nil {
			t.Fatalf("CompileDNSAllowList error: %v", err)
		}
		if strings.Contains(string(dns), "*.example.com") {
			t.Errorf("BREAK: Wildcard domain '*.example.com' appears in DNS allow-list without AllowWildcard=true.\noutput:\n%s", string(dns))
		}
	})

	t.Run("wildcard domain with false AllowWildcard", func(t *testing.T) {
		f := false
		p := &Policy{
			Version: "1",
			Agent:   AgentConfig{Name: "test"},
			Egress: []EgressRule{
				{Domain: "*.internal.corp.com", Ports: []int{443}, AllowWildcard: &f},
			},
		}
		got, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("CompileGatewayConfig error: %v", err)
		}
		if strings.Contains(string(got), "*.internal.corp.com") {
			t.Errorf("BREAK: Wildcard domain '*.internal.corp.com' appears in compiled config with AllowWildcard=false.\noutput:\n%s", string(got))
		}
	})
}

// TestAdversary_ChecksumFileExistsAndNonEmpty verifies the third_party
// checksum file exists and is non-empty, as claimed by the security spec.
func TestAdversary_ChecksumFileExistsAndNonEmpty(t *testing.T) {
	checksumPath := filepath.Join("..", "..", "third_party", "agentgateway", "CHECKSUM")

	data, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatalf("CHECKSUM file not found at %s: %v", checksumPath, err)
	}

	content := strings.TrimSpace(string(data))
	if len(content) == 0 {
		t.Error("CHECKSUM file is empty")
	}

	// Should contain a SHA-256 hash (64 hex chars)
	parts := strings.Fields(content)
	if len(parts) < 1 {
		t.Error("CHECKSUM file has no content")
	} else if len(parts[0]) != 64 {
		t.Errorf("CHECKSUM first field length is %d, expected 64 (SHA-256 hex): %q", len(parts[0]), parts[0])
	}

	// Verify VERSION file also exists
	versionPath := filepath.Join("..", "..", "third_party", "agentgateway", "VERSION")
	vdata, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatalf("VERSION file not found at %s: %v", versionPath, err)
	}
	if len(strings.TrimSpace(string(vdata))) == 0 {
		t.Error("VERSION file is empty")
	}
}

// TestAdversary_DNSAllowListNoHostPorts verifies that the DNS allow-list
// contains clean domain names only (no port suffixes like ":443").
func TestAdversary_DNSAllowListNoHostPorts(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443, 8443}},
			{Domain: "data.example.org", Ports: []int{443}},
		},
	}
	got, err := CompileDNSAllowList(p)
	if err != nil {
		t.Fatalf("CompileDNSAllowList error: %v", err)
	}

	output := string(got)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.Contains(line, ":") {
			t.Errorf("DNS allow-list line %q contains a port separator ':' — expected domain only", line)
		}
	}
}

// TestAdversary_DeterministicOutput verifies that the same policy produces
// identical output every time (determinism claim).
func TestAdversary_DeterministicOutput(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test-agent"},
		Egress: []EgressRule{
			{Domain: "z.example.com", Ports: []int{443}},
			{Domain: "a.example.com", Ports: []int{443}}, // out of order
		},
		Credentials: []Credential{
			{ID: "b-cred", Type: "header", Header: "Authorization", Value: "secret-b"},
			{ID: "a-cred", Type: "header", Header: "Authorization", Value: "secret-a"},
		},
		MCPServers: []MCPServer{
			{Name: "z-server", Transport: "stdio", Command: "cmd-z"},
			{Name: "a-server", Transport: "stdio", Command: "cmd-a"},
		},
		Ingress: []IngressRule{
			{Path: "/b", Port: 7719},
			{Path: "/a", Port: 7718},
		},
	}

	// Run twice and compare each function
	for i := 0; i < 3; i++ {
		gc1, _ := CompileGatewayConfig(p)
		gc2, _ := CompileGatewayConfig(p)
		if string(gc1) != string(gc2) {
			t.Errorf("CompileGatewayConfig produced different output on run %d", i)
		}

		cr1, _ := CompileCredentialRules(p)
		cr2, _ := CompileCredentialRules(p)
		if string(cr1) != string(cr2) {
			t.Errorf("CompileCredentialRules produced different output on run %d", i)
		}

		dns1, _ := CompileDNSAllowList(p)
		dns2, _ := CompileDNSAllowList(p)
		if string(dns1) != string(dns2) {
			t.Errorf("CompileDNSAllowList produced different output on run %d", i)
		}
	}
}