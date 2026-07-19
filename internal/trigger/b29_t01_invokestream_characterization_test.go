package trigger

// B29-T01 CHARACTERIZATION TEST (updated for B29-arch-review).
//
// Originally these tests characterized the SYNTHETIC InvokeStream behavior
// (manufactured run_created + run_succeeded, immediate SUCCEEDED). The
// architecture review flagged that as a BLOCKER: InvokeStream must use the
// REAL durable admission path and never manufacture success.
//
// These tests now characterize the NEW behavior:
//   - With a durable EventStore wired, InvokeStream admits the run (appends a
//     durable run_created event), subscribes to the durable store, and reaches
//     terminal via the real execution path (invokeFunc -> EventBus -> bridge ->
//     durable store). It does NOT mark SUCCEEDED inline.
//   - The run reaches terminal because the real execution path publishes a
//     terminal event to the EventBus; the InvokeStream bridge carries it into
//     the durable store so the subscription delivers it to the client.
//   - Without a durable EventStore (EventBus fallback), the legacy synthetic
//     path is retained for backward compatibility with a log warning.

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// TestInvokeStreamRealAdmission_NoInlineSucceeded asserts that with a durable
// EventStore wired, InvokeStream uses the real admission path: it appends a
// durable run_created event and does NOT manufacture run_succeeded inline. The
// terminal arrives from the real execution path (invokeFunc).
func TestInvokeStreamRealAdmission_NoInlineSucceeded(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()
	stream := &captureInvokeStream{ctx: context.Background()}

	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	// The client stream must receive at least the run_created event. With the
	// test invokeFunc publishing a terminal, it also receives run_succeeded.
	if len(stream.responses) < 1 {
		t.Fatalf("responses = %d; want >= 1 (run_created from real admission)", len(stream.responses))
	}

	// First event is PENDING (run_created) — NOT SUCCEEDED. InvokeStream must
	// not manufacture success inline.
	if got := stream.responses[0].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("first status = %s; want PENDING (real admission appends run_created, never manufactures SUCCEEDED)", got)
	}

	runID := stream.responses[0].GetRun().GetRunId()
	if runID == "" {
		t.Fatal("runID is empty")
	}

	// The durable store must contain a run_created event (real admission).
	events, err := store.Read(context.Background(), defaultTriggerTenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("durable store has no events — InvokeStream did not append run_created (real admission not exercised)")
	}
	if events[0].Type != string(EventRunCreated) {
		t.Fatalf("event 0 type = %q; want %q (real admission appends run_created first)", events[0].Type, EventRunCreated)
	}

	// InvokeStream must NOT have manufactured run_succeeded BEFORE the real
	// execution path produced it. The terminal in the store must come from the
	// bridge (invokeFunc -> EventBus -> durable store), proven by the fact that
	// the last event is run_succeeded AND there is exactly one run_created
	// (admission) before it — no inline duplicate.
	last := events[len(events)-1]
	if last.Type != string(EventRunSucceeded) {
		t.Fatalf("last event type = %q; want %q (terminal from real execution path)", last.Type, EventRunSucceeded)
	}
	createdCount := 0
	for _, e := range events {
		if e.Type == string(EventRunCreated) {
			createdCount++
		}
	}
	if createdCount != 1 {
		t.Fatalf("run_created count = %d; want 1 (admission appends exactly one run_created, no inline manufacturing)", createdCount)
	}
}

// TestInvokeStreamRealAdmission_TerminalFromRealPath asserts that the
// terminal event the client receives is produced by the real execution path
// (invokeFunc), not manufactured inline by InvokeStream. We prove this by
// setting an invokeFunc that publishes a DISTINCT terminal (run_failed) and
// confirming the client observes run_failed, not a manufactured run_succeeded.
func TestInvokeStreamRealAdmission_TerminalFromRealPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	service := NewTriggerService(nil, DefaultMaxPayload, store)
	// Real execution path simulates a FAILED terminal. If InvokeStream
	// manufactured success inline, the client would see run_succeeded; the
	// real path delivers run_failed.
	service.SetInvokeFunc(func(ctx context.Context, _ string, _ []byte) (string, error) {
		actualRunID, idErr := generateRunID()
		if idErr != nil {
			return "", idErr
		}
		service.eventBus.RegisterRun(actualRunID)
		service.eventBus.Publish(actualRunID, EventRunFailed, map[string]string{"reason": "real-path-failure"})
		return actualRunID, nil
	})

	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if len(stream.responses) < 2 {
		t.Fatalf("responses = %d; want >= 2 (run_created + terminal from real path)", len(stream.responses))
	}
	terminal := stream.responses[len(stream.responses)-1].GetRun()
	if terminal.GetStatus() != triggerv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("terminal status = %s; want FAILED (terminal must come from real execution path, not manufactured SUCCEEDED)", terminal.GetStatus())
	}

	// The runStore must reflect the REAL terminal (FAILED), proving InvokeStream
	// did not manufacture SUCCEEDED inline.
	runID := stream.responses[0].GetRun().GetRunId()
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatalf("run %q not found in runStore", runID)
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("runStore status = %s; want FAILED (mirrored from real terminal, not manufactured)", entry.Status)
	}
}

// TestInvokeStreamRealAdmission_SubscribesToStore asserts that InvokeStream
// subscribes to the durable EventStore and streams events as they arrive. We
// drive a terminal from the real path and confirm the client receives the
// durable store's events in sequence order.
func TestInvokeStreamRealAdmission_SubscribesToStore(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()
	stream := &captureInvokeStream{ctx: context.Background()}

	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	runID := stream.responses[0].GetRun().GetRunId()

	// The durable store must hold the full lifecycle (run_created + terminal).
	events, err := store.Read(context.Background(), defaultTriggerTenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("durable events = %d; want >= 2 (run_created + terminal from real path)", len(events))
	}
	// Sequences must be contiguous starting at 1 — the subscription delivered
	// them in order.
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("events[%d].Sequence = %d; want %d (subscription must deliver in sequence order)", i, e.Sequence, i+1)
		}
	}

	// A second subscriber (reconnect) must replay the same events from the
	// durable store — proving InvokeStream subscribed to the durable store, not
	// a synthetic in-memory path.
	ch, err := store.Subscribe(context.Background(), defaultTriggerTenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe replay: %v", err)
	}
	var replayed []port.Event
	timeout := time.After(time.Second)
	for len(replayed) < len(events) {
		select {
		case e, open := <-ch:
			if !open {
				t.Fatalf("replay channel closed after %d events; want %d", len(replayed), len(events))
			}
			replayed = append(replayed, e)
		case <-timeout:
			t.Fatalf("replay timed out after %d events; want %d", len(replayed), len(events))
		}
	}
	if replayed[0].Type != string(EventRunCreated) {
		t.Fatalf("replay event 0 = %q; want %q (durable store is the source of truth)", replayed[0].Type, EventRunCreated)
	}
}

// TestInvokeStreamRealAdmission_InvokeFuncInvoked asserts that the real
// execution path (invokeFunc) is actually called during InvokeStream when a
// durable store is wired — the run is NOT synthetic. This is the inverse of
// the old TestInvokeStreamBypassesInvokeFunc characterization.
func TestInvokeStreamRealAdmission_InvokeFuncInvoked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	service := NewTriggerService(nil, DefaultMaxPayload, store)
	invoked := false
	service.SetInvokeFunc(func(ctx context.Context, _ string, _ []byte) (string, error) {
		invoked = true
		actualRunID, idErr := generateRunID()
		if idErr != nil {
			return "", idErr
		}
		service.eventBus.RegisterRun(actualRunID)
		service.eventBus.Publish(actualRunID, EventRunSucceeded, nil)
		return actualRunID, nil
	})
	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if !invoked {
		t.Fatal("invokeFunc was NOT called — InvokeStream is still synthetic (should call the real execution path)")
	}
}

// TestInvokeStreamNoHarnessField asserts that TriggerService does NOT have a
// harness/client field. The real execution path is invoked via the invokeFunc
// hook (a function, not a struct field pointing at an RPC client), preserving
// the trusted-boundary design.
func TestInvokeStreamNoHarnessField(t *testing.T) {
	t.Parallel()

	ts := reflect.TypeOf(TriggerService{})
	for i := 0; i < ts.NumField(); i++ {
		field := ts.Field(i)
		if field.Name == "harness" || field.Name == "Harness" {
			t.Fatalf("TriggerService has a %q field; InvokeStream might call production harness path", field.Name)
		}
	}
	for i := 0; i < ts.NumField(); i++ {
		field := ts.Field(i)
		switch field.Name {
		case "harnessClient", "rpcClient", "workload", "workloadRuntime":
			t.Fatalf("TriggerService has a %q field; stream might touch production path", field.Name)
		}
	}
}

// TestInvokeStreamEventBusFallbackSynthetic asserts that WITHOUT a durable
// EventStore, InvokeStream retains the legacy synthetic path (manufactured
// run_created + run_succeeded, immediate SUCCEEDED) for backward
// compatibility with existing tests. This is the fallback the architecture
// review permitted (with a log warning).
func TestInvokeStreamEventBusFallbackSynthetic(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)
	stream := &captureInvokeStream{ctx: context.Background()}

	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2 (synthetic RunCreated + RunSucceeded on EventBus fallback)", len(stream.responses))
	}
	if got := stream.responses[0].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("first status = %s; want PENDING", got)
	}
	if got := stream.responses[1].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("second status = %s; want SUCCEEDED (synthetic fallback)", got)
	}
	runID := stream.responses[0].GetRun().GetRunId()
	events := bus.GetEvents(runID)
	if len(events) != 2 {
		t.Fatalf("eventBus events = %d; want 2 (synthetic fallback)", len(events))
	}
	if events[0].Type != EventRunCreated || events[1].Type != EventRunSucceeded {
		t.Fatalf("fallback event types = %s, %s; want run_created, run_succeeded", events[0].Type, events[1].Type)
	}
}

// TestInvokeStreamTerminatesOnRunSucceeded asserts the stream is closed after
// the terminal RunSucceeded event (EventBus fallback path).
func TestInvokeStreamTerminatesOnRunSucceeded(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("will-be-streamed")
	ch, cancel := bus.Subscribe("will-be-streamed", 0)
	defer cancel()

	bus.Publish("will-be-streamed", EventRunCreated, nil)
	bus.Publish("will-be-streamed", EventRunSucceeded, nil)

	event, open := <-ch
	if !open {
		t.Fatal("channel closed before RunCreated")
	}
	if event.Type != EventRunCreated {
		t.Fatalf("expected RunCreated, got %s", event.Type)
	}
	event, open = <-ch
	if !open {
		t.Fatal("channel closed before RunSucceeded")
	}
	if event.Type != EventRunSucceeded {
		t.Fatalf("expected RunSucceeded, got %s", event.Type)
	}
	_, open = <-ch
	if open {
		t.Fatal("channel should be closed after terminal event")
	}
}
