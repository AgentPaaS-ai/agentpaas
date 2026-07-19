package routedrun

import (
	"context"
	"testing"
	"time"
)

func TestLoadResumeCheckpoint_InitialAttemptNoCheckpoint(t *testing.T) {
	store := newTestStore(t)
	loader := NewResumeCheckpointLoader(store)
	ctx := context.Background()

	// No checkpoint exists for this attempt.
	_, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err == nil {
		t.Fatal("expected not-found error for initial attempt")
	}
}

func TestLoadResumeCheckpoint_ReturnsLatestSafeCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create two safe checkpoints.
	for i := 1; i <= 2; i++ {
		cp := &SemanticCheckpoint{
			CheckpointID:    CheckpointID("cp-a1-" + string(rune('0'+i))),
			AttemptID:       AttemptID("a1"),
			RunID:           RunID("r1"),
			WorkflowID:      WorkflowID("wf1"),
			Phase:           "phase" + string(rune('0'+i)),
			CompletedWork:   []string{"work" + string(rune('0'+i))},
			SafeToResume:    true,
			Sequence:        int64(i),
			CreatedAt:       time.Now().UTC(),
		}
		// Compute digest to pass verification.
		cp.CheckpointDigest = recomputeCheckpointDigest(cp)
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	loader := NewResumeCheckpointLoader(store)
	data, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err != nil {
		t.Fatalf("LoadResumeCheckpoint: %v", err)
	}

	// Should return the latest (seq 2).
	if data.Phase != "phase2" {
		t.Fatalf("expected phase2, got %s", data.Phase)
	}
	if data.CheckpointID != "cp-a1-2" {
		t.Fatalf("expected cp-a1-2, got %s", data.CheckpointID)
	}
	if data.ResumeReason != ResumeReasonFailureContinuation {
		t.Fatalf("expected failure_continuation, got %s", data.ResumeReason)
	}
}

func TestLoadResumeCheckpoint_DigestMismatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID:    CheckpointID("cp-a1-1"),
		AttemptID:       AttemptID("a1"),
		RunID:           RunID("r1"),
		Phase:           "phase1",
		SafeToResume:    true,
		Sequence:        1,
		CreatedAt:       time.Now().UTC(),
		// Set wrong digest.
		CheckpointDigest: "deadbeef",
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

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
		CheckpointID:    CheckpointID("cp-a1-1"),
		AttemptID:       AttemptID("a1"),
		RunID:           RunID("r1"),
		Phase:           "phase1",
		SafeToResume:    true,
		Sequence:        1,
		CreatedAt:       time.Now().UTC(),
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
		CheckpointID:    CheckpointID("cp-a1-1"),
		AttemptID:       AttemptID("a1"),
		RunID:           RunID("r1"),
		Phase:           "phase1",
		CompletedWork:   []string{"done"},
		SafeToResume:    true,
		Sequence:        1,
		CreatedAt:       time.Now().UTC(),
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
		t.Fatal("expected run_id mismatch error")
	}
}

func TestValidateResumeCheckpoint_EmptyCheckpointID(t *testing.T) {
	data := &ResumeCheckpointData{
		RunID:       RunID("r1"),
		ResumeReason: ResumeReasonFailureContinuation,
	}
	err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
	if err == nil {
		t.Fatal("expected error for empty checkpoint ID")
	}
}

func TestValidateResumeCheckpoint_InvalidResumeReason(t *testing.T) {
	data := &ResumeCheckpointData{
		CheckpointID: "cp-a1-1",
		RunID:       RunID("r1"),
		ResumeReason: ResumeReason("bogus_reason"),
	}
	err := ValidateResumeCheckpoint(data, RunID("r1"), "", "")
	if err == nil {
		t.Fatal("expected error for invalid resume reason")
	}
}

func TestSerializeDeserializeResumeCheckpoint(t *testing.T) {
	original := &ResumeCheckpointData{
		CheckpointID:     "cp-a1-1",
		SourceAttemptID:  "a1",
		RunID:            RunID("r1"),
		Phase:            "phase1",
		CompletedWork:    []string{"work"},
		ResumeReason:     ResumeReasonFailureContinuation,
	}

	data, err := original.SerializeForHarness()
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	restored, err := DeserializeResumeCheckpoint(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if restored.CheckpointID != original.CheckpointID {
		t.Fatalf("checkpoint ID mismatch")
	}
	if restored.Phase != original.Phase {
		t.Fatalf("phase mismatch")
	}
	if restored.ResumeReason != original.ResumeReason {
		t.Fatalf("resume reason mismatch")
	}
}

func TestResumeReasonValid(t *testing.T) {
	valid := []ResumeReason{
		ResumeReasonFailureContinuation,
		ResumeReasonOperatorPauseResume,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %s to be valid", r)
		}
	}

	invalid := []ResumeReason{
		ResumeReason(""),
		ResumeReason("bogus"),
		ResumeReason("trigger_spoofed"),
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %s to be invalid", r)
		}
	}
}

func TestLoadResumeCheckpoint_EmptyDigestOK(t *testing.T) {
	// A checkpoint with empty digest should pass (backward compat).
	store := newTestStore(t)
	ctx := context.Background()

	cp := &SemanticCheckpoint{
		CheckpointID:    CheckpointID("cp-a1-1"),
		AttemptID:       AttemptID("a1"),
		RunID:           RunID("r1"),
		Phase:           "phase1",
		SafeToResume:    true,
		Sequence:        1,
		CreatedAt:       time.Now().UTC(),
		// No CheckpointDigest set — should be empty.
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	loader := NewResumeCheckpointLoader(store)
	data, err := loader.LoadResumeCheckpoint(ctx, AttemptID("a1"), RunID("r1"), ResumeReasonFailureContinuation)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if data.Phase != "phase1" {
		t.Fatalf("expected phase1, got %s", data.Phase)
	}
}
