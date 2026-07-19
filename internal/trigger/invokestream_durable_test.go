package trigger

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// newTestDurableService creates a TriggerService backed by a
// DurableEventStore rooted in a temp dir. Returns the service, the store, and
// a cleanup.
func newTestDurableService(t *testing.T) (*TriggerService, *DurableEventStore, func()) {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	service := NewTriggerService(nil, DefaultMaxPayload, store)
	cleanup := func() {
		_ = store.Close()
	}
	return service, store, cleanup
}

// TestInvokeStreamUsesDurableEventStore verifies that InvokeStream subscribes
// to the durable EventStore when one is provided. The synthetic admission
// path (Created -> Succeeded) still runs, but the events are durable.
func TestInvokeStreamUsesDurableEventStore(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()

	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2", len(stream.responses))
	}
	runID := stream.responses[0].GetRun().GetRunId()
	if runID == "" {
		t.Fatal("runID is empty")
	}

	// The events must be durable in the EventStore. We use a fixed tenant
	// convention (the default tenant for trigger-originated runs).
	events, err := store.Read(context.Background(), defaultTriggerTenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("durable events = %d; want 2 (created + succeeded)", len(events))
	}
	if events[0].Type != string(EventRunCreated) {
		t.Fatalf("event 0 type = %q; want %q", events[0].Type, EventRunCreated)
	}
	if events[1].Type != string(EventRunSucceeded) {
		t.Fatalf("event 1 type = %q; want %q", events[1].Type, EventRunSucceeded)
	}
}

// TestInvokeStreamReconnectReceivesSameRun verifies the durable contract:
// after InvokeStream completes (synthetic success), reconnecting with the
// same runID and cursor 0 replays the same events without creating a new run.
// This is the spec invariant: "Reconnecting with the same invocation and
// cursor never creates another run."
func TestInvokeStreamReconnectReceivesSameRun(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()

	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	runID := stream.responses[0].GetRun().GetRunId()

	// Simulate a reconnect: subscribe to the same run from cursor 0. This
	// must replay the 2 committed events and NOT create a new run.
	ch, err := store.Subscribe(context.Background(), defaultTriggerTenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	var received []port.Event
	timeout := time.After(time.Second)
	for len(received) < 2 {
		select {
		case e, open := <-ch:
			if !open {
				t.Fatalf("channel closed after %d events; want 2", len(received))
			}
			received = append(received, e)
		case <-timeout:
			t.Fatalf("timed out waiting for event %d (got %d)", len(received), 2)
		}
	}
	if received[0].Sequence != 1 || received[1].Sequence != 2 {
		t.Fatalf("replay sequences = [%d, %d]; want [1, 2]", received[0].Sequence, received[1].Sequence)
	}

	// The runStore must still have exactly one run for this runID (no
	// duplicate created on reconnect).
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatal("run not found in runStore after reconnect")
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("run status = %s; want SUCCEEDED", entry.Status)
	}
}

// TestInvokeStreamSurvivesRestart verifies that after constructing a new
// service backed by the same state dir, the previously-streamed run's events
// are still readable. This is the restart-proof contract.
func TestInvokeStreamSurvivesRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")

	// First service: stream a run.
	store1, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore 1: %v", err)
	}
	service1 := NewTriggerService(nil, DefaultMaxPayload, store1)
	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service1.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	runID := stream.responses[0].GetRun().GetRunId()
	if err := store1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	// "Restart" — new store at the same path.
	store2, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore 2: %v", err)
	}
	defer func() { _ = store2.Close() }()

	events, err := store2.Read(context.Background(), defaultTriggerTenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read after restart: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("after restart events = %d; want 2", len(events))
	}
	if events[1].Type != string(EventRunSucceeded) {
		t.Fatalf("event 1 type after restart = %q; want %q", events[1].Type, EventRunSucceeded)
	}
}
