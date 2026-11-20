package routedrun

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadResumeCheckpoint_Basic(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		SafeToResume: true,
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
	}
	cp.CheckpointDigest = recomputeCheckpointDigest(cp)
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	loader := NewResumeCheckpointLoader(store)
	data, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if data.ResumeReason != ResumeReasonFailureContinuation {
		t.Fatalf("expected failure_continuation, got %s", data.ResumeReason)
	}
}

func TestLoadResumeCheckpoint_DigestMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		SafeToResume: true,
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	// Tamper with the checkpoint file on disk: overwrite digest with a bad value.
	tamperDigest(t, store, CheckpointID("cp-a1-1"), "deadbeef")

	loader := NewResumeCheckpointLoader(store)
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}

func TestLoadResumeCheckpoint_RunIDMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		SafeToResume: true,
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
	}
	cp.CheckpointDigest = recomputeCheckpointDigest(cp)
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	loader := NewResumeCheckpointLoader(store)
	// Request with wrong run ID.
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("wrong"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("expected run_id mismatch error")
	}
}

func TestLoadResumeCheckpoint_OperatorPauseResume(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID:  CheckpointID("cp-a1-1"),
		AttemptID:     AttemptID("a1"),
		RunID:         RunID("r1"),
		Phase:         "phase1",
		CompletedWork: []string{"done"},
		SafeToResume:  true,
		Sequence:      1,
		CreatedAt:     time.Now().UTC(),
	}
	cp.CheckpointDigest = recomputeCheckpointDigest(cp)
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	loader := NewResumeCheckpointLoader(store)
	data, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonOperatorPauseResume)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if data.ResumeReason != ResumeReasonOperatorPauseResume {
		t.Fatalf("expected operator_pause_resume, got %s", data.ResumeReason)
	}
}

func TestValidateResumeCheckpoint_Valid(t *testing.T) {
	data := &ResumeCheckpointData{
		CheckpointID: "cp-a1-1",
		RunID:       RunID("r1"),
		ResumeReason: ResumeReasonFailureContinuation,
	}
	err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestValidateResumeCheckpoint_RunIDMismatch(t *testing.T) {
	data := &ResumeCheckpointData{
		CheckpointID: "cp-a1-1",
		RunID:       RunID("r1"),
		ResumeReason: ResumeReasonFailureContinuation,
	}
	err := ValidateResumeCheckpoint(data, RunID("wrong"), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResumeCheckpoint_EmptyRunID(t *testing.T) {
	data := &ResumeCheckpointData{
		CheckpointID: "cp-a1-1",
		RunID:       RunID(""),
		ResumeReason: ResumeReasonFailureContinuation,
	}
	err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResumeCheckpoint_InvalidResumeReason(t *testing.T) {
	invalidReasons := []ResumeReason{
		"",
		"invalid_reason",
		"hack",
	}
	for _, r := range invalidReasons {
		data := &ResumeCheckpointData{
			CheckpointID: "cp-a1-1",
			RunID:       RunID("r1"),
			ResumeReason: r,
		}
		err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
		if err == nil {
			t.Errorf("expected %s to be invalid", r)
		}
	}
}

func TestLoadResumeCheckpoint_SafeCheckpointEmptyDigestRejected(t *testing.T) {
	// B27 safe checkpoint (SafeToResume=true) must have a digest.
	// An empty digest on a safe checkpoint is a missing integrity proof.
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-safe"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		SafeToResume: true,
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
		// No CheckpointDigest -- will be auto-computed by SaveCheckpoint.
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	// Tamper with the checkpoint file on disk: clear the digest to simulate
	// a corrupted checkpoint with a missing integrity proof.
	tamperDigest(t, store, CheckpointID("cp-a1-safe"), "")

	loader := NewResumeCheckpointLoader(store)
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("expected error for safe checkpoint with empty digest")
	}
}

func TestLoadResumeCheckpoint_EmptyDigestOK(t *testing.T) {
	// A B26-era heartbeat (SafeToResume=false) with empty digest is simply
	// not returned by GetLatestCheckpoint (which only returns safe checkpoints).
	// This is backward compat: old heartbeats are ignored, not errors.
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID: CheckpointID("cp-a1-1"),
		AttemptID:    AttemptID("a1"),
		RunID:        RunID("r1"),
		Phase:        "phase1",
		SafeToResume: false, // heartbeat, not a safe checkpoint
		Sequence:     1,
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	loader := NewResumeCheckpointLoader(store)
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("expected error: no safe checkpoint for heartbeat-only attempt")
	}
	// Verify it's a "not found" error, not a digest error.
	if !strings.Contains(err.Error(), "no safe checkpoint") {
		t.Fatalf("expected 'no safe checkpoint' error, got: %v", err)
	}
}

// tamperDigest reads the checkpoint file from disk, modifies the
// checkpoint_digest field, and writes it back. Used to simulate disk tampering.
func tamperDigest(t *testing.T, store *LocalStore, cpID CheckpointID, newDigest string) {
	t.Helper()
	cpPath := store.checkpointPath(cpID)
	data, err := os.ReadFile(cpPath)
	if err != nil {
		t.Fatalf("tamperDigest: read checkpoint: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("tamperDigest: unmarshal: %v", err)
	}
	m["checkpoint_digest"] = newDigest
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("tamperDigest: marshal: %v", err)
	}
	if err := os.WriteFile(cpPath, out, 0o600); err != nil {
		t.Fatalf("tamperDigest: write: %v", err)
	}
}
