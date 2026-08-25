package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// r2 — new vectors only. Do not weaken adversary_t15_test.go.

// TestAdversaryT16_InjectedNameDoesNotFailClosed
// SC11: newline/NUL/control/.. must be rejected before stamp. The r1 fix
// blanks the name and still returns a parsed AgentYAML, so pack signs the
// rest of the document. Fail-closed is a pack error, not a silent strip.
func TestAdversaryT16_InjectedNameDoesNotFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\\ninjected\"\n    transport: http\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		return
	}
	// ADVERSARY BREAK: SC11 requires reject-before-stamp. Silent name wipe
	// lets pack succeed and can hide a mixed good+injected list.
	t.Fatalf("ADVERSARY BREAK: LoadAgentYAML accepted injected mcp name (err=nil, servers=%+v)", ay.MCPServers)
}

// TestAdversaryT16_MixedGoodAndInjectedStillStampsGood
// Partial-invalid list: illegal name is wiped, legal sibling is still
// stamped. Spec fail-closed is pack error for the whole list.
func TestAdversaryT16_MixedGoodAndInjectedStillStampsGood(t *testing.T) {
	var ay AgentYAML
	err := yaml.Unmarshal([]byte("name: t12a-agent-mcp\nmcp_servers:\n  - name: \"../evil\"\n    transport: http\n  - name: t12a-mcp-m\n    transport: http\n"), &ay)
	if err != nil {
		return
	}
	m := agentYAMLCanonicalMap(&ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		return
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		return
	}
	// ADVERSARY BREAK: mixed injected+legal list still stamps the legal name
	// instead of failing the pack.
	t.Fatalf("ADVERSARY BREAK: mixed injected mcp_servers still stamped: %#v", servers)
}

// TestAdversaryT16_WhitespacePaddedNameStampedUntrimmed
// Go stamps the raw name; cloud bind trims. A padded slug can match a
// hosted MCP the signed bytes do not exactly name (SC5 exact match).
func TestAdversaryT16_WhitespacePaddedNameStampedUntrimmed(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: " t12a-mcp-m ", Transport: "http"},
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
	if name == strings.TrimSpace(name) {
		return
	}
	// ADVERSARY BREAK: padded name is signed as-is; bind trims and may
	// match a different hosted slug (SC5 exact).
	t.Fatalf("ADVERSARY BREAK: untrimmed mcp name stamped onto lock: %q", name)
}

// TestAdversaryT16_NBSPNameStamped
// NBSP is Unicode space: Go TrimSpace treats the entry as non-empty-like
// but stamps the NBSP-bearing token. Bind JS trim() then matches the
// hosted ASCII name.
func TestAdversaryT16_NBSPNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u00a0", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for NBSP name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u00a0') {
		// ADVERSARY BREAK: NBSP survives stamp; cloud bind trims it to the
		// hosted ASCII name and grants.
		t.Fatalf("ADVERSARY BREAK: NBSP mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT16_UnicodeDotTraversalNotRejected
// SC11 / soul INJECTION: ASCII ".." is rejected; U+2024 ONE DOT LEADER
// pairs and U+FF0E FULLWIDTH FULL STOP are not.
func TestAdversaryT16_UnicodeDotTraversalNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"foo\u2024\u2024bar\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.Contains(ay.MCPServers[0].Name, "\u2024") {
		// ADVERSARY BREAK: unicode-dot traversal token accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: unicode-dot mcp server name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT16_PolicyYAMLNotCopiedOntoStamp
// SC9 / D166: pack must stamp only agent.yaml mcp_servers, never policy.
func TestAdversaryT16_PolicyYAMLNotCopiedOntoStamp(t *testing.T) {
	ay := &AgentYAML{Name: "t12a-agent-mcp"}
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["mcp_servers"]; ok {
		t.Fatalf("ADVERSARY BREAK: empty agent.yaml invented mcp_servers: %#v", m["mcp_servers"])
	}
}
