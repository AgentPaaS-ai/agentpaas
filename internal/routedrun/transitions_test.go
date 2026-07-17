package routedrun

import (
	"testing"
)

// --- Run status transitions ---

func TestRunTransition_Valid(t *testing.T) {
	tests := []struct {
		name string
		from RunStatus
		to   RunStatus
	}{
		{"PENDING->RUNNING", RunStatusPending, RunStatusRunning},
		{"PENDING->CANCELLED", RunStatusPending, RunStatusCancelled},
		{"RUNNING->SUCCEEDED", RunStatusRunning, RunStatusSucceeded},
		{"RUNNING->FAILED", RunStatusRunning, RunStatusFailed},
		{"RUNNING->CANCELLED", RunStatusRunning, RunStatusCancelled},
		{"RUNNING->PAUSE_REQUESTED", RunStatusRunning, RunStatusPauseRequested},
		{"RUNNING->BUDGET_EXCEEDED", RunStatusRunning, RunStatusBudgetExceeded},
		{"RUNNING->EXPIRED", RunStatusRunning, RunStatusExpired},
		{"PAUSE_REQUESTED->PAUSED", RunStatusPauseRequested, RunStatusPaused},
		{"PAUSE_REQUESTED->RUNNING", RunStatusPauseRequested, RunStatusRunning},
		{"PAUSE_REQUESTED->CANCELLED", RunStatusPauseRequested, RunStatusCancelled},
		{"PAUSED->RUNNING", RunStatusPaused, RunStatusRunning},
		{"PAUSED->NEEDS_REPLAN", RunStatusPaused, RunStatusNeedsReplan},
		{"PAUSED->CANCELLED", RunStatusPaused, RunStatusCancelled},
		{"NEEDS_REPLAN->RUNNING", RunStatusNeedsReplan, RunStatusRunning},
		{"NEEDS_REPLAN->CANCELLED", RunStatusNeedsReplan, RunStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRunTransition(tt.from, tt.to); err != nil {
				t.Errorf("ValidateRunTransition(%q, %q) = %v, want nil", tt.from, tt.to, err)
			}
		})
	}
}

func TestRunTransition_Invalid(t *testing.T) {
	tests := []struct {
		name string
		from RunStatus
		to   RunStatus
	}{
		{"RUNNING->PENDING", RunStatusRunning, RunStatusPending},
		{"SUCCEEDED->RUNNING", RunStatusSucceeded, RunStatusRunning},
		{"CANCELLED->RUNNING", RunStatusCancelled, RunStatusRunning},
		{"SUCCEEDED->FAILED", RunStatusSucceeded, RunStatusFailed},
		{"PENDING->SUCCEEDED", RunStatusPending, RunStatusSucceeded},
		{"PAUSED->SUCCEEDED", RunStatusPaused, RunStatusSucceeded},
		{"NEEDS_REPLAN->SUCCEEDED", RunStatusNeedsReplan, RunStatusSucceeded},
		{"BUDGET_EXCEEDED->RUNNING", RunStatusBudgetExceeded, RunStatusRunning},
		{"EXPIRED->RUNNING", RunStatusExpired, RunStatusRunning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateRunTransition(tt.from, tt.to); err == nil {
				t.Errorf("ValidateRunTransition(%q, %q) = nil, want error", tt.from, tt.to)
			}
		})
	}
}

func TestRunTransition_Apply(t *testing.T) {
	got, err := ApplyRunTransition(RunStatusPending, RunStatusRunning)
	if err != nil {
		t.Fatalf("ApplyRunTransition(PENDING, RUNNING) = %v", err)
	}
	if got != RunStatusRunning {
		t.Errorf("got %q, want %q", got, RunStatusRunning)
	}

	// Invalid should return original status
	got, err = ApplyRunTransition(RunStatusSucceeded, RunStatusRunning)
	if err == nil {
		t.Error("expected error for terminal->running transition")
	}
	if got != RunStatusSucceeded {
		t.Errorf("expected original status %q on error, got %q", RunStatusSucceeded, got)
	}
}

// --- Workflow status transitions ---

func TestWorkflowTransition_Valid(t *testing.T) {
	tests := []struct {
		from, to WorkflowStatus
	}{
		{WorkflowStatusPending, WorkflowStatusRunning},
		{WorkflowStatusRunning, WorkflowStatusSucceeded},
		{WorkflowStatusRunning, WorkflowStatusPauseRequested},
		{WorkflowStatusPauseRequested, WorkflowStatusPaused},
		{WorkflowStatusPauseRequested, WorkflowStatusRunning},
		{WorkflowStatusPaused, WorkflowStatusRunning},
		{WorkflowStatusPaused, WorkflowStatusNeedsReplan},
		{WorkflowStatusNeedsReplan, WorkflowStatusRunning},
		{WorkflowStatusRunning, WorkflowStatusCancelled},
		{WorkflowStatusPaused, WorkflowStatusCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateWorkflowTransition(tt.from, tt.to); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorkflowTransition_Invalid(t *testing.T) {
	if err := ValidateWorkflowTransition(WorkflowStatusSucceeded, WorkflowStatusRunning); err == nil {
		t.Error("expected error for succeeded->running")
	}
}

// --- Node status transitions ---

func TestNodeTransition_Valid(t *testing.T) {
	tests := []struct {
		from, to NodeStatus
	}{
		{NodeStatusPending, NodeStatusReady},
		{NodeStatusReady, NodeStatusLaunching},
		{NodeStatusLaunching, NodeStatusRunning},
		{NodeStatusRunning, NodeStatusSucceeded},
		{NodeStatusRunning, NodeStatusPauseRequested},
		{NodeStatusPauseRequested, NodeStatusPaused},
		{NodeStatusPauseRequested, NodeStatusRunning},
		{NodeStatusPaused, NodeStatusRunning},
		{NodeStatusRunning, NodeStatusFailed},
		{NodeStatusPending, NodeStatusCancelled},
		{NodeStatusPending, NodeStatusSkipped},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateNodeTransition(tt.from, tt.to); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestNodeTransition_Invalid(t *testing.T) {
	if err := ValidateNodeTransition(NodeStatusSucceeded, NodeStatusRunning); err == nil {
		t.Error("expected error for terminal->running")
	}
	if err := ValidateNodeTransition(NodeStatusPending, NodeStatusRunning); err == nil {
		t.Error("expected error for pending->running (need ready->launching->running)")
	}
}

// --- Service status transitions ---

func TestServiceTransition_Valid(t *testing.T) {
	tests := []struct {
		from, to ServiceStatus
	}{
		{ServiceStatusDeclared, ServiceStatusStarting},
		{ServiceStatusStarting, ServiceStatusReady},
		{ServiceStatusReady, ServiceStatusUnhealthy},
		{ServiceStatusUnhealthy, ServiceStatusReady},
		{ServiceStatusUnhealthy, ServiceStatusFenced},
		{ServiceStatusFenced, ServiceStatusStopping},
		{ServiceStatusStopping, ServiceStatusStopped},
		{ServiceStatusDeclared, ServiceStatusFailed},
		{ServiceStatusReady, ServiceStatusStopping},
		{ServiceStatusStopping, ServiceStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateServiceTransition(tt.from, tt.to); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// --- Child batch status transitions ---

func TestChildBatchTransition_Valid(t *testing.T) {
	tests := []struct {
		from, to ChildBatchStatus
	}{
		{ChildBatchIntent, ChildBatchAllocated},
		{ChildBatchAllocated, ChildBatchRunning},
		{ChildBatchRunning, ChildBatchJoining},
		{ChildBatchJoining, ChildBatchSucceeded},
		{ChildBatchRunning, ChildBatchPauseRequested},
		{ChildBatchPauseRequested, ChildBatchPaused},
		{ChildBatchPaused, ChildBatchRunning},
		{ChildBatchStopping, ChildBatchStopped},
		{ChildBatchIntent, ChildBatchCancelled},
		{ChildBatchRunning, ChildBatchFailed},
		{ChildBatchStopping, ChildBatchCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateChildBatchTransition(tt.from, tt.to); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestChildBatchTransition_Invalid(t *testing.T) {
	if err := ValidateChildBatchTransition(ChildBatchSucceeded, ChildBatchRunning); err == nil {
		t.Error("expected error for succeeded->running")
	}
	if err := ValidateChildBatchTransition(ChildBatchIntent, ChildBatchSucceeded); err == nil {
		t.Error("expected error for intent->succeeded (need allocated->running->joining)")
	}
}

// --- Attempt status transitions ---

func TestAttemptTransition_Valid(t *testing.T) {
	tests := []struct {
		from, to AttemptStatus
	}{
		{AttemptStatusPending, AttemptStatusRunning},
		{AttemptStatusRunning, AttemptStatusSucceeded},
		{AttemptStatusRunning, AttemptStatusFailed},
		{AttemptStatusRunning, AttemptStatusFenced},
		{AttemptStatusRunning, AttemptStatusNeedsReplan},
		{AttemptStatusNeedsReplan, AttemptStatusRunning},
		{AttemptStatusNeedsReplan, AttemptStatusCancelled},
		{AttemptStatusPending, AttemptStatusCancelled},
		{AttemptStatusPending, AttemptStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateAttemptTransition(tt.from, tt.to); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestAttemptTransition_Invalid(t *testing.T) {
	tests := []struct {
		from, to AttemptStatus
	}{
		{AttemptStatusSucceeded, AttemptStatusRunning},
		{AttemptStatusFailed, AttemptStatusRunning},
		{AttemptStatusFenced, AttemptStatusRunning},
		{AttemptStatusCancelled, AttemptStatusRunning},
		{AttemptStatusPending, AttemptStatusSucceeded},
		{AttemptStatusPending, AttemptStatusFenced},
	}
	for _, tt := range tests {
		t.Run(tt.from.String()+"/"+tt.to.String(), func(t *testing.T) {
			if err := ValidateAttemptTransition(tt.from, tt.to); err == nil {
				t.Errorf("expected error for %s -> %s", tt.from, tt.to)
			}
		})
	}
}

// --- Transition error type ---

func TestTransitionError_Error(t *testing.T) {
	err := NewTransitionError("RunStatus", RunStatusSucceeded, RunStatusRunning)
	if err.Error() == "" {
		t.Error("TransitionError.Error() should not be empty")
	}
	if err.Resource != "RunStatus" {
		t.Errorf("Resource = %q, want %q", err.Resource, "RunStatus")
	}
	if err.FromState != RunStatusSucceeded {
		t.Errorf("FromState = %v, want %v", err.FromState, RunStatusSucceeded)
	}
	if err.ToState != RunStatusRunning {
		t.Errorf("ToState = %v, want %v", err.ToState, RunStatusRunning)
	}
}

// --- Cross-type invalid transitions (unknown statuses) ---

func TestTransition_InvalidUnknownFrom(t *testing.T) {
	// Unknown from state should produce an error
	err := ValidateRunTransition(RunStatus(99), RunStatusRunning)
	if err == nil {
		t.Error("expected error for unknown from state")
	}
}

func TestTransition_InvalidUnknownTo(t *testing.T) {
	err := ValidateRunTransition(RunStatusPending, RunStatus(99))
	if err == nil {
		t.Error("expected error for unknown to state")
	}
}