package delegation

import (
	"context"
	"fmt"
)

// TaskOutbox wraps a Store so that a terminal task-CAS and its result/event
// commit with the same atomicity contract as trigger.Outbox. If any step
// fails, the caller sees the error and the mutation is treated as
// uncommitted.
//
// Ordering: state CAS → PutResult → AppendEvent. If the CAS fails, result
// and event are never written. If the result write fails, the event is never
// appended. If the event append fails, the state mutation is treated as
// uncommitted — the caller must retry; the idempotent CAS will either
// succeed (fresh attempt) or conflict (already committed).
type TaskOutbox struct {
	store Store
}

// NewTaskOutbox creates a TaskOutbox wrapping the given Store.
func NewTaskOutbox(store Store) *TaskOutbox {
	return &TaskOutbox{store: store}
}

// eventTypeToStatus maps a terminal EventType to the corresponding TaskStatus.
var eventTypeToStatus = map[EventType]TaskStatus{
	EventTaskSucceeded: TaskStatusSucceeded,
	EventTaskFailed:    TaskStatusFailed,
	EventTaskCancelled: TaskStatusCancelled,
	EventTaskExpired:   TaskStatusExpired,
}

// isTerminalEventType returns true if the EventType represents a terminal task event.
func isTerminalEventType(et EventType) bool {
	_, ok := eventTypeToStatus[et]
	return ok
}

// CommitTerminal atomically transitions a task to a terminal state, stores
// the result, and appends the terminal event.
//
// Parameters:
//   - task: the current task state (used for transition validation + status)
//   - expectedGen: the expected generation for CAS
//   - result: the terminal result (must have matching Status)
//   - ev: the terminal TaskEvent (Type must be one of: SUCCEEDED, FAILED,
//     CANCELLED, EXPIRED)
//
// If the event type is TASK_DENIED, no result is stored (denied tasks don't
// produce results — they produce a denial reason on the task status).
//
// Returns the sequence number of the appended event.
func (o *TaskOutbox) CommitTerminal(
	ctx context.Context,
	task Task,
	expectedGen int64,
	result *Result,
	ev TaskEvent,
) (int64, error) {
	// Verify the task ID matches.
	if ev.TaskID != task.TaskID {
		return 0, fmt.Errorf("task_outbox: event task_id %q does not match task %q",
			ev.TaskID, task.TaskID)
	}

	// Validate the event type is terminal.
	if !isTerminalEventType(ev.Type) {
		return 0, fmt.Errorf("task_outbox: event type %q is not a terminal type", ev.Type)
	}

	// Map event type to target status.
	targetStatus, ok := eventTypeToStatus[ev.Type]
	if !ok {
		return 0, fmt.Errorf("task_outbox: cannot map event type %q to task status", ev.Type)
	}

	// Validate the transition.
	if err := ValidateTaskTransition(task.Status, targetStatus); err != nil {
		return 0, fmt.Errorf("task_outbox: %w", err)
	}

	// Phase 1: CAS the task to the terminal status.
	task.Status = targetStatus
	if err := o.store.CASTask(ctx, task, expectedGen); err != nil {
		return 0, fmt.Errorf("task_outbox: state commit: %w", err)
	}

	// Phase 2: Store the result (if applicable).
	// TASK_DENIED doesn't have a result — it has a denial reason on the task.
	if targetStatus != TaskStatusDenied && result != nil {
		result.Status = targetStatus
		result.TaskID = task.TaskID
		result.WorkflowID = task.WorkflowID
		if err := o.store.PutResult(ctx, *result); err != nil {
			return 0, fmt.Errorf("task_outbox: result commit: %w", err)
		}
	}

	// Phase 3: Append the terminal event.
	seq, err := o.store.AppendEvent(ctx, ev)
	if err != nil {
		return 0, fmt.Errorf("task_outbox: event append: %w", err)
	}

	return seq, nil
}
