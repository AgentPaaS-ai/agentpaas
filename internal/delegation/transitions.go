package delegation

import "fmt"

// TaskTransitions defines legal TaskStatus transitions.
var TaskTransitions = map[TaskStatus]map[TaskStatus]bool{
	TaskStatusPending: {
		TaskStatusAdmitted:  true,
		TaskStatusDenied:    true,
		TaskStatusCancelled: true,
		TaskStatusExpired:   true,
	},
	TaskStatusAdmitted: {
		TaskStatusRunning:   true,
		TaskStatusCancelled: true,
		TaskStatusExpired:   true,
		TaskStatusDenied:    true,
	},
	TaskStatusRunning: {
		TaskStatusSucceeded: true,
		TaskStatusFailed:    true,
		TaskStatusCancelled: true,
		TaskStatusExpired:   true,
	},
	// Terminal states: no outgoing transitions
	TaskStatusSucceeded: {},
	TaskStatusFailed:    {},
	TaskStatusCancelled: {},
	TaskStatusExpired:   {},
	TaskStatusDenied:    {},
}

// ValidateTaskTransition validates that a TaskStatus transition is legal.
// Returns nil if valid, or a *TransitionError if invalid.
func ValidateTaskTransition(from, to TaskStatus) error {
	if from == to && from.IsTerminal() {
		// Idempotent transition to same terminal is allowed.
		// Caller must separately verify that result/reason match.
		return nil
	}
	if allowed, ok := TaskTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return &TransitionError{
		Resource:  "TaskStatus",
		FromState: from.String(),
		ToState:   to.String(),
		Message:   fmt.Sprintf("invalid state transition: cannot move TaskStatus from %s to %s", from.String(), to.String()),
	}
}

// TransitionError is returned when an invalid state transition is attempted.
type TransitionError struct {
	Resource  string `json:"resource"`
	FromState string `json:"from_state"`
	ToState   string `json:"to_state"`
	Message   string `json:"message"`
}

// Error returns the error message.
func (e *TransitionError) Error() string {
	return e.Message
}