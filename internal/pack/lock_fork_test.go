package pack

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

const policyV1Empty = "version: \"1.0\"\negress: []\n"

func forkPackTestConfig(t *testing.T, projectDir string, pubKS identity.KeyStore, policyYAML []byte) LockConfig {
	t.Helper()
	installFakeTool(t, "syft", `#!/bin/sh
printf '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	return LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:       &AgentYAML{Name: "fork-agent", Version: "0.2.0"},
		Runtime:         RuntimeType("python"),
		BaseImageDigest: "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:  "test",
		Platform:        "linux/arm64",
		SourceDateEpoch: testTime(),
		KeyStore:        store,
		KeyID:           store.keyID,
		PolicyYAML:      policyYAML,
		PublisherKeyStore: pubKS,
		ProjectDir:      projectDir,
	}
}

func signedCreatedEntry(t *testing.T, name, version, pubName string) (*ProvenanceEntry, string, *ecdsa.PrivateKey) {
	t.Helper()
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)
	e := &ProvenanceEntry{
		Action:               "created",
		PublisherFingerprint: fp,
		PublisherName:        pubName,
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:            name,
		AgentVersion:         version,
		Timestamp:            now,
	}
	signProvenanceEntry(t, e, key)
	return e, fp, key
}

func writeLineageFile(t *testing.T, dir string, parentPolicy []byte, prov []ProvenanceEntry, tailFP, tailName string) {
	t.Helper()
	lf := LineageFile{
		Version: 1,
		Parent: LineageParent{
			AgentName:            "parent",
			AgentVersion:         "1.0.0",
			PublisherFingerprint: tailFP,
			PublisherName:        tailName,
			LockDigest:           digestString("parent-lock"),
			BundleDigest:         digestString("parent-bundle"),
			PolicyDigest:         digestString("parent-policy"),
			PolicyYAMLB64:        base64.StdEncoding.EncodeToString(parentPolicy),
			Provenance:           prov,
		},
		ForkedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(lf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAgentLock_ForkPack_TwoEntries(t *testing.T) {
	dir := t.TempDir()
	pubKS, pubID := publisherTestStore(t)
	parentPolicy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, parentPolicy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, parentPolicy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if len(lock.Provenance) != 2 {
		t.Fatalf("provenance len = %d, want 2", len(lock.Provenance))
	}
	if lock.Provenance[0].Action != "created" {
		t.Fatalf("entry[0] action = %q", lock.Provenance[0].Action)
	}
	if lock.Provenance[1].Action != "forked" {
		t.Fatalf("entry[1] action = %q", lock.Provenance[1].Action)
	}
	if lock.Provenance[1].PublisherFingerprint != pubID.Fingerprint {
		t.Fatal("fork entry signer mismatch")
	}
	if err := verifyEntrySignatureAndFingerprint(&lock.Provenance[1]); err != nil {
		t.Fatalf("entry[1] sig: %v", err)
	}
	lineageProv, _ := json.Marshal([]ProvenanceEntry{*e0})
	lockParentProv, _ := json.Marshal(lock.Provenance[:1])
	if string(lineageProv) != string(lockParentProv) {
		t.Fatalf("parent provenance not preserved:\nlineage %s\nlock %s", lineageProv, lockParentProv)
	}
	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if !report.Verified {
		t.Fatalf("VerifyProvenance failed: %v", report.Warnings)
	}
}

func TestCreateAgentLock_ForkPack_NoPolicyDelta(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if lock.Provenance[1].PolicyDelta != nil {
		t.Fatalf("expected nil policy_delta, got %+v", lock.Provenance[1].PolicyDelta)
	}
}

func TestCreateAgentLock_ForkPack_EgressAdded(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	parentPolicy := []byte(policyV1Empty)
	childPolicy := []byte("version: \"1.0\"\negress:\n  - domain: api.example.com\n    ports: [443]\n")
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, parentPolicy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, childPolicy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	delta := lock.Provenance[1].PolicyDelta
	if delta == nil || len(delta.EgressAdded) == 0 {
		t.Fatalf("expected egress added, got %+v", delta)
	}
	if !strings.Contains(delta.EgressAdded[0], "api.example.com") {
		t.Fatalf("egress_added = %v", delta.EgressAdded)
	}
}

func TestCreateAgentLock_ForkPack_CorruptParentSignature(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	e0.EntrySignature = "AAAA"
	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0}, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAgentLock_ForkPack_NoPublisherIdentity(t *testing.T) {
	dir := t.TempDir()
	policy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0}, tailFP, "alice")

	cfg := forkPackTestConfig(t, dir, nil, policy)
	cfg.PublisherKeyStore = nil
	_, err := CreateAgentLock(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "identity init") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAgentLock_ForkPack_ChainCap(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policy := []byte(policyV1Empty)
	e0, tailFP, parentKey := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	prov := make([]ProvenanceEntry, 0, 32)
	prov = append(prov, *e0)
	for i := 1; i < 32; i++ {
		e := &ProvenanceEntry{
			Action:               "forked",
			PublisherFingerprint: tailFP,
			PublisherName:        "alice",
			PublisherPublicKeyPEM: e0.PublisherPublicKeyPEM,
			AgentName:            "parent",
			AgentVersion:         "1.0.0",
			ParentLockDigest:     digestString("hop"),
			ParentBundleDigest:   digestString("bundle"),
			ParentPolicyDigest:   digestString("policy"),
			Timestamp:            time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		signProvenanceEntry(t, e, parentKey)
		prov = append(prov, *e)
	}
	writeLineageFile(t, dir, policy, prov, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err == nil {
		t.Fatal("expected cap error")
	}
	if !strings.Contains(err.Error(), errProvenanceChainCap) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateAgentLock_ForkPack_SelfFork(t *testing.T) {
	dir := t.TempDir()
	pubKS, pubID := publisherTestStore(t)
	policy := []byte(policyV1Empty)
	now := time.Now().UTC().Truncate(time.Second)
	e0 := &ProvenanceEntry{
		Action:               "created",
		PublisherFingerprint: pubID.Fingerprint,
		PublisherName:        pubID.Name,
		PublisherPublicKeyPEM: pubID.PublicKeyPEM,
		AgentName:            "parent",
		AgentVersion:         "1.0.0",
		Timestamp:            now,
	}
	entryCanonical, err := provenanceEntryCanonical(e0)
	if err != nil {
		t.Fatal(err)
	}
	entryDigest := sha256Sum(entryCanonical)
	sig, err := identity.SignAsPublisher(pubKS, entryDigest)
	if err != nil {
		t.Fatal(err)
	}
	e0.EntrySignature = base64.StdEncoding.EncodeToString(sig)
	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0}, pubID.Fingerprint, pubID.Name)

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if len(lock.Provenance) != 2 {
		t.Fatalf("len = %d", len(lock.Provenance))
	}
	if lock.Provenance[0].PublisherFingerprint != pubID.Fingerprint {
		t.Fatal("self-fork parent entry fingerprint mismatch")
	}
	if lock.Provenance[1].PublisherFingerprint != pubID.Fingerprint {
		t.Fatal("self-fork fork entry fingerprint mismatch")
	}
	report, _ := VerifyProvenance(lock)
	if !report.Verified {
		t.Fatalf("verify failed: %v", report.Warnings)
	}
}

func TestCreateAgentLock_ForkPack_ThreeHopChain(t *testing.T) {
	dir := t.TempDir()
	pubKS, pubID := publisherTestStore(t)
	policy := []byte(policyV1Empty)

	keyA, pubPEMA, fpA := newTestKeyPair(t)
	e0 := &ProvenanceEntry{
		Action: "created", PublisherFingerprint: fpA, PublisherName: "alice",
		PublisherPublicKeyPEM: string(pubPEMA), AgentName: "a", AgentVersion: "1",
		Timestamp: time.Now().UTC().Truncate(time.Second),
	}
	signProvenanceEntry(t, e0, keyA)

	keyB, pubPEMB, fpB := newTestKeyPair(t)
	e1 := &ProvenanceEntry{
		Action: "forked", PublisherFingerprint: fpB, PublisherName: "bob",
		PublisherPublicKeyPEM: string(pubPEMB), AgentName: "b", AgentVersion: "1",
		ParentLockDigest: digestString("l1"), ParentBundleDigest: digestString("b1"),
		ParentPolicyDigest: digestString("p1"),
		Timestamp:          time.Now().UTC().Add(time.Second),
	}
	signProvenanceEntry(t, e1, keyB)

	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0, *e1}, fpB, "bob")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	if len(lock.Provenance) != 3 {
		t.Fatalf("len = %d", len(lock.Provenance))
	}
	if lock.Provenance[2].PublisherFingerprint != pubID.Fingerprint {
		t.Fatal("tail signer should be forker")
	}
	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Verified {
		t.Fatalf("chain verify failed: %v", report.Warnings)
	}
}

func TestCreateAgentLock_ForkPack_TamperAfterPack(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policy))
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	lock.Provenance[1].AgentVersion = "tampered"
	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatal(err)
	}
	if report.Verified {
		t.Fatal("expected verification failure after tamper")
	}
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}