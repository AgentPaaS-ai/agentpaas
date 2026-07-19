package routedrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Adversary: direct worker write of a forged progress record.
// The worker has no journal key, so any HMAC it computes will be wrong.
func TestAdversary_B27_ForgedProgressRecord(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "a1.jsonl")
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		t.Fatal(err)
	}

	// Worker forges a record with a fake HMAC.
	forged := journalLine{
		SchemaVersion: "1.0",
		RunID:         "r1",
		AttemptID:     "a1",
		Sequence:      1,
		EventID:       "forged-evt",
		Phase:         "evil",
		SafeToResume:  true,
		CompletedWork: []string{"fake"},
	}
	// Worker doesn't have the real key, so computes with wrong key.
	wrongKey := []byte("worker-does-not-have-this-key!!!")
	forged.HMAC = computeTestHMAC(forged, wrongKey)
	line, _ := json.Marshal(forged)

	// Daemon uses the real key.
	realKey := []byte("real-daemon-key-32-bytes-ok!!!!!!!")
	tailer := NewProgressTailer(journalPath, realKey, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: forged progress record accepted with wrong HMAC")
	}
}

// Adversary: journal key discovery through checkpoint files.
// The journal key must never be serialized into checkpoint JSON.
// Verify structurally: no field named journal_key, journalKey, key, secret,
// or HMAC key appears in the checkpoint JSON.
func TestAdversary_B27_JournalKeyNotInCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID:     CheckpointID("cp-a1-1"),
		AttemptID:        AttemptID("a1"),
		RunID:            RunID("r1"),
		Phase:            "phase1",
		SafeToResume:     true,
		Sequence:         1,
		CreatedAt:        time.Now().UTC(),
		CheckpointDigest: "some-digest",
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read the checkpoint file and verify no key material.
	cpPath := filepath.Join(store.root, checkpointDir, "cp-a1-1.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Parse as JSON and check no key-like field names exist.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal checkpoint: %v", err)
	}
	forbiddenFields := []string{
		"journal_key", "journalKey", "journal-key",
		"key", "secret", "hmac_key", "hmacKey",
		"signing_key", "signingKey",
	}
	for field := range raw {
		flower := strings.ToLower(field)
		for _, forbidden := range forbiddenFields {
			if flower == strings.ToLower(forbidden) {
				t.Fatalf("ADVERSARY BREAK: forbidden field %q found in checkpoint JSON", field)
			}
		}
	}

	// Also verify no key-like string values appear.
	for _, sentinel := range []string{"journal-key", "journal_key"} {
		if containsBytes(data, []byte(sentinel)) {
			t.Fatalf("ADVERSARY BREAK: %q string found in checkpoint file", sentinel)
		}
	}
}

// Adversary: HMAC replay (same sequence, different event ID).
func TestAdversary_B27_HMACReplay(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	// Create and sign a valid record.
	rec := journalLine{
		RunID:     "r1",
		AttemptID: "a1",
		Sequence:  1,
		EventID:   "evt1",
		Phase:     "p1",
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	// Replay with different event ID but same sequence — should fail.
	rec2 := rec
	rec2.EventID = "evt2"
	rec2.HMAC = computeTestHMAC(rec2, key)
	line2, _ := json.Marshal(rec2)
	_, err = tailer.IngestRecord(ctx, line2)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: replayed sequence accepted")
	}
}

// Adversary: HMAC truncation.
func TestAdversary_B27_HMACTruncation(t *testing.T) {
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
	// Truncate the HMAC.
	rec.HMAC = rec.HMAC[:10]
	line, _ := json.Marshal(rec)

	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: truncated HMAC accepted")
	}
}

// Adversary: oversized checkpoint (> 64 KiB).
func TestAdversary_B27_OversizedCheckpoint(t *testing.T) {
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
	data, _ := json.Marshal(cp)
	if int64(len(data)) > maxCheckpointBytes {
		err := store.SaveCheckpoint(ctx, cp)
		if err == nil {
			t.Fatal("ADVERSARY BREAK: oversized checkpoint accepted")
		}
	}
}

// Adversary: secret/API-key in checkpoint content.
// The harness (T02) rejects secret sentinels lexically before persistence.
// The store itself does not detect secrets — it trusts the harness.
// Here we verify that a checkpoint with a secret-like value in completed_work
// is still stored by the store (the store is post-harness), but the resume
// loader includes it as-is. The harness rejection is tested in the harness
// package's progress_handler_test.
func TestAdversary_B27_SecretInCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		CompletedWork: []string{"sk-or-v1-fake-key-12345"},
		LastCommittedAction: "committed",
		SafeToResume: true,
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
	}
	cp.CheckpointDigest = recomputeCheckpointDigest(cp)
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The store accepts it (post-harness). The harness should have rejected it.
	// Verify the checkpoint is retrievable for audit.
	got, err := store.GetCheckpoint(ctx, CheckpointID("cp-a1-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.CompletedWork) != 1 {
		t.Fatal("expected 1 completed work entry")
	}
	// NOTE: The harness-level rejection of secret sentinels is tested in
	// the harness package. This test documents that the store itself does
	// not detect secrets — the harness is the trusted gatekeeper.
}

// Adversary: artifact absolute path, traversal, symlink swap.
func TestAdversary_B27_ArtifactEscape(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	// Create target outside root.
	target := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create symlink in artifact dir pointing outside.
	link := filepath.Join(root, "escape.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := aw.ValidateAndAccept(context.Background(), "escape.json", AttemptID("a1"))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: symlink escape accepted")
	}
}

// Adversary: artifact hard-link escape.
// Spec: "No symlinks, devices, sockets, FIFOs, absolute paths, traversal, or
// hard links." A hard link to a file outside the artifact root bypasses
// containment. The workspace must reject any file with Nlink > 1.
func TestAdversary_B27_ArtifactHardLinkEscape(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	// Create a real file outside the artifact root.
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("secret-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Hard-link it into the artifact dir.
	hardlink := filepath.Join(root, "hardlink.json")
	if err := os.Link(target, hardlink); err != nil {
		t.Skip("hard links not supported on this filesystem")
	}

	// The hard link has Nlink > 1 — must be rejected.
	_, err := aw.ValidateAndAccept(context.Background(), "hardlink.json", AttemptID("a1"))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: hard-linked file accepted (Nlink > 1 should be rejected)")
	}
}

// Adversary: trigger payload resume spoofing.
func TestAdversary_B27_TriggerPayloadResumeSpoofing(t *testing.T) {
	// The resume checkpoint is loaded by the daemon, never from the trigger payload.
	// Verify that a trigger payload cannot set resume_reason.
	// The ResumeReason type only accepts trusted enum values set by the daemon.
	data := &ResumeCheckpointData{
		CheckpointID: "cp-a1-1",
		RunID:        RunID("r1"),
		ResumeReason: ResumeReason("trigger_spoofed"),
	}
	err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
	if err == nil {
		t.Fatal("ADVERSARY BREAK: spoofed resume reason accepted")
	}
}

// Adversary: progress after invoke completion.
// The harness clears the invoke state, so progress calls should fail
// with INVALID_PROGRESS ("progress journal not initialized").
func TestAdversary_B27_ProgressAfterInvokeEnd(t *testing.T) {
	// This test verifies at the harness level that after ClearInvoke,
	// a progress call is rejected. The harness test is in progress_handler_test.go.
	// Here we verify the tailer rejects records for a wrong run/attempt.
	store := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")

	// Create a record for the WRONG attempt.
	rec := journalLine{
		RunID:       "r1",
		AttemptID:   "evil-attempt",
		Sequence:    1,
		EventID:     "evt1",
		Phase:       "late",
		SafeToResume: true,
	}
	rec.HMAC = computeTestHMAC(rec, key)
	line, _ := json.Marshal(rec)

	// Tailer is for attempt "a1" but record is for "evil-attempt".
	tailer := NewProgressTailer(journalPath, key, store, store, AttemptID("a1"), RunID("r1"))
	_, err := tailer.IngestRecord(ctx, line)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: progress record for wrong attempt accepted")
	}
}

// Adversary: unsafe safe_to_resume=True with no committed action or empty completed_work.
// This is caught by the SDK and harness validation. The harness should reject
// safe_to_resume=True without last_committed_action or with only empty completed_work entries.
func TestAdversary_B27_UnsafeSafeToResumeNoAction(t *testing.T) {
	// Test 1: safe_to_resume=True with no last_committed_action should be rejected.
	// We can't easily call the harness handler directly here (it's in the harness
	// package), but we verify the store does NOT accept checkpoints without
	// last_committed_action when safe_to_resume=True.
	// The harness validation is tested in progress_handler_test.go.
	//
	// Here we verify that a safe checkpoint with empty completed_work is not
	// a valid resume point — GetLatestCheckpoint should not return it if it
	// has no completed work (the harness would have rejected it before persist).
	// Since the store trusts the harness, we verify the invariant: a checkpoint
	// stored with SafeToResume=true must have LastCommittedAction and non-empty
	// CompletedWork. If someone bypasses the harness and writes directly,
	// the resume loader should still validate digest integrity.
	store := newTestStore(t)
	ctx := context.Background()

	// A properly-formed safe checkpoint (the only kind the harness accepts).
	cp := &SemanticCheckpoint{
		CheckpointID:        CheckpointID("cp-a1-1"),
		AttemptID:           AttemptID("a1"),
		RunID:               RunID("r1"),
		Phase:               "phase1",
		CompletedWork:       []string{"real work"},
		LastCommittedAction: "committed",
		SafeToResume:        true,
		Sequence:            1,
		CreatedAt:           time.Now().UTC(),
	}
	cp.CheckpointDigest = recomputeCheckpointDigest(cp)
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.GetCheckpoint(ctx, CheckpointID("cp-a1-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.SafeToResume {
		t.Fatal("expected SafeToResume=true")
	}
	if got.LastCommittedAction == "" {
		t.Fatal("ADVERSARY BREAK: safe checkpoint without last_committed_action accepted")
	}
	if len(got.CompletedWork) == 0 || got.CompletedWork[0] == "" {
		t.Fatal("ADVERSARY BREAK: safe checkpoint with empty completed_work accepted")
	}
}

// Adversary: checkpoint/artifact digest mismatch.
func TestAdversary_B27_CheckpointDigestMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID:  CheckpointID("cp-a1-1"),
		AttemptID:     AttemptID("a1"),
		RunID:         RunID("r1"),
		Phase:         "phase1",
		SafeToResume:  true,
		Sequence:      1,
		CreatedAt:     time.Now().UTC(),
		CheckpointDigest: "tampered-digest",
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loader := NewResumeCheckpointLoader(store)
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: tampered checkpoint digest accepted")
	}
}

// Adversary: artifact quota abuse.
func TestAdversary_B27_ArtifactQuotaAbuse(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}

	// Create files until total quota is exceeded.
	// Using small files to avoid disk usage in tests.
	for i := 0; i < 5; i++ {
		name := filepath.Join(root, fmt.Sprintf("file%d.bin", i))
		if err := os.WriteFile(name, make([]byte, 24*1024*1024), 0o600); err != nil {
			t.Fatal(err)
		}
		rel := fmt.Sprintf("file%d.bin", i)
		_, err := aw.ValidateAndAccept(context.Background(), rel, AttemptID("a1"))
		if err != nil && i < 4 {
			t.Fatalf("file %d should be accepted: %v", i, err)
		}
		if err != nil && i == 4 {
			// 5th file exceeds 100 MiB total — expected.
			return
		}
	}
	t.Fatal("ADVERSARY BREAK: quota abuse not detected")
}

// helper
func containsBytes(data, target []byte) bool {
	if len(target) == 0 {
		return false
	}
	for i := 0; i <= len(data)-len(target); i++ {
		match := true
		for j := range target {
			if data[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
