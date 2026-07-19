package routedrun

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

func TestSaveCheckpoint_CreatesAndRetrieves(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	cp := &SemanticCheckpoint{
		CheckpointID:    CheckpointID("cp-a1-1"),
		AttemptID:       AttemptID("a1"),
		RunID:           RunID("r1"),
		WorkflowID:      WorkflowID("wf1"),
		Phase:           "phase1",
		CompletedWork:   []string{"did A"},
		RemainingWork:   []string{"do B"},
		SafeToResume:    true,
		Sequence:        1,
		CreatedAt:       time.Now().UTC(),
	}

	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	got, err := store.GetCheckpoint(ctx, CheckpointID("cp-a1-1"))
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.Phase != "phase1" {
		t.Fatalf("expected phase 'phase1', got %s", got.Phase)
	}
	if got.CheckpointID != "cp-a1-1" {
		t.Fatalf("expected checkpoint_id 'cp-a1-1', got %s", got.CheckpointID)
	}

	// Verify file permissions.
	path := filepath.Join(store.root, checkpointDir, "cp-a1-1.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}
}

func TestSaveCheckpoint_NeverMutates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:       RunID("r1"),
		Phase:       "original",
		SafeToResume: true,
		Sequence:    1,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Try to save with different content — should fail.
	cp2 := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:       RunID("r1"),
		Phase:       "modified",
		SafeToResume: true,
		Sequence:    1,
		CreatedAt:   time.Now().UTC(),
	}
	err := store.SaveCheckpoint(ctx, cp2)
	if err == nil {
		t.Fatal("expected error for duplicate checkpoint")
	}

	// Verify original is unchanged.
	got, _ := store.GetCheckpoint(ctx, CheckpointID("cp-a1-1"))
	if got.Phase != "original" {
		t.Fatalf("checkpoint was mutated: expected 'original', got '%s'", got.Phase)
	}
}

func TestGetLatestCheckpoint_ReturnsHighestSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		cp := &SemanticCheckpoint{
			CheckpointID: CheckpointID(fmt.Sprintf("cp-a1-%d", i)),
			AttemptID:    AttemptID("a1"),
			RunID:       RunID("r1"),
			Phase:       fmt.Sprintf("phase%d", i),
			SafeToResume: true,
			Sequence:    int64(i),
			CreatedAt:   time.Now().UTC(),
		}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	got, err := store.GetLatestCheckpoint(ctx, AttemptID("a1"))
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got.Sequence != 3 {
		t.Fatalf("expected sequence 3, got %d", got.Sequence)
	}
	if got.Phase != "phase3" {
		t.Fatalf("expected phase3, got %s", got.Phase)
	}
}

func TestGetLatestCheckpoint_IgnoresHeartbeats(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create a safe checkpoint.
	cp1 := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:       RunID("r1"),
		Phase:       "safe1",
		SafeToResume: true,
		Sequence:    1,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, cp1); err != nil {
		t.Fatal(err)
	}

	// Create a heartbeat (safe_to_resume=false).
	cp2 := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-2"),
		AttemptID:    AttemptID("a1"),
		RunID:       RunID("r1"),
		Phase:       "heartbeat",
		SafeToResume: false,
		Sequence:    2,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, cp2); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetLatestCheckpoint(ctx, AttemptID("a1"))
	if err != nil {
		t.Fatalf("GetLatest: %v", err)
	}
	if got.Phase != "safe1" {
		t.Fatalf("expected 'safe1', got '%s'", got.Phase)
	}
}

func TestSaveAttemptProgress_CreatesAndRetrieves(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	p := &AttemptProgress{
		SchemaVersion: CurrentSchemaVersion,
		AttemptID:     AttemptID("a1"),
		RunID:         RunID("r1"),
		LastPhase:     "working",
		LastHeartbeat: time.Now().UTC(),
		LastSequence:  5,
	}
	if err := store.SaveAttemptProgress(ctx, AttemptID("a1"), p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.GetAttemptProgress(ctx, AttemptID("a1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastPhase != "working" {
		t.Fatalf("expected 'working', got '%s'", got.LastPhase)
	}
	if got.LastSequence != 5 {
		t.Fatalf("expected seq 5, got %d", got.LastSequence)
	}
}

func TestProgressTailer_ValidSafeCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	// Create journal with one safe checkpoint record.
	rec := journalLine{
		SchemaVersion:       "1.0",
		WorkflowID:          "wf1",
		NodeID:              "n1",
		RunID:               "r1",
		AttemptID:           "a1",
		LeaseID:             "l1",
		Sequence:            1,
		Timestamp:           time.Now().UTC().Format(time.RFC3339Nano),
		EventID:            "evt1",
		Phase:              "phase1",
		CompletedWork:      []string{"did work"},
		RemainingWork:      []string{"more"},
		ArtifactRefs:       []string{"out.json"},
		LastCommittedAction: "committed",
		SafeToResume:       true,
		CheckpointDigest:   "abc123",
		ArtifactMetaDigest: "def456",
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, line, 0o600); err != nil {
		t.Fatal(err)
	}

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	cpID, err := tailer.IngestRecord(ctx, line[:len(line)-1])
	if err != nil {
		t.Fatalf("IngestRecord: %v", err)
	}
	if cpID == "" {
		t.Fatal("expected checkpoint ID for safe checkpoint")
	}

	// Verify checkpoint was persisted.
	cp, err := store.GetCheckpoint(ctx, CheckpointID(cpID))
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if cp.Phase != "phase1" {
		t.Fatalf("expected phase1, got %s", cp.Phase)
	}
	if !cp.SafeToResume {
		t.Fatal("expected SafeToResume=true")
	}
}

func TestProgressTailer_HeartbeatNoCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	rec := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "working",
		SafeToResume: false,
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	cpID, err := tailer.IngestRecord(ctx, line)
	if err != nil {
		t.Fatalf("IngestRecord: %v", err)
	}
	if cpID != "" {
		t.Fatalf("heartbeat should not produce checkpoint ID, got %s", cpID)
	}

	// Verify progress was still recorded.
	p, err := store.GetAttemptProgress(ctx, AttemptID("a1"))
	if err != nil {
		t.Fatalf("GetProgress: %v", err)
	}
	if p.LastPhase != "working" {
		t.Fatalf("expected phase 'working', got '%s'", p.LastPhase)
	}
}

func TestProgressTailer_TamperedHMAC(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")

	rec := journalLine{
		RunID:       "r1",
		AttemptID:   "a1",
		Sequence:    1,
		EventID:     "evt1",
		Phase:       "p",
		SafeToResume: false,
	}
	// Sign with wrong key, verify with right key.
	rec.HMAC = computeTestHMAC(rec, []byte("wrong-key-32-bytes-long-enough!!"))
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, []byte("right-key-32-bytes-long-enough!"), store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("expected HMAC verification error")
	}
}

func TestProgressTailer_ReplayedSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	rec := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "p",
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// Replay same sequence.
	rec2 := rec
	rec2.EventID = "evt2"
	rec2.HMAC = computeTestHMAC(rec2, key)
	line2, _ := json.Marshal(rec2)
	_, err = tailer.IngestRecord(ctx, line2)
	if err == nil {
		t.Fatal("expected replay error")
	}
}

func TestProgressTailer_ReorderedSequence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))

	// Ingest seq 2 first.
	rec2 := journalLine{RunID: "r1", AttemptID: "a1", Sequence: 2, EventID: "e2", Phase: "p2"}
	rec2.HMAC = computeTestHMAC(rec2, key)
	line2, _ := json.Marshal(rec2)
	_, err := tailer.IngestRecord(ctx, line2)
	if err != nil {
		t.Fatalf("ingest seq 2: %v", err)
	}

	// Try to ingest seq 1 (out of order — lower than last).
	rec1 := journalLine{RunID: "r1", AttemptID: "a1", Sequence: 1, EventID: "e1", Phase: "p1"}
	rec1.HMAC = computeTestHMAC(rec1, key)
	line1, _ := json.Marshal(rec1)
	_, err = tailer.IngestRecord(ctx, line1)
	if err == nil {
		t.Fatal("expected reordered sequence error (1 after 2)")
	}
}

func TestProgressTailer_RunIDMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	rec := journalLine{
		RunID:     "wrong-run",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "e1",
		Phase:     "p",
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("expected run_id mismatch error")
	}
}

func TestProgressTailer_AttemptIDMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	rec := journalLine{
		RunID:     "r1",
		AttemptID: "wrong-attempt",
		Sequence:  1,
		EventID:   "e1",
		Phase:     "p",
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("expected attempt_id mismatch error")
	}
}

func TestProgressTailer_CheckpointSizeLimit(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create a checkpoint that exceeds 64 KiB.
	bigWork := make([]string, 50)
	for i := range bigWork {
		bigWork[i] = string(make([]byte, 1024))
	}
	cp := &SemanticCheckpoint{
		CheckpointID:  CheckpointID("cp-big-1"),
		AttemptID:     AttemptID("a1"),
		RunID:         RunID("r1"),
		CompletedWork: bigWork,
		SafeToResume:  true,
		Sequence:      1,
		CreatedAt:     time.Now().UTC(),
	}
	err := store.SaveCheckpoint(ctx, cp)
	if err == nil {
		// Check if the data actually exceeds 64 KiB.
		data, _ := json.Marshal(cp)
		if int64(len(data)) > maxCheckpointBytes {
			t.Fatal("expected size cap exceeded error")
		}
	}
}

// ---------------------------------------------------------------------------
// F5: Tampered journal audit event + resume_capability=none
// ---------------------------------------------------------------------------

// testAuditAppender is an in-memory audit.AuditAppender that collects records.
type testAuditAppender struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (a *testAuditAppender) Append(record audit.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return nil
}

func (a *testAuditAppender) Records() []audit.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]audit.AuditRecord, len(a.records))
	copy(out, a.records)
	return out
}

func TestProgressTailer_TamperedJournalAuditEvent(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	// Write a valid first line then a tampered second line.
	rec1 := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "working",
	}
	rec1.HMAC = computeTestHMAC(rec1, key)
	line1, _ := json.Marshal(rec1)
	line1 = append(line1, '\n')

	// Tampered: signed with wrong key
	rec2 := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  2,
		EventID:   "evt2",
		Phase:     "working2",
	}
	rec2.HMAC = computeTestHMAC(rec2, []byte("wrong-key-32-bytes-long-enough!!"))
	line2, _ := json.Marshal(rec2)
	line2 = append(line2, '\n')

	journalData := append(line1, line2...)
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journalData, 0o600); err != nil {
		t.Fatal(err)
	}

	auditApp := &testAuditAppender{}
	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	tailer.SetAuditAppender(auditApp)

	tailer.Start(ctx)

	// Wait for the tailer to process and exit (it returns on tampered record).
	// Use a polling loop instead of Stop(), since Stop() closes stopCh which
	// may preempt the ticker before the journal is read.
	deadline := time.Now().Add(3 * time.Second)
	var records []audit.AuditRecord
	for time.Now().Before(deadline) {
		records = auditApp.Records()
		if len(records) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel() // ensure cleanup
	tailer.Stop()

	// Verify audit event was emitted.
	if len(records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(records))
	}
	r := records[0]
	if r.EventType != "progress_journal_invalid" {
		t.Errorf("expected event_type 'progress_journal_invalid', got '%s'", r.EventType)
	}
	if r.Actor != "daemon" {
		t.Errorf("expected actor 'daemon', got '%s'", r.Actor)
	}
	if runID, ok := r.Payload["run_id"].(string); !ok || runID != "r1" {
		t.Errorf("expected payload run_id 'r1', got %v", r.Payload["run_id"])
	}
	if attemptID, ok := r.Payload["attempt_id"].(string); !ok || attemptID != "a1" {
		t.Errorf("expected payload attempt_id 'a1', got %v", r.Payload["attempt_id"])
	}
	if errMsg, ok := r.Payload["error"].(string); !ok || errMsg == "" {
		t.Errorf("expected non-empty error in payload, got %v", r.Payload["error"])
	}

	// Verify resume capability was persisted as none.
	p, err := store.GetAttemptProgress(ctx, AttemptID("a1"))
	if err != nil {
		t.Fatalf("GetAttemptProgress: %v", err)
	}
	if p.ResumeCapability == nil {
		t.Fatal("expected ResumeCapability to be set, got nil")
	}
	if *p.ResumeCapability != ResumeCapNone {
		t.Errorf("expected ResumeCapNone, got %d", *p.ResumeCapability)
	}
	if p.LastPhase != "journal_invalid" {
		t.Errorf("expected LastPhase 'journal_invalid', got '%s'", p.LastPhase)
	}
}

func TestProgressTailer_TamperedJournalWithoutAuditAppender(t *testing.T) {
	// Verify that tailer still stops on tampered journal when no
	// audit appender is set (no panic, no error).
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	rec := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "p",
	}
	rec.HMAC = computeTestHMAC(rec, []byte("wrong-key-32-bytes-long-enough!!"))
	line, _ := json.Marshal(rec)
	line = append(line, '\n')

	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, line, 0o600); err != nil {
		t.Fatal(err)
	}

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	// No SetAuditAppender call — must not panic.
	tailer.Start(ctx)
	// Give the tailer time to poll and process.
	time.Sleep(200 * time.Millisecond)
	tailer.Stop()
	// Reaching here without panic is the success condition.
}

// ---------------------------------------------------------------------------
// F10c: Trailing partial-line handling
// ---------------------------------------------------------------------------

func TestSplitLines_NoTrailingFragment(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{
			name: "single complete line",
			data: []byte("hello\n"),
			want: []string{"hello"},
		},
		{
			name: "two complete lines",
			data: []byte("hello\nworld\n"),
			want: []string{"hello", "world"},
		},
		{
			name: "trailing fragment without newline",
			data: []byte("hello\nwor"),
			want: []string{"hello"},
		},
		{
			name: "only trailing fragment",
			data: []byte("hello"),
			want: nil,
		},
		{
			name: "empty input",
			data: []byte{},
			want: nil,
		},
		{
			name: "line with embedded newlines",
			data: []byte("a\nb\nc\n"),
			want: []string{"a", "b", "c"},
		},
		{
			name: "trailing partial with embedded newline",
			data: []byte("a\nb\nc"),
			want: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := splitLines(tt.data)
			got := make([]string, len(lines))
			for i, l := range lines {
				got[i] = string(l)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d lines, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestProgressTailer_PartialLineNotIngested(t *testing.T) {
	// Write a partial line (no trailing newline) and verify the tailer
	// does not attempt to parse it before the line is completed.
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	// Write a valid first line with newline.
	rec1 := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "working",
	}
	rec1.HMAC = computeTestHMAC(rec1, key)
	line1, _ := json.Marshal(rec1)
	line1 = append(line1, '\n')

	// Build a complete valid second line, then split it at a midpoint.
	rec2 := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  2,
		EventID:   "evt2",
		Phase:     "done",
	}
	rec2.HMAC = computeTestHMAC(rec2, key)
	line2, _ := json.Marshal(rec2)
	line2 = append(line2, '\n')

	// Split line2 into two fragments: first without newline.
	splitPoint := len(line2) / 2
	firstHalf := line2[:splitPoint]
	secondHalf := line2[splitPoint:]

	// Write first complete line + first half of second line (no newline in second half).
	journalData := append(line1, firstHalf...)
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, journalData, 0o600); err != nil {
		t.Fatal(err)
	}

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	tailer.Start(ctx)

	// Wait for the tailer to process the first line.
	deadline := time.Now().Add(3 * time.Second)
	var gotFirstLine bool
	for time.Now().Before(deadline) {
		p, err := store.GetAttemptProgress(ctx, AttemptID("a1"))
		if err == nil && p.LastSequence == 1 {
			gotFirstLine = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gotFirstLine {
		t.Fatal("tailer did not process the first line within timeout")
	}

	// Now append the second half.
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(secondHalf); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	// Wait for tailer to pick up and process the completed second line.
	deadline = time.Now().Add(3 * time.Second)
	var gotSecondLine bool
	for time.Now().Before(deadline) {
		p2, err := store.GetAttemptProgress(ctx, AttemptID("a1"))
		if err == nil && p2.LastSequence == 2 {
			gotSecondLine = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	tailer.Stop()

	// Second line should now be ingested (the completed line is valid).
	if !gotSecondLine {
		p2, _ := store.GetAttemptProgress(ctx, AttemptID("a1"))
		t.Fatalf("second line not ingested within timeout; LastSequence=%d", p2.LastSequence)
	}
}

// Helpers

func newTestStore(t *testing.T) *LocalStore {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenLocalStore(dir)
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	return store
}

func computeTestHMAC(rec journalLine, key []byte) string {
	rec.HMAC = ""
	canonical, _ := json.Marshal(rec)
	mac := hmac.New(sha256.New, key)
	mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}
