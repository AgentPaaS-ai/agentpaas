package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgressJournalWriter_ValidHeartbeat(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "attempt1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{
		WorkflowID:  "wf1",
		NodeID:      "node1",
		RunID:       "run1",
		AttemptID:   "attempt1",
		LeaseID:     "lease1",
		LeaseExpiry: time.Now().Add(30 * time.Minute),
	}
	w, err := newProgressJournalWriter(journalPath, key, identity)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	defer w.close()

	cpID, err := w.append("evt1", "starting", nil, nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if cpID != "" {
		t.Fatalf("heartbeat should not produce checkpoint ID, got %s", cpID)
	}

	// Verify journal file has 1 line.
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 journal line, got %d", len(lines))
	}

	// Parse and verify HMAC.
	var rec progressJournalRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !verifyJournalRecord(&rec, key) {
		t.Fatal("HMAC verification failed for valid record")
	}
	if rec.Sequence != 1 {
		t.Fatalf("expected sequence 1, got %d", rec.Sequence)
	}
	if rec.SafeToResume {
		t.Fatal("heartbeat should have safe_to_resume=false")
	}
}

func TestProgressJournalWriter_SafeCheckpoint(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "attempt1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{
		WorkflowID:  "wf1",
		NodeID:      "node1",
		RunID:       "run1",
		AttemptID:   "attempt1",
		LeaseID:     "lease1",
		LeaseExpiry: time.Now().Add(30 * time.Minute),
	}
	w, err := newProgressJournalWriter(journalPath, key, identity)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	defer w.close()

	cpID, err := w.append(
		evt1(), "phase1",
		[]string{"did work"},
		[]string{"more work"},
		[]string{"out.json"},
		"committed action",
		true,
		"abcd1234", "efgh5678",
	)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if cpID == "" {
		t.Fatal("safe_to_resume should produce checkpoint ID")
	}
	if !strings.HasPrefix(cpID, "cp-attempt1-") {
		t.Fatalf("checkpoint ID should start with cp-attempt1-, got %s", cpID)
	}
}

func TestProgressJournalWriter_DuplicateEventID(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "attempt1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{
		RunID:     "run1",
		AttemptID: "attempt1",
	}
	w, _ := newProgressJournalWriter(journalPath, key, identity)
	defer w.close()

	eventID := "dup-evt-123"
	_, err := w.append(eventID, "phase1", nil, nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	cpID, err := w.append(eventID, "phase1", nil, nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if cpID != "" {
		t.Fatalf("duplicate event should return empty checkpoint ID, got %s", cpID)
	}

	// Verify only 1 line in journal.
	data, _ := os.ReadFile(journalPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line after dup, got %d", len(lines))
	}
}

func TestProgressJournalWriter_MonotonicSequence(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{RunID: "r1", AttemptID: "a1"}
	w, _ := newProgressJournalWriter(journalPath, key, identity)
	defer w.close()

	for i := 0; i < 5; i++ {
		_, err := w.append(evtN(i), "p", nil, nil, nil, "", false, "", "")
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	data, _ := os.ReadFile(journalPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var rec progressJournalRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}
		if rec.Sequence != int64(i+1) {
			t.Fatalf("line %d: expected seq %d, got %d", i, i+1, rec.Sequence)
		}
		if !verifyJournalRecord(&rec, key) {
			t.Fatalf("line %d: HMAC verification failed", i)
		}
	}
}

func TestProgressJournalWriter_HMACKnownVector(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("known-test-key-32-bytes-ok!!!!!!!")
	identity := progressIdentity{
		WorkflowID: "wf", NodeID: "n", RunID: "r", AttemptID: "a", LeaseID: "l",
		LeaseExpiry: time.Now().Add(time.Hour),
	}
	w, _ := newProgressJournalWriter(journalPath, key, identity)
	defer w.close()

	_, err := w.append("evt-x", "phase", []string{"work"}, nil, []string{"f.json"}, "act", true, "d1", "d2")
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	data, _ := os.ReadFile(journalPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var rec progressJournalRecord
	json.Unmarshal([]byte(lines[0]), &rec)

	// Verify HMAC with correct key.
	if !verifyJournalRecord(&rec, key) {
		t.Fatal("HMAC should verify with correct key")
	}
	// Verify HMAC with wrong key fails.
	wrongKey := []byte("wrong-key-32-bytes-long-enough!!!")
	if verifyJournalRecord(&rec, wrongKey) {
		t.Fatal("HMAC should NOT verify with wrong key")
	}
}

func TestProgressJournalWriter_ReorderedSequence(t *testing.T) {
	// Verify that sequence numbers are strictly monotonic in the writer.
	// The daemon tailer (T03) will reject out-of-order records.
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	w, _ := newProgressJournalWriter(journalPath, key, progressIdentity{RunID: "r", AttemptID: "a"})
	defer w.close()

	_, _ = w.append("e1", "p1", nil, nil, nil, "", false, "", "")
	_, _ = w.append("e2", "p2", nil, nil, nil, "", false, "", "")
	_, _ = w.append("e3", "p3", nil, nil, nil, "", false, "", "")

	data, _ := os.ReadFile(journalPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i, line := range lines {
		var rec progressJournalRecord
		json.Unmarshal([]byte(line), &rec)
		if rec.Sequence != int64(i+1) {
			t.Fatalf("line %d: expected seq %d, got %d", i, i+1, rec.Sequence)
		}
	}
}

func TestJournalKeyGeneration(t *testing.T) {
	key1, err := generateJournalKey()
	if err != nil {
		t.Fatalf("generateJournalKey: %v", err)
	}
	if len(key1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key1))
	}
	key2, _ := generateJournalKey()
	if string(key1) == string(key2) {
		t.Fatal("keys should be different (random)")
	}
}

func TestJournalKeySaveLoadRemove(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "attempt-secrets", "attempt1.journal-key")
	key := []byte("secret-key-for-testing-purposes!!")

	if err := saveJournalKey(keyPath, key); err != nil {
		t.Fatalf("saveJournalKey: %v", err)
	}

	// Verify permissions.
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}

	// Verify parent dir permissions.
	dirInfo, err := os.Stat(filepath.Dir(keyPath))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected 0700 dir, got %o", dirInfo.Mode().Perm())
	}

	loaded, err := loadJournalKey(keyPath)
	if err != nil {
		t.Fatalf("loadJournalKey: %v", err)
	}
	if string(loaded) != string(key) {
		t.Fatal("loaded key does not match saved key")
	}

	if err := removeJournalKey(keyPath); err != nil {
		t.Fatalf("removeJournalKey: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("key file should be removed")
	}
}

func TestProgressJournalWriter_KeyNeverInRecord(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("super-secret-key-not-in-records!")
	w, _ := newProgressJournalWriter(journalPath, key, progressIdentity{RunID: "r", AttemptID: "a"})
	defer w.close()

	_, _ = w.append("e1", "p", nil, nil, nil, "", false, "", "")

	data, _ := os.ReadFile(journalPath)
	if strings.Contains(string(data), string(key)) {
		t.Fatal("journal key must not appear in journal records")
	}
}

func TestComputeCheckpointDigest(t *testing.T) {
	rec := &progressJournalRecord{
		Phase:               "phase1",
		CompletedWork:       []string{"did A"},
		RemainingWork:       []string{"do B"},
		LastCommittedAction: "committed",
		SafeToResume:        true,
	}
	d1 := computeCheckpointDigest(rec)
	d2 := computeCheckpointDigest(rec)
	if d1 != d2 {
		t.Fatal("same input should produce same digest")
	}
	if len(d1) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d", len(d1))
	}

	// Different input should produce different digest.
	rec2 := &progressJournalRecord{
		Phase:               "phase2",
		CompletedWork:       []string{"did A"},
		RemainingWork:       []string{"do B"},
		LastCommittedAction: "committed",
		SafeToResume:        true,
	}
	d3 := computeCheckpointDigest(rec2)
	if d1 == d3 {
		t.Fatal("different input should produce different digest")
	}
}

// helper functions
func evt1() string {
	return "evt-unique-1"
}

func evtN(i int) string {
	return "evt-" + string(rune('A'+i)) + "-test"
}
