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

func TestCreateAgentLock_DoesNotAutoDeclareLLMProviderHost(t *testing.T) {
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
	if !got["wttr.in"] {
		t.Fatalf("agent_yaml.egress = %v, want wttr.in present", egress)
	}
	if got["openrouter.ai"] {
		t.Fatalf("agent_yaml.egress = %v, want openrouter.ai absent (no auto-declare)", egress)
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

const ingressPolicyYAML = `version: "1.0"
agent:
  name: parvezagent
egress:
  - domain: "openrouter.ai"
    ports: [443]
ingress:
  - path: /webhook
    port: 8080
`

func TestCreateAgentLock_StampsPolicyIngressIntoAgentYAML(t *testing.T) {
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
		AgentYAML:         &AgentYAML{Name: "parvezagent"},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        []byte(ingressPolicyYAML),
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
	ingress, ok := ay["ingress"].([]interface{})
	if !ok {
		t.Fatalf("agent_yaml.ingress missing: %s", raw)
	}
	if len(ingress) != 1 || ingress[0] != "/webhook:8080" {
		t.Fatalf("agent_yaml.ingress = %v, want [/webhook:8080]", ingress)
	}
	idx, ok := parsed["component_index"].(map[string]interface{})
	if !ok {
		t.Fatalf("component_index missing: %s", raw)
	}
	idxIngress, ok := idx["ingress"].([]interface{})
	if !ok {
		t.Fatalf("component_index.ingress missing: %s", raw)
	}
	if len(idxIngress) != 1 || idxIngress[0] != "/webhook:8080" {
		t.Fatalf("component_index.ingress = %v, want [/webhook:8080]", idxIngress)
	}
}

const piiRejectPolicyYAML = `version: "1.0"
agent:
  name: pii-reject-agent
egress:
  - domain: "openrouter.ai"
    ports: [443]
guardrails:
  pii:
    action: reject
    builtins: [Ssn, Email, CreditCard, DriversLicense, Key]
    reject_status: 422
    reject_body: pii_rejected
`

func TestCreateAgentLock_StampsPolicyPIIIntoAgentYAML(t *testing.T) {
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
		AgentYAML:         &AgentYAML{Name: "pii-reject-agent"},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        []byte(piiRejectPolicyYAML),
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
		t.Fatalf("agent_yaml missing: %s", raw)
	}
	gr, ok := ay["guardrails"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml.guardrails missing: %s", raw)
	}
	pii, ok := gr["pii"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml.guardrails.pii missing: %s", raw)
	}
	if pii["action"] != "reject" {
		t.Fatalf("pii.action=%v", pii["action"])
	}
	builtins, ok := pii["builtins"].([]interface{})
	if !ok {
		t.Fatalf("pii.builtins=%v", pii["builtins"])
	}
	got := map[string]bool{}
	for _, v := range builtins {
		s, _ := v.(string)
		got[s] = true
	}
	for _, want := range []string{"Ssn", "Email", "CreditCard", "DriversLicense", "Key"} {
		if !got[want] {
			t.Fatalf("builtins=%v missing %s", builtins, want)
		}
	}
	canon := agentYAMLCanonicalMap(lock.AgentYAML)
	cgr, ok := canon["guardrails"].(map[string]interface{})
	if !ok {
		t.Fatal("canonical map missing guardrails")
	}
	if _, ok := cgr["pii"]; !ok {
		t.Fatal("canonical map missing guardrails.pii")
	}
	idx, ok := parsed["component_index"].(map[string]interface{})
	if !ok {
		t.Fatalf("component_index missing: %s", raw)
	}
	igr, ok := idx["guardrails"].(map[string]interface{})
	if !ok {
		t.Fatalf("component_index.guardrails missing: %s", raw)
	}
	if _, ok := igr["pii"]; !ok {
		t.Fatalf("component_index.guardrails.pii missing: %s", raw)
	}
}

func TestCreateAgentLock_OmitsPIIWhenAbsent_SC8(t *testing.T) {
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
		AgentYAML:         &AgentYAML{Name: "weather-agent"},
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
	ay := parsed["agent_yaml"].(map[string]interface{})
	if _, ok := ay["guardrails"]; ok {
		t.Fatalf("SC8: guardrails must be omitted when policy has no pii: %s", raw)
	}
}

func TestCreateAgentLock_DoesNotStampB19SequenceAsPII(t *testing.T) {
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
	b19 := []byte(`version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "openrouter.ai"
    ports: [443]
guardrails:
  - type: regex
    pattern: "(?i)(password|secret)"
    action: block
`)
	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         &AgentYAML{Name: "test-agent"},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        b19,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	raw, _ := json.Marshal(lock)
	var parsed map[string]interface{}
	_ = json.Unmarshal(raw, &parsed)
	ay := parsed["agent_yaml"].(map[string]interface{})
	if _, ok := ay["guardrails"]; ok {
		t.Fatalf("B19 sequence must not stamp mapping pii: %s", raw)
	}
}

const llmBudgetPolicyYAML = `version: "1.0"
agent:
  name: budget-agent
egress:
  - domain: "openrouter.ai"
    ports: [443]
llm_budget:
  max_tokens: 1
  max_tokens_per_request: 1
`

func TestCreateAgentLock_StampsPolicyLLMBudget(t *testing.T) {
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
		AgentYAML:         &AgentYAML{Name: "budget-agent"},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PolicyYAML:        []byte(llmBudgetPolicyYAML),
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
		t.Fatalf("agent_yaml missing: %s", raw)
	}
	assertLLMBudgetStamp(t, ay["llm_budget"], "agent_yaml.llm_budget", raw)
	canon := agentYAMLCanonicalMap(lock.AgentYAML)
	assertLLMBudgetStamp(t, canon["llm_budget"], "canonical agent_yaml.llm_budget", raw)
	idx, ok := parsed["component_index"].(map[string]interface{})
	if !ok {
		t.Fatalf("component_index missing: %s", raw)
	}
	assertLLMBudgetStamp(t, idx["llm_budget"], "component_index.llm_budget", raw)
}

func TestCreateAgentLock_OmitsLLMBudgetWhenAbsent(t *testing.T) {
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
		AgentYAML:         &AgentYAML{Name: "weather-agent"},
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
	ay := parsed["agent_yaml"].(map[string]interface{})
	if _, ok := ay["llm_budget"]; ok {
		t.Fatalf("llm_budget must be omitted when policy has none: %s", raw)
	}
	if _, ok := agentYAMLCanonicalMap(lock.AgentYAML)["llm_budget"]; ok {
		t.Fatal("canonical agent_yaml must omit llm_budget when policy has none")
	}
	idx, ok := parsed["component_index"].(map[string]interface{})
	if !ok {
		t.Fatalf("component_index missing: %s", raw)
	}
	if _, ok := idx["llm_budget"]; ok {
		t.Fatalf("component_index.llm_budget must be omitted when policy has none: %s", raw)
	}
}

func assertLLMBudgetStamp(t *testing.T, raw interface{}, where string, lockRaw []byte) {
	t.Helper()
	budget, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("%s missing: %s", where, lockRaw)
	}
	if !llmBudgetNumberIs(budget["max_tokens"], 1) {
		t.Fatalf("%s.max_tokens=%v (%T), want 1", where, budget["max_tokens"], budget["max_tokens"])
	}
	if !llmBudgetNumberIs(budget["max_tokens_per_request"], 1) {
		t.Fatalf("%s.max_tokens_per_request=%v (%T), want 1", where, budget["max_tokens_per_request"], budget["max_tokens_per_request"])
	}
}

func llmBudgetNumberIs(v interface{}, want int) bool {
	switch n := v.(type) {
	case int:
		return n == want
	case int64:
		return n == int64(want)
	case float64:
		return n == float64(want)
	case json.Number:
		i, err := n.Int64()
		return err == nil && i == int64(want)
	default:
		return false
	}
}
