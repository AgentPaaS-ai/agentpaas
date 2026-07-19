package harness

import (
	"path/filepath"
	"testing"
	"time"
)

// Test that handleProgress rejects control characters in phase.
func TestHandleProgress_ControlCharsInPhase(t *testing.T) {
	srv, state := setupProgressTestServer(t)

	// Phase with a null byte.
	req := rpcRequest{
		ID:     "1",
		Method: "progress",
		Params: map[string]any{
			"event_id": "evt1",
			"phase":    "ev\x00il",
		},
	}
	resp := srv.handleProgress(req, state)
	if resp.OK {
		t.Fatal("expected rejection of control char in phase")
	}
	if resp.Code != "INVALID_PROGRESS" {
		t.Fatalf("expected INVALID_PROGRESS, got %s", resp.Code)
	}
}

// Test that handleProgress rejects safe_to_resume=True with empty completed_work.
func TestHandleProgress_SafeResumeEmptyCompletedWork(t *testing.T) {
	srv, state := setupProgressTestServer(t)

	req := rpcRequest{
		ID:     "2",
		Method: "progress",
		Params: map[string]any{
			"event_id":            "evt2",
			"phase":               "phase1",
			"completed_work":      []any{""}, // empty string entry
			"last_committed_action": "committed",
			"safe_to_resume":      true,
		},
	}
	resp := srv.handleProgress(req, state)
	if resp.OK {
		t.Fatal("expected rejection of safe_to_resume with empty completed_work")
	}
	if resp.Code != "INVALID_PROGRESS" {
		t.Fatalf("expected INVALID_PROGRESS, got %s", resp.Code)
	}
}

// Test that handleProgress rejects secret sentinels in checkpoint content.
func TestHandleProgress_SecretSentinelRejection(t *testing.T) {
	srv, state := setupProgressTestServer(t)

	req := rpcRequest{
		ID:     "3",
		Method: "progress",
		Params: map[string]any{
			"event_id":            "evt3",
			"phase":               "phase1",
			"completed_work":      []any{"sk-or-v1-fake-key"},
			"last_committed_action": "committed",
			"safe_to_resume":      true,
		},
	}
	resp := srv.handleProgress(req, state)
	if resp.OK {
		t.Fatal("expected rejection of secret sentinel in checkpoint content")
	}
	if resp.Code != "CHECKPOINT_REJECTED" {
		t.Fatalf("expected CHECKPOINT_REJECTED, got %s", resp.Code)
	}
}

// Test that handleProgress rejects progress when journal is not initialized.
func TestHandleProgress_JournalNotInitialized(t *testing.T) {
	srv := &harnessRPCServer{}
	state := &rpcInvokeState{} // no journal set

	req := rpcRequest{
		ID:     "4",
		Method: "progress",
		Params: map[string]any{
			"event_id": "evt4",
			"phase":    "phase1",
		},
	}
	resp := srv.handleProgress(req, state)
	if resp.OK {
		t.Fatal("expected rejection when journal not initialized")
	}
	if resp.Code != "INVALID_PROGRESS" {
		t.Fatalf("expected INVALID_PROGRESS, got %s", resp.Code)
	}
}

// Test that handleProgress rejects progress when lease expired.
func TestHandleProgress_LeaseExpired(t *testing.T) {
	srv, state := setupProgressTestServer(t)

	state.leaseExpired.Store(true)

	req := rpcRequest{
		ID:     "5",
		Method: "progress",
		Params: map[string]any{
			"event_id": "evt5",
			"phase":    "phase1",
		},
	}
	resp := srv.handleProgress(req, state)
	if resp.OK {
		t.Fatal("expected rejection when lease expired")
	}
	if resp.Code != "LEASE_EXPIRED" {
		t.Fatalf("expected LEASE_EXPIRED, got %s", resp.Code)
	}
}

// Test that handleProgress returns resume_checkpoint when set on invoke state.
func TestHandleProgress_ResumeCheckpointReturned(t *testing.T) {
	srv, state := setupProgressTestServer(t)

	// Set resume data on the invoke state.
	state.resumeCheckpoint = map[string]any{
		"checkpoint_id": "cp-prev-1",
		"phase":         "previous_phase",
	}
	state.resumeReason = "failure_continuation"

	req := rpcRequest{
		ID:     "6",
		Method: "progress",
		Params: map[string]any{
			"event_id": "evt6",
			"phase":    "starting",
		},
	}
	resp := srv.handleProgress(req, state)
	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	result, ok := resp.Result.(progressResponse)
	if !ok {
		t.Fatalf("expected progressResponse, got %T", resp.Result)
	}
	if result.ResumeCheckpoint == nil {
		t.Fatal("expected resume_checkpoint in response")
	}
	if result.ResumeReason != "failure_continuation" {
		t.Fatalf("expected resume_reason 'failure_continuation', got %q", result.ResumeReason)
	}
}

// setupProgressTestServer creates a harness RPC server with a journal writer
// and invoke state ready for progress handler tests.
func setupProgressTestServer(t *testing.T) (*harnessRPCServer, *rpcInvokeState) {
	t.Helper()

	dir := t.TempDir()
	journalPath := filepath.Join(dir, "j.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{
		WorkflowID:  "wf1",
		NodeID:      "node1",
		RunID:       "r1",
		AttemptID:   "a1",
		LeaseExpiry: time.Now().Add(time.Hour),
	}
	jw, err := newProgressJournalWriter(journalPath, key, identity)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	t.Cleanup(func() { _ = jw.close() })

	srv := &harnessRPCServer{}
	state := &rpcInvokeState{
		progressJournal:  jw,
		progressIdentity: identity,
	}
	return srv, state
}
