package audit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ========================================================================
// B3-T06 ADVERSARY TESTS FOR AUDIT EXPORT BUNDLE AND OFFLINE VERIFICATION
// ========================================================================

// setupBundleTest is a helper for adversary tests. Creates audit + checkpoints,
// exports to bundleDir, returns paths, keys, manifest, cleanup.
func setupBundleTest(t *testing.T, auditCount, checkpointCadence int) (
	auditPath, cpPath, bundleDir string,
	keyDER, pubKeyDER []byte,
	pubKey *ecdsa.PublicKey,
	manifest *BundleManifest,
	cleanup func(),
) {
	t.Helper()
	dir := t.TempDir()
	auditPath = filepath.Join(dir, "audit.jsonl")
	cpPath = filepath.Join(dir, "audit.checkpoints")

	// Generate key pair
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubKeyDER, err = x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	keyDER, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pubKey = &key.PublicKey

	// Write audit records
	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	m, err := NewCheckpointManager(cpPath, int64(checkpointCadence), keyDER)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	for i := 0; i < auditCount; i++ {
		rec := AuditRecord{
			Timestamp:      fmt.Sprintf("2025-01-01T00:00:%02dZ", i),
			EventType:      "adversary_bundle",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"n": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if checkpointCadence > 0 && (i+1)%checkpointCadence == 0 {
			seq, hash := w.CurrentHead()
			if _, err := m.CreateCheckpoint(seq, hash); err != nil {
				t.Fatalf("CreateCheckpoint at seq %d: %v", seq, err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close mgr: %v", err)
	}

	bundleDir = t.TempDir()
	signingKey := key
	manifest, err = ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     signingKey,
		PubKeyDER:      pubKeyDER,
	})
	if err != nil {
		t.Fatalf("ExportAuditBundle: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	cleanup = func() {}
	return
}

// TestAdversaryT06_BundleTamper attempts to modify a JSONL segment in the exported bundle.
func TestAdversaryT06_BundleTamper(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, _, cleanup := setupBundleTest(t, 10, 5)
	defer cleanup()

	auditBundlePath := filepath.Join(bundleDir, "audit.jsonl")
	data, err := os.ReadFile(auditBundlePath)
	if err != nil {
		t.Fatalf("ReadFile bundle audit: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 5 {
		t.Fatal("not enough lines to tamper")
	}
	var rec AuditRecord
	if err := json.Unmarshal([]byte(lines[3]), &rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	rec.Payload["n"] = 9999
	tampered, _ := json.Marshal(rec)
	lines[3] = string(tampered)
	if err := os.WriteFile(auditBundlePath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile tamper: %v", err)
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}

	tamperDetected := false
	for _, issue := range result.ChainResult.Issues {
		if issue.Type == ErrTypeTamperMiddle || strings.Contains(issue.Message, "record_hash mismatch") {
			tamperDetected = true
			t.Logf("PASS: bundle tamper detected: %s", issue.Message)
		}
	}
	if !tamperDetected && result.ManifestValid {
		t.Fatal("BREAK: bundle tamper undetected — chain integrity bypass (HIGH)")
	} else if tamperDetected {
		t.Log("PASS: bundle tamper correctly detected")
	}
}

// TestAdversaryT06_ManifestForgery modifies the signed manifest.
func TestAdversaryT06_ManifestForgery(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, manifest, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	manifest.AuditRecordCount = 999
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatalf("WriteFile manifest tamper: %v", err)
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}

	if result.ManifestValid {
		t.Fatal("BREAK: manifest forgery succeeded — signature/hash check bypass (HIGH)")
	}
	t.Log("PASS: manifest forgery correctly rejected (ManifestValid=false)")
}

// TestAdversaryT06_MissingSegment removes a required file from the bundle.
func TestAdversaryT06_MissingSegment(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, _, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	auditBundlePath := filepath.Join(bundleDir, "audit.jsonl")
	if err := os.Remove(auditBundlePath); err != nil {
		t.Fatalf("Remove audit segment: %v", err)
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	_, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err == nil {
		t.Fatal("BREAK: missing segment accepted without error (HIGH)")
	}
	t.Logf("PASS: missing segment correctly rejected with error: %v", err)
}

// TestAdversaryT06_WrongPublicKey verifies with wrong key/fingerprint.
func TestAdversaryT06_WrongPublicKey(t *testing.T) {
	_, _, bundleDir, _, _, _, _, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	wrongPubDER, _ := x509.MarshalPKIXPublicKey(&wrongKey.PublicKey)
	wrongFingerprint := PubKeyFingerprint(wrongPubDER)

	result, err := VerifyAuditBundle(bundleDir, wrongFingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}

	if result.FingerprintMatch {
		t.Fatal("BREAK: wrong public key fingerprint matched (MEDIUM)")
	}
	if result.ManifestValid {
		t.Fatal("BREAK: verification with wrong key reported ManifestValid=true (HIGH)")
	}
	t.Log("PASS: wrong public key correctly rejected (fingerprint mismatch)")
}

// TestAdversaryT06_CheckpointGap simulates checkpoint gap in bundle.
func TestAdversaryT06_CheckpointGap(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, _, cleanup := setupBundleTest(t, 10, 3)
	defer cleanup()

	cpBundlePath := filepath.Join(bundleDir, "audit.checkpoints")
	data, err := os.ReadFile(cpBundlePath)
	if err != nil {
		t.Fatalf("Read checkpoints: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 3 {
		// Remove the middle checkpoint (index 1) to create a seq gap
		lines = append(lines[:1], lines[2:]...)
		if err := os.WriteFile(cpBundlePath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			t.Fatalf("WriteFile cp tamper: %v", err)
		}
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}

	gapDetected := false
	for _, issue := range result.ChainResult.Issues {
		if issue.Type == ErrTypeMissingCheckpoint || strings.Contains(issue.Message, "gap") {
			gapDetected = true
			t.Logf("PASS: checkpoint gap detected: %s", issue.Message)
		}
	}
	if !gapDetected && len(result.ChainResult.Issues) == 0 {
		t.Fatal("BREAK: checkpoint gap undetected (HIGH)")
	}
}

// TestAdversaryT06_CleanWorkspaceIsolation verifies bundle has no reference to original chain paths.
func TestAdversaryT06_CleanWorkspaceIsolation(t *testing.T) {
	auditPath, cpPath, bundleDir, _, _, _, _, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestData, _ := os.ReadFile(manifestPath)
	if strings.Contains(string(manifestData), auditPath) || strings.Contains(string(manifestData), cpPath) {
		t.Fatal("BREAK: bundle contains original chain paths (LOW)")
	}
	t.Log("PASS: bundle is isolated from original workspace paths")
}

// TestAdversaryT06_BundleReplay tests replay acceptance (design gap per docs).
func TestAdversaryT06_BundleReplay(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, _, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle replay: %v", err)
	}
	if !result.ManifestValid {
		t.Log("PASS: replay rejected (unexpected)")
		return
	}
	t.Log("BREAK: bundle replay accepted (no timestamp/nonce enforcement) (MEDIUM)")
}

// TestAdversaryT06_TrustMetadataTampering modifies AIDs/public keys in the bundle manifest.
func TestAdversaryT06_TrustMetadataTampering(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, manifest, cleanup := setupBundleTest(t, 5, 5)
	defer cleanup()

	manifest.PubKeyFingerprint = "deadbeef00000000000000000000000000000000000000000000000000000000"
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		t.Fatalf("WriteFile metadata tamper: %v", err)
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}

	if result.FingerprintMatch {
		t.Fatal("BREAK: tampered pub key fingerprint accepted (HIGH)")
	}
	t.Log("PASS: trust metadata tampering correctly rejected")
}

// TestAdversaryT06_LargeBundle tests 1000+ records for panic/determinism.
func TestAdversaryT06_LargeBundle(t *testing.T) {
	_, _, bundleDir, _, pubKeyDER, _, manifest, cleanup := setupBundleTest(t, 1200, 100)
	defer cleanup()

	if manifest.AuditRecordCount != 1200 {
		t.Fatalf("expected 1200 records, got %d", manifest.AuditRecordCount)
	}

	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle large: %v", err)
	}
	if len(result.ChainResult.Issues) > 0 {
		t.Fatalf("large bundle had issues: %v", result.ChainResult.Issues)
	}
	t.Log("PASS: large bundle (1200 records) verified without panic or issues")
}

// TestAdversaryT06_EmptyBundle tests graceful failure on empty/invalid bundle.
func TestAdversaryT06_EmptyBundle(t *testing.T) {
	bundleDir := t.TempDir()

	_, err := VerifyAuditBundle(bundleDir, "", true)
	if err == nil {
		t.Fatal("BREAK: empty bundle did not error (HIGH)")
	}
	t.Logf("PASS: empty bundle correctly rejected with error: %v", err)
}

// End of adversary_t06_test.go — all vectors exercised.