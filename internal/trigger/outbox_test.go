package trigger

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// fakeStateStore is a minimal port.TransactionalStateStore used by outbox
// tests. It records CasRun calls and can be configured to fail on the Nth
// call to simulate a rollback.
type fakeStateStore struct {
	mu             sync.Mutex
	casRunCalls    []port.RunState
	casRunErr      error
	casRunFailOn   int // 1-based; if >0, the Nth CasRun call returns casRunErr
	casRunCallCount int
}

func (f *fakeStateStore) CasDeployment(_ context.Context, v port.DeploymentState, _ int64) error {
	return nil
}
func (f *fakeStateStore) GetDeployment(context.Context, string, string) (*port.DeploymentState, error) {
	return nil, nil
}
func (f *fakeStateStore) CasRun(_ context.Context, v port.RunState, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.casRunCallCount++
	f.casRunCalls = append(f.casRunCalls, v)
	if f.casRunFailOn > 0 && f.casRunCallCount == f.casRunFailOn {
		return f.casRunErr
	}
	return nil
}
func (f *fakeStateStore) GetRun(context.Context, string, string) (*port.RunState, error) {
	return nil, nil
}
func (f *fakeStateStore) CasAttempt(_ context.Context, v port.AttemptState, _ int64) error { return nil }
func (f *fakeStateStore) GetAttempt(context.Context, string, string) (*port.AttemptState, error) {
	return nil, nil
}
func (f *fakeStateStore) CasWorkflow(_ context.Context, v port.WorkflowState, _ int64) error {
	return nil
}
func (f *fakeStateStore) GetWorkflow(context.Context, string, string) (*port.WorkflowState, error) {
	return nil, nil
}
func (f *fakeStateStore) ListDeployments(context.Context, string) ([]*port.DeploymentState, error) {
	return nil, nil
}
func (f *fakeStateStore) ListRuns(context.Context, string, string) ([]*port.RunState, error) {
	return nil, nil
}

// TestOutboxCommitWithEventAtomic commits both the state mutation and the
// event. After commit, the event is readable from the EventStore and the
// state CasRun was called.
func TestOutboxCommitWithEventAtomic(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	state := &fakeStateStore{}
	outbox := NewOutbox(state, store)

	ctx := context.Background()
	tenant := "tenant-ob"
	runID := "run-ob"
	runState := port.RunState{
		TenantID: tenant,
		RunID:    runID,
		Status:   "running",
	}
	event := port.Event{
		TenantID: tenant,
		RunID:    runID,
		Type:     "run_started",
		Payload:  []byte("payload"),
	}

	seq, err := outbox.CommitWithEvent(ctx, runState, event)
	if err != nil {
		t.Fatalf("CommitWithEvent: %v", err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d; want 1", seq)
	}

	// State CasRun was called exactly once.
	if len(state.casRunCalls) != 1 {
		t.Fatalf("CasRun calls = %d; want 1", len(state.casRunCalls))
	}
	if state.casRunCalls[0].Status != "running" {
		t.Fatalf("CasRun state = %q; want %q", state.casRunCalls[0].Status, "running")
	}

	// Event is durable in the EventStore.
	events, err := store.Read(ctx, tenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d; want 1", len(events))
	}
	if events[0].Type != "run_started" {
		t.Fatalf("event type = %q; want %q", events[0].Type, "run_started")
	}
}

// TestOutboxRollbackOnStateFailure verifies that if the state CasRun fails,
// the event is NOT appended to the EventStore (both roll back atomically).
func TestOutboxRollbackOnStateFailure(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	stateErr := errors.New("cas conflict")
	state := &fakeStateStore{
		casRunErr:    stateErr,
		casRunFailOn: 1,
	}
	outbox := NewOutbox(state, store)

	ctx := context.Background()
	tenant := "tenant-rb"
	runID := "run-rb"
	runState := port.RunState{
		TenantID: tenant,
		RunID:    runID,
		Status:   "running",
	}
	event := port.Event{
		TenantID: tenant,
		RunID:    runID,
		Type:     "run_started",
		Payload:  []byte("payload"),
	}

	_, err := outbox.CommitWithEvent(ctx, runState, event)
	if err == nil {
		t.Fatal("CommitWithEvent returned nil error; want cas conflict")
	}
	if !errors.Is(err, stateErr) {
		t.Fatalf("err = %v; want to wrap %v", err, stateErr)
	}

	// Event must NOT be in the EventStore — rollback succeeded.
	events, err := store.Read(ctx, tenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %d; want 0 (rollback should prevent the event)", len(events))
	}
	latest, err := store.LatestSequence(ctx, tenant, runID)
	if err != nil {
		t.Fatalf("LatestSequence: %v", err)
	}
	if latest != 0 {
		t.Fatalf("latest = %d; want 0 (no event after rollback)", latest)
	}
}

// TestOutboxRollbackOnEventAppendFailure verifies that if the event Append
// fails (simulated by closing the store), the state mutation is considered
// uncommitted. Because the state store is a fake that records calls, we
// verify the CasRun happened but the event did not land; the caller sees the
// append error.
func TestOutboxRollbackOnEventAppendFailure(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	state := &fakeStateStore{}
	outbox := NewOutbox(state, store)

	ctx := context.Background()
	tenant := "tenant-eb"
	runID := "run-eb"
	runState := port.RunState{
		TenantID: tenant,
		RunID:    runID,
		Status:   "running",
	}
	event := port.Event{
		TenantID: tenant,
		RunID:    runID,
		Type:     "run_started",
		Payload:  []byte("payload"),
	}

	// Close the event store so Append returns ErrEventStoreClosed. The outbox
	// must surface that error and the state mutation is effectively rolled
	// back (in a real 2PC store the prepare would be undone; here the fake
	// state store records the attempt but the caller sees the failure and
	// must not treat the run as committed).
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := outbox.CommitWithEvent(ctx, runState, event)
	if err == nil {
		t.Fatal("CommitWithEvent returned nil error; want event store closed")
	}

	// State CasRun was attempted (prepare phase).
	if len(state.casRunCalls) != 1 {
		t.Fatalf("CasRun calls = %d; want 1 (prepare attempted)", len(state.casRunCalls))
	}

	// No event landed because the store is closed. We cannot Read from a
	// closed store, so just verify LatestSequence is 0 (it returns 0 for a
	// closed store or an unknown run).
	latest, _ := store.LatestSequence(ctx, tenant, runID)
	if latest != 0 {
		t.Fatalf("latest = %d; want 0 (no event landed)", latest)
	}
}
