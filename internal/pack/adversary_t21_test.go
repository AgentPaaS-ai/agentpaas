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

// r7 — new vectors only. Do not weaken adversary_t15–t20.

// TestAdversaryT21_HangulFillerNameStamped
// r6 closed leftover Cf (ISS/ALM/TAG) and unicode Zs/Zl/Zp. U+3164 HANGUL
// FILLER is Lo (not Cf, not Zs) and is omitted from mcpInvisibleRune /
// mcpUnicodeSpaceRune. Visible-blank glyph can be stamped as a hosted token.
func TestAdversaryT21_HangulFillerNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u3164", Transport: "http"},
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
	if strings.ContainsRune(name, '\u3164') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: Hangul filler stamped or strip-folded onto slug.
		t.Fatalf("ADVERSARY BREAK: Hangul filler (U+3164) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT21_BrailleBlankNameStamped
// U+2800 BRAILLE PATTERN BLANK is So, not Zs/Cf. Looks empty, not in SC5.
func TestAdversaryT21_BrailleBlankNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u2800", Transport: "http"},
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
	if strings.ContainsRune(name, '\u2800') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: Braille blank stamped or folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: Braille blank (U+2800) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT21_IdeographicVSSupplementStamped
// SC5 variation selectors are U+FE00–U+FE0F only. U+E0100 (VS17) is the
// ideographic VS supplement plane and is omitted from mcpInvisibleRune.
func TestAdversaryT21_IdeographicVSSupplementStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U000E0100", Transport: "http"},
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
	if strings.ContainsRune(name, '\U000E0100') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: VS17 stamped or strip-folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: ideographic VS17 (U+E0100) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT21_LanguageTagNameStamped
// SC5 TAG range is U+E0020–U+E007F. U+E0001 LANGUAGE TAG is leftover Cf
// outside that instance-patched window.
func TestAdversaryT21_LanguageTagNameStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U000E0001", Transport: "http"},
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
	if strings.ContainsRune(name, '\U000E0001') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: LANGUAGE TAG stamped or folded onto hosted slug.
		t.Fatalf("ADVERSARY BREAK: LANGUAGE TAG (U+E0001) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT21_HalfwidthHangulFillerStamped
// U+FFA0 HALFWIDTH HANGUL FILLER is Lo, visible-blank, not in SC5.
func TestAdversaryT21_HalfwidthHangulFillerStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\uffa0", Transport: "http"},
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
	if strings.ContainsRune(name, '\uffa0') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: halfwidth Hangul filler stamped or folded.
		t.Fatalf("ADVERSARY BREAK: U+FFA0 mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT21_CreateAgentLockSucceedsOnHangulFiller
// SC11: injected / invisible-looking names must fail the pack. r6 closed
// CreateAgentLock on mcpNameHasInvisible. Hangul filler is not in that set.
func TestAdversaryT21_CreateAgentLockSucceedsOnHangulFiller(t *testing.T) {
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
			ImageDigest:      digestString("image-r7-hf"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r7-hf"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\u3164", Transport: "http"},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r7-hf"),
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
			if strings.ContainsRune(name, '\u3164') || name == "t12a-mcp-m" {
				// ADVERSARY BREAK: CreateAgentLock signed a Hangul-filler name
				// (or strip-stamped the hosted slug). SC11 requires pack error.
				t.Fatalf("ADVERSARY BREAK: CreateAgentLock stamped Hangul-filler mcp name %q", name)
			}
		}
	}
	// ADVERSARY BREAK: SC11 requires pack error on invisible-looking mcp name.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Hangul filler mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT21_LoadAgentYAMLHangulFillerNotRejected
// LoadAgentYAML must fail-close on a visible-blank filler, not accept a
// token bind exact-match would treat as a distinct hosted slug.
func TestAdversaryT21_LoadAgentYAMLHangulFillerNotRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\u3164\"\n    transport: http\n")
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
	if strings.ContainsRune(ay.MCPServers[0].Name, '\u3164') || ay.MCPServers[0].Name == "t12a-mcp-m" {
		// ADVERSARY BREAK: Hangul filler accepted into stamp input.
		t.Fatalf("ADVERSARY BREAK: U+3164 mcp name accepted by LoadAgentYAML: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT21_UnmarshalBrailleBlankNotRejected
func TestAdversaryT21_UnmarshalBrailleBlankNotRejected(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: \"t12a-mcp-m\u2800\"\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		return
	}
	if len(ay.MCPServers) == 0 {
		return
	}
	if strings.ContainsRune(ay.MCPServers[0].Name, '\u2800') {
		// ADVERSARY BREAK: Braille blank accepted as hosted MCP name.
		t.Fatalf("ADVERSARY BREAK: Braille blank mcp name accepted: %q", ay.MCPServers[0].Name)
	}
}

// TestAdversaryT21_AssembleAgentLockDoesNotErrorOnRejectedName
// SC11 names assembleAgentLock as a fail-closed entrypoint. The function
// has no error return and copies AgentYAML verbatim, so a rejected name
// is stamped when callers skip validateLockConfig.
func TestAdversaryT21_AssembleAgentLockDoesNotErrorOnRejectedName(t *testing.T) {
	key, _ := testKeyPair(t)
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock := assembleAgentLock(LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r7-asm"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r7-asm"),
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "../evil", Transport: "http"},
				{Name: "t12a-mcp-m", Transport: "http"},
			},
		},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r7-asm"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
	}, []byte(`{"spdxVersion":"SPDX-2.3"}`), digestString("sbom-r7-asm"), string(pubPEM), key, "cosign://test", digestString("policy-r7-asm"))
	if lock == nil || lock.AgentYAML == nil {
		return
	}
	found := false
	for _, s := range lock.AgentYAML.MCPServers {
		if s.Name == "../evil" || strings.Contains(s.Name, "..") {
			found = true
		}
	}
	if found {
		// ADVERSARY BREAK: assembleAgentLock stamped a rejected traversal
		// name. SC11 requires error, not copy-through.
		t.Fatalf("ADVERSARY BREAK: assembleAgentLock stamped rejected mcp name (servers=%+v)", lock.AgentYAML.MCPServers)
	}
}

// TestAdversaryT21_MixedHangulFillerAndLegalStillStampsLegal
// SC13: mixed illegal+legal list is not stamped. If Hangul filler is not
// treated as illegal, the legal sibling is still signed.
func TestAdversaryT21_MixedHangulFillerAndLegalStillStampsLegal(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u3164", Transport: "http"},
			{Name: "t12a-mcp-m", Transport: "http"},
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
	// ADVERSARY BREAK: mixed filler+legal list was stamped (SC13 leftover
	// if filler is illegal; if filler is treated as legal this is still a
	// signed lookalike sibling next to the hosted slug).
	t.Fatalf("ADVERSARY BREAK: mixed Hangul-filler + legal mcp_servers stamped (%d entries)", len(servers))
}

// TestAdversaryT21_R6OghamSpaceStillRejected
// Re-probe r6 unicode-space close: U+1680 OGHAM SPACE MARK is Zs.
func TestAdversaryT21_R6OghamSpaceStillRejected(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u1680", Transport: "http"},
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
	if name == "t12a-mcp-m" || strings.ContainsRune(name, '\u1680') {
		t.Fatalf("ADVERSARY BREAK: r6 Ogham-space reject regressed: %q", name)
	}
}

// TestAdversaryT21_R6CreateAgentLockStillErrorsOnIdeographicSpace
func TestAdversaryT21_R6CreateAgentLockStillErrorsOnIdeographicSpace(t *testing.T) {
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
			ImageDigest:      digestString("image-r7-ideo"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r7-ideo"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\u3000", Transport: "http"},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r7-ideo"),
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
	t.Fatalf("ADVERSARY BREAK: r6 CreateAgentLock U+3000 reject regressed (lock=%+v)", lock.AgentName)
}
