package pack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// flipOneBitInB64Signature decodes a base64 entry signature, flips one bit, re-encodes.
func flipOneBitInB64Signature(sigB64 string) string {
	raw, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(raw) == 0 {
		return "AAAA"
	}
	raw[0] ^= 0x01
	return base64.StdEncoding.EncodeToString(raw)
}

func adversaryBuildParentChain(t *testing.T, n int) ([]ProvenanceEntry, string, string) {
	t.Helper()
	if n < 1 {
		t.Fatal("n must be >= 1")
	}
	e0, tailFP, parentKey := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	prov := []ProvenanceEntry{*e0}
	for i := 1; i < n; i++ {
		e := &ProvenanceEntry{
			Action:                "forked",
			PublisherFingerprint:  tailFP,
			PublisherName:         "alice",
			PublisherPublicKeyPEM: e0.PublisherPublicKeyPEM,
			AgentName:             "parent",
			AgentVersion:          "1.0.0",
			ParentLockDigest:      digestString("hop"),
			ParentBundleDigest:    digestString("bundle"),
			ParentPolicyDigest:    digestString("policy"),
			Timestamp:             time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		signProvenanceEntry(t, e, parentKey)
		prov = append(prov, *e)
	}
	return prov, tailFP, "alice"
}

func TestAdversary_CorruptParentSignature_OneBitFlip(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	e0.EntrySignature = flipOneBitInB64Signature(e0.EntrySignature)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack succeeded with bit-flipped parent entry signature")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("expected fail-closed lineage corrupt, got: %v", err)
	}
}

func TestAdversary_Entry0NotCreated_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	key, pubPEM, fp := newTestKeyPair(t)
	e0 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "parent",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      digestString("x"),
		Timestamp:             time.Now().UTC(),
	}
	signProvenanceEntry(t, e0, key)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, fp, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack accepted lineage with entry[0] action forked")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_ForkedEntryMissingParentLockDigest_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, parentKey := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  tailFP,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: e0.PublisherPublicKeyPEM,
		AgentName:             "child",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      "",
		ParentBundleDigest:    digestString("b"),
		ParentPolicyDigest:    digestString("p"),
		Timestamp:             time.Now().UTC().Add(time.Second),
	}
	signProvenanceEntry(t, e1, parentKey)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0, *e1}, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack accepted forked entry with empty parent_lock_digest")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_MiddleEntryBadSignature_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, parentKey := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  tailFP,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: e0.PublisherPublicKeyPEM,
		AgentName:             "mid",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      digestString("l"),
		ParentBundleDigest:    digestString("b"),
		ParentPolicyDigest:    digestString("p"),
		Timestamp:             time.Now().UTC().Add(time.Second),
	}
	signProvenanceEntry(t, e1, parentKey)
	e1.EntrySignature = flipOneBitInB64Signature(e1.EntrySignature)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0, *e1}, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack accepted middle entry with corrupt signature")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_TailSignerMismatch_ParentFingerprintSwap_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	_, _, fpB := newTestKeyPair(t)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, fpB, "attacker")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack accepted lineage with parent.publisher_fingerprint != last entry signer")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
	_ = tailFP
}

func TestAdversary_ChainCap_31ParentPlus1_Succeeds(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	prov, tailFP, _ := adversaryBuildParentChain(t, 31)
	writeLineageFile(t, dir, policyYAML, prov, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatalf("expected 31+1=32 chain to succeed, got: %v", err)
	}
	if len(lock.Provenance) != 32 {
		t.Fatalf("provenance len = %d, want 32", len(lock.Provenance))
	}
	report, err := VerifyProvenance(lock)
	if err != nil || !report.Verified {
		t.Fatalf("VerifyProvenance failed: err=%v verified=%v warnings=%v", err, report.Verified, report.Warnings)
	}
}

func TestAdversary_ChainCap_32ParentPlus1_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	prov, tailFP, _ := adversaryBuildParentChain(t, 32)
	writeLineageFile(t, dir, policyYAML, prov, tailFP, "alice")

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: pack accepted 33-entry chain (32 parent + 1 new)")
	}
	if !strings.Contains(err.Error(), errProvenanceChainCap) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_ParentProvenanceBytePreserved_JSONEqual(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	parentPolicy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	lineageProv := []ProvenanceEntry{*e0}
	writeLineageFile(t, dir, parentPolicy, lineageProv, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, parentPolicy))
	if err != nil {
		t.Fatal(err)
	}
	lineageJSON, _ := json.Marshal(lineageProv)
	lockParentJSON, _ := json.Marshal(lock.Provenance[:len(lineageProv)])
	if string(lineageJSON) != string(lockParentJSON) {
		t.Fatalf("BREAK: parent provenance not byte-preserved:\nlineage %s\nlock    %s", lineageJSON, lockParentJSON)
	}
}

func TestAdversary_PolicyDelta_EgressRemoved(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	parentPolicy := []byte("version: \"1\"\negress:\n  - domain: api.example.com\n    ports: [443]\n")
	childPolicy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, parentPolicy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, childPolicy))
	if err != nil {
		t.Fatal(err)
	}
	delta := lock.Provenance[1].PolicyDelta
	if delta == nil || len(delta.EgressRemoved) == 0 {
		t.Fatalf("expected egress removed in policy_delta, got %+v", delta)
	}
	want, err := policy.ComputeDelta(parentPolicy, childPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if want == nil || len(want.EgressRemoved) == 0 {
		t.Fatal("sanity: ComputeDelta should report egress removed")
	}
	if !strings.Contains(delta.EgressRemoved[0], "api.example.com") {
		t.Fatalf("egress_removed = %v", delta.EgressRemoved)
	}
}

func TestAdversary_PolicyDelta_UsesLineagePolicyBytes_NotLock(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	lineageParentPolicy := []byte("version: \"1\"\negress:\n  - domain: lineage-only.example.com\n    ports: [443]\n")
	packPolicy := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, lineageParentPolicy, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, packPolicy))
	if err != nil {
		t.Fatal(err)
	}
	delta := lock.Provenance[1].PolicyDelta
	if delta == nil || len(delta.EgressRemoved) == 0 {
		t.Fatal("BREAK: delta should reflect lineage policy_yaml_b64 parent egress removal")
	}
	if !strings.Contains(delta.EgressRemoved[0], "lineage-only.example.com") {
		t.Fatalf("delta should be from lineage policy, got %+v", delta)
	}
}

func TestAdversary_ForkPack_NoPublisherIdentity_FailClosed(t *testing.T) {
	dir := t.TempDir()
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	cfg := forkPackTestConfig(t, dir, nil, policyYAML)
	cfg.PublisherKeyStore = nil
	_, err := CreateAgentLock(context.Background(), cfg)
	if err == nil {
		t.Fatal("BREAK: fork pack without publisher identity succeeded")
	}
	if !strings.Contains(err.Error(), "identity init") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_NewForkEntrySignature_Verifies(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyEntrySignatureAndFingerprint(&lock.Provenance[1]); err != nil {
		t.Fatalf("BREAK: new fork entry signature must verify: %v", err)
	}
	lock.Provenance[1].AgentVersion = "tampered-after-pack"
	if err := verifyEntrySignatureAndFingerprint(&lock.Provenance[1]); err == nil {
		t.Fatal("BREAK: tampered fork entry still verifies")
	}
}

func TestAdversary_LineageOversized_Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lineageFileName)
	padding := strings.Repeat("a", int(maxLineageFileBytes))
	raw := `{"version":1,"parent":{"agent_name":"a","agent_version":"1","publisher_fingerprint":"","publisher_name":"","lock_digest":"","bundle_digest":"","policy_digest":"","policy_yaml_b64":"","provenance":[]},"forked_at":"2020-01-01T00:00:00Z","pad":"` + padding + `"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("BREAK: oversized lineage not rejected: %v", err)
	}
}

func TestAdversary_LineageUnknownField_Rejected(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":1,"parent":{"agent_name":"a","agent_version":"1","publisher_fingerprint":"","publisher_name":"","lock_digest":"","bundle_digest":"","policy_digest":"","policy_yaml_b64":"","provenance":[]},"forked_at":"2020-01-01T00:00:00Z","evil":true}`
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil {
		t.Fatal("BREAK: unknown field in lineage accepted")
	}
}

func TestAdversary_LineageVersionString_Rejected(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":"1","parent":{"agent_name":"a","agent_version":"1","publisher_fingerprint":"","publisher_name":"","lock_digest":"","bundle_digest":"","policy_digest":"","policy_yaml_b64":"","provenance":[]},"forked_at":"2020-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil {
		t.Fatal("BREAK: string version accepted")
	}
}

func TestAdversary_EmptyParentProvenance_Rejected(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	lf := LineageFile{
		Version: 1,
		Parent: LineageParent{
			AgentName:            "parent",
			AgentVersion:         "1.0.0",
			PublisherFingerprint: "abc",
			PublisherName:        "alice",
			LockDigest:           digestString("lock"),
			BundleDigest:         digestString("bundle"),
			PolicyDigest:         digestString("policy"),
			PolicyYAMLB64:        base64.StdEncoding.EncodeToString(policyYAML),
			Provenance:           []ProvenanceEntry{},
		},
		ForkedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(lf)
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err == nil {
		t.Fatal("BREAK: empty parent provenance accepted")
	}
	if !strings.Contains(err.Error(), errLineageCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func TestAdversary_NoLineage_PacksAsCreatedRegression(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Provenance) != 1 || lock.Provenance[0].Action != "created" {
		t.Fatalf("expected single created entry, got %+v", lock.Provenance)
	}
}

func TestAdversary_LineageDeletedAfterFork_PacksAsOriginal(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	if err := os.Remove(filepath.Join(dir, lineageFileName)); err != nil {
		t.Fatal(err)
	}

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatalf("pack after lineage delete should succeed as original: %v", err)
	}
	if len(lock.Provenance) != 1 || lock.Provenance[0].Action != "created" {
		t.Fatalf("BREAK: expected created-only pack, got %d entries", len(lock.Provenance))
	}
}

func TestAdversary_SecondForkFromPackedLock_ThreeEntryChainVerifies(t *testing.T) {
	dir := t.TempDir()
	pubKS, pubID := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock1, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(lock1.Provenance) != 2 {
		t.Fatalf("first pack len = %d", len(lock1.Provenance))
	}

	lf2 := LineageFile{
		Version: 1,
		Parent: LineageParent{
			AgentName:            lock1.AgentName,
			AgentVersion:         lock1.AgentVersion,
			PublisherFingerprint: pubID.Fingerprint,
			PublisherName:        pubID.Name,
			LockDigest:           digestString("lock2"),
			BundleDigest:         digestString("bundle2"),
			PolicyDigest:         digestString("policy2"),
			PolicyYAMLB64:        base64.StdEncoding.EncodeToString(policyYAML),
			Provenance:           append([]ProvenanceEntry(nil), lock1.Provenance...),
		},
		ForkedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(lf2)
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	lock2, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatalf("second fork pack: %v", err)
	}
	if len(lock2.Provenance) != 3 {
		t.Fatalf("second pack provenance len = %d, want 3", len(lock2.Provenance))
	}
	report, err := VerifyProvenance(lock2)
	if err != nil || !report.Verified {
		t.Fatalf("3-hop chain verify failed: err=%v verified=%v %v", err, report.Verified, report.Warnings)
	}
}

func TestAdversary_ParentDecreasingTimestamps_WarnOnVerify(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, parentKey := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	tLater := time.Now().UTC().Add(2 * time.Hour)
	tEarlier := time.Now().UTC().Add(-2 * time.Hour)
	e0.Timestamp = tLater
	signProvenanceEntry(t, e0, parentKey)
	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  tailFP,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: e0.PublisherPublicKeyPEM,
		AgentName:             "mid",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      digestString("l"),
		ParentBundleDigest:    digestString("b"),
		ParentPolicyDigest:    digestString("p"),
		Timestamp:             tEarlier,
	}
	signProvenanceEntry(t, e1, parentKey)
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0, *e1}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatal(err)
	}
	warned := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "timestamp") && strings.Contains(w, "clock skew") {
			warned = true
			break
		}
	}
	if !warned {
		t.Fatalf("expected clock skew warning on decreasing parent timestamps, warnings=%v", report.Warnings)
	}
}

func TestAdversary_NewForkTimestamp_NotBeforeParentTail(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	parentTail := lock.Provenance[0].Timestamp
	newFork := lock.Provenance[1].Timestamp
	if newFork.Before(parentTail) {
		t.Fatalf("BREAK: new fork timestamp %v before parent tail %v", newFork, parentTail)
	}
}

func TestAdversary_TamperLockProvenanceAfterPack_VerifyFails(t *testing.T) {
	dir := t.TempDir()
	pubKS, _ := publisherTestStore(t)
	policyYAML := []byte(policyV1Empty)
	e0, tailFP, _ := signedCreatedEntry(t, "parent", "1.0.0", "alice")
	writeLineageFile(t, dir, policyYAML, []ProvenanceEntry{*e0}, tailFP, "alice")

	lock, err := CreateAgentLock(context.Background(), forkPackTestConfig(t, dir, pubKS, policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	lock.Provenance[1].AgentVersion = "evil"
	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatal(err)
	}
	if report.Verified {
		t.Fatal("BREAK: VerifyProvenance passed after tampering fork entry")
	}
}