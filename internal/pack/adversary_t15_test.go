package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAdversaryT15_EmptyNameServerStampedOntoLock
// SC2: absent/empty mcp_servers stay off the lock. A nameless entry is empty-like.
func TestAdversaryT15_EmptyNameServerStampedOntoLock(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		return
	}
	// ADVERSARY BREAK: nameless/empty-like server still lands on signed agent_yaml.
	t.Fatalf("ADVERSARY BREAK: empty-name mcp_servers stamped onto lock agent_yaml: %#v", raw)
}

// TestAdversaryT15_WhitespaceOnlyNameStamped
// Bind trims names and skips empty; pack still signs the whitespace token.
func TestAdversaryT15_WhitespaceOnlyNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "   ", Transport: "http"},
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
	if strings.TrimSpace(name) == "" {
		// ADVERSARY BREAK: whitespace-only name is signed into lock agent_yaml.
		t.Fatalf("ADVERSARY BREAK: whitespace-only mcp server name stamped: %#v", servers[0])
	}
}

// TestAdversaryT15_AllEmptyFieldsStillAppended
// CreateAgentLock/canonical always appends an entry even when every field is omitempty-empty.
func TestAdversaryT15_AllEmptyFieldsStillAppended(t *testing.T) {
	ay := &AgentYAML{
		Name:       "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{{}},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		return
	}
	// ADVERSARY BREAK: [{}] is empty-like and must stay off the lock (SC2).
	t.Fatalf("ADVERSARY BREAK: fully empty mcp_servers entry stamped onto lock: %#v", raw)
}

// TestAdversaryT15_NewlineNameNotRejected
// INJECTION: newlines in stamped name survive LoadAgentYAML into the signed envelope.
func TestAdversaryT15_NewlineNameNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\\ninjected\"\n    transport: http\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if ay == nil || len(ay.MCPServers) == 0 {
		t.Fatal("expected mcp_servers to parse")
	}
	if strings.Contains(ay.MCPServers[0].Name, "\n") {
		// ADVERSARY BREAK: newline in mcp_servers[].name accepted into stamp input.
		t.Fatalf("ADVERSARY BREAK: newline in mcp server name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT15_NullByteNameNotRejected
func TestAdversaryT15_NullByteNameNotRejected(t *testing.T) {
	var ay AgentYAML
	err := yaml.Unmarshal([]byte("name: t12a-agent-mcp\nmcp_servers:\n  - name: \"t12a-mcp-m\\u0000evil\"\n    transport: http\n"), &ay)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.ContainsRune(ay.MCPServers[0].Name, 0) {
		// ADVERSARY BREAK: NUL in mcp_servers[].name accepted.
		t.Fatalf("ADVERSARY BREAK: NUL in mcp server name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT15_PathTraversalNameNotRejected
func TestAdversaryT15_PathTraversalNameNotRejected(t *testing.T) {
	var ay AgentYAML
	err := yaml.Unmarshal([]byte("name: t12a-agent-mcp\nmcp_servers:\n  - name: \"../other-tenant-mcp\"\n    transport: http\n"), &ay)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.Contains(ay.MCPServers[0].Name, "..") {
		// ADVERSARY BREAK: path traversal token accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: path traversal mcp server name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT15_SignedCanonicalIncludesMCPServers
// Regression: Go lock canonical MUST cover mcp_servers (cloud verify currently drops them).
func TestAdversaryT15_SignedCanonicalIncludesMCPServers(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: 2,
		AgentName:     "t12a-agent-mcp",
		AgentYAML: &AgentYAML{
			Name: "t12a-agent-mcp",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m", Transport: "http", AllowedTools: []string{"fetch"}},
			},
		},
	}
	raw, err := json.Marshal(lockCanonicalMap(lock, false))
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ay, ok := parsed["agent_yaml"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml missing: %s", raw)
	}
	if _, ok := ay["mcp_servers"]; !ok {
		t.Fatalf("ADVERSARY BREAK: signed lock canonical dropped mcp_servers: %s", raw)
	}
}

// TestAdversaryT15_DuplicateNamesBothStamped
func TestAdversaryT15_DuplicateNamesBothStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m", Transport: "http"},
			{Name: "t12a-mcp-m", Transport: "http", URL: "https://evil.example/mcp"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers")
	}
	servers, ok := raw.([]map[string]interface{})
	if !ok || len(servers) != 2 {
		t.Fatalf("want 2 stamped duplicates, got %#v", raw)
	}
	// Contract gap documented for cloud bind (two loop iterations / last-token-wins).
	if servers[0]["name"] != "t12a-mcp-m" || servers[1]["name"] != "t12a-mcp-m" {
		t.Fatalf("duplicate names not preserved: %#v", servers)
	}
}
