package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAppendGenesis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "test_event",
		DeploymentMode: "local",
		Actor:          "test_actor",
		Payload:        map[string]interface{}{"key": "value"},
	}

	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	seq, hash := w.CurrentHead()
	if seq != 1 {
		t.Fatalf("expected seq=1, got %d", seq)
	}
	if hash == "" {
		t.Fatal("expected non-empty record_hash")
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var lines []AuditRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		lines = append(lines, r)
	}

	if len(lines) != 1 {
		t.Fatalf("expected 1 record, got %d", len(lines))
	}
	if lines[0].Seq != 1 {
		t.Fatalf("expected seq=1, got %d", lines[0].Seq)
	}
	if lines[0].PrevHash != "" {
		t.Fatalf("expected prev_hash=\"\", got %q", lines[0].PrevHash)
	}
	if lines[0].RecordHash == "" {
		t.Fatal("expected non-empty record_hash")
	}

	// Verify record_hash is correct SHA-256 of canonical JSON without record_hash
	canonical, err := lines[0].CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal: %v", err)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(canonical))
	if lines[0].RecordHash != expectedHash {
		t.Fatalf("record_hash mismatch: got %q, expected %q", lines[0].RecordHash, expectedHash)
	}
}

func TestAppendChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	records := []AuditRecord{
		{Timestamp: "2025-06-18T12:00:00Z", EventType: "evt1", DeploymentMode: "local", Actor: "alice", Payload: map[string]interface{}{"a": 1}},
		{Timestamp: "2025-06-18T12:01:00Z", EventType: "evt2", DeploymentMode: "local", Actor: "bob", Payload: map[string]interface{}{"b": 2}},
		{Timestamp: "2025-06-18T12:02:00Z", EventType: "evt3", DeploymentMode: "local", Actor: "carol", Payload: map[string]interface{}{"c": 3}},
	}

	for i, rec := range records {
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append record %d: %v", i, err)
		}
	}

	seq, hash := w.CurrentHead()
	if seq != 3 {
		t.Fatalf("expected seq=3, got %d", seq)
	}

	// Read back all records
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var lines []AuditRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		lines = append(lines, r)
	}

	if len(lines) != 3 {
		t.Fatalf("expected 3 records, got %d", len(lines))
	}

	// Verify chain
	for i := 0; i < len(lines); i++ {
		if lines[i].Seq != int64(i+1) {
			t.Fatalf("record %d: expected seq=%d, got %d", i, i+1, lines[i].Seq)
		}
		if i == 0 {
			if lines[i].PrevHash != "" {
				t.Fatalf("record %d: expected prev_hash=\"\", got %q", i, lines[i].PrevHash)
			}
		} else {
			if lines[i].PrevHash != lines[i-1].RecordHash {
				t.Fatalf("record %d: prev_hash=%q, expected %q", i, lines[i].PrevHash, lines[i-1].RecordHash)
			}
		}
	}

	if lines[2].RecordHash != hash {
		t.Fatalf("head hash mismatch: got %q, expected %q", hash, lines[2].RecordHash)
	}
}

func TestCanonicalJSONStability(t *testing.T) {
	// Same record content should produce same record_hash regardless of map iteration order
	payload1 := map[string]interface{}{
		"z": "last",
		"a": "first",
		"m": "middle",
		"nested": map[string]interface{}{
			"b": 2,
			"a": 1,
		},
	}

	payload2 := map[string]interface{}{
		"a": "first",
		"m": "middle",
		"z": "last",
		"nested": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	}

	r1 := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "deploy",
		DeploymentMode: "hosted",
		Actor:          "system",
		Payload:        payload1,
		HostedContext: &HostedContext{
			TenantID:        "tenant-1",
			ProjectID:       "proj-42",
			Region:          "us-east-1",
			RuntimeProvider: "lambda",
		},
	}

	r2 := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "deploy",
		DeploymentMode: "hosted",
		Actor:          "system",
		Payload:        payload2,
		HostedContext: &HostedContext{
			TenantID:        "tenant-1",
			ProjectID:       "proj-42",
			Region:          "us-east-1",
			RuntimeProvider: "lambda",
		},
	}

	canon1, err := r1.CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal r1: %v", err)
	}
	canon2, err := r2.CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal r2: %v", err)
	}

	if string(canon1) != string(canon2) {
		t.Fatalf("canonical JSON differs for same content:\n  got1: %s\n  got2: %s", string(canon1), string(canon2))
	}

	hash1 := fmt.Sprintf("%x", sha256.Sum256(canon1))
	hash2 := fmt.Sprintf("%x", sha256.Sum256(canon2))
	if hash1 != hash2 {
		t.Fatalf("record_hash differs: %q vs %q", hash1, hash2)
	}
}

func TestConcurrentAppendRace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			rec := AuditRecord{
				Timestamp:      "2025-06-18T12:00:00Z",
				EventType:      "race",
				DeploymentMode: "local",
				Actor:          fmt.Sprintf("worker-%d", id),
				Payload:        map[string]interface{}{"id": id},
			}
			if err := w.Append(rec); err != nil {
				t.Errorf("Append: %v", err)
			}
		}(i)
	}

	wg.Wait()

	seq, hash := w.CurrentHead()
	if seq != n {
		t.Fatalf("expected seq=%d, got %d", n, seq)
	}
	if hash == "" {
		t.Fatal("expected non-empty head hash")
	}

	// Read back and verify all 100 records
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	seen := make(map[int64]bool)
	var lines []AuditRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		lines = append(lines, r)
	}

	if len(lines) != n {
		t.Fatalf("expected %d records, got %d", n, len(lines))
	}

	for _, r := range lines {
		if r.Seq < 1 || r.Seq > n {
			t.Fatalf("seq out of range: %d", r.Seq)
		}
		if seen[r.Seq] {
			t.Fatalf("duplicate seq: %d", r.Seq)
		}
		seen[r.Seq] = true
	}

	// Verify chain integrity
	for i := 1; i < len(lines); i++ {
		if lines[i].PrevHash != lines[i-1].RecordHash {
			t.Fatalf("chain broken at seq %d -> %d", lines[i-1].Seq, lines[i].Seq)
		}
	}

	t.Logf("Verified %d records with unique seq, intact chain", n)
}

func TestFsyncFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	// Write one record, then close.
	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "test",
		DeploymentMode: "local",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Make the file read-only so opening a writer for append fails.
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	// Opening a new writer on a read-only file with O_RDWR must fail.
	_, err = NewAuditWriter(path)
	if err == nil {
		t.Fatal("expected error for NewAuditWriter on read-only file, got nil")
	}
	t.Logf("Got expected fail-closed error: %v", err)

	// Restore for cleanup.
	_ = os.Chmod(path, 0644)
}

func TestAppendAfterCloseFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "test",
		DeploymentMode: "local",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append after close must fail.
	rec2 := AuditRecord{
		Timestamp:      "2025-06-18T12:01:00Z",
		EventType:      "test2",
		DeploymentMode: "local",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	err = w.Append(rec2)
	if err == nil {
		t.Fatal("expected error for Append after Close, got nil")
	}
	t.Logf("Got expected fail-closed error: %v", err)
}

func TestDeploymentModePresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "deploy",
		DeploymentMode: "hosted",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var readRec AuditRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &readRec); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
	}

	if readRec.DeploymentMode != "hosted" {
		t.Fatalf("expected deployment_mode=\"hosted\", got %q", readRec.DeploymentMode)
	}
}

func TestHostedContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "deploy",
		DeploymentMode: "hosted",
		Actor:          "deployer",
		Payload:        map[string]interface{}{"image": "nginx:1.25"},
		HostedContext: &HostedContext{
			TenantID:        "tenant-abc",
			ProjectID:       "proj-42",
			Region:          "us-east-1",
			RuntimeProvider: "lambda",
		},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var readRec AuditRecord
	for _, line := range splitLines(string(data)) {
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &readRec); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
	}

	if readRec.HostedContext == nil {
		t.Fatal("expected hosted_context to be present")
	}
	if readRec.HostedContext.TenantID != "tenant-abc" {
		t.Fatalf("tenant_id: got %q, expected %q", readRec.HostedContext.TenantID, "tenant-abc")
	}
	if readRec.HostedContext.ProjectID != "proj-42" {
		t.Fatalf("project_id: got %q, expected %q", readRec.HostedContext.ProjectID, "proj-42")
	}
	if readRec.HostedContext.Region != "us-east-1" {
		t.Fatalf("region: got %q, expected %q", readRec.HostedContext.Region, "us-east-1")
	}
	if readRec.HostedContext.RuntimeProvider != "lambda" {
		t.Fatalf("runtime_provider: got %q, expected %q", readRec.HostedContext.RuntimeProvider, "lambda")
	}
}

// splitLines splits a string into lines, preserving empty lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestReconstructHeadOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	// Write some records
	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-06-18T12:00:00Z",
			EventType:      "reconstruct",
			DeploymentMode: "local",
			Actor:          "test",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	expectedSeq, expectedHash := w.CurrentHead()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and verify head is reconstructed
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter (2nd): %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	if seq != expectedSeq {
		t.Fatalf("reconstructed seq: got %d, expected %d", seq, expectedSeq)
	}
	if hash != expectedHash {
		t.Fatalf("reconstructed hash: got %q, expected %q", hash, expectedHash)
	}

	// Appending should continue from seq=6
	rec := AuditRecord{
		Timestamp:      "2025-06-18T13:00:00Z",
		EventType:      "reconstruct_cont",
		DeploymentMode: "local",
		Actor:          "test",
		Payload:        map[string]interface{}{},
	}
	if err := w2.Append(rec); err != nil {
		t.Fatalf("Append after reopen: %v", err)
	}
	seq, _ = w2.CurrentHead()
	if seq != 6 {
		t.Fatalf("expected seq=6 after reopen+append, got %d", seq)
	}
}