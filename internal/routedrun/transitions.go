package routedrun

// ---------------------------------------------------------------------------
// State transition validation
// ---------------------------------------------------------------------------

// RunTransitions defines legal RunStatus transitions.
// Key: from state, Value: set of allowed to states.
import "fmt"

var RunTransitions = map[RunStatus]map[RunStatus]bool{
	RunStatusPending: {
		RunStatusRunning:   true,
		RunStatusCancelled: true,
		RunStatusFailed:    true,
	},
	RunStatusRunning: {
		RunStatusPauseRequested: true,
		RunStatusSucceeded:      true,
		RunStatusFailed:         true,
		RunStatusCancelled:      true,
		RunStatusBudgetExceeded: true,
		RunStatusExpired:        true,
	},
	RunStatusPauseRequested: {
		RunStatusPaused:         true,
		RunStatusRunning:        true,
		RunStatusCancelled:      true,
		RunStatusFailed:         true,
		RunStatusBudgetExceeded: true,
		RunStatusExpired:        true,
	},
	RunStatusPaused: {
		RunStatusRunning:     true,
		RunStatusNeedsReplan: true,
		RunStatusCancelled:   true,
		RunStatusFailed:      true,
		RunStatusExpired:     true,
	},
	RunStatusNeedsReplan: {
		RunStatusRunning:   true,
		RunStatusCancelled: true,
		RunStatusFailed:    true,
		RunStatusExpired:   true,
	},
	// Terminal states: no outgoing transitions
	RunStatusSucceeded:      {},
	RunStatusFailed:         {},
	RunStatusCancelled:      {},
	RunStatusBudgetExceeded: {},
	RunStatusExpired:        {},
}

// WorkflowTransitions defines legal WorkflowStatus transitions.
var WorkflowTransitions = map[WorkflowStatus]map[WorkflowStatus]bool{
	WorkflowStatusPending: {
		WorkflowStatusRunning:   true,
		WorkflowStatusCancelled: true,
		WorkflowStatusFailed:    true,
	},
	WorkflowStatusRunning: {
		WorkflowStatusPauseRequested: true,
		WorkflowStatusSucceeded:      true,
		WorkflowStatusFailed:         true,
		WorkflowStatusCancelled:      true,
		WorkflowStatusBudgetExceeded: true,
		WorkflowStatusExpired:        true,
	},
	WorkflowStatusPauseRequested: {
		WorkflowStatusPaused:         true,
		WorkflowStatusRunning:        true,
		WorkflowStatusCancelled:      true,
		WorkflowStatusFailed:         true,
		WorkflowStatusBudgetExceeded: true,
		WorkflowStatusExpired:        true,
	},
	WorkflowStatusPaused: {
		WorkflowStatusRunning:     true,
		WorkflowStatusNeedsReplan: true,
		WorkflowStatusCancelled:   true,
		WorkflowStatusFailed:      true,
		WorkflowStatusExpired:     true,
	},
	WorkflowStatusNeedsReplan: {
		WorkflowStatusRunning:   true,
		WorkflowStatusCancelled: true,
		WorkflowStatusFailed:    true,
		WorkflowStatusExpired:   true,
	},
	WorkflowStatusSucceeded:      {},
	WorkflowStatusFailed:         {},
	WorkflowStatusCancelled:      {},
	WorkflowStatusExpired:        {},
	WorkflowStatusBudgetExceeded: {},
}

// NodeTransitions defines legal NodeStatus transitions.
var NodeTransitions = map[NodeStatus]map[NodeStatus]bool{
	NodeStatusPending: {
		NodeStatusReady:     true,
		NodeStatusCancelled: true,
		NodeStatusSkipped:   true,
	},
	NodeStatusReady: {
		NodeStatusLaunching: true,
		NodeStatusCancelled: true,
		NodeStatusSkipped:   true,
	},
	NodeStatusLaunching: {
		NodeStatusRunning:   true,
		NodeStatusFailed:    true,
		NodeStatusCancelled: true,
	},
	NodeStatusRunning: {
		NodeStatusPauseRequested: true,
		NodeStatusSucceeded:      true,
		NodeStatusFailed:         true,
		NodeStatusCancelled:      true,
	},
	NodeStatusPauseRequested: {
		NodeStatusPaused:    true,
		NodeStatusRunning:   true,
		NodeStatusCancelled: true,
		NodeStatusFailed:    true,
	},
	NodeStatusPaused: {
		NodeStatusRunning:     true,
		NodeStatusNeedsReplan: true,
		NodeStatusCancelled:   true,
	},
	NodeStatusNeedsReplan: {
		NodeStatusRunning:   true,
		NodeStatusCancelled: true,
	},
	NodeStatusSucceeded: {},
	NodeStatusFailed:    {},
	NodeStatusCancelled: {},
	NodeStatusSkipped:   {},
}

// ServiceTransitions defines legal ServiceStatus transitions.
var ServiceTransitions = map[ServiceStatus]map[ServiceStatus]bool{
	ServiceStatusDeclared: {
		ServiceStatusStarting: true,
		ServiceStatusFailed:   true,
	},
	ServiceStatusStarting: {
		ServiceStatusReady:   true,
		ServiceStatusFailed:  true,
		ServiceStatusStopped: true,
	},
	ServiceStatusReady: {
		ServiceStatusUnhealthy: true,
		ServiceStatusStopping:  true,
		ServiceStatusFailed:    true,
	},
	ServiceStatusUnhealthy: {
		ServiceStatusReady:    true,
		ServiceStatusFenced:   true,
		ServiceStatusStopping: true,
		ServiceStatusFailed:   true,
	},
	ServiceStatusFenced: {
		ServiceStatusStopping: true,
		ServiceStatusFailed:   true,
	},
	ServiceStatusStopping: {
		ServiceStatusStopped: true,
		ServiceStatusFailed:  true,
	},
	ServiceStatusStopped: {},
	ServiceStatusFailed:  {},
}

// ChildBatchTransitions defines legal ChildBatchStatus transitions.
var ChildBatchTransitions = map[ChildBatchStatus]map[ChildBatchStatus]bool{
	ChildBatchIntent: {
		ChildBatchAllocated: true,
		ChildBatchCancelled: true,
	},
	ChildBatchAllocated: {
		ChildBatchRunning:   true,
		ChildBatchCancelled: true,
	},
	ChildBatchRunning: {
		ChildBatchPauseRequested: true,
		ChildBatchJoining:        true,
		ChildBatchFailed:         true,
		ChildBatchCancelled:      true,
	},
	ChildBatchPauseRequested: {
		ChildBatchPaused:    true,
		ChildBatchRunning:   true,
		ChildBatchStopping:  true,
		ChildBatchCancelled: true,
	},
	ChildBatchPaused: {
		ChildBatchRunning:   true,
		ChildBatchStopping:  true,
		ChildBatchCancelled: true,
	},
	ChildBatchJoining: {
		ChildBatchSucceeded: true,
		ChildBatchFailed:    true,
		ChildBatchCancelled: true,
	},
	ChildBatchStopping: {
		ChildBatchStopped:   true,
		ChildBatchFailed:    true,
		ChildBatchCancelled: true,
	},
	ChildBatchSucceeded: {},
	ChildBatchFailed:    {},
	ChildBatchCancelled: {},
}

// AttemptTransitions defines legal AttemptStatus transitions.
var AttemptTransitions = map[AttemptStatus]map[AttemptStatus]bool{
	AttemptStatusPending: {
		AttemptStatusRunning:   true,
		AttemptStatusCancelled: true,
		AttemptStatusFailed:    true,
	},
	AttemptStatusRunning: {
		AttemptStatusNeedsReplan: true,
		AttemptStatusSucceeded:   true,
		AttemptStatusFailed:      true,
		AttemptStatusFenced:      true,
		AttemptStatusCancelled:   true,
	},
	AttemptStatusNeedsReplan: {
		AttemptStatusRunning:   true,
		AttemptStatusCancelled: true,
		AttemptStatusFailed:    true,
	},
	AttemptStatusSucceeded: {},
	AttemptStatusFailed:    {},
	AttemptStatusFenced:    {},
	AttemptStatusCancelled: {},
}

// ---------------------------------------------------------------------------
// Transition validation functions
// ---------------------------------------------------------------------------

// ValidateRunTransition validates that a RunStatus transition is legal.
// Returns nil if valid, or a *TransitionError if invalid.
func ValidateRunTransition(from, to RunStatus) error {
	if allowed, ok := RunTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("RunStatus", from, to)
}

// ValidateWorkflowTransition validates that a WorkflowStatus transition is legal.
func ValidateWorkflowTransition(from, to WorkflowStatus) error {
	if allowed, ok := WorkflowTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("WorkflowStatus", from, to)
}

// ValidateNodeTransition validates that a NodeStatus transition is legal.
func ValidateNodeTransition(from, to NodeStatus) error {
	if allowed, ok := NodeTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("NodeStatus", from, to)
}

// ValidateServiceTransition validates that a ServiceStatus transition is legal.
func ValidateServiceTransition(from, to ServiceStatus) error {
	if allowed, ok := ServiceTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("ServiceStatus", from, to)
}

// ValidateChildBatchTransition validates that a ChildBatchStatus transition is legal.
func ValidateChildBatchTransition(from, to ChildBatchStatus) error {
	if allowed, ok := ChildBatchTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("ChildBatchStatus", from, to)
}

// ValidateAttemptTransition validates that an AttemptStatus transition is legal.
func ValidateAttemptTransition(from, to AttemptStatus) error {
	if allowed, ok := AttemptTransitions[from]; ok {
		if allowed[to] {
			return nil
		}
	}
	return NewTransitionError("AttemptStatus", from, to)
}

// ApplyRunTransition validates and applies a RunStatus transition.
// Returns the new status on success, or the original status and a *TransitionError.
func ApplyRunTransition(current RunStatus, target RunStatus) (RunStatus, error) {
	if err := ValidateRunTransition(current, target); err != nil {
		return current, fmt.Errorf("apply run transition: %w", err)
	}
	return target, nil
}
