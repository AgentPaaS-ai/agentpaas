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

// r8 — new vectors only. Do not weaken adversary_t15–t21.

func r8CreateLock(t *testing.T, name string) (*AgentLock, error) {
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
			ImageDigest:      digestString("image-r8-" + name),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r8-" + name),
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
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r8-" + name),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
}

func r8CanonicalName(ay *AgentYAML) (string, bool) {
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

// TestAdversaryT22_YAMLDisabledTrueDroppedBeforeStamp
// IV-PACK-DROP-SIBLINGS: MCPServerDecl UnmarshalYAML copies only
// name/transport/url/allowed_tools. Author disabled:true vanishes
// before CreateAgentLock, so the signed lock cannot honor SC8.
func TestAdversaryT22_YAMLDisabledTrueDroppedBeforeStamp(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    disabled: true\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := agentYAMLCanonicalMap(&ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		// ADVERSARY BREAK: disable intent omitted the whole list (silent drop).
		t.Fatal("ADVERSARY BREAK: disabled:true yaml dropped mcp_servers entirely")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("ADVERSARY BREAK: disabled:true yaml produced empty mcp_servers stamp")
	}
	if _, has := servers[0]["disabled"]; !has {
		// ADVERSARY BREAK: SC8 disable sibling never reaches the signed lock.
		t.Fatalf("ADVERSARY BREAK: pack dropped disabled:true before stamp: %#v", servers[0])
	}
}

// TestAdversaryT22_YAMLEnabledFalseDroppedBeforeStamp
func TestAdversaryT22_YAMLEnabledFalseDroppedBeforeStamp(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    enabled: false\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := agentYAMLCanonicalMap(&ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("ADVERSARY BREAK: enabled:false yaml dropped mcp_servers entirely")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("ADVERSARY BREAK: enabled:false yaml produced empty mcp_servers stamp")
	}
	if _, has := servers[0]["enabled"]; !has {
		// ADVERSARY BREAK: SC8 enabled:false never reaches the signed lock.
		t.Fatalf("ADVERSARY BREAK: pack dropped enabled:false before stamp: %#v", servers[0])
	}
}

// TestAdversaryT22_YAMLTypeStdioDroppedBeforeStamp
// Missing transport + type:stdio in author YAML is dropped; stamp is a
// name-only entry. Cloud shouldBindServer then treats empty transport +
// !url as bindable hosted HTTP (implied stdio → grant).
func TestAdversaryT22_YAMLTypeStdioDroppedBeforeStamp(t *testing.T) {
	var ay AgentYAML
	payload := "name: t12a-agent-mcp\nmcp_servers:\n  - name: t12a-mcp-m\n    type: stdio\n"
	if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m := agentYAMLCanonicalMap(&ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("ADVERSARY BREAK: type:stdio yaml dropped mcp_servers entirely")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("ADVERSARY BREAK: type:stdio yaml produced empty mcp_servers stamp")
	}
	if _, has := servers[0]["type"]; !has {
		// ADVERSARY BREAK: type:stdio omitted; signed entry looks name-only.
		t.Fatalf("ADVERSARY BREAK: pack dropped type:stdio before stamp: %#v", servers[0])
	}
}

// TestAdversaryT22_LoadAgentYAMLDropsDisabledSibling
func TestAdversaryT22_LoadAgentYAMLDropsDisabledSibling(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    disabled: true\n    enabled: false\n    type: stdio\n    oauth: {client_id: x}\n    cwd: /tmp\n    notes: local-only\n")
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
		t.Fatal("ADVERSARY BREAK: LoadAgentYAML + stamp omitted mcp_servers after sibling-rich yaml")
	}
	servers, _ := raw.([]map[string]interface{})
	if len(servers) == 0 {
		t.Fatal("ADVERSARY BREAK: sibling-rich yaml produced empty stamp")
	}
	entry := servers[0]
	for _, key := range []string{"disabled", "enabled", "type", "oauth", "cwd", "notes"} {
		if _, has := entry[key]; !has {
			// ADVERSARY BREAK: SC8 whole-subtree siblings never leave pack.
			t.Fatalf("ADVERSARY BREAK: LoadAgentYAML/stamp dropped %q sibling: %#v", key, entry)
		}
	}
}

// TestAdversaryT22_MongolianFVSStamped
// IV-UNLISTED-CF-MN: U+180B MONGOLIAN FREE VARIATION SELECTOR ONE is Mn,
// not in mcpInvisibleRune (VS is U+FE00–FE0F) or mcpVisibleBlankRune
// (VS17+ is U+E0100–E01EF). r7 closed U+E0100 only.
func TestAdversaryT22_MongolianFVSStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u180b", Transport: "http"},
		},
	}
	name, ok := r8CanonicalName(ay)
	if !ok {
		// Silent omit of an unlisted format glyph is still SC11 fail-open
		// unless CreateAgentLock also errors (checked below).
		t.Fatal("ADVERSARY BREAK: U+180B silently omitted from stamp (strip-then-omit, not pack error)")
	}
	if strings.ContainsRune(name, '\u180b') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: Mongolian FVS stamped or strip-folded onto slug.
		t.Fatalf("ADVERSARY BREAK: Mongolian FVS (U+180B) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT22_CreateAgentLockSucceedsOnMongolianFVS
func TestAdversaryT22_CreateAgentLockSucceedsOnMongolianFVS(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\u180b")
	if err != nil {
		return
	}
	// ADVERSARY BREAK: SC11 requires pack error on unlisted format glyph.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Mongolian FVS mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_KhmerInherentVowelStamped
// U+17B4 KHMER VOWEL INHERENT AQ is Mn Cf-adjacent, unnamed in SC5.
func TestAdversaryT22_KhmerInherentVowelStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u17b4", Transport: "http"},
		},
	}
	name, ok := r8CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+17B4 silently omitted from stamp (not pack error)")
	}
	if strings.ContainsRune(name, '\u17b4') || name == "t12a-mcp-m" {
		t.Fatalf("ADVERSARY BREAK: Khmer inherent vowel (U+17B4) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT22_CreateAgentLockSucceedsOnKhmerInherentVowel
func TestAdversaryT22_CreateAgentLockSucceedsOnKhmerInherentVowel(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\u17b4")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Khmer U+17B4 mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_MusicalFormatStamped
// U+1D173 MUSICAL SYMBOL BEGIN BEAM is Cf outside SC5 leftover-Cf list.
func TestAdversaryT22_MusicalFormatStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U0001D173", Transport: "http"},
		},
	}
	name, ok := r8CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+1D173 silently omitted from stamp (not pack error)")
	}
	if strings.ContainsRune(name, '\U0001D173') || name == "t12a-mcp-m" {
		t.Fatalf("ADVERSARY BREAK: musical format (U+1D173) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT22_CreateAgentLockSucceedsOnMusicalFormat
func TestAdversaryT22_CreateAgentLockSucceedsOnMusicalFormat(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\U0001D173")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on musical format U+1D173 (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_KaithiNumberSignStamped
// U+110BD KAITHI NUMBER SIGN is Cf, unnamed in SC5 leftover-Cf.
func TestAdversaryT22_KaithiNumberSignStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U000110BD", Transport: "http"},
		},
	}
	name, ok := r8CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+110BD silently omitted from stamp (not pack error)")
	}
	if strings.ContainsRune(name, '\U000110BD') || name == "t12a-mcp-m" {
		t.Fatalf("ADVERSARY BREAK: KAITHI NUMBER SIGN (U+110BD) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT22_CreateAgentLockSucceedsOnKaithiNumberSign
func TestAdversaryT22_CreateAgentLockSucceedsOnKaithiNumberSign(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\U000110BD")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on KAITHI U+110BD (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_AssembleAgentLockDoesNotErrorOnMongolianFVS
func TestAdversaryT22_AssembleAgentLockDoesNotErrorOnMongolianFVS(t *testing.T) {
	key, _ := testKeyPair(t)
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock := assembleAgentLock(LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r8-asm-fvs"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r8-asm-fvs"),
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\u180b", Transport: "http"},
				{Name: "t12a-mcp-m", Transport: "http"},
			},
		},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r8-asm-fvs"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
	}, []byte(`{"spdxVersion":"SPDX-2.3"}`), digestString("sbom-r8-asm-fvs"), string(pubPEM), key, "cosign://test", digestString("policy-r8-asm-fvs"))
	if lock == nil {
		return
	}
	// ADVERSARY BREAK: SC11 names assembleAgentLock as fail-closed.
	t.Fatalf("ADVERSARY BREAK: assembleAgentLock succeeded on Mongolian FVS + legal mixed list (servers=%+v)", lock.AgentYAML.MCPServers)
}

// TestAdversaryT22_MixedMongolianFVSAndLegalStillStamps
// SC13: mixed illegal+legal must not stamp.
func TestAdversaryT22_MixedMongolianFVSAndLegalStillStamps(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\u180b", Transport: "http"},
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
	t.Fatalf("ADVERSARY BREAK: mixed Mongolian-FVS + legal mcp_servers stamped (%d entries)", len(servers))
}

// TestAdversaryT22_TabPaddedNameCreateAgentLock
// IV-C0-PAD: TAB is C0. rejectPackedMCPServers should error (SC11).
// If it succeeds, strip/trim must not fold onto the hosted slug.
func TestAdversaryT22_TabPaddedNameCreateAgentLock(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\t")
	if err != nil {
		return
	}
	name, ok := r8CanonicalName(lock.AgentYAML)
	if ok && (name == "t12a-mcp-m" || strings.ContainsRune(name, '\t')) {
		t.Fatalf("ADVERSARY BREAK: TAB-padded name strip-folded or stamped: %q", name)
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on TAB-padded mcp name (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_ZWSPCreateAgentLockStillErrors
// Re-probe r3/r4 ZWSP close via CreateAgentLock (not silent omit).
func TestAdversaryT22_ZWSPCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\u200b")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: r3 ZWSP CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT22_HangulChoseongFillerCreateAgentLock
// r7 tested U+3164. SC5 also names U+115F HANGUL CHOSEONG FILLER.
func TestAdversaryT22_HangulChoseongFillerCreateAgentLock(t *testing.T) {
	lock, err := r8CreateLock(t, "t12a-mcp-m\u115f")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on Hangul choseong filler U+115F (lock=%+v)", lock.AgentName)
}
