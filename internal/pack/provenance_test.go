package pack

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

// newTestKeyPair generates a fresh ECDSA P-256 key pair for testing.
// Returns the private key, PEM-encoded public key, and fingerprint.
func newTestKeyPair(t *testing.T) (*ecdsa.PrivateKey, []byte, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	fp := hex.EncodeToString(func() []byte {
		der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
		sum := sha256.Sum256(der)
		return sum[:]
	}())
	return key, pubPEM, fp
}

// signProvenanceEntry signs a provenance entry with the given key, setting
// the EntrySignature field.
func signProvenanceEntry(t *testing.T, e *ProvenanceEntry, key *ecdsa.PrivateKey) {
	t.Helper()
	e.EntrySignature = ""
	canonical, err := provenanceEntryCanonical(e)
	if err != nil {
		t.Fatalf("provenanceEntryCanonical: %v", err)
	}
	digest := sha256.Sum256(canonical)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign entry: %v", err)
	}
	e.EntrySignature = base64.StdEncoding.EncodeToString(sig)
}

// TestVerifyProvenance_ValidCreatedOnly verifies a lock with a single
// "created" provenance entry passes verification.
func TestVerifyProvenance_ValidCreatedOnly(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	entry := &ProvenanceEntry{
		Action:               "created",
		PublisherFingerprint: fp,
		PublisherName:        "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:            "weather-agent",
		AgentVersion:         "1.0.0",
		ParentLockDigest:     "",
		ParentBundleDigest:   "",
		ParentPolicyDigest:   "",
		Timestamp:            now,
	}
	signProvenanceEntry(t, entry, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		AgentName:     "weather-agent",
		AgentVersion:  "1.0.0",
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
			SignedAt:      now,
		},
		Provenance: []ProvenanceEntry{*entry},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if !report.Verified {
		t.Fatalf("expected Verified=true, got false. Warnings: %v", report.Warnings)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(report.Entries))
	}
	if report.Entries[0].Action != "created" {
		t.Fatalf("entry[0].Action = %q, want \"created\"", report.Entries[0].Action)
	}
}

// TestVerifyProvenance_ValidCreatedAndForked verifies a lock with two
// provenance entries (created + forked) passes verification.
func TestVerifyProvenance_ValidCreatedAndForked(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	// entry[0]: created
	e0 := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "weather-agent",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      "",
		ParentBundleDigest:    "",
		ParentPolicyDigest:    "",
		Timestamp:             now,
	}
	signProvenanceEntry(t, e0, key)

	// entry[1]: forked
	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "weather-agent",
		AgentVersion:          "1.1.0",
		ParentLockDigest:      digestString("parent-lock"),
		ParentBundleDigest:    digestString("parent-bundle"),
		ParentPolicyDigest:    digestString("parent-policy"),
		Timestamp:             now.Add(time.Hour),
	}
	signProvenanceEntry(t, e1, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		AgentName:     "weather-agent",
		AgentVersion:  "1.1.0",
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
			SignedAt:      now.Add(time.Hour),
		},
		Provenance: []ProvenanceEntry{*e0, *e1},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if !report.Verified {
		t.Fatalf("expected Verified=true, got false. Warnings: %v", report.Warnings)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(report.Entries))
	}
}

// TestVerifyProvenance_ForgedSignatureMiddle fails when entry[1] has a
// signature that doesn't match its content.
func TestVerifyProvenance_ForgedSignatureMiddle(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	// Build valid 2-entry chain.
	e0 := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
	}
	signProvenanceEntry(t, e0, key)

	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.1.0",
		ParentLockDigest:      digestString("lock"),
		Timestamp:             now.Add(time.Hour),
	}
	signProvenanceEntry(t, e1, key)

	// Tamper with e1's content but keep the original signature.
	e1.AgentName = "tampered-agent"

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*e0, *e1},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for forged signature, got true")
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "entry[1]") && strings.Contains(w, "signature verification failed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected signature verification failure warning, got: %v", report.Warnings)
	}
}

// TestVerifyProvenance_Entry0NotCreated fails when the first entry has
// an action other than "created".
func TestVerifyProvenance_Entry0NotCreated(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	entry := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		ParentLockDigest:      digestString("lock"),
		Timestamp:             now,
	}
	signProvenanceEntry(t, entry, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*entry},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for entry[0] not being 'created'")
	}
}

// TestVerifyProvenance_ForkWithEmptyParentDigest fails when a fork entry
// has an empty parent_lock_digest.
func TestVerifyProvenance_ForkWithEmptyParentDigest(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	e0 := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
	}
	signProvenanceEntry(t, e0, key)

	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.1.0",
		ParentLockDigest:      "", // empty — should fail
		ParentBundleDigest:    "",
		ParentPolicyDigest:    "",
		Timestamp:             now.Add(time.Hour),
	}
	signProvenanceEntry(t, e1, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*e0, *e1},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for fork with empty parent_lock_digest")
	}
}

// TestVerifyProvenance_LastSignerNotPublisher fails when the last provenance
// entry's signer fingerprint doesn't match the lock's publisher fingerprint.
func TestVerifyProvenance_LastSignerNotPublisher(t *testing.T) {
	key0, pubPEM0, fp0 := newTestKeyPair(t)
	key1, pubPEM1, fp1 := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	// entry[0] signed by key0
	e0 := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp0,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM0),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
	}
	signProvenanceEntry(t, e0, key0)

	// entry[1] signed by key1
	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp1,
		PublisherName:         "bob",
		PublisherPublicKeyPEM: string(pubPEM1),
		AgentName:             "agent",
		AgentVersion:          "1.1.0",
		ParentLockDigest:      digestString("lock"),
		Timestamp:             now.Add(time.Hour),
	}
	signProvenanceEntry(t, e1, key1)

	// Publisher is key0 (alice), but last entry is signed by key1 (bob).
	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp0,
			PublicKeyPEM:  string(pubPEM0),
		},
		Provenance: []ProvenanceEntry{*e0, *e1},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false when last signer != lock publisher")
	}
}

// TestVerifyProvenance_OutOfOrderTimestampsWarns verifies that out-of-order
// timestamps produce a warning but do not cause verification failure.
func TestVerifyProvenance_OutOfOrderTimestampsWarns(t *testing.T) {
	key, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	e0 := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now.Add(time.Hour), // later
	}
	signProvenanceEntry(t, e0, key)

	e1 := &ProvenanceEntry{
		Action:                "forked",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.1.0",
		ParentLockDigest:      digestString("lock"),
		Timestamp:             now, // earlier — out of order
	}
	signProvenanceEntry(t, e1, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*e0, *e1},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if !report.Verified {
		t.Fatalf("expected Verified=true (timestamps out of order is a warning, not a failure)")
	}
	hasWarning := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "clock skew") {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Fatalf("expected clock skew warning, got warnings: %v", report.Warnings)
	}
}

// TestVerifyProvenance_FingerprintPEMMismatch fails when an entry's
// publisher_fingerprint doesn't match its embedded PEM.
func TestVerifyProvenance_FingerprintPEMMismatch(t *testing.T) {
	key, pubPEM, _ := newTestKeyPair(t)
	_, _, fakeFp := newTestKeyPair(t) // different key's fingerprint
	now := time.Now().UTC().Truncate(time.Second)

	entry := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fakeFp, // mismatched fingerprint
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM), // this key doesn't match fakeFp
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
	}
	signProvenanceEntry(t, entry, key)

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fakeFp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*entry},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for fingerprint/PEM mismatch")
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "fingerprint") && strings.Contains(w, "does not match") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fingerprint mismatch warning, got: %v", report.Warnings)
	}
}

// TestFormatProvenance_OneEntry verifies the terminal rendering of a
// 1-entry provenance chain.
func TestFormatProvenance_OneEntry(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	report := &ProvenanceReport{
		Verified:       true,
		ChainSemantics: chainSemantics,
		Entries: []ProvenanceEntrySummary{
			{
				Index:                0,
				Action:               "created",
				PublisherName:        "parvez",
				PublisherFingerprint: "a1b2c3d4e5f6789012345678abcdef0123456789abcdef0123456789abcdef01",
				AgentName:            "weather-agent",
				AgentVersion:         "1.0.0",
				Timestamp:            now,
			},
		},
	}

	got := FormatProvenance(report)
	// Check key substrings.
	if !strings.Contains(got, "Provenance:") {
		t.Fatalf("missing Provenance header: %s", got)
	}
	if !strings.Contains(got, "created") {
		t.Fatalf("missing 'created': %s", got)
	}
	if !strings.Contains(got, "weather-agent 1.0.0") {
		t.Fatalf("missing agent/version: %s", got)
	}
	if !strings.Contains(got, "parvez") {
		t.Fatalf("missing publisher name: %s", got)
	}
	if !strings.Contains(got, "a1b2c3d4") {
		t.Fatalf("missing short fingerprint: %s", got)
	}
	if !strings.Contains(got, "2026-07-01") {
		t.Fatalf("missing date: %s", got)
	}
}

// TestFormatProvenance_ThreeEntries verifies the terminal rendering of a
// 3-entry provenance chain with policy deltas.
func TestFormatProvenance_ThreeEntries(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	report := &ProvenanceReport{
		Verified:       true,
		ChainSemantics: chainSemantics,
		Entries: []ProvenanceEntrySummary{
			{
				Index:                0,
				Action:               "created",
				PublisherName:        "parvez",
				PublisherFingerprint: "a1b2c3d4e5f6789012345678abcdef0123456789abcdef0123456789abcdef01",
				AgentName:            "weather-agent",
				AgentVersion:         "1.0.0",
				Timestamp:            now,
			},
			{
				Index:                1,
				Action:               "forked",
				PublisherName:        "maria",
				PublisherFingerprint: "9f8e7d6c5b4a3210fedcba0987654321fedcba0987654321fedcba0987654321",
				AgentName:            "weather-agent",
				AgentVersion:         "1.1.0",
				Timestamp:            now.Add(24 * time.Hour),
				PolicyDelta: &PolicyDelta{
					EgressAdded: []string{"api.slack.com:443"},
				},
			},
			{
				Index:                2,
				Action:               "forked",
				PublisherName:        "lucia",
				PublisherFingerprint: "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
				AgentName:            "weather-agent",
				AgentVersion:         "2.0.0",
				Timestamp:            now.Add(48 * time.Hour),
				PolicyDelta: &PolicyDelta{
					EgressAdded:      []string{"api.openai.com:443"},
					CredentialsAdded: []string{"openai-key"},
				},
			},
		},
	}

	got := FormatProvenance(report)

	// Entry 1
	if !strings.Contains(got, "1. created") {
		t.Fatalf("missing entry 1 'created': %s", got)
	}
	if !strings.Contains(got, "parvez") {
		t.Fatalf("missing parvez: %s", got)
	}

	// Entry 2
	if !strings.Contains(got, "2. forked") {
		t.Fatalf("missing entry 2 'forked': %s", got)
	}
	if !strings.Contains(got, "maria") {
		t.Fatalf("missing maria: %s", got)
	}
	if !strings.Contains(got, "+egress api.slack.com:443") {
		t.Fatalf("missing slack egress delta: %s", got)
	}

	// Entry 3
	if !strings.Contains(got, "3. forked") {
		t.Fatalf("missing entry 3 'forked': %s", got)
	}
	if !strings.Contains(got, "lucia") {
		t.Fatalf("missing lucia: %s", got)
	}
	if !strings.Contains(got, "+egress api.openai.com:443") {
		t.Fatalf("missing openai egress delta: %s", got)
	}
	if !strings.Contains(got, "+credentials openai-key") {
		t.Fatalf("missing openai-key credential delta: %s", got)
	}
}

// TestVerifyProvenance_PublishedLockEmptyProvenance fails when a lock has
// a publisher block but an empty provenance chain.
func TestVerifyProvenance_PublishedLockEmptyProvenance(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:        "alice",
			Fingerprint: "abc",
		},
		Provenance: nil,
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for published lock with empty provenance")
	}
}

// TestVerifyProvenance_LocalOnlyEmptyProvenance passes when a lock has no
// publisher and an empty provenance chain (local-only pack).
func TestVerifyProvenance_LocalOnlyEmptyProvenance(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: 2,
		AgentName:     "agent",
		AgentVersion:  "1.0.0",
		// No publisher, no provenance.
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if !report.Verified {
		t.Fatalf("expected Verified=true for local-only lock with empty provenance")
	}
	if len(report.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(report.Entries))
	}
}

// TestVerifyProvenance_ChainSemantics verifies the ChainSemantics field
// is always populated.
func TestVerifyProvenance_ChainSemantics(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: 2,
		AgentName:     "agent",
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.ChainSemantics != chainSemantics {
		t.Fatalf("ChainSemantics = %q, want %q", report.ChainSemantics, chainSemantics)
	}
}

// TestVerifyProvenance_NilLock returns an error for nil lock.
func TestVerifyProvenance_NilLock(t *testing.T) {
	_, err := VerifyProvenance(nil)
	if err == nil {
		t.Fatal("expected error for nil lock")
	}
}

// TestVerifyProvenance_EmptyEntrySignature fails when a provenance entry
// has an empty entry_signature.
func TestVerifyProvenance_EmptyEntrySignature(t *testing.T) {
	_, pubPEM, fp := newTestKeyPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	entry := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  fp,
		PublisherName:         "alice",
		PublisherPublicKeyPEM: string(pubPEM),
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
		EntrySignature:        "", // empty — not signed
	}

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:          "alice",
			Fingerprint:   fp,
			PublicKeyPEM:  string(pubPEM),
		},
		Provenance: []ProvenanceEntry{*entry},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for empty entry_signature")
	}
}

// TestVerifyProvenance_BadPublicKeyPEM fails when an entry has an invalid
// public key PEM.
func TestVerifyProvenance_BadPublicKeyPEM(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	entry := &ProvenanceEntry{
		Action:                "created",
		PublisherFingerprint:  "abc",
		PublisherName:         "alice",
		PublisherPublicKeyPEM: "not-valid-pem",
		AgentName:             "agent",
		AgentVersion:          "1.0.0",
		Timestamp:             now,
		EntrySignature:        "bogus",
	}

	lock := &AgentLock{
		SchemaVersion: 2,
		Publisher: &PublisherInfo{
			Name:        "alice",
			Fingerprint: "abc",
		},
		Provenance: []ProvenanceEntry{*entry},
	}

	report, err := VerifyProvenance(lock)
	if err != nil {
		t.Fatalf("VerifyProvenance: %v", err)
	}
	if report.Verified {
		t.Fatal("expected Verified=false for bad public key PEM")
	}
}

// TestFormatProvenance_NilReport returns (none).
func TestFormatProvenance_NilReport(t *testing.T) {
	if got := FormatProvenance(nil); got != "Provenance: (none)" {
		t.Fatalf("FormatProvenance(nil) = %q, want \"Provenance: (none)\"", got)
	}
}

// TestFormatProvenance_EmptyEntries returns (none).
func TestFormatProvenance_EmptyEntries(t *testing.T) {
	report := &ProvenanceReport{}
	if got := FormatProvenance(report); got != "Provenance: (none)" {
		t.Fatalf("FormatProvenance(empty) = %q, want \"Provenance: (none)\"", got)
	}
}