package audit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCheckpointLine appends a single checkpoint line to a JSONL file.
func writeCheckpointLine(t *testing.T, path string, cp *CheckpointRecord) {
	t.Helper()
	line, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s\n", string(line)); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
}

func TestVerifyChainIntact(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	// Write 5 records using the proper AuditWriter
	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "verify_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Create a checkpoint at seq 5
	seq, hash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cp := NewCheckpoint(1, seq, hash, "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h
	writeCheckpointLine(t, cpPath, cp)

	// Verify the chain
	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues, got %d issues: %v", len(result.Issues), result.Issues)
	}
	if result.AuditRecordCount != 5 {
		t.Fatalf("expected 5 audit records, got %d", result.AuditRecordCount)
	}
}

func TestVerifyChainedCheckpoints(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Write 15 records
	for i := 0; i < 15; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "chain_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if (i+1)%5 == 0 {
			seq, hash := w.CurrentHead()
			cp := NewCheckpoint(int64((i+1)/5), seq, hash, "")
			cp.Timestamp = "2025-01-01T00:00:00Z"
			if i >= 5 {
				// Link to previous checkpoint — load the last one
				prevSeq := int64(i/5)
				cp.PrevCheckpointHash = fmt.Sprintf("prev_checkpoint_%d", prevSeq)
			}
			h, err := cp.computeCheckpointHash()
			if err != nil {
				t.Fatalf("computeCheckpointHash: %v", err)
			}
			cp.CheckpointHash = h
			writeCheckpointLine(t, cpPath, cp)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify the chain — will detect broken checkpoint chain (we used fake prev hashes)
	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}

	// Checkpoint chain is broken (fake prev hashes)
	foundCheckpointChain := false
	for _, issue := range result.Issues {
		if issue.Type == ErrTypeCheckpointChain {
			foundCheckpointChain = true
		}
	}
	if !foundCheckpointChain {
		t.Fatal("expected checkpoint chain break detection")
	}
}

func TestVerifyTamperMiddleRecord(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "tamper_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	seq, hash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cp := NewCheckpoint(1, seq, hash, "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h
	writeCheckpointLine(t, cpPath, cp)

	// Tamper the third record (line 3, index 2)
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	var rec3 AuditRecord
	if err := json.Unmarshal([]byte(lines[2]), &rec3); err != nil {
		t.Fatalf("Unmarshal line 3: %v", err)
	}
	rec3.Payload = map[string]interface{}{"tampered": true}
	modifiedLine, _ := json.Marshal(rec3)
	lines[2] = string(modifiedLine)
	if err := os.WriteFile(auditPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify — must detect tamper
	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected issues for tampered chain, got none")
	}

	// Check that tamper type is detected
	foundTamper := false
	for _, issue := range result.Issues {
		if issue.Type == ErrTypeTamperMiddle {
			foundTamper = true
			t.Logf("Found tamper: %s", issue.Message)
		}
	}
	if !foundTamper {
		t.Fatalf("expected tamper_middle detection, got issues: %v", result.Issues)
	}
}

func TestVerifyTailTruncation(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	for i := 0; i < 10; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "trunc_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	seq, hash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Create a checkpoint at seq 10 (all records written)
	cp := NewCheckpoint(1, seq, hash, "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h
	writeCheckpointLine(t, cpPath, cp)

	// Truncate the file: keep only the first 5 records
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	truncated := strings.Join(lines[:5], "\n")
	if err := os.WriteFile(auditPath, []byte(truncated), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Verify — must detect tail truncation
	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected issues for truncated chain, got none")
	}

	foundTruncation := false
	for _, issue := range result.Issues {
		if issue.Type == ErrTypeTailTruncation {
			foundTruncation = true
			t.Logf("Found truncation: %s", issue.Message)
		}
	}
	if !foundTruncation {
		t.Fatalf("expected tail_truncation detection, got issues: %v", result.Issues)
	}
}

func TestVerifyMissingCheckpointFile(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "nonexistent.checkpoints")

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

	// No checkpoints file exists
	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Type == ErrTypeMissingCheckpoint {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected missing_checkpoint issue for nonexistent file")
	}
}

func TestVerifyEmptyAuditFile(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	// Empty audit file
	if err := os.WriteFile(auditPath, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create a valid checkpoint that doesn't correspond to anything
	cp := NewCheckpoint(1, 1, "some_hash", "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h
	writeCheckpointLine(t, cpPath, cp)

	result, err := VerifyAuditChain(auditPath, cpPath, nil)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	_ = result
}

func TestVerifyWithSignature(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "audit.checkpoints")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	for i := 0; i < 3; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "sig_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	seq, hash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Create signed checkpoint
	cp := NewCheckpoint(1, seq, hash, "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	if err := cp.Sign(key); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	writeCheckpointLine(t, cpPath, cp)

	// Verify with public key
	result, err := VerifyAuditChain(auditPath, cpPath, &key.PublicKey)
	if err != nil {
		t.Fatalf("VerifyAuditChain: %v", err)
	}
	if len(result.Issues) > 0 {
		t.Fatalf("expected no issues with valid signature, got: %v", result.Issues)
	}

	// Verify with wrong key
	wrongKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	result2, err := VerifyAuditChain(auditPath, cpPath, &wrongKey.PublicKey)
	if err != nil {
		t.Fatalf("VerifyAuditChain (wrong key): %v", err)
	}
	foundBadSig := false
	for _, issue := range result2.Issues {
		if issue.Type == ErrTypeSignature {
			foundBadSig = true
			break
		}
	}
	if !foundBadSig {
		t.Fatal("expected invalid_signature issue with wrong public key")
	}
}

func TestReadAuditChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	// Use the writer to create properly hashed records
	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 3; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "read_test",
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

	readBack, err := readAuditChain(path)
	if err != nil {
		t.Fatalf("readAuditChain: %v", err)
	}
	if len(readBack) != 3 {
		t.Fatalf("expected 3 records, got %d", len(readBack))
	}
}

func TestVerifyReorderDetection(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "reorder_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Swap lines 2 and 3
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	lines[1], lines[2] = lines[2], lines[1]
	if err := os.WriteFile(auditPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read audit chain directly should detect the reorder
	_, err = readAuditChain(auditPath)
	if err == nil {
		t.Fatal("expected error from readAuditChain for reordered records")
	}
	t.Logf("Got expected reorder error: %v", err)
}