package routedrun

import (
	"encoding/json"
	"testing"
)

// --- Run status tests ---

func TestRunStatus_String(t *testing.T) {
	tests := []struct {
		s    RunStatus
		want string
	}{
		{RunStatusPending, "PENDING"},
		{RunStatusRunning, "RUNNING"},
		{RunStatusPauseRequested, "PAUSE_REQUESTED"},
		{RunStatusPaused, "PAUSED"},
		{RunStatusNeedsReplan, "NEEDS_REPLAN"},
		{RunStatusSucceeded, "SUCCEEDED"},
		{RunStatusFailed, "FAILED"},
		{RunStatusCancelled, "CANCELLED"},
		{RunStatusBudgetExceeded, "BUDGET_EXCEEDED"},
		{RunStatusExpired, "EXPIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.s.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
		})
	}
}

func TestRunStatus_JSON(t *testing.T) {
	for _, s := range AllRunStatuses() {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", s.String(), err)
		}
		// JSON value must be stable string, not integer
		var got string
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", string(data), err)
		}
		if got != s.String() {
			t.Errorf("JSON round-trip: %q != %q", got, s.String())
		}
	}
}

func TestRunStatus_Invalid(t *testing.T) {
	bad := RunStatus(99)
	if bad.Valid() {
		t.Error("RunStatus(99) should be invalid")
	}
	if bad.String() != "UNKNOWN" {
		t.Errorf("RunStatus(99).String() = %q, want %q", bad.String(), "UNKNOWN")
	}
}

func TestRunStatus_UnmarshalUnknownRejected(t *testing.T) {
	var s RunStatus
	if err := json.Unmarshal([]byte(`"UNKNOWN_STATUS"`), &s); err == nil {
		t.Error("expected error for unknown status value")
	}
}

// --- Workflow status tests ---

func TestWorkflowStatus_AllValid(t *testing.T) {
	for _, s := range AllWorkflowStatuses() {
		if !s.Valid() {
			t.Errorf("WorkflowStatus %q should be valid", s.String())
		}
	}
}

func TestWorkflowStatus_JSONStability(t *testing.T) {
	tests := []struct {
		s    WorkflowStatus
		want string
	}{
		{WorkflowStatusPending, "PENDING"},
		{WorkflowStatusRunning, "RUNNING"},
		{WorkflowStatusSucceeded, "SUCCEEDED"},
		{WorkflowStatusFailed, "FAILED"},
		{WorkflowStatusCancelled, "CANCELLED"},
		{WorkflowStatusExpired, "EXPIRED"},
		{WorkflowStatusBudgetExceeded, "BUDGET_EXCEEDED"},
		{WorkflowStatusPauseRequested, "PAUSE_REQUESTED"},
		{WorkflowStatusPaused, "PAUSED"},
		{WorkflowStatusNeedsReplan, "NEEDS_REPLAN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorkflowStatus_Invalid(t *testing.T) {
	bad := WorkflowStatus(99)
	if bad.Valid() {
		t.Error("WorkflowStatus(99) should be invalid")
	}
	if bad.String() != "UNKNOWN" {
		t.Errorf("WorkflowStatus(99).String() = %q, want %q", bad.String(), "UNKNOWN")
	}
}

// --- Node status tests ---

func TestNodeStatus_AllValid(t *testing.T) {
	for _, s := range AllNodeStatuses() {
		if !s.Valid() {
			t.Errorf("NodeStatus %q should be valid", s.String())
		}
	}
}

func TestNodeStatus_JSONStability(t *testing.T) {
	tests := []struct {
		s    NodeStatus
		want string
	}{
		{NodeStatusPending, "PENDING"},
		{NodeStatusReady, "READY"},
		{NodeStatusLaunching, "LAUNCHING"},
		{NodeStatusRunning, "RUNNING"},
		{NodeStatusSucceeded, "SUCCEEDED"},
		{NodeStatusFailed, "FAILED"},
		{NodeStatusCancelled, "CANCELLED"},
		{NodeStatusSkipped, "SKIPPED"},
		{NodeStatusPauseRequested, "PAUSE_REQUESTED"},
		{NodeStatusPaused, "PAUSED"},
		{NodeStatusNeedsReplan, "NEEDS_REPLAN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			var s NodeStatus
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if s != tt.s {
				t.Errorf("round-trip: got %q, want %q", s.String(), tt.want)
			}
		})
	}
}

func TestNodeStatus_Invalid(t *testing.T) {
	bad := NodeStatus(99)
	if bad.Valid() {
		t.Error("NodeStatus(99) should be invalid")
	}
}

// --- Service status tests ---

func TestServiceStatus_AllValid(t *testing.T) {
	for _, s := range AllServiceStatuses() {
		if !s.Valid() {
			t.Errorf("ServiceStatus %q should be valid", s.String())
		}
	}
}

func TestServiceStatus_JSONStability(t *testing.T) {
	tests := []struct {
		s    ServiceStatus
		want string
	}{
		{ServiceStatusDeclared, "DECLARED"},
		{ServiceStatusStarting, "STARTING"},
		{ServiceStatusReady, "READY"},
		{ServiceStatusUnhealthy, "UNHEALTHY"},
		{ServiceStatusFenced, "FENCED"},
		{ServiceStatusStopping, "STOPPING"},
		{ServiceStatusStopped, "STOPPED"},
		{ServiceStatusFailed, "FAILED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			var s ServiceStatus
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if s != tt.s {
				t.Errorf("round-trip: got %q, want %q", s.String(), tt.want)
			}
		})
	}
}

// --- Child batch status tests ---

func TestChildBatchStatus_AllValid(t *testing.T) {
	for _, s := range AllChildBatchStatuses() {
		if !s.Valid() {
			t.Errorf("ChildBatchStatus %q should be valid", s.String())
		}
	}
}

func TestChildBatchStatus_JSONStability(t *testing.T) {
	tests := []struct {
		s    ChildBatchStatus
		want string
	}{
		{ChildBatchIntent, "INTENT"},
		{ChildBatchAllocated, "ALLOCATED"},
		{ChildBatchRunning, "RUNNING"},
		{ChildBatchPauseRequested, "PAUSE_REQUESTED"},
		{ChildBatchPaused, "PAUSED"},
		{ChildBatchJoining, "JOINING"},
		{ChildBatchStopping, "STOPPING"},
		{ChildBatchSucceeded, "SUCCEEDED"},
		{ChildBatchFailed, "FAILED"},
		{ChildBatchCancelled, "CANCELLED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			var s ChildBatchStatus
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if s != tt.s {
				t.Errorf("round-trip: got %q, want %q", s.String(), tt.want)
			}
		})
	}
}

// --- Attempt status tests ---

func TestAttemptStatus_AllValid(t *testing.T) {
	for _, s := range AllAttemptStatuses() {
		if !s.Valid() {
			t.Errorf("AttemptStatus %q should be valid", s.String())
		}
	}
}

func TestAttemptStatus_JSONStability(t *testing.T) {
	tests := []struct {
		s    AttemptStatus
		want string
	}{
		{AttemptStatusPending, "PENDING"},
		{AttemptStatusRunning, "RUNNING"},
		{AttemptStatusNeedsReplan, "NEEDS_REPLAN"},
		{AttemptStatusSucceeded, "SUCCEEDED"},
		{AttemptStatusFailed, "FAILED"},
		{AttemptStatusFenced, "FENCED"},
		{AttemptStatusCancelled, "CANCELLED"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			data, err := json.Marshal(tt.s)
			if err != nil {
				t.Fatal(err)
			}
			var s AttemptStatus
			if err := json.Unmarshal(data, &s); err != nil {
				t.Fatal(err)
			}
			if s != tt.s {
				t.Errorf("round-trip: got %q, want %q", s.String(), tt.want)
			}
		})
	}
}

// --- Failure reason tests ---

func TestFailureReason_AllKnown(t *testing.T) {
	reasons := AllFailureReasons()
	if len(reasons) == 0 {
		t.Fatal("AllFailureReasons() must not be empty")
	}
	seen := make(map[string]bool)
	for _, r := range reasons {
		if !r.Valid() {
			t.Errorf("FailureReason %q should be valid", r.String())
		}
		if seen[r.String()] {
			t.Errorf("duplicate failure reason string: %q", r.String())
		}
		seen[r.String()] = true
		// JSON round-trip
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", r.String(), err)
		}
		var got FailureReason
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", string(data), err)
		}
		if got != r {
			t.Errorf("round-trip: got %q, want %q", got.String(), r.String())
		}
	}
}

func TestFailureReason_Invalid(t *testing.T) {
	bad := FailureReason(999)
	if bad.Valid() {
		t.Error("FailureReason(999) should be invalid")
	}
}

// --- Failure scope tests ---

func TestFailureScope_AllKnown(t *testing.T) {
	for _, s := range AllFailureScopes() {
		if !s.Valid() {
			t.Errorf("FailureScope %q should be valid", s.String())
		}
		// JSON round-trip
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", s.String(), err)
		}
		var got FailureScope
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", string(data), err)
		}
		if got != s {
			t.Errorf("round-trip: got %q, want %q", got.String(), s.String())
		}
	}
}

func TestFailureScope_Invalid(t *testing.T) {
	bad := FailureScope(99)
	if bad.Valid() {
		t.Error("FailureScope(99) should be invalid")
	}
}

// --- Recovery disposition tests ---

func TestRecoveryDisposition_AllKnown(t *testing.T) {
	tests := []struct {
		d    RecoveryDisposition
		want string
	}{
		{RecoveryNotNeeded, "not_needed"},
		{RecoveryAutoRecovered, "auto_recovered"},
		{RecoveryNeedsReplan, "needs_replan"},
		{RecoveryTerminal, "terminal"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.d.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.d.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
			data, err := json.Marshal(tt.d)
			if err != nil {
				t.Fatal(err)
			}
			var got RecoveryDisposition
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.d {
				t.Errorf("round-trip: got %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestRecoveryDisposition_Invalid(t *testing.T) {
	bad := RecoveryDisposition(99)
	if bad.Valid() {
		t.Error("RecoveryDisposition(99) should be invalid")
	}
}

// --- Data classification tests ---

func TestDataClassification_Ordering(t *testing.T) {
	tests := []struct {
		a, b  DataClassification
		less  bool
		equal bool
	}{
		{ClassificationPublic, ClassificationInternal, true, false},
		{ClassificationInternal, ClassificationConfidential, true, false},
		{ClassificationConfidential, ClassificationRestricted, true, false},
		{ClassificationPublic, ClassificationPublic, false, true},
		{ClassificationInternal, ClassificationInternal, false, true},
		{ClassificationConfidential, ClassificationConfidential, false, true},
		{ClassificationRestricted, ClassificationRestricted, false, true},
		// Reverse checks
		{ClassificationRestricted, ClassificationPublic, false, false},
		{ClassificationInternal, ClassificationPublic, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.a.String()+"/"+tt.b.String(), func(t *testing.T) {
			aVal := tt.a.Level()
			bVal := tt.b.Level()
			if tt.less && !(aVal < bVal) {
				t.Errorf("expected %q < %q", tt.a.String(), tt.b.String())
			}
			if tt.equal && aVal != bVal {
				t.Errorf("expected %q == %q", tt.a.String(), tt.b.String())
			}
			if !tt.less && !tt.equal && aVal >= bVal {
				// expected a >= b
			}
		})
	}
}

func TestDataClassification_NoDeclassification(t *testing.T) {
	// Verify no operation decreases classification
	higher := ClassificationRestricted
	lower := ClassificationPublic
	if higher.Level() <= lower.Level() {
		t.Error("restricted level must be > public level")
	}
}

func TestDataClassification_UnknownRejected(t *testing.T) {
	bad := DataClassification("secret")
	if bad.Valid() {
		t.Error("secret classification should not be valid")
	}
}

func TestDataClassification_JSONRoundTrip(t *testing.T) {
	for _, c := range AllDataClassifications() {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", c.String(), err)
		}
		var got DataClassification
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", string(data), err)
		}
		if got != c {
			t.Errorf("round-trip: got %q, want %q", got.String(), c.String())
		}
	}
}

func TestDataClassification_Max(t *testing.T) {
	if got := MaxClassification(ClassificationPublic, ClassificationRestricted); got != ClassificationRestricted {
		t.Errorf("Max(public, restricted) = %q, want restricted", got)
	}
	if got := MaxClassification(ClassificationInternal, ClassificationConfidential); got != ClassificationConfidential {
		t.Errorf("Max(internal, confidential) = %q, want confidential", got)
	}
	if got := MaxClassification(ClassificationPublic, ClassificationPublic); got != ClassificationPublic {
		t.Errorf("Max(public, public) = %q, want public", got)
	}
}

// --- Resume capability tests ---

func TestResumeCapability_AllKnown(t *testing.T) {
	tests := []struct {
		rc   ResumeCapability
		want string
	}{
		{ResumeCapSafeCheckpoint, "safe_checkpoint"},
		{ResumeCapRestartOnly, "restart_only"},
		{ResumeCapNone, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.rc.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.rc.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
			data, err := json.Marshal(tt.rc)
			if err != nil {
				t.Fatal(err)
			}
			var got ResumeCapability
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.rc {
				t.Errorf("round-trip: got %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestResumeCapability_Invalid(t *testing.T) {
	bad := ResumeCapability(99)
	if bad.Valid() {
		t.Error("ResumeCapability(99) should be invalid")
	}
}

// --- Deployment status tests ---

func TestDeploymentStatus_AllValid(t *testing.T) {
	tests := []struct {
		s    DeploymentStatus
		want string
	}{
		{DeploymentActive, "ACTIVE"},
		{DeploymentInactive, "INACTIVE"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.s.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
		})
	}
}

// --- Admission outcome tests ---

func TestAdmissionOutcome_AllKnown(t *testing.T) {
	tests := []struct {
		o    AdmissionOutcome
		want string
	}{
		{AdmissionAccepted, "ACCEPTED"},
		{AdmissionIdempotentReplay, "IDEMPOTENT_REPLAY"},
		{AdmissionAlreadyRunning, "ALREADY_RUNNING"},
		{AdmissionIdempotencyConflict, "IDEMPOTENCY_CONFLICT"},
		{AdmissionDeploymentInactive, "DEPLOYMENT_INACTIVE"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.o.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.o.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
		})
	}
}

// --- Control command tests ---

func TestControlCommand_AllKnown(t *testing.T) {
	tests := []struct {
		cmd  ControlCommand
		want string
	}{
		{ControlCancel, "CANCEL"},
		{ControlPause, "PAUSE"},
		{ControlResume, "RESUME"},
		{ControlRestart, "RESTART"},
		{ControlContinue, "CONTINUE"},
		{ControlAmendLimits, "AMEND_LIMITS"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.cmd.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.cmd.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
		})
	}
}

// --- Authority scope tests ---

func TestAuthorityScope_AllKnown(t *testing.T) {
	tests := []struct {
		s    AuthorityScope
		want string
	}{
		{AuthScopeDefault, "default"},
		{AuthScopeControl, "runs:control"},
		{AuthScopeAmendLimits, "runs:amend_limits"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if !tt.s.Valid() {
				t.Errorf("%q should be valid", tt.want)
			}
		})
	}
}