package pack

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// r5 — new vectors only. Do not weaken adversary_t15/t16/t17/t18.

// TestAdversaryT19_LRMNameStamped
// r4 stripped ZWSP/WJ/soft-hyphen/CGJ. U+200E LEFT-TO-RIGHT MARK is the
// same invisible-format class and is still treated as a literal token.
func TestAdversaryT19_LRMNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u200e", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for LRM name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u200e') {
		// ADVERSARY BREAK: U+200E survives stamp; signed token is not the
		// visible hosted slug (SC5 exact / SC11 leftover Cf).
		t.Fatalf("ADVERSARY BREAK: LRM mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT19_RLMNameStamped
// U+200F RIGHT-TO-LEFT MARK omitted from mcpInvisibleRune.
func TestAdversaryT19_RLMNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u200f", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for RLM name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u200f') {
		// ADVERSARY BREAK: U+200F survives stamp.
		t.Fatalf("ADVERSARY BREAK: RLM mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT19_LRINameStamped
// U+2066 LRI is a bidi isolate omitted from the r4 Cf table (stopped at U+2064).
func TestAdversaryT19_LRINameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2066", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for LRI name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u2066') {
		// ADVERSARY BREAK: U+2066 survives stamp; r4 closed U+2060–U+2064 only.
		t.Fatalf("ADVERSARY BREAK: LRI mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT19_NELNameFoldedOntoHostedSlug
// U+0085 NEXT LINE is unicode.IsSpace. Stamp does TrimSpace then signs
// the remainder, folding an injected C1 control onto the hosted slug.
func TestAdversaryT19_NELNameFoldedOntoHostedSlug(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u0085", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u0085') {
		// ADVERSARY BREAK: NEL is either stamped or TrimSpace-folded onto
		// the exact hosted slug (SC5 / SC11).
		t.Fatalf("ADVERSARY BREAK: NEL mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT19_SingleFullwidthDotNotRejected
// SC11 leftover: fold table only fail-closes on ".." after fold. A single
// U+FF0E FULLWIDTH FULL STOP is a "." homoglyph and is still stamped.
func TestAdversaryT19_SingleFullwidthDotNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"foo\uff0ebar\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ay.MCPServers) == 0 {
		t.Fatal("expected server")
	}
	if strings.Contains(ay.MCPServers[0].Name, "\uff0e") {
		// ADVERSARY BREAK: single fullwidth-dot accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: single fullwidth-dot mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT19_VariationSelectorNameStamped
// U+FE0F VS16 is an invisible selector omitted from mcpInvisibleRune.
func TestAdversaryT19_VariationSelectorNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\ufe0f", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for VS16 name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\ufe0f') {
		// ADVERSARY BREAK: variation selector survives stamp.
		t.Fatalf("ADVERSARY BREAK: VS16 mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT19_CreateAgentLockSucceedsOnInjectedName
// SC11: injected names must fail the pack (error). LoadAgentYAML errors;
// CreateAgentLock still returns a signed lock after canonical omit.
func TestAdversaryT19_CreateAgentLockSucceedsOnInjectedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	installFakeTool(t, "syft", `#!/bin/sh
printf '%s' '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r5-inj"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r5-inj"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "../evil", Transport: "http"},
				{Name: "t12a-mcp-m", Transport: "http"},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r5-inj"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		return
	}
	// ADVERSARY BREAK: SC11 requires pack error. CreateAgentLock signed
	// a lock whose in-memory list contained an injected name.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on injected mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT19_LoadAgentYAMLLRMNotRejected
// Bind-side exact match fail-closes on LRM today, but SC11 says injected
// / invisible names fail the pack. Load must error, not stamp-after-strip.
func TestAdversaryT19_LoadAgentYAMLLRMNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\u200e\"\n    transport: http\n")
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
	if strings.Contains(ay.MCPServers[0].Name, "\u200e") {
		// ADVERSARY BREAK: LRM name accepted into stamp input.
		t.Fatalf("ADVERSARY BREAK: LRM mcp name accepted by LoadAgentYAML: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT19_R4WordJoinerStillStripped
// Re-probe r4 MEDIUM-1 close: U+2060 must not survive stamp.
func TestAdversaryT19_R4WordJoinerStillStripped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2060", Transport: "http"},
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
	if strings.ContainsRune(name, '\u2060') {
		t.Fatalf("ADVERSARY BREAK: r4 U+2060 strip regressed: %q", name)
	}
}
