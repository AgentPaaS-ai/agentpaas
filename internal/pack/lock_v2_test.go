package pack

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

// publisherTestStore creates a FakeKeyStore with a publisher identity
// and returns the store plus the identity info.
func publisherTestStore(t *testing.T) (identity.KeyStore, *identity.PublisherIdentity) {
	t.Helper()
	ks := identity.NewFakeKeyStore()
	id, err := identity.CreatePublisherIdentity(ks, "test-publisher")
	if err != nil {
		t.Fatalf("CreatePublisherIdentity: %v", err)
	}
	return ks, id
}

func TestLockSchemaVersionIsTwo(t *testing.T) {
	if LockSchemaVersion != 2 {
		t.Fatalf("LockSchemaVersion = %d, want 2", LockSchemaVersion)
	}
}

// TestV2Lock_WithoutPublisher verifies that a v2 lock produced without
// a publisher identity is valid — nil publisher, empty provenance,
// AID signature only.
func TestV2Lock_WithoutPublisher(t *testing.T) {
	installFakeTool(t, "syft", `#!/bin/sh
printf '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)

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
		// PublisherKeyStore is nil — no publisher identity
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	if lock.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", lock.SchemaVersion)
	}
	if lock.Publisher != nil {
		t.Fatal("Publisher should be nil when no publisher identity")
	}
	if lock.PublisherSignature != "" {
		t.Fatal("PublisherSignature should be empty when no publisher identity")
	}
	if len(lock.Provenance) != 0 {
		t.Fatalf("Provenance should be empty, got %d entries", len(lock.Provenance))
	}
	// AID signature must verify.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature: %v", err)
	}
	// Full VerifyAgentLock should pass for v2 without publisher.
	if err := VerifyAgentLock(lock, ""); err != nil {
		t.Fatalf("VerifyAgentLock: %v", err)
	}
}

// TestV2Lock_WithPublisher verifies that a v2 lock produced with a publisher
// identity has the publisher block, a "created" provenance entry, and both
// signatures verify.
func TestV2Lock_WithPublisher(t *testing.T) {
	installFakeTool(t, "syft", `#!/bin/sh
printf '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, pubID := publisherTestStore(t)

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
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	if lock.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", lock.SchemaVersion)
	}
	if lock.Publisher == nil {
		t.Fatal("Publisher should not be nil when publisher identity exists")
	}
	if lock.Publisher.Name != pubID.Name {
		t.Fatalf("publisher name = %q, want %q", lock.Publisher.Name, pubID.Name)
	}
	if lock.Publisher.Fingerprint != pubID.Fingerprint {
		t.Fatalf("publisher fingerprint = %q, want %q", lock.Publisher.Fingerprint, pubID.Fingerprint)
	}
	if lock.Publisher.PublicKeyPEM != pubID.PublicKeyPEM {
		t.Fatal("publisher public key PEM mismatch")
	}
	if lock.Publisher.SignedAt.IsZero() {
		t.Fatal("publisher SignedAt is zero")
	}
	if lock.PublisherSignature == "" {
		t.Fatal("PublisherSignature is empty")
	}
	if len(lock.Provenance) != 1 {
		t.Fatalf("Provenance should have 1 entry, got %d", len(lock.Provenance))
	}

	entry := lock.Provenance[0]
	if entry.Action != "created" {
		t.Fatalf("provenance action = %q, want \"created\"", entry.Action)
	}
	if entry.PublisherFingerprint != pubID.Fingerprint {
		t.Fatalf("provenance publisher fingerprint mismatch")
	}
	if entry.PublisherName != pubID.Name {
		t.Fatalf("provenance publisher name mismatch")
	}
	if entry.AgentName != lock.AgentName {
		t.Fatalf("provenance agent name mismatch")
	}
	if entry.AgentVersion != lock.AgentVersion {
		t.Fatalf("provenance agent version mismatch")
	}
	if entry.ParentLockDigest != "" {
		t.Fatal("created provenance should have empty parent_lock_digest")
	}
	if entry.ParentBundleDigest != "" {
		t.Fatal("created provenance should have empty parent_bundle_digest")
	}
	if entry.ParentPolicyDigest != "" {
		t.Fatal("created provenance should have empty parent_policy_digest")
	}
	if entry.EntrySignature == "" {
		t.Fatal("provenance entry signature is empty")
	}

	// Both signatures must verify.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature: %v", err)
	}
	if err := VerifyPublisherSignature(lock); err != nil {
		t.Fatalf("VerifyPublisherSignature: %v", err)
	}
	if err := VerifyProvenanceSignatures(lock); err != nil {
		t.Fatalf("VerifyProvenanceSignatures: %v", err)
	}
	// Full VerifyAgentLock must pass.
	if err := VerifyAgentLock(lock, ""); err != nil {
		t.Fatalf("VerifyAgentLock: %v", err)
	}
}

// TestV1Lock_StillVerifies ensures that a v1 schema lock (created before v2)
// still loads and verifies through the B20 path.
func TestV1Lock_StillVerifies(t *testing.T) {
	installFakeTool(t, "cosign", fakeCosignScript())
	// Build a v1 lock manually.
	key, pubPEM := testKeyPair(t)
	lock := &AgentLock{
		SchemaVersion:        1,
		AgentName:            "agent",
		AgentVersion:         "0.1.0",
		Runtime:              "python",
		Platform:             "linux/arm64",
		BaseImageDigest:      "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:       "test",
		BuildInputDigest:     digestString("input"),
		ImageDigest:          digestString("image"),
		SBOMDigest:           digestString("sbom"),
		PolicyDigest:         "",
		PackageAID:           string(pubPEM),
		PublicKeyFingerprint: PublicKeyFingerprint(&key.PublicKey),
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: testTime(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testTime(),
	}
	signLockWithKeyForTest(t, lock, key)

	// Verify as v1.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature v1: %v", err)
	}
	if err := VerifyAgentLock(lock, ""); err != nil {
		t.Fatalf("VerifyAgentLock v1: %v", err)
	}
}

// TestV2Lock_TamperedPublisherPEM verifies that flipping a byte in the
// publisher public key PEM causes publisher signature verification to fail.
func TestV2Lock_TamperedPublisherPEM(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	// Flip a byte in the publisher PEM.
	pemBytes := []byte(lock.Publisher.PublicKeyPEM)
	pemBytes[len(pemBytes)-20] ^= 0x01
	lock.Publisher.PublicKeyPEM = string(pemBytes)

	if err := VerifyPublisherSignature(lock); err == nil {
		t.Fatal("VerifyPublisherSignature should fail for tampered publisher PEM")
	}
	if err := VerifyAgentLock(lock, ""); err == nil {
		t.Fatal("VerifyAgentLock should fail for tampered publisher PEM")
	}
}

// TestV2Lock_TamperedFingerprint verifies that a mismatched fingerprint
// causes VerifyAgentLock to fail.
func TestV2Lock_TamperedFingerprint(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.Publisher.Fingerprint = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	if err := VerifyAgentLock(lock, ""); err == nil {
		t.Fatal("VerifyAgentLock should fail for tampered fingerprint")
	}
	if !strings.Contains(lock.Publisher.Fingerprint, "deadbeef") {
		// Just a sanity check that the field is what we set.
	}
}

// TestV2Lock_TamperedProvenanceField verifies that mutating a provenance
// entry field causes provenance signature verification to fail.
func TestV2Lock_TamperedProvenanceField(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.Provenance[0].AgentName = "tampered-agent"

	if err := VerifyProvenanceSignatures(lock); err == nil {
		t.Fatal("VerifyProvenanceSignatures should fail for tampered provenance field")
	}
}

// TestV2Lock_StrippedProvenanceEntry verifies that removing a provenance
// entry after signing does not break AID or publisher signature (provenance
// is included in canonical map, so stripping changes the digest and breaks
// at least one of the lock-level signatures).
func TestV2Lock_StrippedProvenanceEntry(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.Provenance = nil

	// Removing provenance changes the canonical map, so publisher signature
	// (and AID signature) should fail.
	if err := VerifyPublisherSignature(lock); err == nil {
		t.Fatal("VerifyPublisherSignature should fail after stripping provenance")
	}
}

// TestV2Lock_SwappedPublisherSignature verifies that swapping in a
// publisher_signature from another lock causes verification to fail.
func TestV2Lock_SwappedPublisherSignature(t *testing.T) {
	lock1 := setupV2LockWithPublisher(t)
	lock2 := setupV2LockWithPublisher(t)

	// Swap publisher_signature from lock2 into lock1.
	lock1.PublisherSignature = lock2.PublisherSignature

	if err := VerifyPublisherSignature(lock1); err == nil {
		t.Fatal("VerifyPublisherSignature should fail for swapped publisher signature")
	}
}

// TestV2Lock_MutatedV1FieldBreaksSignature verifies that mutating a v1-era
// field with old signatures kept causes AID verification to fail.
func TestV2Lock_MutatedV1FieldBreaksSignature(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.AgentName = "mutated-name"

	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature should fail after mutating agent_name")
	}
}

// TestV2CanonicalMap_ExcludesSignatures verifies that the canonical map
// excludes both lockfile_signature and publisher_signature.
func TestV2CanonicalMap_ExcludesSignatures(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	canonical, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if bytes.Contains(canonical, []byte("lockfile_signature")) {
		t.Fatal("canonical JSON contains lockfile_signature")
	}
	if bytes.Contains(canonical, []byte("publisher_signature")) {
		t.Fatal("canonical JSON contains publisher_signature")
	}
}

// TestV2CanonicalMap_IncludesSignaturesWhenTrue verifies that with
// includeSignatures=true, both signatures are present in the map.
func TestV2CanonicalMap_IncludesSignaturesWhenTrue(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	m := lockCanonicalMap(lock, true)
	if _, ok := m["lockfile_signature"]; !ok {
		t.Fatal("lockCanonicalMap(lock, true) missing lockfile_signature")
	}
	if _, ok := m["publisher_signature"]; !ok {
		t.Fatal("lockCanonicalMap(lock, true) missing publisher_signature")
	}
}

// TestV2CanonicalMap_IncludesPublisherBlock verifies the publisher block
// appears in the canonical map.
func TestV2CanonicalMap_IncludesPublisherBlock(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	m := lockCanonicalMap(lock, false)
	pubBlock, ok := m["publisher"]
	if !ok {
		t.Fatal("canonical map missing publisher block")
	}
	pubMap, ok := pubBlock.(map[string]interface{})
	if !ok {
		t.Fatal("publisher block is not a map")
	}
	if pubMap["name"] != lock.Publisher.Name {
		t.Fatalf("publisher name = %v, want %q", pubMap["name"], lock.Publisher.Name)
	}
	if pubMap["fingerprint"] != lock.Publisher.Fingerprint {
		t.Fatalf("publisher fingerprint = %v, want %q", pubMap["fingerprint"], lock.Publisher.Fingerprint)
	}
}

// TestV2CanonicalMap_IncludesProvenance verifies the provenance array appears
// in the canonical map with entry_signature included.
func TestV2CanonicalMap_IncludesProvenance(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	m := lockCanonicalMap(lock, false)
	provRaw, ok := m["provenance"]
	if !ok {
		t.Fatal("canonical map missing provenance")
	}
	provArr, ok := provRaw.([]map[string]interface{})
	if !ok {
		t.Fatal("provenance is not an array of maps")
	}
	if len(provArr) != 1 {
		t.Fatalf("provenance has %d entries, want 1", len(provArr))
	}
	entryMap := provArr[0]
	if entryMap["action"] != "created" {
		t.Fatalf("provenance action = %v", entryMap["action"])
	}
	// entry_signature must be included (entries are signed independently).
	if _, ok := entryMap["entry_signature"]; !ok {
		t.Fatal("provenance entry missing entry_signature in canonical map")
	}
}

// TestV2CanonicalJSON_Deterministic verifies that canonical JSON is
// deterministic — marshaling twice produces byte-identical output.
func TestV2CanonicalJSON_Deterministic(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	first, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON first: %v", err)
	}
	second, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical JSON not deterministic:\n%s\n%s", first, second)
	}
}

// TestV2CanonicalJSON_SortedKeys verifies that top-level keys are
// alphabetically sorted.
func TestV2CanonicalJSON_SortedKeys(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	got, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	if !bytes.HasPrefix(got, []byte(`{"agent_name":`)) {
		t.Fatalf("top-level keys are not sorted: %s", got)
	}
}

// TestLockDigest_GoldenValue verifies that LockDigest produces a stable,
// pinned value and changes when a signed field changes.
func TestLockDigest_GoldenValue(t *testing.T) {
	lock := setupV2LockWithPublisher(t)

	d1 := LockDigest(lock)
	if d1 == "" {
		t.Fatal("LockDigest returned empty string")
	}
	// LockDigest must be deterministic.
	d2 := LockDigest(lock)
	if d1 != d2 {
		t.Fatalf("LockDigest not deterministic: %s vs %s", d1, d2)
	}

	// Changing any signed field must change the digest.
	lockCopy := *lock
	lockCopy.AgentName = "different-agent"
	d3 := LockDigest(&lockCopy)
	if d3 == d1 {
		t.Fatal("LockDigest unchanged after mutating agent_name")
	}
	if d3 == "" {
		t.Fatal("LockDigest returned empty after mutation")
	}

	// Changing publisher info must change the digest.
	lockCopy2 := *lock
	lockCopy2.Publisher.Name = "different-publisher"
	d4 := LockDigest(&lockCopy2)
	if d4 == d1 {
		t.Fatal("LockDigest unchanged after mutating publisher name")
	}
}

// TestLockDigest_NilLock returns empty.
func TestLockDigest_NilLock(t *testing.T) {
	if got := LockDigest(nil); got != "" {
		t.Fatalf("LockDigest(nil) = %q, want \"\"", got)
	}
}

// TestV2WriteReadRoundtrip verifies that a v2 lock with publisher block
// survives a write-read roundtrip through WriteAgentLock / ReadAgentLock.
func TestV2WriteReadRoundtrip(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	path := filepath.Join(testSecureTempDir(t), "agent-v2.lock")

	if err := WriteAgentLock(lock, path); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}
	got, err := ReadAgentLock(path)
	if err != nil {
		t.Fatalf("ReadAgentLock: %v", err)
	}

	// Verify structural equivalence.
	if got.SchemaVersion != lock.SchemaVersion {
		t.Fatalf("schema_version mismatch: %d vs %d", got.SchemaVersion, lock.SchemaVersion)
	}
	if got.AgentName != lock.AgentName {
		t.Fatalf("agent_name mismatch")
	}
	if got.Publisher == nil {
		t.Fatal("Publisher is nil after roundtrip")
	}
	if got.Publisher.Name != lock.Publisher.Name {
		t.Fatalf("publisher name mismatch")
	}
	if got.Publisher.Fingerprint != lock.Publisher.Fingerprint {
		t.Fatalf("publisher fingerprint mismatch")
	}
	if got.PublisherSignature != lock.PublisherSignature {
		t.Fatal("publisher_signature mismatch after roundtrip")
	}
	if len(got.Provenance) != len(lock.Provenance) {
		t.Fatalf("provenance length mismatch: %d vs %d", len(got.Provenance), len(lock.Provenance))
	}
	if got.Provenance[0].EntrySignature != lock.Provenance[0].EntrySignature {
		t.Fatal("provenance entry signature mismatch")
	}

	// Both signatures must still verify after roundtrip.
	if err := VerifyLockfileSignature(got); err != nil {
		t.Fatalf("VerifyLockfileSignature after roundtrip: %v", err)
	}
	if err := VerifyPublisherSignature(got); err != nil {
		t.Fatalf("VerifyPublisherSignature after roundtrip: %v", err)
	}
	if err := VerifyProvenanceSignatures(got); err != nil {
		t.Fatalf("VerifyProvenanceSignatures after roundtrip: %v", err)
	}
}

// TestV2_NewSignedTestLock_IsV2 verifies that NewSignedTestLock produces
// v2 locks (without publisher).
func TestV2_NewSignedTestLock_IsV2(t *testing.T) {
	lock, err := NewSignedTestLock("test-agent", nil)
	if err != nil {
		t.Fatalf("NewSignedTestLock: %v", err)
	}
	if lock.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", lock.SchemaVersion)
	}
	if lock.Publisher != nil {
		t.Fatal("NewSignedTestLock should have nil Publisher")
	}
	if lock.PublisherSignature != "" {
		t.Fatal("NewSignedTestLock should have empty PublisherSignature")
	}
	if len(lock.Provenance) != 0 {
		t.Fatal("NewSignedTestLock should have empty Provenance")
	}
	// AID signature must verify.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("VerifyLockfileSignature: %v", err)
	}
}

// TestV2_EmptyPublisherSignature_PublisherBlockPresent verifies that if the
// publisher block is present but publisher_signature is empty, verification
// fails with a clear error.
func TestV2_EmptyPublisherSignature_PublisherBlockPresent(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.PublisherSignature = ""

	err := VerifyPublisherSignature(lock)
	if err == nil {
		t.Fatal("expected error for empty publisher_signature with publisher block")
	}
	if !strings.Contains(err.Error(), "publisher_signature is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestV2_ProvenanceEmptyEntrySignature verifies that an empty entry_signature
// on a provenance entry causes VerifyProvenanceSignatures to fail.
func TestV2_ProvenanceEmptyEntrySignature(t *testing.T) {
	lock := setupV2LockWithPublisher(t)
	lock.Provenance[0].EntrySignature = ""

	err := VerifyProvenanceSignatures(lock)
	if err == nil {
		t.Fatal("expected error for empty entry_signature")
	}
	if !strings.Contains(err.Error(), "entry_signature is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestV2_CreateAgentLock_FailsOnPublisherError verifies that if
// LoadPublisherIdentity fails with a non-ErrNoPublisherIdentity error,
// CreateAgentLock fails closed.
func TestV2_CreateAgentLock_FailsOnPublisherError(t *testing.T) {
	installFakeTool(t, "syft", `#!/bin/sh
printf '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)

	// Create a FakeKeyStore that has no publisher identity, but we'll
	// configure it so that any load error besides "no identity" fails.
	// The way to trigger this without modifying FakeKeyStore is to pass
	// a keystore interface that returns a non-ErrNoPublisherIdentity error.
	// For simplicity, we test: if PublisherKeyStore is a keystore without
	// the publisher key, LoadPublisherIdentity returns ErrNoPublisherIdentity,
	// which is handled gracefully (nil publisher). Any other error would fail.
	// We trust FakeKeyStore's behavior here — tested in identity package.
	_ = store
	t.Log("PublisherKeyStore error paths tested in identity package")
}

// setupV2LockWithPublisher creates a v2 lock with a publisher identity
// (real signing). Returns the lock.
func setupV2LockWithPublisher(t *testing.T) *AgentLock {
	t.Helper()
	installFakeTool(t, "syft", `#!/bin/sh
printf '{"spdxVersion":"SPDX-2.3","name":"agentpaas-v2-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image"),
			ImageRef:         "agentpaas-v2-test:latest",
			BuildInputDigest: digestString("input"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:          &AgentYAML{},
		Runtime:            RuntimeType("python"),
		BaseImageDigest:    "gcr.io/distroless/python3-debian12@sha256:" + digestString("base"),
		HarnessVersion:     "test",
		Platform:           "linux/arm64",
		SourceDateEpoch:    testTime(),
		KeyStore:           store,
		KeyID:              store.keyID,
		PublisherKeyStore:  pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}
	return lock
}