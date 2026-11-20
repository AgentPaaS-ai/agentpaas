package delegation

import (
	"context"
	"fmt"
)

// TaskWaiter provides wait/subscribe APIs on top of a Store so that a parent
// orchestration can suspend, checkpoint, and resume without polling.
type TaskWaiter struct {
	store Store
}

// NewTaskWaiter creates a TaskWaiter wrapping the given Store.
func NewTaskWaiter(store Store) *TaskWaiter {
	return &TaskWaiter{store: store}
}

// WaitTerminal blocks until the task reaches a terminal status, then returns
// the task and all events with sequence > afterSeq. The caller passes a
// cursor (afterSeq) to resume from a known position.
//
// If the context is cancelled before the task becomes terminal, the method
// returns the context error.
func (w *TaskWaiter) WaitTerminal(
	ctx context.Context,
	taskID TaskID,
	afterSeq int64,
) (Task, []TaskEvent, error) {
	// Subscribe first to catch the race: events might arrive between our
	// terminal check and the subscribe call. Subscribe replays events
	// > afterSeq, so we never miss events.
	ch, cancel, err := w.store.SubscribeEvents(ctx, taskID, afterSeq)
	if err != nil {
		return Task{}, nil, fmt.Errorf("task_waiter: subscribe: %w", err)
	}
	defer cancel()

	// Check if the task is already terminal. If so, collect all events
	// from the replay and return immediately.
	task, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		return Task{}, nil, fmt.Errorf("task_waiter: get task: %w", err)
	}
	if task == nil {
		return Task{}, nil, fmt.Errorf("task_waiter: task %q not found", taskID)
	}

	if task.Status.IsTerminal() {
		// Collect events already delivered via replay.
		events := drainChannel(ch)
		return *task, events, nil
	}

	// Wait for terminal event or context cancellation.
	var events []TaskEvent
	for {
		select {
		case <-ctx.Done():
			return Task{}, events, ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				// Channel closed — task reached terminal or context cancelled.
				// Re-check task status.
				task, err := w.store.GetTask(ctx, taskID)
				if err != nil {
					return Task{}, events, fmt.Errorf("task_waiter: get task after channel close: %w", err)
				}
				return *task, events, nil
			}
			events = append(events, ev)
			if isTerminalEventType(ev.Type) {
				// Drain any remaining events from the buffered channel.
				remaining := drainChannel(ch)
				events = append(events, remaining...)
				// Re-fetch task for the latest status.
				task, err := w.store.GetTask(ctx, taskID)
				if err != nil {
					return Task{}, events, fmt.Errorf("task_waiter: get task after terminal: %w", err)
				}
				return *task, events, nil
			}
		}
	}
}

// Subscribe returns a channel of events for a task, replaying existing
// events with sequence > afterSeq. The returned cancel function unsubscribes
// the channel and closes it safely.
//
// The channel is closed when:
//   - The context is cancelled.
//   - A terminal event is delivered.
//   - The cancel function is called.
func (w *TaskWaiter) Subscribe(
	ctx context.Context,
	taskID TaskID,
	afterSeq int64,
) (<-chan TaskEvent, func(), error) {
	return w.store.SubscribeEvents(ctx, taskID, afterSeq)
}

// drainChannel reads all currently buffered events from a channel without
// blocking indefinitely. It returns the events in order.
func drainChannel(ch <-chan TaskEvent) []TaskEvent {
	var events []TaskEvent
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		default:
			return events
		}
	}
}
