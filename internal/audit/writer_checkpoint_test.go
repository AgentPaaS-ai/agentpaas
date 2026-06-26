package audit

import (
	"bufio"
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
	if pub1.X.Cmp(pub2.X) != 0 || pub1.Y.Cmp(pub2.Y) != 0 {
		t.Fatal("expected same public key on reload")
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