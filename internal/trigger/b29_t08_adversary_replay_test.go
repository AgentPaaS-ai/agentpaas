package trigger

// B29-T08 ADVERSARY TEST — replay attack: re-subscribe with an old cursor
// must not create a new run or re-execute.
//
// Attack: an adversary invokes a run, receives the terminal event, then
// re-invokes with the SAME idempotency key (or re-subscribes with an old
// cursor). The adversary expects the second invocation to create a second
// run (duplicate) or re-execute the agent — double-charging, duplicate
// side effects, or replay of stale results.
//
// Invariant under test:
//   - Re-invoking with the same idempotency key returns the SAME run ID
//     (IdempotencyReplayed), not a new run.
//   - The idempotency store holds exactly ONE entry for that key.
//   - Re-subscribing with an old cursor replays events but does NOT create
//     a new run or re-execute.

import (
	"context"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
)

// TestAdversary_B29_ReplaySameIdempotencyKeyDoesNotCreateSecondRun verifies
// that invoking twice with the same idempotency key produces exactly ONE
// run — the second invoke replays the original run, never creating a
// duplicate.
func TestAdversary_B29_ReplaySameIdempotencyKeyDoesNotCreateSecondRun(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	idem, err := NewIdempotencyStore(testIdempotencyFile(t), time.Hour, nil)
	if err != nil {
		t.Fatalf("NewIdempotencyStore: %v", err)
	}
	service := NewTriggerService(nil, DefaultMaxPayload, bus, idem)

	// First invocation — records a run.
	stream1 := &captureInvokeStream{ctx: context.Background()}
	req := &triggerv1.InvokeRequest{
		AgentName:      "test-agent",
		IdempotencyKey: "key-replay-1",
		Payload:        []byte("payload-A"),
	}
	if err := service.InvokeStream(req, stream1); err != nil {
		t.Fatalf("InvokeStream 1: %v", err)
	}
	if len(stream1.responses) == 0 {
		t.Fatal("ADVERSARY BREAK: first InvokeStream returned no responses")
	}
	runID1 := stream1.responses[0].GetRun().GetRunId()
	if runID1 == "" {
		t.Fatal("ADVERSARY BREAK: first run ID is empty")
	}

	// Count entries before replay.
	if got := idem.EntryCount(); got != 1 {
		t.Fatalf("ADVERSARY BREAK: idempotency entries after first invoke = %d; want 1 (one run)", got)
	}

	// Adversary: re-invoke with the SAME idempotency key. The store must
	// return IdempotencyReplayed — same run, not a new run.
	stream2 := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(req, stream2); err != nil {
		t.Fatalf("InvokeStream 2 (replay): %v", err)
	}
	if len(stream2.responses) == 0 {
		t.Fatal("ADVERSARY BREAK: replayed InvokeStream returned no responses")
	}
	runID2 := stream2.responses[0].GetRun().GetRunId()
	if runID2 != runID1 {
		t.Fatalf("ADVERSARY BREAK: replay created a new run %q; want %q (no duplicate runs)", runID2, runID1)
	}

	// The idempotency store must still hold exactly ONE entry — no second
	// entry was created by the replay.
	if got := idem.EntryCount(); got != 1 {
		t.Fatalf("ADVERSARY BREAK: idempotency entries after replay = %d; want 1 (no duplicate)", got)
	}

	// The runStore must contain exactly one entry for that run ID, and it
	// must be SUCCEEDED (terminal) — the replay did not re-execute and
	// flip the status back to PENDING/RUNNING.
	entry, ok := service.runStore.Get(runID1)
	if !ok {
		t.Fatal("ADVERSARY BREAK: run not found in runStore after replay")
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("ADVERSARY BREAK: replayed run status = %s; want SUCCEEDED (no re-execution)", entry.Status)
	}

	// Total runs in the store must be 1 — the replay did not register a
	// new run.
	if got := len(service.runStore.List()); got != 1 {
		t.Fatalf("ADVERSARY BREAK: runStore has %d runs after replay; want 1 (no duplicate)", got)
	}
}

// TestAdversary_B29_ReplayOldCursorReplaysWithoutNewRun verifies that
// re-subscribing with an old cursor replays the committed events but does
// NOT create a new run or re-execute. We drive this through the durable
// store path so the replay is observable.
func TestAdversary_B29_ReplayOldCursorReplaysWithoutNewRun(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()

	// Initial invocation.
	stream1 := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream1); err != nil {
		t.Fatalf("InvokeStream 1: %v", err)
	}
	if len(stream1.responses) != 2 {
		t.Fatalf("ADVERSARY BREAK: first InvokeStream responses = %d; want 2", len(stream1.responses))
	}
	runID := stream1.responses[0].GetRun().GetRunId()
	runsBeforeReplay := len(service.runStore.List())

	// Adversary: re-subscribe with cursor 0 (old cursor). This must replay
	// the two committed events, NOT create a new run.
	ch, err := store.Subscribe(context.Background(), defaultTriggerTenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe replay: %v", err)
	}
	var seqs []int64
	timeout := time.After(2 * time.Second)
	for len(seqs) < 2 {
		select {
		case e, open := <-ch:
			if !open {
				t.Fatalf("ADVERSARY BREAK: replay channel closed after %d events; want 2", len(seqs))
			}
			seqs = append(seqs, e.Sequence)
		case <-timeout:
			t.Fatalf("ADVERSARY BREAK: replay did not deliver 2 events (got %d)", len(seqs))
		}
	}
	if seqs[0] != 1 || seqs[1] != 2 {
		t.Fatalf("ADVERSARY BREAK: replay sequences = %v; want [1, 2] (no new run, original events)", seqs)
	}

	// No new run was created by re-subscribing with the old cursor.
	runsAfterReplay := len(service.runStore.List())
	if runsAfterReplay != runsBeforeReplay {
		t.Fatalf("ADVERSARY BREAK: replay created a new run — before=%d after=%d", runsBeforeReplay, runsAfterReplay)
	}

	// The original run must still be SUCCEEDED (terminal) — replay did
	// not re-execute or flip status.
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatal("ADVERSARY BREAK: original run not found after replay")
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("ADVERSARY BREAK: run status after replay = %s; want SUCCEEDED (no re-execution)", entry.Status)
	}
}
