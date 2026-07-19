package trigger

// B29-T01 CHARACTERIZATION TEST — freezes current behavior; B29
// replacement tasks are expected to update or fail these tests.
//
// Observation 2: InvokeStream is synthetic. It does NOT call the
// production admission path — no harness RPC, no agent/gateway
// container started, no real model invocation. The run is immediately
// marked SUCCEEDED without executing any agent logic.

import (
	"context"
	"reflect"
	"testing"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
)

// TestInvokeStreamIsSynthetic_NoProductionPath asserts that InvokeStream
// does not call the production admission path. Specifically:
//  1. TriggerService has no harness field (no harness RPC client).
//  2. The run transitions directly from Created → Succeeded without a
//     Started or Progress event.
//  3. Exactly 2 events are emitted: RunCreated, RunSucceeded (terminal).
//  4. No invokeFunc hook is involved (InvokeStream bypasses it entirely).
func TestInvokeStreamIsSynthetic_NoProductionPath(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)
	stream := &captureInvokeStream{ctx: context.Background()}

	err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	// Assert: exactly 2 events sent to stream.
	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2 (RunCreated + RunSucceeded)", len(stream.responses))
	}

	// Assert: first event is pending (RunCreated).
	if got := stream.responses[0].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("first status = %s; want PENDING (from RunCreated)", got)
	}

	// Assert: second event is succeeded (RunSucceeded) — terminal.
	if got := stream.responses[1].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("second status = %s; want SUCCEEDED (from RunSucceeded)", got)
	}

	// Assert: both responses share the same runID.
	if stream.responses[0].GetRun().GetRunId() == "" {
		t.Fatal("runID is empty")
	}
	if stream.responses[0].GetRun().GetRunId() != stream.responses[1].GetRun().GetRunId() {
		t.Fatalf("runIDs differ: %q vs %q",
			stream.responses[0].GetRun().GetRunId(),
			stream.responses[1].GetRun().GetRunId())
	}

	// Assert: run is in runStore and is SUCCEEDED.
	runID := stream.responses[0].GetRun().GetRunId()
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatalf("run %q not found in runStore", runID)
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("runStore status = %s; want SUCCEEDED", entry.Status)
	}

	// Assert: exactly 2 events in the event bus.
	events := bus.GetEvents(runID)
	if len(events) != 2 {
		t.Fatalf("eventBus events = %d; want 2", len(events))
	}
	if events[0].Type != EventRunCreated {
		t.Fatalf("event 0 type = %s; want run_created", events[0].Type)
	}
	if events[1].Type != EventRunSucceeded {
		t.Fatalf("event 1 type = %s; want run_succeeded", events[1].Type)
	}
}

// TestInvokeStreamNoHarnessField asserts that TriggerService does NOT
// have a harness/client field. This proves InvokeStream does not call
// into the harness RPC server.
func TestInvokeStreamNoHarnessField(t *testing.T) {
	t.Parallel()

	// Reflect on TriggerService to verify no "harness" field exists.
	ts := reflect.TypeOf(TriggerService{})
	for i := 0; i < ts.NumField(); i++ {
		field := ts.Field(i)
		if field.Name == "harness" || field.Name == "Harness" {
			t.Fatalf("TriggerService has a %q field; InvokeStream might call production harness path", field.Name)
		}
	}
	// Also verify no "harnessClient", "rpcClient", or "workload" fields.
	for i := 0; i < ts.NumField(); i++ {
		field := ts.Field(i)
		switch field.Name {
		case "harnessClient", "rpcClient", "workload", "workloadRuntime":
			t.Fatalf("TriggerService has a %q field; stream might touch production path", field.Name)
		}
	}
}

// TestInvokeStreamRunSucceededWithoutExecution asserts that the run
// is marked SUCCEEDED before any real work could possibly happen.
// The events show: RunCreated → RunSucceeded with no RunStarted or
// RunProgress in between. This proves it's synthetic.
func TestInvokeStreamRunSucceededWithoutExecution(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)
	stream := &captureInvokeStream{ctx: context.Background()}

	err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2", len(stream.responses))
	}

	runID := stream.responses[0].GetRun().GetRunId()
	events := bus.GetEvents(runID)
	foundStarted := false
	foundProgress := false
	for _, e := range events {
		if e.Type == EventRunStarted {
			foundStarted = true
		}
		if e.Type == EventRunProgress {
			foundProgress = true
		}
	}
	if foundStarted {
		t.Fatal("found RunStarted event; InvokeStream should not emit RunStarted (synthetic)")
	}
	if foundProgress {
		t.Fatal("found RunProgress event; InvokeStream should not emit RunProgress (synthetic)")
	}
}

// TestInvokeStreamBypassesInvokeFunc proves that invokeFunc is NOT
// called during InvokeStream. We set invokeFunc to a function that
// would panic, then call InvokeStream — it must succeed.
func TestInvokeStreamBypassesInvokeFunc(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)

	// Set invokeFunc to something that would fail if called.
	service.invokeFunc = func(ctx context.Context, agentName string, payload []byte) (string, error) {
		t.Error("invokeFunc was called during InvokeStream — should not happen")
		return "", nil
	}

	stream := &captureInvokeStream{ctx: context.Background()}
	err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2", len(stream.responses))
	}
}

// TestInvokeStreamTerminatesOnRunSucceeded asserts the stream is
// closed after the terminal RunSucceeded event.
func TestInvokeStreamTerminatesOnRunSucceeded(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()

	// Register the run first, then subscribe.
	bus.RegisterRun("will-be-streamed")
	ch, cancel := bus.Subscribe("will-be-streamed", 0)
	defer cancel()

	// Publish synthetic events like InvokeStream does
	bus.Publish("will-be-streamed", EventRunCreated, nil)
	bus.Publish("will-be-streamed", EventRunSucceeded, nil)

	// Read first event (RunCreated)
	event, open := <-ch
	if !open {
		t.Fatal("channel closed before RunCreated")
	}
	if event.Type != EventRunCreated {
		t.Fatalf("expected RunCreated, got %s", event.Type)
	}

	// Read second event (RunSucceeded) — should be followed by channel close
	event, open = <-ch
	if !open {
		t.Fatal("channel closed before RunSucceeded")
	}
	if event.Type != EventRunSucceeded {
		t.Fatalf("expected RunSucceeded, got %s", event.Type)
	}

	// Channel must be closed now (terminal event).
	_, open = <-ch
	if open {
		t.Fatal("channel should be closed after terminal event")
	}
}
