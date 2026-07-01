package audit

import (
	"bufio"
	"crypto/x509"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleAuditRecord(i int) AuditRecord {
	return AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "writer_checkpoint_test",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        map[string]interface{}{"i": i},
	}
}

func TestAuditWriterWithCheckpointsCreatesSignedCheckpoints(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	keyDER, pubKey, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	w, err := NewAuditWriterWithCheckpoints(auditPath, cpPath, 5, keyDER)
	if err != nil {
		t.Fatalf("NewAuditWriterWithCheckpoints: %v", err)
	}

	for i := 0; i < 10; i++ {
		if err := w.Append(sampleAuditRecord(i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	checkpoints, err := readCheckpoints(cpPath)
	if err != nil {
		t.Fatalf("readCheckpoints: %v", err)
	}
	if len(checkpoints) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(checkpoints))
	}
	if checkpoints[0].HeadAnchorSeq != 5 {
		t.Fatalf("checkpoint 1 head_anchor_seq = %d, want 5", checkpoints[0].HeadAnchorSeq)
	}
	if checkpoints[1].HeadAnchorSeq != 10 {
		t.Fatalf("checkpoint 2 head_anchor_seq = %d, want 10", checkpoints[1].HeadAnchorSeq)
	}
	for i, cp := range checkpoints {
		if !cp.VerifySignature(pubKey) {
			t.Fatalf("checkpoint %d signature invalid", i+1)
		}
	}
}

func TestAuditWriterWithCheckpointsTailTruncationDetected(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	keyDER, pubKey, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	w, err := NewAuditWriterWithCheckpoints(auditPath, cpPath, 5, keyDER)
	if err != nil {
		t.Fatalf("NewAuditWriterWithCheckpoints: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := w.Append(sampleAuditRecord(i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 10 {
		t.Fatalf("expected at least 10 audit lines, got %d", len(lines))
	}
	truncated := strings.Join(lines[:7], "\n") + "\n"
	if err := os.WriteFile(auditPath, []byte(truncated), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := VerifyAuditChain(auditPath, cpPath, pubKey)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Type == ErrTypeTailTruncation {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tail_truncation issue, got %v", result.Issues)
	}
}

func TestLoadOrGenerateCheckpointKeyPersists(t *testing.T) {
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "test-passphrase-for-checkpoint-key")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "audit-checkpoint-key.der")

	der1, pub1, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCheckpointKey (first): %v", err)
	}
	der2, pub2, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCheckpointKey (second): %v", err)
	}
	if string(der1) != string(der2) {
		t.Fatal("expected same key DER on reload")
	}
	pubDER1, err := x509.MarshalPKIXPublicKey(pub1)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey (first): %v", err)
	}
	pubDER2, err := x509.MarshalPKIXPublicKey(pub2)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey (second): %v", err)
	}
	if string(pubDER1) != string(pubDER2) {
		t.Fatal("expected same public key on reload")
	}
}

func TestLoadOrGenerateCheckpointKey_FilePermissions(t *testing.T) {
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "test-passphrase-for-checkpoint-key")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "audit-checkpoint-key.der")

	_, _, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCheckpointKey: %v", err)
	}

	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Stat checkpoint key file: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("checkpoint key file permissions = %#o, want 0600", fi.Mode().Perm())
	}
}

func TestCheckpointKeyIsEncryptedAtRest(t *testing.T) {
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "test-passphrase-encryption")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "audit-checkpoint-key.der")

	der, _, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateCheckpointKey: %v", err)
	}

	// Read the file — it must NOT be raw DER
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Raw DER starts with 0x30 (ASN.1 SEQUENCE). Encrypted format is JSON.
	if len(data) > 0 && data[0] == 0x30 {
		t.Fatal("checkpoint key file is raw DER (unencrypted); expected encrypted JSON envelope")
	}
	// Must contain JSON envelope fields
	if !strings.Contains(string(data), `"version"`) {
		t.Fatal("checkpoint key file is not a JSON envelope")
	}

	// The DER must still be valid and parseable
	_, err = PublicKeyFromCheckpointKeyDER(der)
	if err != nil {
		t.Fatalf("PublicKeyFromCheckpointKeyDER: %v", err)
	}
}

func TestCheckpointKeyWrongPassphraseFails(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "audit-checkpoint-key.der")

	// Generate with passphrase A
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "passphrase-A")
	_, _, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerate (A): %v", err)
	}

	// Load with passphrase B — must fail
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "passphrase-B")
	_, _, err = LoadOrGenerateCheckpointKey(keyPath)
	if err == nil {
		t.Fatal("expected error loading with wrong passphrase, got nil")
	}
}

func TestCheckpointKeyLegacyDERMigration(t *testing.T) {
	t.Setenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE", "migration-test-pass")
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "audit-checkpoint-key.der")

	// Write legacy raw DER
	der, pubKey, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}
	if err := os.WriteFile(keyPath, der, 0600); err != nil {
		t.Fatalf("WriteFile legacy DER: %v", err)
	}

	// Load — should succeed (legacy migration, logs warning)
	loadedDER, loadedPub, err := LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerate legacy: %v", err)
	}
	if string(loadedDER) != string(der) {
		t.Fatal("legacy DER mismatch")
	}
	// Public key should match
	pubDER1, _ := x509.MarshalPKIXPublicKey(pubKey)
	pubDER2, _ := x509.MarshalPKIXPublicKey(loadedPub)
	if string(pubDER1) != string(pubDER2) {
		t.Fatal("public key mismatch on legacy load")
	}
}

func TestNewAuditWriterBackwardCompatNoCheckpoints(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	if err := w.Append(sampleAuditRecord(0)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(auditPath + ".checkpoints"); !os.IsNotExist(err) {
		// no checkpoint file should exist
		if err == nil {
			t.Fatal("unexpected checkpoint file for plain NewAuditWriter")
		}
	}
	// Ensure audit file has one JSON line
	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		if scanner.Text() != "" {
			count++
			var rec AuditRecord
			if err := json.Unmarshal([]byte(scanner.Text()), &rec); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 audit record, got %d", count)
	}
}
