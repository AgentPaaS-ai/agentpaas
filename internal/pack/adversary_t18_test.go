package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// r4 — new vectors only. Do not weaken adversary_t15/t16/t17.

// TestAdversaryT18_WordJoinerNameStamped
// r3 stripped ZWSP/ZWNJ/ZWJ/BOM. U+2060 WORD JOINER is the same
// invisible-format class and is still treated as a literal token.
func TestAdversaryT18_WordJoinerNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2060", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for WORD JOINER name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u2060') {
		// ADVERSARY BREAK: U+2060 survives stamp; signed token is not the
		// visible hosted slug (SC5 exact / SC11 injection class leftover).
		t.Fatalf("ADVERSARY BREAK: WORD JOINER mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT18_SoftHyphenNameStamped
// U+00AD SOFT HYPHEN is an invisible format rune omitted from
// mcpInvisibleRune / stripMCPInvisibleName.
func TestAdversaryT18_SoftHyphenNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u00ad", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for soft-hyphen name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u00ad') {
		// ADVERSARY BREAK: soft hyphen survives stamp.
		t.Fatalf("ADVERSARY BREAK: soft-hyphen mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT18_SinologicalDotTraversalNotRejected
// SC11 leftover: r3 folded U+3002 / U+2027. U+A78F LATIN LETTER
// SINOLOGICAL DOT is another "." confusable still treated as a literal.
func TestAdversaryT18_SinologicalDotTraversalNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"foo\ua78f\ua78fbar\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.Contains(ay.MCPServers[0].Name, "\ua78f") {
		// ADVERSARY BREAK: sinological-dot pair accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: sinological-dot mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT18_RaisedDotTraversalNotRejected
// U+2E33 RAISED DOT is another "." homoglyph omitted from mcpDotEquivalents.
func TestAdversaryT18_RaisedDotTraversalNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"foo\u2e33\u2e33bar\"\n    transport: http\n")
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
	if strings.Contains(ay.MCPServers[0].Name, "\u2e33") {
		// ADVERSARY BREAK: SC11 unicode-dot class is still instance-patched.
		t.Fatalf("ADVERSARY BREAK: raised-dot mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT18_CombiningGraphemeJoinerStamped
// U+034F COMBINING GRAPHEME JOINER is invisible and not in the r3 strip table.
func TestAdversaryT18_CombiningGraphemeJoinerStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u034f", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for CGJ name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u034f') {
		// ADVERSARY BREAK: CGJ survives stamp.
		t.Fatalf("ADVERSARY BREAK: combining-grapheme-joiner mcp name stamped: %q", name)
	}
}

// TestAdversaryT18_R3IdeographicDotStillRejected
// Re-probe r3 MEDIUM-1 close: U+3002 pair must still fail LoadAgentYAML.
func TestAdversaryT18_R3IdeographicDotStillRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"foo\u3002\u3002bar\"\n    transport: http\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: r3 U+3002 reject regressed (ay=%+v)", ay)
	}
}
