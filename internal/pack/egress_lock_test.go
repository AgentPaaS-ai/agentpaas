package pack

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

const egressPolicyYAML = `version: "1.0"
agent:
  name: weather-agent
  description: "E2E egress stamp"
egress:
  - domain: "wttr.in"
    ports: [443]
  - domain: "openrouter.ai"
    ports: [443]
`

func TestCreateAgentLock_StampsPolicyEgressIntoAgentYAML(t *testing.T) {
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
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         &AgentYAML{},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        []byte(egressPolicyYAML),
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	raw, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("json.Marshal lock: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal lock: %v", err)
	}
	ay, ok := parsed["agent_yaml"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml missing or not an object: %s", raw)
	}
	egress, ok := ay["egress"].([]interface{})
	if !ok {
		t.Fatalf("agent_yaml.egress missing: %s", raw)
	}
	got := make(map[string]bool, len(egress))
	for _, v := range egress {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("egress entry %v is not a string", v)
		}
		got[s] = true
	}
	if !got["wttr.in"] || !got["openrouter.ai"] {
		t.Fatalf("agent_yaml.egress = %v, want wttr.in and openrouter.ai", egress)
	}
}

const llmAutoDeclarePolicyYAML = `version: "1.0"
agent:
  name: weather-agent
  description: "E2E egress stamp"
egress:
  - domain: "wttr.in"
    ports: [443]
`

func TestCreateAgentLock_AutoDeclaresLLMProviderHost(t *testing.T) {
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
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			LLM: LLMConfig{Provider: "openrouter"},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        []byte(llmAutoDeclarePolicyYAML),
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	raw, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("json.Marshal lock: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal lock: %v", err)
	}
	ay, ok := parsed["agent_yaml"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml missing or not an object: %s", raw)
	}
	egress, ok := ay["egress"].([]interface{})
	if !ok {
		t.Fatalf("agent_yaml.egress missing: %s", raw)
	}
	got := make(map[string]bool, len(egress))
	for _, v := range egress {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("egress entry %v is not a string", v)
		}
		got[s] = true
	}
	if !got["wttr.in"] || !got["openrouter.ai"] {
		t.Fatalf("agent_yaml.egress = %v, want wttr.in and openrouter.ai", egress)
	}
}

func TestAgentYAMLCanonicalMap_IncludesEgress(t *testing.T) {
	src := []string{"wttr.in", "openrouter.ai"}
	ay := &AgentYAML{Name: "weather", Egress: src}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["egress"]
	if !ok {
		t.Fatal("canonical map missing egress")
	}
	got, ok := raw.([]string)
	if !ok {
		t.Fatalf("egress type %T, want []string", raw)
	}
	if len(got) != 2 || got[0] != "wttr.in" || got[1] != "openrouter.ai" {
		t.Fatalf("egress = %v, want [wttr.in openrouter.ai]", got)
	}
	got[0] = "mutated"
	if ay.Egress[0] != "wttr.in" {
		t.Fatal("canonical map must copy egress strings")
	}
}

func TestAgentYAML_StringListEgressUnmarshals(t *testing.T) {
	var ay AgentYAML
	err := yaml.Unmarshal([]byte("name: weather\negress:\n  - example.com\n  - wttr.in\n"), &ay)
	if err != nil {
		t.Fatalf("unmarshal agent.yaml string-list egress: %v", err)
	}
	if len(ay.Egress) != 2 || ay.Egress[0] != "example.com" || ay.Egress[1] != "wttr.in" {
		t.Fatalf("Egress = %#v, want [example.com wttr.in]", ay.Egress)
	}
}
