package delegation

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper for error assertions
// ---------------------------------------------------------------------------

func assertError(t *testing.T, wantContains string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantContains)
	}
	if !stringContains(err.Error(), wantContains) {
		t.Fatalf("expected error containing %q, got: %v", wantContains, err)
	}
}

func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 1. CommitTerminal denied if non-transition
// ---------------------------------------------------------------------------

func TestTaskOutbox_CommitTerminal_DeniedNonTransition(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusAdmitted // non-terminal
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	outbox := NewTaskOutbox(store)

	// Try to commit a terminal event — but the task is non-terminal,
	// and we're asking to move to SUCCEEDED which IS allowed from ADMITTED->SUCCEEDED?
	// Wait — ADMITTED can only go to RUNNING, CANCELLED, EXPIRED, DENIED.
	// So ADMITTED->SUCCEEDED is an illegal transition.
	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded

	ev := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskSucceeded,
		CreatedAt:  time.Now().UTC(),
	}

	_, err := outbox.CommitTerminal(ctx, task, 0, &result, ev)
	assertError(t, "invalid state transition", err)

	// Also test: task already terminal → cannot transition to another terminal
	task2 := makeValidTask()
	task2.IdempotencyKey = "idem-test-2"
	task2.Status = TaskStatusSucceeded // already terminal
	if err := store.CreateTask(ctx, task2); err != nil {
		t.Fatalf("CreateTask task2: %v", err)
	}
	result2 := makeValidResult(task2.TaskID)
	result2.Status = TaskStatusFailed // different terminal

	ev2 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task2.TaskID,
		WorkflowID: task2.WorkflowID,
		TenantID:   task2.TenantID,
		Type:       EventTaskFailed,
		CreatedAt:  time.Now().UTC(),
	}

	// Succeeded->Failed should fail (non-transition from terminal)
	_, err = outbox.CommitTerminal(ctx, task2, 0, &result2, ev2)
	assertError(t, "invalid state transition", err)
}

// ---------------------------------------------------------------------------
// 2. Event sequence contiguous
// ---------------------------------------------------------------------------

func TestTaskOutbox_EventSequenceContiguous(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create task
	task := makeValidTask()
	task.Status = TaskStatusAdmitted
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Append a non-terminal event first (simulating lifecycle)
	ev1 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskStarted,
		CreatedAt:  time.Now().UTC(),
	}
	seq1, err := store.AppendEvent(ctx, ev1)
	if err != nil {
		t.Fatalf("AppendEvent ev1: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("expected seq=1, got %d", seq1)
	}

	// Now advance task to running, then commit terminal
	task.Status = TaskStatusRunning
	if err := store.CASTask(ctx, task, 0); err != nil {
		t.Fatalf("CASTask to RUNNING: %v", err)
	}

	outbox := NewTaskOutbox(store)
	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded

	ev2 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskSucceeded,
		CreatedAt:  time.Now().UTC(),
	}

	task.Generation = 1 // after CASTask
	_, err = outbox.CommitTerminal(ctx, task, 1, &result, ev2)
	if err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	// Verify sequences are {1, 2}
	events, err := store.ListEvents(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Sequence != 1 {
		t.Errorf("event[0].seq = %d, want 1", events[0].Sequence)
	}
	if events[1].Sequence != 2 {
		t.Errorf("event[1].seq = %d, want 2", events[1].Sequence)
	}
}

// ---------------------------------------------------------------------------
// 3. Waiter wakes on terminal
// ---------------------------------------------------------------------------

func TestTaskWaiter_WakesOnTerminal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusRunning
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	waiter := NewTaskWaiter(store)

	// Start a goroutine that commits terminal after a short delay.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		outbox := NewTaskOutbox(store)
		result := makeValidResult(task.TaskID)
		result.Status = TaskStatusSucceeded

		ev := TaskEvent{
			EventID:    makeEventID(),
			TaskID:     task.TaskID,
			WorkflowID: task.WorkflowID,
			TenantID:   task.TenantID,
			Type:       EventTaskSucceeded,
			CreatedAt:  time.Now().UTC(),
		}

		if _, err := outbox.CommitTerminal(ctx, task, 0, &result, ev); err != nil {
			t.Errorf("CommitTerminal in goroutine: %v", err)
		}
	}()

	// Block waiting for terminal.
	gotTask, events, err := waiter.WaitTerminal(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("WaitTerminal: %v", err)
	}
	wg.Wait()

	if !gotTask.Status.IsTerminal() {
		t.Errorf("expected terminal task, got status=%s", gotTask.Status)
	}
	if gotTask.Status != TaskStatusSucceeded {
		t.Errorf("expected SUCCEEDED, got %s", gotTask.Status)
	}

	// Events should include the terminal event.
	foundTerminal := false
	for _, e := range events {
		if e.Type == EventTaskSucceeded {
			foundTerminal = true
			break
		}
	}
	if !foundTerminal {
		t.Error("expected TASK_SUCCEEDED event in results")
	}
}

// ---------------------------------------------------------------------------
// 4. Disconnect: cancel ctx, resubscribe after_sequence, no duplicate
// ---------------------------------------------------------------------------

func TestTaskWaiter_DisconnectResubscribe_NoDuplicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusRunning
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	waiter := NewTaskWaiter(store)

	// Start the waiter in a goroutine.
	type result struct {
		task   Task
		events []TaskEvent
		err    error
	}
	resultCh := make(chan result, 1)

	go func() {
		gotTask, events, err := waiter.WaitTerminal(ctx, task.TaskID, 0)
		resultCh <- result{task: gotTask, events: events, err: err}
	}()

	// Emit a non-terminal event.
	ev1 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskStarted,
		CreatedAt:  time.Now().UTC(),
	}
	seq1, err := store.AppendEvent(ctx, ev1)
	if err != nil {
		t.Fatalf("AppendEvent ev1: %v", err)
	}

	// Let the waiter process it, then disconnect.
	time.Sleep(30 * time.Millisecond)
	cancel()

	// Wait for the goroutine to exit (should get context.Canceled).
	res := <-resultCh
	if res.err == nil {
		t.Error("expected context.Canceled error after disconnect")
	}

	// Resume with after_sequence = the last event we saw (seq1).
	ctx2 := context.Background()
	// Now commit terminal.
	outbox := NewTaskOutbox(store)
	resultObj := makeValidResult(task.TaskID)
	resultObj.Status = TaskStatusSucceeded

	ev2 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskSucceeded,
		CreatedAt:  time.Now().UTC(),
	}

	if _, err := outbox.CommitTerminal(ctx2, task, 0, &resultObj, ev2); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	// Resubscribe after seq1 — should get only event with seq2.
	gotTask2, events2, err := waiter.WaitTerminal(ctx2, task.TaskID, seq1)
	if err != nil {
		t.Fatalf("WaitTerminal after reconnect: %v", err)
	}
	if gotTask2.Status != TaskStatusSucceeded {
		t.Errorf("expected SUCCEEDED, got %s", gotTask2.Status)
	}

	// Dedup check: we should have exactly 1 event (seq2), not duplicates.
	if len(events2) != 1 {
		t.Errorf("expected 1 event after resubscribe, got %d", len(events2))
	}
	if len(events2) > 0 && events2[0].Sequence != seq1+1 {
		t.Errorf("expected seq=%d, got %d", seq1+1, events2[0].Sequence)
	}
}

// ---------------------------------------------------------------------------
// 5. Duplicate AppendEvent same sequence rejected (idempotent by EventID)
// ---------------------------------------------------------------------------

func TestMemoryStore_AppendEvent_DuplicateEventIDRejected(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	eid := makeEventID()
	ev := TaskEvent{
		EventID:    eid,
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}

	seq1, err := store.AppendEvent(ctx, ev)
	if err != nil {
		t.Fatalf("AppendEvent first: %v", err)
	}
	if seq1 != 1 {
		t.Errorf("expected seq=1, got %d", seq1)
	}

	// Same event ID again → should be rejected (or idempotent, returning existing seq?)
	// The spec says "rejected" — let's verify it returns an error.
	ev2 := TaskEvent{
		EventID:    eid, // same EventID
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}

	_, err = store.AppendEvent(ctx, ev2)
	if err == nil {
		t.Error("expected error for duplicate event ID")
	} else {
		assertError(t, "duplicate", err)
	}

	// Verify only 1 event exists.
	events, err := store.ListEvents(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// 6. Parent restart simulation: only cursor needed
// ---------------------------------------------------------------------------

func TestTaskWaiter_ParentRestart_OnlyCursorNeeded(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusRunning
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Simulate: some events were already processed before "restart".
	ev1 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}
	_, err := store.AppendEvent(ctx, ev1)
	if err != nil {
		t.Fatalf("AppendEvent ev1: %v", err)
	}

	ev2 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskStarted,
		CreatedAt:  time.Now().UTC(),
	}
	seq2, err := store.AppendEvent(ctx, ev2)
	if err != nil {
		t.Fatalf("AppendEvent ev2: %v", err)
	}

	// Commit terminal.
	outbox := NewTaskOutbox(store)
	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded

	ev3 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: task.WorkflowID,
		TenantID:   task.TenantID,
		Type:       EventTaskSucceeded,
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := outbox.CommitTerminal(ctx, task, 0, &result, ev3); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	// "Restart": the parent only has the cursor (last known seq = seq2).
	// It should be able to resume and get only new events (seq3).
	waiter := NewTaskWaiter(store)
	gotTask, events, err := waiter.WaitTerminal(ctx, task.TaskID, seq2)
	if err != nil {
		t.Fatalf("WaitTerminal after restart: %v", err)
	}
	if gotTask.Status != TaskStatusSucceeded {
		t.Errorf("expected SUCCEEDED, got %s", gotTask.Status)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 new event, got %d", len(events))
	}
	if len(events) > 0 && events[0].Sequence != seq2+1 {
		t.Errorf("expected seq=%d, got %d", seq2+1, events[0].Sequence)
	}

	// Also verify: if we provide 0 as cursor, we get all events.
	allEvents, err := store.ListEvents(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("ListEvents all: %v", err)
	}
	if len(allEvents) != 3 {
		t.Errorf("expected 3 events total, got %d", len(allEvents))
	}
}

// ---------------------------------------------------------------------------
// 7. Slow consumer does not block PutResult / AppendEvent
// ---------------------------------------------------------------------------

func TestTaskWaiter_SlowConsumer_DoesNotBlockCommit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusRunning
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	waiter := NewTaskWaiter(store)

	// Subscribe with a small (blocking) buffer but don't consume.
	// Subscribe should use a buffered channel so it doesn't block the publisher.
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()

	ch, cancelSub, err := waiter.Subscribe(subCtx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancelSub()

	// Emit several events — they should not block even if consumer is slow.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			ev := TaskEvent{
				EventID:    makeEventID(),
				TaskID:     task.TaskID,
				WorkflowID: task.WorkflowID,
				TenantID:   task.TenantID,
				Type:       EventTaskProgress,
				CreatedAt:  time.Now().UTC(),
			}
			if _, err := store.AppendEvent(ctx, ev); err != nil {
				t.Errorf("AppendEvent %d: %v", i, err)
				return
			}
		}
	}()

	// Wait for the publisher to finish (should not block).
	select {
	case <-done:
		// Success — publisher didn't block.
	case <-time.After(2 * time.Second):
		t.Fatal("AppendEvent blocked on slow consumer")
	}

	// Drain the channel to verify events were delivered.
	_ = ch // Silently drain — receive a few.
	count := 0
drainLoop:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break drainLoop
			}
			count++
		default:
			break drainLoop
		}
	}
	if count == 0 {
		t.Error("expected some events to be delivered")
	}
}
