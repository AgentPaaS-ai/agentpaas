package pack

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// validPolicyYAML is a minimal valid policy for testing.
const validPolicyYAML = `version: "1.0"
agent:
  name: test-agent
  description: "Test agent"
egress:
  - domain: "api.example.com"
    ports: [443]
credentials:
  - id: "my-key"
    type: header
    header: "X-API-Key"
    value: "${env:KEY}"
`

// privateCIDRPolicy is a policy with a private CIDR without allow_private,
// which causes a validation error.
const privateCIDRPolicy = `version: "1.0"
agent:
  name: test-agent
egress:
  - cidr: "10.0.0.0/8"
    ports: [5432]
`

func TestComputePolicyDigest_Valid(t *testing.T) {
	digest, err := ComputePolicyDigest([]byte(validPolicyYAML))
	if err != nil {
		t.Fatalf("ComputePolicyDigest: %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest length = %d, want 64", len(digest))
	}
	if _, decErr := hex.DecodeString(digest); decErr != nil {
		t.Fatalf("digest is not valid hex: %v", decErr)
	}
	if digest == strings.Repeat("0", 64) {
		t.Fatal("digest is all zeros")
	}
}

func TestComputePolicyDigest_Empty(t *testing.T) {
	digest, err := ComputePolicyDigest(nil)
	if err != nil {
		t.Fatalf("ComputePolicyDigest(nil): %v", err)
	}
	if digest != "" {
		t.Fatalf("digest = %q, want empty", digest)
	}

	digest, err = ComputePolicyDigest([]byte{})
	if err != nil {
		t.Fatalf("ComputePolicyDigest([]): %v", err)
	}
	if digest != "" {
		t.Fatalf("digest = %q, want empty", digest)
	}
}

func TestComputePolicyDigest_InvalidYAML(t *testing.T) {
	_, err := ComputePolicyDigest([]byte("not: valid: yaml: [["))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestComputePolicyDigest_CustomPIIPatternsRefused(t *testing.T) {
	yml := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
guardrails:
  pii:
    action: reject
    builtins: [Ssn]
    patterns:
      - "\\d{3}-\\d{3}-\\d{4}"
    reject_status: 422
    reject_body: pii_rejected
`
	_, err := ComputePolicyDigest([]byte(yml))
	if err == nil {
		t.Fatal("ComputePolicyDigest accepted custom PII patterns")
	}
	msg := err.Error()
	for _, want := range []string{
		"guardrails.pii.patterns",
		"custom PII patterns are not supported",
		"CreditCard",
		"Ssn",
		"Email",
		"DriversLicense",
		"Key",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("digest error %q missing %q", msg, want)
		}
	}
	got, err := ComputeInstalledPolicyDigest([]byte(yml))
	if err != nil {
		t.Fatalf("installed digest must still hash a lock that already carries patterns: %v", err)
	}
	again, err := ComputeInstalledPolicyDigest([]byte(yml))
	if err != nil || again != got {
		t.Fatalf("installed digest unstable: %v %s %s", err, got, again)
	}
}

func TestCreateAgentLock_RefusesAgentYAMLCustomPIIPatterns(t *testing.T) {
	pattern := `\d{3}-\d{3}-\d{4}`
	agent := &AgentYAML{
		Name: "weather-agent",
		Guardrails: &AgentGuardrails{
			PII: &AgentPIIGuardrail{
				Action:   "reject",
				Builtins: []string{"Ssn"},
				Patterns: []string{pattern},
			},
		},
	}
	cases := []struct {
		name   string
		policy []byte
	}{
		{name: "missing policy"},
		{name: "policy without mapping-form pii", policy: []byte(validPolicyYAML)},
		{name: "sequence-form guardrails", policy: []byte(`version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
guardrails:
  - type: regex
    pattern: "(?i)password"
    action: block
`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CreateAgentLock(context.Background(), LockConfig{
				AgentYAML:  agent,
				PolicyYAML: tc.policy,
			})
			if err == nil {
				t.Fatal("CreateAgentLock signed a lock carrying custom patterns")
			}
			msg := err.Error()
			for _, want := range []string{
				"guardrails.pii.patterns",
				"custom PII patterns are not supported",
				"CreditCard",
				"Ssn",
				"Email",
				"DriversLicense",
				"Key",
			} {
				if !strings.Contains(msg, want) {
					t.Fatalf("refusal %q missing %q", msg, want)
				}
			}
			if strings.Contains(msg, pattern) && strings.Contains(msg, "agent.lock") {
				t.Fatalf("refusal must not write a lock that carries the pattern: %s", msg)
			}
		})
	}
}

func TestCreateAgentLock_MappingFormPIIOverwritesAgentYAMLPatterns(t *testing.T) {
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

	custom := `\d{3}-\d{3}-\d{4}`
	agent := &AgentYAML{
		Name: "pii-reject-agent",
		Guardrails: &AgentGuardrails{
			PII: &AgentPIIGuardrail{
				Action:   "reject",
				Builtins: []string{"Ssn"},
				Patterns: []string{custom},
			},
		},
	}
	if err := RefuseAgentYAMLCustomPIIPatterns(agent, []byte(piiRejectPolicyYAML)); err != nil {
		t.Fatalf("mapping-form policy must overwrite, not refuse before build: %v", err)
	}
	if err := RefuseAgentYAMLCustomPIIPatterns(&AgentYAML{
		Guardrails: &AgentGuardrails{PII: &AgentPIIGuardrail{Action: "reject", Builtins: []string{"Ssn"}}},
	}, nil); err != nil {
		t.Fatalf("built-in-only agent.yaml must not be refused: %v", err)
	}

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         agent,
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
	for _, path := range []string{"agent_yaml", "component_index"} {
		block, ok := parsed[path].(map[string]interface{})
		if !ok {
			t.Fatalf("%s missing: %s", path, raw)
		}
		gr, ok := block["guardrails"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s.guardrails missing: %s", path, raw)
		}
		pii, ok := gr["pii"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s.guardrails.pii missing: %s", path, raw)
		}
		if _, ok := pii["patterns"]; ok {
			t.Fatalf("%s.guardrails.pii.patterns = %v, want omitted after policy overwrite", path, pii["patterns"])
		}
		if strings.Contains(string(raw), custom) {
			t.Fatalf("lock still carries custom pattern %q", custom)
		}
	}
}

func TestComputePolicyDigest_ValidationErrors(t *testing.T) {
	_, err := ComputePolicyDigest([]byte(privateCIDRPolicy))
	if err == nil {
		t.Fatal("expected validation error for private CIDR without allow_private")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("error does not mention validation: %v", err)
	}
	if !strings.Contains(err.Error(), "CIDR egress rules are not yet supported") {
		t.Fatalf("error does not mention CIDR rejection: %v", err)
	}
}

func TestComputePolicyDigest_Stable(t *testing.T) {
	d1, err := ComputePolicyDigest([]byte(validPolicyYAML))
	if err != nil {
		t.Fatalf("ComputePolicyDigest 1: %v", err)
	}
	d2, err := ComputePolicyDigest([]byte(validPolicyYAML))
	if err != nil {
		t.Fatalf("ComputePolicyDigest 2: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("digests differ: %s != %s", d1, d2)
	}
}

func TestCreateAgentLock_WithPolicy(t *testing.T) {
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

	policyBytes := []byte(validPolicyYAML)
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
		PolicyYAML:        policyBytes,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if lock.PolicyDigest == "" {
		t.Fatal("PolicyDigest is empty when PolicyYAML is provided")
	}
	if len(lock.PolicyDigest) != 64 {
		t.Fatalf("PolicyDigest length = %d, want 64", len(lock.PolicyDigest))
	}

	wantDigest, err := ComputePolicyDigest(policyBytes)
	if err != nil {
		t.Fatalf("ComputePolicyDigest: %v", err)
	}
	if lock.PolicyDigest != wantDigest {
		t.Fatalf("PolicyDigest = %s, want %s", lock.PolicyDigest, wantDigest)
	}
}

func TestCreateAgentLock_NoPolicy(t *testing.T) {
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
		PolicyYAML:        nil,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if lock.PolicyDigest != "" {
		t.Fatalf("PolicyDigest = %q, want empty (backward compat)", lock.PolicyDigest)
	}
}

func TestCreateAgentLock_InvalidPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	installFakeTool(t, "syft", `#!/bin/sh
printf '%s' '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)

	_, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:       &AgentYAML{},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
		KeyStore:        store,
		KeyID:           store.keyID,
		PolicyYAML:      []byte(privateCIDRPolicy),
	})
	if err == nil {
		t.Fatal("expected error for invalid policy")
	}
	if !strings.Contains(err.Error(), "policy validation") {
		t.Fatalf("error does not mention policy validation: %v", err)
	}
}

func TestComputePolicyDigest_FullPolicyFile(t *testing.T) {
	digest, err := ComputePolicyDigest([]byte(validPolicyYAML))
	if err != nil {
		t.Fatalf("ComputePolicyDigest(validPolicyYAML): %v", err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest length = %d, want 64", len(digest))
	}
}
