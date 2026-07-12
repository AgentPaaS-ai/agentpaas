package pack

import (
	"context"
	"encoding/hex"
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
		AgentYAML:       &AgentYAML{},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
		KeyStore:        store,
		KeyID:           store.keyID,
		PolicyYAML:      policyBytes,
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
		AgentYAML:       &AgentYAML{},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
		KeyStore:        store,
		KeyID:           store.keyID,
		PolicyYAML:      nil,
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
