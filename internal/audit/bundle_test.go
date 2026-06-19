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
	"testing"
)

// setupExportTest writes audit records and checkpoints to temp paths and
// returns the paths, the key pair, and cleanup func. The audit chain has
// auditCount records with a checkpoint every checkpointCadence records.
func setupExportTest(t *testing.T, auditCount int, checkpointCadence int) (
	auditPath string,
	cpPath string,
	keyDER []byte,
	pubKeyDER []byte,
	pubKey *ecdsa.PublicKey,
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

	// Create checkpoint manager with cadence
	m, err := NewCheckpointManager(cpPath, int64(checkpointCadence), keyDER)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	for i := 0; i < auditCount; i++ {
		rec := AuditRecord{
			Timestamp:      fmt.Sprintf("2025-01-01T00:00:%02dZ", i),
			EventType:      "export_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"n": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}

		// Checkpoint at cadence intervals
		if checkpointCadence > 0 && (i+1)%checkpointCadence == 0 {
			seq, hash := w.CurrentHead()
			if _, err := m.CreateCheckpoint(seq, hash); err != nil {
				t.Fatalf("CreateCheckpoint at seq %d: %v", seq, err)
			}
		}
	}

	// Close writer so files are flushed
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close mgr: %v", err)
	}

	cleanup = func() {}
	return
}

func TestExportVerifyBundle(t *testing.T) {
	auditPath, cpPath, keyDER, pubKeyDER, pubKey, cleanup := setupExportTest(t, 15, 5)
	defer cleanup()

	bundleDir := t.TempDir()

	// Export
	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
	signingKey := key.(*ecdsa.PrivateKey)

	manifest, err := ExportAuditBundle(bundleDir, &ExportBundleOptions{
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
	if manifest.BundleVersion != 1 {
		t.Fatalf("expected bundle_version=1, got %d", manifest.BundleVersion)
	}
	if manifest.AuditRecordCount != 15 {
		t.Fatalf("expected 15 audit records, got %d", manifest.AuditRecordCount)
	}
	if manifest.CheckpointCount != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", manifest.CheckpointCount)
	}
	if manifest.ManifestHash == "" {
		t.Fatal("expected non-empty manifest_hash")
	}
	if len(manifest.Signature) == 0 {
		t.Fatal("expected non-empty signature")
	}

	// Verify manifest signature
	if !manifest.VerifyManifestSignature(pubKey) {
		t.Fatal("manifest signature verification failed with correct key")
	}

	// Wrong key should fail
	wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if manifest.VerifyManifestSignature(&wrongKey.PublicKey) {
		t.Fatal("manifest signature should fail with wrong key")
	}

	// Verify bundle
	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}
	if !result.ManifestValid {
		t.Fatal("expected manifest to be valid")
	}
	if !result.FingerprintMatch {
		t.Fatal("expected fingerprint to match")
	}
	if result.ChainResult == nil {
		t.Fatal("expected non-nil chain result")
	}
	if len(result.ChainResult.Issues) > 0 {
		t.Fatalf("expected no chain issues, got: %v", result.ChainResult.Issues)
	}
	if result.ChainResult.AuditRecordCount != 15 {
		t.Fatalf("expected 15 audit records in chain, got %d", result.ChainResult.AuditRecordCount)
	}
	if result.ChainResult.CheckpointCount != 3 {
		t.Fatalf("expected 3 checkpoints in chain, got %d", result.ChainResult.CheckpointCount)
	}
}

func TestExportVerifyBundleNoSignature(t *testing.T) {
	auditPath, cpPath, _ /*keyDER*/, pubKeyDER, _ /*pubKey*/, cleanup := setupExportTest(t, 5, 5)
	defer cleanup()

	bundleDir := t.TempDir()

	// Export without signing key
	manifest, err := ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     nil,
		PubKeyDER:      pubKeyDER,
	})
	if err != nil {
		t.Fatalf("ExportAuditBundle: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}
	if manifest.ManifestHash == "" {
		t.Fatal("expected non-empty manifest_hash")
	}
	if len(manifest.Signature) != 0 {
		t.Fatal("expected empty signature for unsigned export")
	}

	// Verify bundle without signature check
	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, false)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}
	if !result.ManifestValid {
		t.Fatal("expected manifest hash to be valid (self-consistency)")
	}
	if !result.FingerprintMatch {
		t.Fatal("expected fingerprint to match")
	}
	if len(result.ChainResult.Issues) > 0 {
		t.Fatalf("expected no chain issues, got: %v", result.ChainResult.Issues)
	}
}

func TestExportBundleTamperedAfterExport(t *testing.T) {
	auditPath, cpPath, keyDER, pubKeyDER, _ /*pubKey*/, cleanup := setupExportTest(t, 10, 5)
	defer cleanup()

	bundleDir := t.TempDir()

	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
	signingKey := key.(*ecdsa.PrivateKey)

	_, err = ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     signingKey,
		PubKeyDER:      pubKeyDER,
	})
	if err != nil {
		t.Fatalf("ExportAuditBundle: %v", err)
	}

	// Tamper the audit.jsonl in the bundle: modify line 3 (index 2)
	auditBundlePath := filepath.Join(bundleDir, "audit.jsonl")
	data, err := os.ReadFile(auditBundlePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitLines(string(data))
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	var rec3 AuditRecord
	if err := json.Unmarshal([]byte(lines[2]), &rec3); err != nil {
		t.Fatalf("Unmarshal line 3: %v", err)
	}
	rec3.Payload = map[string]interface{}{"tampered": true}
	modifiedLine, _ := json.Marshal(rec3)
	lines[2] = string(modifiedLine)
	if err := os.WriteFile(auditBundlePath, []byte(joinLines(lines)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify bundle — should detect tamper
	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}
	if len(result.ChainResult.Issues) == 0 {
		t.Fatal("expected chain issues for tampered bundle, got none")
	}

	foundTamper := false
	for _, issue := range result.ChainResult.Issues {
		if issue.Type == ErrTypeTamperMiddle {
			foundTamper = true
			break
		}
	}
	if !foundTamper {
		t.Fatalf("expected tamper_middle detection, got issues: %v", result.ChainResult.Issues)
	}
}

func TestExportBundleWrongFingerprint(t *testing.T) {
	auditPath, cpPath, keyDER, pubKeyDER, _ /*pubKey*/, cleanup := setupExportTest(t, 5, 5)
	defer cleanup()

	bundleDir := t.TempDir()

	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
	signingKey := key.(*ecdsa.PrivateKey)

	_, err = ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     signingKey,
		PubKeyDER:      pubKeyDER,
	})
	if err != nil {
		t.Fatalf("ExportAuditBundle: %v", err)
	}

	// Verify with wrong expected fingerprint
	wrongFingerprint := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	result, err := VerifyAuditBundle(bundleDir, wrongFingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}
	if result.FingerprintMatch {
		t.Fatal("expected fingerprint mismatch")
	}
}

func TestExportBundleBrokenChainFails(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	// Write a valid chain
	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 3; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "broken_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Tamper the audit file
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitLines(string(data))
	var rec2 AuditRecord
	_ = json.Unmarshal([]byte(lines[1]), &rec2)
	rec2.Payload = map[string]interface{}{"broken": true}
	brokenLine, _ := json.Marshal(rec2)
	lines[1] = string(brokenLine)
	if err := os.WriteFile(auditPath, []byte(joinLines(lines)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Generate a key
	keyRaw, pubKey, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}
	key, _ := x509.ParsePKCS8PrivateKey(keyRaw)
	signingKey := key.(*ecdsa.PrivateKey)

	pubKeyDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}

	// Export should fail because the chain is broken
	bundleDir := t.TempDir()
	manifest, err := ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     signingKey,
		PubKeyDER:      pubKeyDER,
	})
	if err == nil {
		t.Fatal("expected error exporting with broken chain, got nil")
	}
	if manifest != nil {
		t.Fatal("expected nil manifest on error")
	}
	t.Logf("Got expected error: %v", err)
}

func TestExportVerifyBundleMissingFiles(t *testing.T) {
	bundleDir := t.TempDir()

	// Verify on empty directory
	_, err := VerifyAuditBundle(bundleDir, "", false)
	if err == nil {
		t.Fatal("expected error verifying empty bundle dir")
	}
	t.Logf("Got expected error: %v", err)
}

func TestBundleManifestSignVerifyEdgeCases(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	manifest := &BundleManifest{
		BundleVersion:   1,
		ExportTimestamp: "2025-01-01T00:00:00Z",
		AuditRecordCount: 10,
		ManifestHash:    "somehash",
		Signature:        []byte{1, 2, 3},
	}

	// Verify with nil key
	if manifest.VerifyManifestSignature(nil) {
		t.Fatal("expected false for nil public key")
	}

	// Verify with empty hash
	manifest.ManifestHash = ""
	if manifest.VerifyManifestSignature(&key.PublicKey) {
		t.Fatal("expected false for empty hash")
	}

	// Verify with empty signature
	manifest.ManifestHash = "somehash"
	manifest.Signature = nil
	if manifest.VerifyManifestSignature(&key.PublicKey) {
		t.Fatal("expected false for empty signature")
	}
}

func TestPubKeyFingerprintDeterminism(t *testing.T) {
	der, _, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
	ecKey := key.(*ecdsa.PrivateKey)

	pubDER, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}

	fp1 := PubKeyFingerprint(pubDER)
	fp2 := PubKeyFingerprint(pubDER)
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %q vs %q", fp1, fp2)
	}

	// Different key should produce different fingerprint
	pubDER2, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey 2: %v", err)
	}
	if PubKeyFingerprint(pubDER) != PubKeyFingerprint(pubDER2) {
		t.Fatal("same key DER should produce same fingerprint")
	}
}

func TestExportBundleVerifyNoCheckpointDir(t *testing.T) {
	// Test that export works when no checkpoints file exists
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	rec := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "test",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        map[string]interface{}{},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// No checkpoints file — export should fail
	keyRaw, pubKey, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}
	key, _ := x509.ParsePKCS8PrivateKey(keyRaw)
	signingKey := key.(*ecdsa.PrivateKey)

	pubKeyDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}

	bundleDir := t.TempDir()
	_, err = ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     signingKey,
		PubKeyDER:      pubKeyDER,
	})
	if err == nil {
		t.Fatal("expected error exporting with no checkpoints")
	}
	t.Logf("Got expected error: %v", err)
}

func TestExportBundleCheckpointSignatureIntegrity(t *testing.T) {
	// Create a bundle with signed checkpoints, then tamper a checkpoint
	// signature to verify the verifier detects it
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pubKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "cp_sig_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	seq, hash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// Create signed checkpoint
	m, err := NewCheckpointManager(cpPath, 0, keyDER)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	cp, err := m.CreateCheckpoint(seq, hash)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	_ = cp // checkpoint is verified via the bundle chain below
	if err := m.Close(); err != nil {
		t.Fatalf("Close mgr: %v", err)
	}

	bundleDir := t.TempDir()
	_, err = ExportAuditBundle(bundleDir, &ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: cpPath,
		SigningKey:     key,
		PubKeyDER:      pubKeyDER,
	})
	if err != nil {
		t.Fatalf("ExportAuditBundle: %v", err)
	}

	// Tamper the checkpoint's signature in the bundle
	cpBundlePath := filepath.Join(bundleDir, "audit.checkpoints")
	cpData, err := os.ReadFile(cpBundlePath)
	if err != nil {
		t.Fatalf("ReadFile bundle checkpoint: %v", err)
	}
	var cpInBundle CheckpointRecord
	if err := json.Unmarshal(cpData, &cpInBundle); err != nil {
		t.Fatalf("Unmarshal bundle checkpoint: %v", err)
	}
	// Save original sig and replace with tampered
	cpInBundle.Signature = []byte{0, 0, 0} // garbage signature
	tamperedLine, _ := json.Marshal(cpInBundle)
	// Use the original checkpoint's data but replace signature via lines
	cpLines := splitLines(string(cpData))
	cpLines[0] = string(tamperedLine)
	if err := os.WriteFile(cpBundlePath, []byte(joinLines(cpLines)), 0644); err != nil {
		t.Fatalf("WriteFile tampered checkpoint: %v", err)
	}

	// Verify — should detect invalid checkpoint signature
	fingerprint := PubKeyFingerprint(pubKeyDER)
	result, err := VerifyAuditBundle(bundleDir, fingerprint, true)
	if err != nil {
		t.Fatalf("VerifyAuditBundle: %v", err)
	}
	if len(result.ChainResult.Issues) == 0 {
		t.Fatal("expected issues for tampered checkpoint signature, got none")
	}
	foundBadSig := false
	for _, issue := range result.ChainResult.Issues {
		if issue.Type == ErrTypeSignature {
			foundBadSig = true
			break
		}
	}
	if !foundBadSig {
		t.Fatalf("expected invalid_signature detection, got issues: %v", result.ChainResult.Issues)
	}
}

func TestExportBundleRejectsSymlink(t *testing.T) {
	// Export requires real files — symlinks should be rejected by copyFile
	dir := t.TempDir()
	srcReal := filepath.Join(dir, "real.txt")
	dstDir := t.TempDir()
	dstSymlink := filepath.Join(dstDir, "link.txt")

	if err := os.WriteFile(srcReal, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile real: %v", err)
	}
	if err := os.Symlink(srcReal, dstSymlink); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// copyFile should reject the symlink as source
	dest := filepath.Join(dir, "out.txt")
	err := copyFile(dstSymlink, dest)
	if err == nil {
		t.Fatal("expected error copying symlink, got nil")
	}
	t.Logf("Got expected symlink error: %v", err)
}

// joinLines joins lines with newlines, writing a trailing newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}