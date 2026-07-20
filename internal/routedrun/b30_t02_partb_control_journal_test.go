package routedrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// B30-T02 Part B — control-journal-level tests (B3-B4).
//
// B3: Duplicate job envelope idempotency at harness level (spec line 308).
// B4: Python cannot read/write control key or forge result event (spec line
// 309).
//
// These tests exercise the ControlJournal's duplicate-detection and HMAC
// forgery-rejection capabilities that the T05 harness startup will rely on.
// T05 owns the harness startup; Part B tests the journal properties that
// underpin them.
// ---------------------------------------------------------------------------

// TestB30T02PartB_DuplicateJobEnvelope_OneHandler verifies the
// ControlJournal rejects a duplicate-sequence `accepted` event (the
// duplicate job envelope case). Part A's ControlJournal test 4 covers
// monotonic no-gaps; Part B tests the specific duplicate-envelope case:
// appending a second `accepted` event with sequence 1 after one is already
// present must be rejected. The harness startup consumes one job envelope
// exactly once (spec line 289).
func TestB30T02PartB_DuplicateJobEnvelope_OneHandler(t *testing.T) {
	t.Parallel()
	stateRoot := t.TempDir()
	runID := "run-dup-env"
	attemptID := "att-dup-env"
	cj, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()

	// First accepted event (sequence 1): the harness consumed the job
	// envelope exactly once.
	accepted := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-1"}`,
	}
	if err := cj.Append(accepted); err != nil {
		t.Fatalf("first Append accepted: %v", err)
	}

	// Duplicate envelope: a second attempt to append sequence 1 must be
	// rejected (sequence collision). Either an error OR idempotent return
	// is acceptable per the spec; a silent second write is NOT.
	dup := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-1-dup"}`,
	}
	if err := cj.Append(dup); err == nil {
		t.Error("ControlJournal accepted a duplicate-sequence accepted event (duplicate envelope not rejected)")
	}

	// Read back: exactly one accepted event must be present — no second
	// handler invocation implied.
	got, err := cj.Read(1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 event (one handler), got %d", len(got))
	}
	if got[0].EventKind != InvokeJobEventAccepted {
		t.Fatalf("event kind=%s want ACCEPTED", got[0].EventKind)
	}
	if got[0].Payload != `{"envelope":"job-1"}` {
		t.Fatalf("payload changed by duplicate append: %q", got[0].Payload)
	}

	// The next event must be sequence 2 (started), confirming exactly one
	// accepted envelope was consumed.
	if err := cj.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      2,
		Timestamp:     now,
		EventKind:     InvokeJobEventStarted,
		Payload:       `{"started":true}`,
	}); err != nil {
		t.Fatalf("Append started at seq 2: %v", err)
	}
	got2, _ := cj.Read(1)
	if len(got2) != 2 {
		t.Fatalf("want 2 events after started, got %d", len(got2))
	}
	if got2[0].EventKind != InvokeJobEventAccepted || got2[1].EventKind != InvokeJobEventStarted {
		t.Fatalf("event order: %s %s", got2[0].EventKind, got2[1].EventKind)
	}
}

// TestB30T02PartB_DuplicateJobEnvelope_AfterCrashReopensJournal verifies
// the duplicate-envelope rejection survives a daemon restart: reopening the
// journal after a crash and attempting to re-append the same sequence must
// still be rejected. The harness startup must not re-consume the envelope.
func TestB30T02PartB_DuplicateJobEnvelope_AfterCrashReopensJournal(t *testing.T) {
	t.Parallel()
	stateRoot := t.TempDir()
	runID := "run-dup-crash"
	attemptID := "att-dup-crash"
	cj, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()
	if err := cj.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-crash"}`,
	}); err != nil {
		t.Fatalf("first Append: %v", err)
	}

	// Crash: reopen the journal. The lastSeq must be recovered from disk.
	cj2, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("reopen NewControlJournal: %v", err)
	}
	defer func() { _ = cj2.Close() }()

	// Re-append sequence 1 (duplicate envelope after restart): rejected.
	dup := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"envelope":"job-crash-dup"}`,
	}
	if err := cj2.Append(dup); err == nil {
		t.Error("ControlJournal accepted a duplicate-sequence event after reopen (envelope re-consumed)")
	}
	got, _ := cj2.Read(1)
	if len(got) != 1 {
		t.Fatalf("after reopen: want 1 event, got %d", len(got))
	}
	if got[0].Payload != `{"envelope":"job-crash"}` {
		t.Fatalf("payload tampered by duplicate: %q", got[0].Payload)
	}
}

// TestB30T02PartB_PythonCannotForgeResultEvent simulates a forged result
// event from a malicious Python worker: an InvokeJobResult with a wrong HMAC.
// Asserts the ControlJournal's verification rejects the forged event on
// read-back. The harness's persistTerminal (when implemented in T05) must
// refuse to mark success from a forged event. Part B tests at the
// ControlJournal level: append a tampered event file with a forged HMAC and
// assert Read fails.
func TestB30T02PartB_PythonCannotForgeResultEvent(t *testing.T) {
	t.Parallel()
	stateRoot := t.TempDir()
	runID := "run-forge"
	attemptID := "att-forge"
	cj, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()

	// Legitimately append accepted (seq 1) and started (seq 2).
	if err := cj.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"ok":true}`,
	}); err != nil {
		t.Fatalf("Append accepted: %v", err)
	}
	if err := cj.Append(InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      2,
		Timestamp:     now,
		EventKind:     InvokeJobEventStarted,
		Payload:       `{"ok":true}`,
	}); err != nil {
		t.Fatalf("Append started: %v", err)
	}

	// Forge a `succeeded` event with a WRONG HMAC and write it directly to
	// disk (simulating a malicious Python worker that does NOT have the
	// control key but can write files in the control directory — the
	// directory is 0700, but a worker that somehow escaped UID isolation
	// still cannot forge because it lacks the HMAC key).
	forged := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      3,
		Timestamp:     now,
		EventKind:     InvokeJobEventSucceeded,
		Payload:       `{"result_digest":"sha256:forged"}`,
		HMAC:          "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
	}
	data, err := json.Marshal(forged)
	if err != nil {
		t.Fatalf("marshal forged: %v", err)
	}
	forgedPath := filepath.Join(stateRoot, "runs", runID, "control", attemptID, "event-0000000003.json")
	// Use the package-internal atomicWriteFile so the file lands at 0600
	// under the 0700 control dir (matches the journal's own layout).
	if err := atomicWriteFile(forgedPath, data, filePerm); err != nil {
		t.Fatalf("write forged: %v", err)
	}

	// Read back: the forged event must fail HMAC verification. Read returns
	// an error and no events are returned.
	if _, err := cj.Read(1); err == nil {
		t.Fatal("Read must reject the forged event (HMAC mismatch)")
	}

	// The control key must remain unreadable to a non-root worker: file
	// mode 0600 OUTSIDE the control directory. A Python worker that can
	// only write to the control directory cannot read the key.
	keyPath := filepath.Join(stateRoot, "runs", runID, controlKeyFileName)
	fi, err := os.Lstat(keyPath)
	if err != nil {
		t.Fatalf("lstat control-key: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("control-key mode %#o want 0600 (Python must not read)", fi.Mode().Perm())
	}
	controlDir := filepath.Join(stateRoot, "runs", runID, "control", attemptID)
	if keyPath == controlDir || (len(keyPath) > len(controlDir) && keyPath[:len(controlDir)+1] == controlDir+string(filepath.Separator)) {
		t.Fatalf("control-key %s must be outside the control dir %s", keyPath, controlDir)
	}
}

// TestB30T02PartB_PythonCannotForgeResultEvent_TamperedPayload verifies
// that a legitimately-written event whose payload is tampered after the
// fact (HMAC unchanged) is rejected on read-back. This is the
// "forge result event" vector at the journal level.
func TestB30T02PartB_PythonCannotForgeResultEvent_TamperedPayload(t *testing.T) {
	t.Parallel()
	stateRoot := t.TempDir()
	runID := "run-tamper"
	attemptID := "att-tamper"
	cj, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	defer func() { _ = cj.Close() }()
	now := time.Now().UTC()

	// Legitimately append a succeeded event (seq 1).
	orig := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     now,
		EventKind:     InvokeJobEventSucceeded,
		Payload:       `{"result_digest":"sha256:real"}`,
	}
	if err := cj.Append(orig); err != nil {
		t.Fatalf("Append succeeded: %v", err)
	}

	// Tamper: rewrite the event file with a forged result_digest payload
	// but keep the original (now-invalid) HMAC.
	path := filepath.Join(stateRoot, "runs", runID, "control", attemptID, "event-0000000001.json")
	origData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	var rec InvokeJobEvent
	if err := json.Unmarshal(origData, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rec.Payload = `{"result_digest":"sha256:forged"}`
	tampered, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal tampered: %v", err)
	}
	if err := atomicWriteFile(path, tampered, filePerm); err != nil {
		t.Fatalf("rewrite tampered: %v", err)
	}

	// Read must now fail because the HMAC no longer matches the payload.
	if _, err := cj.Read(1); err == nil {
		t.Fatal("Read after payload tamper must fail (HMAC mismatch)")
	}
}
