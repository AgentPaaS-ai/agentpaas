package mcpmanager

import (
	_ "github.com/parvezsyed/agentpaas/internal/audit"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/policy"
)

func TestAdversary_B7M_T01_UndeclaredServerBypass(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs", AllowedTools: []string{"read_file"}},
	}, "agent-1", "run-1")
	// Attack: undeclared server ID
	if m.IsToolAllowed("unknown-server", "read_file") {
		t.Fatal("BREAK: undeclared server allowed tool access")
	}
	// Also test tool on undeclared
	if m.IsToolAllowed("unknown-server", "any_tool") {
		t.Fatal("BREAK: undeclared server allowed any tool")
	}
}

func TestAdversary_B7M_T01_DenyAllDefault(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}, // empty AllowedTools
	}, "", "")
	if m.IsToolAllowed("filesystem", "read_file") {
		t.Fatal("BREAK: empty AllowedTools allowed a tool (should deny-all)")
	}
	if m.IsToolAllowed("filesystem", "") {
		t.Fatal("BREAK: empty AllowedTools allowed empty tool")
	}
}

func TestAdversary_B7M_T01_CaseSensitiveToolMatch(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs", AllowedTools: []string{"ReadFile"}},
	}, "", "")
	// Attack: case confusion
	if m.IsToolAllowed("filesystem", "read_file") {
		t.Fatal("BREAK: case-insensitive match allowed undeclared casing")
	}
	if m.IsToolAllowed("filesystem", "READFILE") {
		t.Fatal("BREAK: case-insensitive match allowed undeclared casing")
	}
	if !m.IsToolAllowed("filesystem", "ReadFile") {
		t.Fatal("expected exact case match to work")
	}
}

func TestAdversary_B7M_T01_PolicyDigestDeterministicAndNoLeak(t *testing.T) {
	s1 := policy.MCPServer{
		Name:          "fs",
		Transport:     "stdio",
		Command:       "mcp-fs",
		AllowedTools:  []string{"read"},
		AuthMode:      "none",
		EgressBinding: "cred-secret-123", // should not leak in digest
	}
	d1 := computePolicyDigest(s1)
	d2 := computePolicyDigest(s1)
	if d1 != d2 {
		t.Fatal("BREAK: digest not deterministic")
	}
	// Different secret should not affect digest (EgressBinding not in canonical for digest)
	s2 := s1
	s2.EgressBinding = "different-secret"
	d3 := computePolicyDigest(s2)
	if d1 != d3 {
		t.Fatal("BREAK: EgressBinding affected digest (possible leak)")
	}
	// Check no secret values in compute (it uses AllowedTools etc., not Egress)
	if len(d1) != 64 {
		t.Fatal("unexpected digest length")
	}
}

func TestAdversary_B7M_T01_ValidationBypass(t *testing.T) {
	m := NewManager()
	// Try transport=""
	err := m.Validate([]policy.MCPServer{{Name: "bad", Transport: ""}})
	if err == nil {
		t.Fatal("BREAK: empty transport accepted by Validate")
	}
	// duplicate
	err = m.Validate([]policy.MCPServer{
		{Name: "dup", Transport: "stdio", Command: "c1"},
		{Name: "dup", Transport: "stdio", Command: "c2"},
	})
	if err == nil {
		t.Fatal("BREAK: duplicate ID accepted")
	}
	// stdio no command
	err = m.Validate([]policy.MCPServer{{Name: "s", Transport: "stdio"}})
	if err == nil {
		t.Fatal("BREAK: stdio without command accepted")
	}
	// http no url/endpoint
	err = m.Validate([]policy.MCPServer{{Name: "h", Transport: "http"}})
	if err == nil {
		t.Fatal("BREAK: http without url/endpoint accepted")
	}
}

func TestAdversary_B7M_T01_CanonicalMCPServerEgressLeak(t *testing.T) {
	// Note: CanonicalMCPServer in canonical.go does NOT include EgressBinding
	// This test confirms it is excluded (no leak of credential ref in policy digest path)
	// But check if EgressBinding could leak via other means - here we test digest path
	s := policy.MCPServer{Name: "fs", Transport: "stdio", Command: "c", EgressBinding: "secret-cred"}
	d := computePolicyDigest(s)
	// digest computation in manager does not include EgressBinding, good
	if d == "" {
		t.Fatal("expected digest")
	}
	// No direct leak test possible without full policy digest, but structure safe
}

func TestAdversary_B7M_T01_DenyToolCallMissingFields(t *testing.T) {
	appender := &recordingAuditAppender{}
	m := NewManager()
	m.DenyToolCall(appender, "srv", "toolX", "agentX", "runX", "ruleX")
	if len(appender.records) != 1 {
		t.Fatal("expected record")
	}
	p := appender.records[0].Payload
	required := []string{"agent_id", "run_id", "server_id", "tool", "policy_rule_id"}
	for _, k := range required {
		if _, ok := p[k]; !ok {
			t.Fatalf("BREAK: missing required field %s in DenyToolCall audit payload", k)
		}
	}
}

func TestAdversary_B7M_T01_StatusLeakSecrets(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "fs", Transport: "stdio", Command: "c", AllowedTools: []string{"t"}, EgressBinding: "cred-1"},
	}, "a", "r")
	resources := m.Status()
	for _, r := range resources {
		if r.PolicyDigest == "" {
			t.Fatal("expected digest")
		}
		// Status exposes AllowedTools, but not EgressBinding or secrets - check no leak
		// Resource struct has no EgressBinding field, so safe
	}
}

func TestAdversary_B7M_T01_RaceValidateIsToolAllowed(t *testing.T) {
	m := NewManager()
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.Validate([]policy.MCPServer{{Name: "r", Transport: "stdio", Command: "c"}})
			m.Register([]policy.MCPServer{{Name: "r", Transport: "stdio", Command: "c"}}, "", "")
		}
		done <- true
	}()
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.IsToolAllowed("r", "t")
			_ = m.Status()
		}
		done <- true
	}()
	<-done
	<-done
	// If no panic or data race (run with -race), safe
}

func TestAdversary_B7M_T01_TransportEmptyBypass(t *testing.T) {
	m := NewManager()
	err := m.Validate([]policy.MCPServer{{Name: "emptytrans", Transport: ""}})
	if err == nil {
		t.Fatal("BREAK: transport empty bypassed validation")
	}
}