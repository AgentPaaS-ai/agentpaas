//go:build adversary

package pack

import (
	"crypto/ecdsa"
	"strings"
	"testing"
	"time"
)

// setupLockWithCapabilities creates a signed lock with capabilities and a
// publisher block. Returns the lock and the publisher private key so the
// caller can verify signatures or tamper.
func setupLockWithCapabilities(t *testing.T) (*AgentLock, *ecdsa.PrivateKey) {
	t.Helper()

	key, pubPEM := testKeyPair(t)

	// Build a lock with capabilities.
	lock := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            "cap-agent",
		AgentVersion:         "0.2.0",
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
		SBOMReferrer:         "oci://agentpaas-cap-test:latest#sbom",
		SignatureReferrer:    "cosign://agentpaas-cap-test:latest",
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: testTime(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testTime(),
		Capabilities: []DeclaredCapability{
			{ID: "text_generation", Description: "Generates text from prompts"},
			{ID: "tool_calling", Description: "Calls external tools"},
		},
		// Publisher block - set before signing so lockfile signature covers it.
		Publisher: &PublisherInfo{
			Name:         "test-publisher",
			Fingerprint:  "", // set below
			PublicKeyPEM: "", // set below
			SignedAt:     time.Now(),
		},
	}

	// Generate publisher key and set identity before signing the lockfile.
	pubKey, pubPEM2 := testKeyPair(t)
	lock.Publisher.Fingerprint = PublicKeyFingerprint(&pubKey.PublicKey)
	lock.Publisher.PublicKeyPEM = string(pubPEM2)

	// Sign lockfile (AID signature). Must be after publisher block is fully populated
	// because publisher fields are included in the canonical map.
	signLockWithKeyForTest(t, lock, key)

	// Sign publisher.
	if err := SignPublisherWithKey(lock, pubKey); err != nil {
		t.Fatalf("SignPublisherWithKey: %v", err)
	}

	return lock, pubKey
}

// TestAdversaryB31_CapabilitiesInCanonicalMap verifies that capabilities
// are included in the canonical map, so tampering after signing is detected.
func TestAdversaryB31_CapabilitiesInCanonicalMap(t *testing.T) {
	lock, _ := setupLockWithCapabilities(t)

	// Control: both signatures verify on the unmodified lock.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("unmodified lock: VerifyLockfileSignature failed: %v", err)
	}
	if err := VerifyPublisherSignature(lock); err != nil {
		t.Fatalf("unmodified lock: VerifyPublisherSignature failed: %v", err)
	}

	// Save original capabilities for restore.
	orig := make([]DeclaredCapability, len(lock.Capabilities))
	copy(orig, lock.Capabilities)

	// Test 1: mutate capability description - lockfile signature fails.
	lock.Capabilities[0].Description = "TAMPERED description"
	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature accepted lock with tampered capability description")
	}

	// Restore.
	copy(lock.Capabilities, orig)

	// Re-verify baseline after restore.
	if err := VerifyLockfileSignature(lock); err != nil {
		t.Fatalf("after restore: VerifyLockfileSignature failed: %v", err)
	}
	if err := VerifyPublisherSignature(lock); err != nil {
		t.Fatalf("after restore: VerifyPublisherSignature failed: %v", err)
	}

	// Test 2: mutate capability ID - publisher signature fails.
	lock.Capabilities[0].ID = "tampered_id"
	if err := VerifyPublisherSignature(lock); err == nil {
		t.Fatal("VerifyPublisherSignature accepted lock with tampered capability ID")
	}

	// Restore.
	copy(lock.Capabilities, orig)

	// Test 3: add a capability not in the original - both fail.
	lock.Capabilities = append(lock.Capabilities, DeclaredCapability{
		ID:          "injected_cap",
		Description: "Injected after signing",
	})
	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature accepted lock with injected capability")
	}
	if err := VerifyPublisherSignature(lock); err == nil {
		t.Fatal("VerifyPublisherSignature accepted lock with injected capability")
	}

	// Restore.
	lock.Capabilities = make([]DeclaredCapability, len(orig))
	copy(lock.Capabilities, orig)

	// Test 4: remove a capability - both fail.
	lock.Capabilities = lock.Capabilities[:1]
	if err := VerifyLockfileSignature(lock); err == nil {
		t.Fatal("VerifyLockfileSignature accepted lock with removed capability")
	}
	if err := VerifyPublisherSignature(lock); err == nil {
		t.Fatal("VerifyPublisherSignature accepted lock with removed capability")
	}
}

// TestAdversaryB31_CapabilitiesCanonicalShape verifies the canonical map
// structure for capabilities: sorted by id, stable field order (id before
// description matching struct tags), and present in the canonical JSON.
func TestAdversaryB31_CapabilitiesCanonicalShape(t *testing.T) {
	lock, _ := setupLockWithCapabilities(t)

	// Build canonical JSON directly.
	canonical, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	canonicalStr := string(canonical)

	// Capabilities should appear in the canonical JSON.
	if !strings.Contains(canonicalStr, "\"capabilities\"") {
		t.Fatal("canonical JSON missing capabilities array")
	}
	if !strings.Contains(canonicalStr, "\"id\":\"text_generation\"") {
		t.Fatal("canonical JSON missing text_generation capability")
	}
	if !strings.Contains(canonicalStr, "\"id\":\"tool_calling\"") {
		t.Fatal("canonical JSON missing tool_calling capability")
	}
	if !strings.Contains(canonicalStr, "\"description\":\"Generates text from prompts\"") {
		t.Fatal("canonical JSON missing description for text_generation")
	}

	// Verify deterministic canonical JSON: two encodings should match.
	canonical2, err := canonicalJSON(lock)
	if err != nil {
		t.Fatalf("canonicalJSON (2nd): %v", err)
	}
	if string(canonical) != string(canonical2) {
		t.Fatal("capabilities canonical JSON is not deterministic")
	}

	// Verify that full canonical includes signatures.
	fullCanonical, err := canonicalJSONFull(lock)
	if err != nil {
		t.Fatalf("canonicalJSONFull: %v", err)
	}
	if !strings.Contains(string(fullCanonical), "\"lockfile_signature\"") {
		t.Fatal("full canonical JSON missing lockfile_signature")
	}
	if !strings.Contains(string(fullCanonical), "\"publisher_signature\"") {
		t.Fatal("full canonical JSON missing publisher_signature")
	}
}

// TestAdversaryB31_UnsortedCapabilitiesDeterminism verifies that sorting
// produces deterministic output regardless of input order.
func TestAdversaryB31_UnsortedCapabilitiesDeterminism(t *testing.T) {
	key, pubPEM := testKeyPair(t)

	lock1 := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            "sort-agent",
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
		SBOMReferrer:         "oci://sort-test:latest#sbom",
		SignatureReferrer:    "cosign://sort-test:latest",
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: testTime(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testTime(),
		Capabilities: []DeclaredCapability{
			{ID: "zzz_last", Description: "Should sort to end"},
			{ID: "aaa_first", Description: "Should sort to beginning"},
			{ID: "middle", Description: "Middle"},
		},
	}

	// Sign lockfile (before fix, this uses unsorted capabilities in canonical form).
	signLockWithKeyForTest(t, lock1, key)

	// Build canonical JSON - after fix, this sorts capabilities.
	canonical1, err := canonicalJSON(lock1)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}

	// Now build a second lock with capabilities already sorted.
	lock2 := &AgentLock{
		SchemaVersion:        LockSchemaVersion,
		AgentName:            "sort-agent",
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
		SBOMReferrer:         "oci://sort-test:latest#sbom",
		SignatureReferrer:    "cosign://sort-test:latest",
		Reproducibility: ReproducibilityMeta{
			SourceDateEpoch: testTime(),
			BaseImagePinned: true,
			DepsLocked:      true,
			TarOrder:        "sorted",
		},
		CreatedAt: testTime(),
		Capabilities: []DeclaredCapability{
			{ID: "aaa_first", Description: "Should sort to beginning"},
			{ID: "middle", Description: "Middle"},
			{ID: "zzz_last", Description: "Should sort to end"},
		},
	}
	// Must use the same key so fingerprint/AID match.
	lock2.PackageAID = lock1.PackageAID
	lock2.PublicKeyFingerprint = lock1.PublicKeyFingerprint
	signLockWithKeyForTest(t, lock2, key)

	canonical2, err := canonicalJSON(lock2)
	if err != nil {
		t.Fatalf("canonicalJSON (sorted): %v", err)
	}

	// After fix: sorting in lockCanonicalMap means both produce identical canonical JSON.
	// Without the fix, order affects canonical form and they would differ.
	if string(canonical1) != string(canonical2) {
		t.Fatalf("lockCanonicalMap produced different output for differently ordered capabilities\ninput1: %s\ninput2: %s", canonical1, canonical2)
	}
}
