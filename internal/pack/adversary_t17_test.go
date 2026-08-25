package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// r3 — new vectors only. Do not weaken adversary_t15_test.go or adversary_t16_test.go.

// TestAdversaryT17_IdeographicFullStopTraversalNotRejected
// SC11 leftover: r2 folded U+2024 / U+FF0E / U+FE52. U+3002 IDEOGRAPHIC
// FULL STOP is the CJK period homoglyph and is still treated as a literal.
func TestAdversaryT17_IdeographicFullStopTraversalNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"foo\u3002\u3002bar\"\n    transport: http\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		return
	}
	if ay == nil || len(ay.MCPServers) == 0 {
		return
	}
	if strings.Contains(ay.MCPServers[0].Name, "\u3002") {
		// ADVERSARY BREAK: SC11 unicode-dot class is incomplete; CJK full
		// stop pair is a ".." equivalent and is signed as a hosted name.
		t.Fatalf("ADVERSARY BREAK: ideographic-full-stop mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT17_IdeographicFullStopStampedOntoLock
// Same glyph must not land on the signed canonical map even if Load
// is bypassed with an in-memory AgentYAML.
func TestAdversaryT17_IdeographicFullStopStampedOntoLock(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "foo\u3002\u3002bar", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		return
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		return
	}
	name, _ := servers[0]["name"].(string)
	if strings.Contains(name, "\u3002") {
		// ADVERSARY BREAK: stamp writes CJK-dot traversal token onto lock.
		t.Fatalf("ADVERSARY BREAK: ideographic-full-stop name stamped: %q", name)
	}
}

// TestAdversaryT17_HyphenationPointDotPairNotRejected
// U+2027 HYPHENATION POINT is another "." homoglyph omitted from
// mcpDotEquivalents.
func TestAdversaryT17_HyphenationPointDotPairNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"foo\u2027\u2027bar\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.Contains(ay.MCPServers[0].Name, "\u2027") {
		// ADVERSARY BREAK: hyphenation-point pair accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: hyphenation-point mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT17_ZWSPNameStamped
// U+200B ZWSP is not unicode.IsSpace (Go) and is not in the r2 bind
// hasDisallowedNameSpace table (starts at U+2000..U+200A). Pack stamps
// an invisible-bearing token.
func TestAdversaryT17_ZWSPNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u200b", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for ZWSP name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u200b') {
		// ADVERSARY BREAK: ZWSP survives stamp; signed token is not the
		// visible hosted slug (SC5 exact / SC11 injection class).
		t.Fatalf("ADVERSARY BREAK: ZWSP mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT17_CreateAgentLockDoesNotErrorOnInjectedName
// SC11: injected names must fail the pack. agentYAMLCanonicalMap
// fail-closes by omitting the key; CreateAgentLock still returns a
// signed lock (silent omit, not an error).
func TestAdversaryT17_MixedIllegalLegalOmitsWithoutError(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "../evil", Transport: "http"},
			{Name: "t12a-mcp-m", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["mcp_servers"]; ok {
		t.Fatalf("ADVERSARY BREAK: mixed illegal+legal still stamped: %#v", m["mcp_servers"])
	}
	// SC11 residual: in-memory pack path has no error channel here.
	// LoadAgentYAML is the only fail-closed entry. Documented as MEDIUM
	// if any caller builds AgentYAML without LoadAgentYAML.
}

// TestAdversaryT17_R2UnicodeDotStillRejected
// Re-probe r2 MEDIUM-5 close: U+2024 pair must still fail LoadAgentYAML.
func TestAdversaryT17_R2UnicodeDotStillRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"foo\u2024\u2024bar\"\n    transport: http\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: r2 U+2024 reject regressed (ay=%+v)", ay)
	}
}
