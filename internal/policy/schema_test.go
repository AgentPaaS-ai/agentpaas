package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidPolicies(t *testing.T) {
	validDir := filepath.Join("..", "..", "testdata", "policy", "valid")
	entries, err := os.ReadDir(validDir)
	if err != nil {
		t.Fatalf("reading valid testdata dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(validDir, entry.Name())
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("opening %s: %v", path, err)
			}
			defer func() { _ = f.Close() }()

			p, err := ParsePolicy(f)
			if err != nil {
				t.Fatalf("ParsePolicy(%s) returned error: %v", entry.Name(), err)
			}
			if p.Version != "1.0" {
				t.Errorf("expected version 1.0, got %q", p.Version)
			}
			if p.Agent.Name == "" {
				t.Errorf("expected non-empty agent name")
			}
		})
	}
}

func TestParseInvalidPolicies(t *testing.T) {
	invalidDir := filepath.Join("..", "..", "testdata", "policy", "invalid")
	entries, err := os.ReadDir(invalidDir)
	if err != nil {
		t.Fatalf("reading invalid testdata dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			path := filepath.Join(invalidDir, entry.Name())
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("opening %s: %v", path, err)
			}
			defer func() { _ = f.Close() }()

			_, err = ParsePolicy(f)
			if err == nil {
				t.Fatalf("ParsePolicy(%s) should have returned an error but got nil", entry.Name())
			}
			t.Logf("expected error for %s: %v", entry.Name(), err)
		})
	}
}

func TestParseNilReader(t *testing.T) {
	_, err := ParsePolicy(nil)
	if err == nil {
		t.Fatal("ParsePolicy(nil) should return error")
	}
}

func TestParseEmptyDocument(t *testing.T) {
	_, err := ParsePolicy(strings.NewReader(""))
	if err == nil {
		t.Fatal("ParsePolicy(empty) should return error")
	}
}

func TestMustParsePanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustParse should panic on invalid input")
		}
	}()
	MustParse(strings.NewReader("garbage: [invalid: yaml"))
}

func TestParseRoundTrip(t *testing.T) {
	input := `version: "1.0"
agent:
  name: roundtrip-agent
  description: "Round trip test"
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "test-key"
    type: header
    header: "X-Key"
    value: "${env:TEST_KEY}"
mcp_servers:
  - name: "local"
    url: "http://localhost:8080"
hooks:
  - name: "alert"
    url: "https://hooks.example.com/alert"
ingress:
  - path: "/hook"
    port: 8080
`

	p, err := ParsePolicy(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParsePolicy roundtrip failed: %v", err)
	}

	// Verify all sections are populated.
	if p.Version != "1.0" {
		t.Errorf("version = %q, want 1.0", p.Version)
	}
	if p.Agent.Name != "roundtrip-agent" {
		t.Errorf("agent.name = %q, want roundtrip-agent", p.Agent.Name)
	}
	if len(p.Egress) != 1 {
		t.Errorf("len(egress) = %d, want 1", len(p.Egress))
	}
	if len(p.Credentials) != 1 {
		t.Errorf("len(credentials) = %d, want 1", len(p.Credentials))
	}
	if len(p.MCPServers) != 1 {
		t.Errorf("len(mcp_servers) = %d, want 1", len(p.MCPServers))
	}
	if len(p.Hooks) != 1 {
		t.Errorf("len(hooks) = %d, want 1", len(p.Hooks))
	}
	if len(p.Ingress) != 1 {
		t.Errorf("len(ingress) = %d, want 1", len(p.Ingress))
	}
}

func TestParseRejectsMultipleDocuments(t *testing.T) {
	input := `version: "1.0"
agent:
  name: a
---
version: "2.0"
agent:
  name: b
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParsePolicy should reject multiple documents")
	}
}

func TestParseRejectsUnknownTopLevelField(t *testing.T) {
	input := `version: "1.0"
agent:
  name: test
unknown_extra: "reject me"
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParsePolicy should reject unknown top-level fields")
	}
}

func TestParseRejectsUnknownNestedField(t *testing.T) {
	input := `version: "1.0"
agent:
  name: test
egress:
  - domain: "example.com"
    ports: [443]
    some_unknown_field: true
`
	_, err := ParsePolicy(strings.NewReader(input))
	if err == nil {
		t.Fatal("ParsePolicy should reject unknown nested fields in egress")
	}
}

// TestVendorContract tests that known typos from the spec are rejected:
// brokerd, allow_wildcards, scalar port.
func TestVendorContractKnownTypos(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "typo_brokerd",
			input: `version: "1.0"
agent:
  name: test
egress:
  - domain: "example.com"
    ports: [443]
    brokerd: "vault"
`,
		},
		{
			name: "typo_allow_wildcards",
			input: `version: "1.0"
agent:
  name: test
egress:
  - domain: "*.example.com"
    ports: [443]
    allow_wildcards: true
`,
		},
		{
			name: "scalar_port",
			input: `version: "1.0"
agent:
  name: test
egress:
  - domain: "example.com"
    port: 443
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePolicy(strings.NewReader(tt.input))
			if err == nil {
				t.Errorf("ParsePolicy should reject known typo %q", tt.name)
			}
		})
	}
}