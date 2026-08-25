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

// r9 — new vectors only. Do not weaken adversary_t15–t22.

func r9CreateLock(t *testing.T, name string) (*AgentLock, error) {
	t.Helper()
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
	return CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r9-" + name),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r9-" + name),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: name, Transport: "http"},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r9-"+name),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
}

func r9CanonicalName(ay *AgentYAML) (string, bool) {
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		return "", false
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		return "", false
	}
	name, _ := servers[0]["name"].(string)
	return name, true
}

// TestAdversaryT23_DevanagariMcStamped
// IV-MC-ME-MARKS: U+093E DEVANAGARI VOWEL SIGN AA is Mc, not Cf/Mn.
// r8 closed leftover Cf/Mn only. Spacing combining marks survive stamp.
func TestAdversaryT23_DevanagariMcStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u093e", Transport: "http"},
		},
	}
	name, ok := r9CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+093E Mc silently omitted from stamp (strip-then-omit, not pack error)")
	}
	if strings.ContainsRune(name, '\u093e') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: Mc combining mark stamped or strip-folded onto slug.
		t.Fatalf("ADVERSARY BREAK: Devanagari Mc (U+093E) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT23_CreateAgentLockSucceedsOnDevanagariMc
func TestAdversaryT23_CreateAgentLockSucceedsOnDevanagariMc(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u093e")
	if err != nil {
		return
	}
	// ADVERSARY BREAK: SC11-class leftover combining mark must pack-error.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Devanagari Mc U+093E (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_EnclosingMeStamped
// U+20DD COMBINING ENCLOSING CIRCLE is Me, not Cf/Mn.
func TestAdversaryT23_EnclosingMeStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u20dd", Transport: "http"},
		},
	}
	name, ok := r9CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+20DD Me silently omitted from stamp (not pack error)")
	}
	if strings.ContainsRune(name, '\u20dd') || name == "t12a-mcp-m" {
		t.Fatalf("ADVERSARY BREAK: enclosing Me (U+20DD) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT23_CreateAgentLockSucceedsOnEnclosingMe
func TestAdversaryT23_CreateAgentLockSucceedsOnEnclosingMe(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u20dd")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on enclosing Me U+20DD (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_VisargaMcCreateAgentLock
// U+0903 DEVANAGARI SIGN VISARGA is Mc (not the r8 Mn window).
func TestAdversaryT23_VisargaMcCreateAgentLock(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u0903")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Visarga Mc U+0903 (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_LoadAgentYAMLSucceedsOnDevanagariMc
func TestAdversaryT23_LoadAgentYAMLSucceedsOnDevanagariMc(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"t12a-mcp-m\u093e\"\n    transport: http\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		return
	}
	if ay == nil {
		return
	}
	// ADVERSARY BREAK: LoadAgentYAML must error on leftover Mc, not stamp.
	t.Fatalf("ADVERSARY BREAK: LoadAgentYAML succeeded on Devanagari Mc U+093E (servers=%+v)", ay.MCPServers)
}

// TestAdversaryT23_AssembleAgentLockSucceedsOnEnclosingMe
func TestAdversaryT23_AssembleAgentLockSucceedsOnEnclosingMe(t *testing.T) {
	key, _ := testKeyPair(t)
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock := assembleAgentLock(LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r9-asm-me"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r9-asm-me"),
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\u20dd", Transport: "http"},
				{Name: "t12a-mcp-m", Transport: "http"},
			},
		},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r9-asm-me"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
	}, []byte(`{"spdxVersion":"SPDX-2.3"}`), digestString("sbom-r9-asm-me"), string(pubPEM), key, "cosign://test", digestString("policy-r9-asm-me"))
	if lock == nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: assembleAgentLock succeeded on enclosing Me + legal mixed list (servers=%+v)", lock.AgentYAML.MCPServers)
}

// TestAdversaryT23_MixedMcAndLegalStillStamps
// SC13 analog: leftover Mc + legal must not stamp the list.
func TestAdversaryT23_MixedMcAndLegalStillStamps(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u093e", Transport: "http"},
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
	t.Fatalf("ADVERSARY BREAK: mixed Devanagari-Mc + legal mcp_servers stamped (%d entries)", len(servers))
}

// TestAdversaryT23_ObjectReplacementStamped
// IV-BLANK-SO: U+FFFC OBJECT REPLACEMENT CHARACTER is So, not Cf/Mn/Zs.
func TestAdversaryT23_ObjectReplacementStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\ufffc", Transport: "http"},
		},
	}
	name, ok := r9CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+FFFC silently omitted from stamp (not pack error)")
	}
	if strings.ContainsRune(name, '\ufffc') || name == "t12a-mcp-m" {
		t.Fatalf("ADVERSARY BREAK: object replacement (U+FFFC) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT23_CreateAgentLockSucceedsOnObjectReplacement
func TestAdversaryT23_CreateAgentLockSucceedsOnObjectReplacement(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\ufffc")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on object replacement U+FFFC (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_OpenBoxCreateAgentLock
// U+2423 OPEN BOX is So blank-ish, unnamed in SC5/SC11.
func TestAdversaryT23_OpenBoxCreateAgentLock(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u2423")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on open box U+2423 (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_ExtraSiblingEnvDropped
// IV-EXTRA-SIBLINGS: SC8 whole-subtree. env / timeout / secrets vanish
// because MCPServerDecl is a named-field allowlist.
func TestAdversaryT23_ExtraSiblingEnvDropped(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    env:\n      FOO: bar\n    timeout: 30\n    secrets: [x]\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if ay == nil || len(ay.MCPServers) == 0 {
		t.Fatal("expected parsed mcp_servers")
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("ADVERSARY BREAK: sibling-rich yaml dropped mcp_servers entirely")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("ADVERSARY BREAK: sibling-rich yaml produced empty stamp")
	}
	entry := servers[0]
	for _, key := range []string{"env", "timeout", "secrets"} {
		if _, has := entry[key]; !has {
			// ADVERSARY BREAK: SC8 whole-subtree extra siblings never leave pack.
			t.Fatalf("ADVERSARY BREAK: pack dropped %q sibling before stamp: %#v", key, entry)
		}
	}
}

// TestAdversaryT23_SoftHyphenCreateAgentLockStillErrors
// Re-probe r6/r8 Cf close via a different input (U+00AD already on strip path).
func TestAdversaryT23_SoftHyphenCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u00ad")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: soft hyphen U+00AD CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_WordJoinerCreateAgentLockStillErrors
func TestAdversaryT23_WordJoinerCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u2060")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: word joiner U+2060 CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT23_YAMLBoolNameYesStampedAsHostedSlug
// IV-YAML-BOOL-NAME: name: yes may coerce. Must not become a hosted slug
// the author did not write, and must not silently omit.
func TestAdversaryT23_YAMLBoolNameYesStampedAsHostedSlug(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: yes\n    transport: http\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := agentYAMLCanonicalMap(&ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		// yes→empty / rejected-omit is fail-closed only if Create also errors.
		return
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		return
	}
	name, _ := servers[0]["name"].(string)
	if name != "yes" && name != "true" && name != "True" {
		// unexpected coercion is a stamp of an author-unwritten token.
		t.Fatalf("ADVERSARY BREAK: YAML name: yes coerced to unexpected stamp %q", name)
	}
}

// TestAdversaryT23_MongolianFVSCreateAgentLockStillErrors
// Thin r8 re-probe (different helper prefix, same family).
func TestAdversaryT23_MongolianFVSCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r9CreateLock(t, "t12a-mcp-m\u180b")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: r8 Mongolian FVS CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}
