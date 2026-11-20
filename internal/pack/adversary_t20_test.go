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

// r6 — new vectors only. Do not weaken adversary_t15/t16/t17/t18/t19.

// TestAdversaryT20_IdeographicSpaceFoldedOntoHostedSlug
// r5 closed NEL (C1) TrimSpace-fold. U+3000 IDEOGRAPHIC SPACE is unicode.IsSpace
// but not C0/C1 and not in mcpInvisibleRune. Stamp TrimSpace-folds it onto the
// exact hosted slug (SC5 invisible-space / SC11).
func TestAdversaryT20_IdeographicSpaceFoldedOntoHostedSlug(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u3000", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u3000') {
		// ADVERSARY BREAK: U+3000 is either stamped or TrimSpace-folded onto
		// the exact hosted slug (SC5 / SC11). Distinct from r5 NEL (C1).
		t.Fatalf("ADVERSARY BREAK: ideographic-space mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT20_EmSpaceFoldedOntoHostedSlug
// U+2003 EM SPACE is Zs. Same TrimSpace fold as U+3000; r1–r5 only hit
// NBSP survival (T16) and NEL C1 (T19), not EM SPACE fold.
func TestAdversaryT20_EmSpaceFoldedOntoHostedSlug(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2003", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u2003') {
		// ADVERSARY BREAK: EM SPACE TrimSpace-folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: EM SPACE mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT20_LineSeparatorFoldedOntoHostedSlug
// U+2028 LINE SEPARATOR is unicode.IsSpace (Zl) and not C0/C1. Stamp
// TrimSpace-folds it onto the hosted slug. Distinct from newline C0 (T15).
func TestAdversaryT20_LineSeparatorFoldedOntoHostedSlug(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2028", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u2028') {
		// ADVERSARY BREAK: U+2028 TrimSpace-folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: LINE SEPARATOR mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT20_ISSNameStamped
// r5 closed U+2066–U+2069 bidi isolates. U+206A INHIBIT SYMMETRIC SWAPPING
// is leftover Cf (U+206A–U+206F) omitted from mcpInvisibleRune.
func TestAdversaryT20_ISSNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u206a", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for ISS name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u206a') {
		// ADVERSARY BREAK: U+206A survives stamp; signed token is not the
		// visible hosted slug (SC5 leftover Cf).
		t.Fatalf("ADVERSARY BREAK: ISS (U+206A) mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT20_ALMNameStamped
// U+061C ARABIC LETTER MARK is a bidi format control omitted from
// mcpInvisibleRune (r5 only added U+200E/U+200F / U+202A–U+202E / U+2066–U+2069).
func TestAdversaryT20_ALMNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u061c", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for ALM name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\u061c') {
		// ADVERSARY BREAK: U+061C survives stamp.
		t.Fatalf("ADVERSARY BREAK: ALM mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT20_CreateAgentLockStripsInvisibleAndStampsSlug
// SC11: injected / invisible names must fail the pack (error). r5 closed
// CreateAgentLock on ASCII ".." inject. rejectPackedMCPServers still does
// not check mcpNameHasInvisible, so an LRM name is stripped then stamped
// as the exact hosted slug.
func TestAdversaryT20_CreateAgentLockStripsInvisibleAndStampsSlug(t *testing.T) {
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
			ImageDigest:      digestString("image-r6-lrm"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r6-lrm"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\u200e", Transport: "http"},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r6-lrm"),
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
	m := agentYAMLCanonicalMap(lock.AgentYAML)
	raw, ok := m["mcp_servers"]
	if ok {
		servers, _ := raw.([]map[string]interface{})
		if len(servers) > 0 {
			name, _ := servers[0]["name"].(string)
			if name == "t12a-mcp-m" {
				// ADVERSARY BREAK: CreateAgentLock stripped LRM and signed the
				// exact hosted slug. SC11 requires pack error, not strip-stamp.
				t.Fatalf("ADVERSARY BREAK: CreateAgentLock stripped invisible and stamped hosted slug %q", name)
			}
		}
	}
	// ADVERSARY BREAK: SC11 requires pack error on invisible mcp name.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on LRM mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT20_LoadAgentYAMLIdeographicSpaceNotRejected
// SC5 / SC11: invisible-space names fail the pack. LoadAgentYAML must
// error on U+3000, not accept a token that stamp will fold onto a slug.
func TestAdversaryT20_LoadAgentYAMLIdeographicSpaceNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\u3000\"\n    transport: http\n")
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
	if strings.ContainsRune(ay.MCPServers[0].Name, '\u3000') || ay.MCPServers[0].Name == "t12a-mcp-m" {
		// ADVERSARY BREAK: ideographic-space name accepted into stamp input.
		t.Fatalf("ADVERSARY BREAK: U+3000 mcp name accepted by LoadAgentYAML: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT20_TagCharNameStamped
// U+E0020 TAG SPACE is an invisible format/tag rune omitted from
// mcpInvisibleRune (SC5 leftover Cf class beyond the r5 list).
func TestAdversaryT20_TagCharNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U000E0020", Transport: "http"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("expected mcp_servers key for TAG name")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("expected stamped server")
	}
	name, _ := servers[0]["name"].(string)
	if strings.ContainsRune(name, '\U000E0020') {
		// ADVERSARY BREAK: TAG SPACE survives stamp.
		t.Fatalf("ADVERSARY BREAK: TAG SPACE mcp name stamped onto lock: %q", name)
	}
}

// TestAdversaryT20_NarrowNoBreakSpaceFoldedOntoHostedSlug
// U+202F NARROW NO-BREAK SPACE is Zs. TrimSpace-folds onto hosted slug.
// Distinct from r2 NBSP (U+00A0) survival test.
func TestAdversaryT20_NarrowNoBreakSpaceFoldedOntoHostedSlug(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u202f", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u202f') {
		// ADVERSARY BREAK: U+202F TrimSpace-folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: NNBSP mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT20_R5CreateAgentLockStillErrorsOnInjectedDotDot
// Re-probe r5 MEDIUM-5 close: ASCII ".." must still fail CreateAgentLock.
func TestAdversaryT20_R5CreateAgentLockStillErrorsOnInjectedDotDot(t *testing.T) {
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
			ImageDigest:      digestString("image-r6-dotdot"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r6-dotdot"),
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
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r6-dotdot"),
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
	t.Fatalf("ADVERSARY BREAK: r5 CreateAgentLock inject-error regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT20_UnmarshalISSNotRejected
// Load/unmarshal of leftover Cf U+206A must fail the pack (SC11), not
// accept a token bind exact-match currently fail-closes on.
func TestAdversaryT20_UnmarshalISSNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"t12a-mcp-m\u206a\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		return
	}
	if len(ay.MCPServers) == 0 {
		return
	}
	if strings.Contains(ay.MCPServers[0].Name, "\u206a") {
		// ADVERSARY BREAK: U+206A accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: ISS mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}
