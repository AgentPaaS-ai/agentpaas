package pack

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"gopkg.in/yaml.v3"
)

// r10 — new vectors only. Do not weaken adversary_t15–t23.

func r10CreateLock(t *testing.T, name string) (*AgentLock, error) {
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
			ImageDigest:      digestString("image-r10-" + name),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r10-" + name),
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
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r10-"+name),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
}

func r10CanonicalName(ay *AgentYAML) (string, bool) {
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

func r10LoadYAML(t *testing.T, name string) (*AgentYAML, error) {
	t.Helper()
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: \"" + name + "\"\n    transport: http\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	return LoadAgentYAML(dir)
}

// TestAdversaryT24_MusicalNullNoteheadStamped
// IV-OTHER-BLANK-SO: U+1D159 MUSICAL SYMBOL NULL NOTEHEAD is So, not in
// r9 Control Pictures / Specials / default-ignorable patches.
func TestAdversaryT24_MusicalNullNoteheadStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U0001D159", Transport: "http"},
		},
	}
	name, ok := r10CanonicalName(ay)
	if !ok {
		t.Fatal("ADVERSARY BREAK: U+1D159 silently omitted from stamp (strip-then-omit, not pack error)")
	}
	if strings.ContainsRune(name, '\U0001D159') || name == "t12a-mcp-m" {
		// ADVERSARY BREAK: leftover blank-So stamped or strip-folded onto slug.
		t.Fatalf("ADVERSARY BREAK: musical null notehead (U+1D159) mcp name folded or stamped: %q", name)
	}
}

// TestAdversaryT24_CreateAgentLockSucceedsOnMusicalNullNotehead
func TestAdversaryT24_CreateAgentLockSucceedsOnMusicalNullNotehead(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\U0001D159")
	if err != nil {
		return
	}
	// ADVERSARY BREAK: SC11 leftover blank-So must pack-error.
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on musical null notehead U+1D159 (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_ShoulderedOpenBoxCreateAgentLock
// U+237D SHOULDERED OPEN BOX is So outside Control Pictures (U+2400–U+243F).
func TestAdversaryT24_ShoulderedOpenBoxCreateAgentLock(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u237d")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: CreateAgentLock succeeded on shouldered open box U+237D (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_LoadAgentYAMLSucceedsOnMusicalNullNotehead
func TestAdversaryT24_LoadAgentYAMLSucceedsOnMusicalNullNotehead(t *testing.T) {
	ay, err := r10LoadYAML(t, "t12a-mcp-m\U0001D159")
	if err != nil {
		return
	}
	if ay == nil {
		return
	}
	// ADVERSARY BREAK: LoadAgentYAML must error on leftover blank-So, not stamp.
	t.Fatalf("ADVERSARY BREAK: LoadAgentYAML succeeded on musical null notehead U+1D159 (servers=%+v)", ay.MCPServers)
}

// TestAdversaryT24_MixedNoteheadAndLegalStillStamps
// SC13 analog: leftover blank-So + legal must not stamp the list.
func TestAdversaryT24_MixedNoteheadAndLegalStillStamps(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m\U0001D159", Transport: "http"},
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
	t.Fatalf("ADVERSARY BREAK: mixed U+1D159 + legal mcp_servers stamped (%d entries)", len(servers))
}

// TestAdversaryT24_AssembleAgentLockSucceedsOnMixedNotehead
func TestAdversaryT24_AssembleAgentLockSucceedsOnMixedNotehead(t *testing.T) {
	key, _ := testKeyPair(t)
	pubPEM, err := publicKeyPEM(&key.PublicKey)
	if err != nil {
		t.Fatalf("publicKeyPEM: %v", err)
	}
	lock := assembleAgentLock(LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-r10-asm-so"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r10-asm-so"),
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{Name: "t12a-mcp-m\U0001D159", Transport: "http"},
				{Name: "t12a-mcp-m", Transport: "http"},
			},
		},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r10-asm-so"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
	}, []byte(`{"spdxVersion":"SPDX-2.3"}`), digestString("sbom-r10-asm-so"), string(pubPEM), key, "cosign://test", digestString("policy-r10-asm-so"))
	if lock == nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: assembleAgentLock succeeded on U+1D159 + legal mixed list (servers=%+v)", lock.AgentYAML.MCPServers)
}

// TestAdversaryT24_BengaliMcCreateAgentLockStillErrors
// IV-OTHER-MC re-probe: U+09BE BENGALI VOWEL SIGN AA is Mc. r9 claimed general-category.
func TestAdversaryT24_BengaliMcCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u09be")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: Bengali Mc U+09BE CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_EnclosingSquareMeCreateAgentLockStillErrors
// IV-OTHER-ME re-probe: U+20DE COMBINING ENCLOSING SQUARE.
func TestAdversaryT24_EnclosingSquareMeCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u20de")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: enclosing Me U+20DE CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_DevanagariMcCreateStillErrors
// Thin r9 HIGH-1 re-probe.
func TestAdversaryT24_DevanagariMcCreateStillErrors(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u093e")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: r9 Devanagari Mc CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_OneDotLeaderCreateAgentLockStillErrors
// IV-DOT-LEFTOVER: U+2024 ONE DOT LEADER is in mcpDotEquivalents.
func TestAdversaryT24_OneDotLeaderCreateAgentLockStillErrors(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u2024")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: one-dot leader U+2024 CreateAgentLock reject regressed (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_C1CSICreateAgentLockStillErrors
// IV-C1-CSI: U+009B CSI. SC11 names NEL U+0085 only.
func TestAdversaryT24_C1CSICreateAgentLockStillErrors(t *testing.T) {
	lock, err := r10CreateLock(t, "t12a-mcp-m\u009b")
	if err != nil {
		return
	}
	t.Fatalf("ADVERSARY BREAK: C1 CSI U+009B CreateAgentLock succeeded (lock=%+v)", lock.AgentName)
}

// TestAdversaryT24_FullwidthHyphenStamped
// IV-DASH-FOLD: U+FF0D FULLWIDTH HYPHEN-MINUS is not a unicode-dot.
// Must not fold onto ASCII '-' hosted slug; Create must not silently accept
// a lookalike token if pack later width-folds.
func TestAdversaryT24_FullwidthHyphenStamped(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a\uff0dmcp\uff0dm", Transport: "http"},
		},
	}
	name, ok := r10CanonicalName(ay)
	if !ok {
		return
	}
	if name == "t12a-mcp-m" || strings.Contains(name, "-") && !strings.ContainsRune(name, '\uff0d') {
		// ADVERSARY BREAK: fullwidth hyphen folded onto ASCII hosted slug.
		t.Fatalf("ADVERSARY BREAK: fullwidth hyphen (U+FF0D) mcp name folded onto ASCII slug: %q", name)
	}
}

// TestAdversaryT24_YAMLBoolNameOn
// IV-YAML-BOOL-NAME-ON: name: on / name: 1 / name: ~ must not become an
// author-unwritten hosted slug.
func TestAdversaryT24_YAMLBoolNameOn(t *testing.T) {
	for _, payload := range []string{
		"name: t12a-agent-mcp\nmcp_servers:\n  - name: on\n    transport: http\n",
		"name: t12a-agent-mcp\nmcp_servers:\n  - name: 1\n    transport: http\n",
		"name: t12a-agent-mcp\nmcp_servers:\n  - name: ~\n    transport: http\n",
	} {
		var ay AgentYAML
		if err := yaml.Unmarshal([]byte(payload), &ay); err != nil {
			continue
		}
		m := agentYAMLCanonicalMap(&ay)
		raw, ok := m["mcp_servers"]
		if !ok {
			continue
		}
		servers, _ := raw.([]map[string]interface{})
		if len(servers) == 0 {
			continue
		}
		name, _ := servers[0]["name"].(string)
		switch name {
		case "on", "1", "~", "true", "True", "yes":
			// coerced-to-written or YAML-bool-as-string is author-visible.
		default:
			if name != "" {
				t.Fatalf("ADVERSARY BREAK: YAML bool-like mcp name coerced to unexpected stamp %q (payload=%q)", name, payload)
			}
		}
	}
}

// TestAdversaryT24_SpecNestedMCPServersNotStamped
// IV-SPEC-NESTED-MCP: v1 spec.mcp_servers must not be a second stamp path.
func TestAdversaryT24_SpecNestedMCPServersNotStamped(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nspec:\n  mcp_servers:\n    - name: t12a-mcp-m\n      transport: http\n")
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
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["mcp_servers"]; ok {
		// ADVERSARY BREAK: spec.mcp_servers leaked onto lock stamp.
		t.Fatalf("ADVERSARY BREAK: spec.mcp_servers stamped onto lock agent_yaml: %#v", m["mcp_servers"])
	}
}

// TestAdversaryT24_MCPServersMapShapeNotStamped
// IV-MCP-SERVERS-MAP: mapping-shaped mcp_servers must not invent a hosted slug.
func TestAdversaryT24_MCPServersMapShapeNotStamped(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  t12a-mcp-m:\n    transport: http\n")
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
	m := agentYAMLCanonicalMap(ay)
	if raw, ok := m["mcp_servers"]; ok {
		t.Fatalf("ADVERSARY BREAK: map-shaped mcp_servers stamped: %#v", raw)
	}
}

// TestAdversaryT24_ExtraSiblingStillStamped
// Thin r9 HIGH-3 re-probe: env/timeout/secrets stay on the canonical stamp.
func TestAdversaryT24_ExtraSiblingStillStamped(t *testing.T) {
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
			t.Fatalf("ADVERSARY BREAK: r9 Extra sibling %q vanished from stamp: %#v", key, entry)
		}
	}
}

// TestAdversaryT24_ExtraDroppedOnReadAgentLock
// IV-EXTRA-JSON-DASH: Extra is json:"-". ReadAgentLock rehydrate then
// lockCanonicalMap must still carry env/timeout or SC8 persist is a hole.
func TestAdversaryT24_ExtraDroppedOnReadAgentLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    env:\n      FOO: bar\n    timeout: 30\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil || ay == nil {
		t.Fatalf("LoadAgentYAML: %v ay=%v", err, ay)
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
			ImageDigest:      digestString("image-r10-extra"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-r10-extra"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         ay,
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-r10-extra"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	before := lockCanonicalMap(lock, true)
	ayBefore, _ := before["agent_yaml"].(map[string]interface{})
	serversBefore, _ := ayBefore["mcp_servers"].([]map[string]interface{})
	if len(serversBefore) == 0 {
		t.Fatal("expected stamped mcp_servers before write")
	}
	if _, has := serversBefore[0]["env"]; !has {
		t.Fatalf("ADVERSARY BREAK: env missing from lockCanonicalMap before write: %#v", serversBefore[0])
	}
	path := filepath.Join(dir, "agent.lock")
	if err := WriteAgentLock(lock, path); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}
	rehydrated, err := ReadAgentLock(path)
	if err != nil {
		t.Fatalf("ReadAgentLock: %v", err)
	}
	after := lockCanonicalMap(rehydrated, true)
	ayAfter, _ := after["agent_yaml"].(map[string]interface{})
	var serversAfter []map[string]interface{}
	switch v := ayAfter["mcp_servers"].(type) {
	case []map[string]interface{}:
		serversAfter = v
	case []interface{}:
		for _, item := range v {
			if rec, ok := item.(map[string]interface{}); ok {
				serversAfter = append(serversAfter, rec)
			}
		}
	}
	if len(serversAfter) == 0 {
		t.Fatal("ADVERSARY BREAK: ReadAgentLock dropped mcp_servers")
	}
	if _, has := serversAfter[0]["env"]; !has {
		// ADVERSARY BREAK: Extra json:"-" dropped on rehydrate; SC8 persist hole.
		t.Fatalf("ADVERSARY BREAK: Extra env dropped on ReadAgentLock rehydrate: %#v", serversAfter[0])
	}
}

// TestAdversaryT24_BlankSoPredicateIsRangePatched
// Documents that mcpBlankSoRune is Control Pictures + Specials, not
// remaining So blanks. U+1D159 must be classified leftover.
func TestAdversaryT24_BlankSoPredicateIsRangePatched(t *testing.T) {
	r := '\U0001D159'
	if !unicode.Is(unicode.So, r) {
		t.Skip("U+1D159 is not So on this unicode tables")
	}
	if mcpBlankSoRune(r) || mcpVisibleBlankRune(r) || mcpServerNameRejected("t12a-mcp-m\U0001D159") {
		return
	}
	// ADVERSARY BREAK: leftover blank-So not on reject path.
	t.Fatalf("ADVERSARY BREAK: U+1D159 So is not leftover-rejected (blankSo=%v visible=%v rejected=%v)",
		mcpBlankSoRune(r), mcpVisibleBlankRune(r), mcpServerNameRejected("t12a-mcp-m\U0001D159"))
}

// TestAdversaryT24_LockfileCanonicalJSONIncludesExtra
// Companion to Extra persist: LockfileCanonicalJSON of a freshly packed
// lock must contain env so cloud verify hashes the sibling.
func TestAdversaryT24_LockfileCanonicalJSONIncludesExtra(t *testing.T) {
	dir := t.TempDir()
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    env:\n      FOO: bar\n")
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil || ay == nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	raw, err := json.Marshal(agentYAMLCanonicalMap(ay))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), "FOO") {
		t.Fatalf("ADVERSARY BREAK: canonical agent_yaml dropped env FOO: %s", raw)
	}
}
