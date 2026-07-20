package routedrun

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Run statuses
// ---------------------------------------------------------------------------

// RunStatus represents the lifecycle status of a run.
type RunStatus int

const (
	RunStatusPending        RunStatus = iota // 0
	RunStatusRunning                         // 1
	RunStatusPauseRequested                  // 2
	RunStatusPaused                          // 3
	RunStatusNeedsReplan                     // 4
	RunStatusSucceeded                       // 5
	RunStatusFailed                          // 6
	RunStatusCancelled                       // 7
	RunStatusBudgetExceeded                  // 8
	RunStatusExpired                         // 9
)

var runStatusNames = map[RunStatus]string{
	RunStatusPending:        "PENDING",
	RunStatusRunning:        "RUNNING",
	RunStatusPauseRequested: "PAUSE_REQUESTED",
	RunStatusPaused:         "PAUSED",
	RunStatusNeedsReplan:    "NEEDS_REPLAN",
	RunStatusSucceeded:      "SUCCEEDED",
	RunStatusFailed:         "FAILED",
	RunStatusCancelled:      "CANCELLED",
	RunStatusBudgetExceeded: "BUDGET_EXCEEDED",
	RunStatusExpired:        "EXPIRED",
}

var runStatusValues = func() map[string]RunStatus {
	m := make(map[string]RunStatus, len(runStatusNames))
	for k, v := range runStatusNames {
		m[v] = k
	}
	return m
}()

// RunStatus.String returns the string representation.
func (s RunStatus) String() string {
	if name, ok := runStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// Valid returns true if s is a known RunStatus.
func (s RunStatus) Valid() bool {
	_, ok := runStatusNames[s]
	return ok
}

// MarshalJSON implements json.Marshaler.
func (s RunStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid RunStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *RunStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("run status unmarshal json: %w", err)
	}
	v, ok := runStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown RunStatus: %q", str)
	}
	*s = v
	return nil
}

// AllRunStatuses returns all valid RunStatus values.
func AllRunStatuses() []RunStatus {
	return []RunStatus{
		RunStatusPending, RunStatusRunning, RunStatusPauseRequested,
		RunStatusPaused, RunStatusNeedsReplan, RunStatusSucceeded,
		RunStatusFailed, RunStatusCancelled, RunStatusBudgetExceeded,
		RunStatusExpired,
	}
}

// IsTerminal returns true for terminal run statuses.
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCancelled,
		RunStatusBudgetExceeded, RunStatusExpired:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Workflow statuses
// ---------------------------------------------------------------------------

// WorkflowStatus represents the lifecycle status of a workflow.
type WorkflowStatus int

const (
	WorkflowStatusPending        WorkflowStatus = iota // 0
	WorkflowStatusRunning                              // 1
	WorkflowStatusPauseRequested                       // 2
	WorkflowStatusPaused                               // 3
	WorkflowStatusNeedsReplan                          // 4
	WorkflowStatusSucceeded                            // 5
	WorkflowStatusFailed                               // 6
	WorkflowStatusCancelled                            // 7
	WorkflowStatusExpired                              // 8
	WorkflowStatusBudgetExceeded                       // 9
)

var workflowStatusNames = map[WorkflowStatus]string{
	WorkflowStatusPending:        "PENDING",
	WorkflowStatusRunning:        "RUNNING",
	WorkflowStatusPauseRequested: "PAUSE_REQUESTED",
	WorkflowStatusPaused:         "PAUSED",
	WorkflowStatusNeedsReplan:    "NEEDS_REPLAN",
	WorkflowStatusSucceeded:      "SUCCEEDED",
	WorkflowStatusFailed:         "FAILED",
	WorkflowStatusCancelled:      "CANCELLED",
	WorkflowStatusExpired:        "EXPIRED",
	WorkflowStatusBudgetExceeded: "BUDGET_EXCEEDED",
}

var workflowStatusValues = func() map[string]WorkflowStatus {
	m := make(map[string]WorkflowStatus, len(workflowStatusNames))
	for k, v := range workflowStatusNames {
		m[v] = k
	}
	return m
}()

// WorkflowStatus.String returns the string representation.
func (s WorkflowStatus) String() string {
	if name, ok := workflowStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// WorkflowStatus.Valid reports whether the workflow status value is valid.
func (s WorkflowStatus) Valid() bool {
	_, ok := workflowStatusNames[s]
	return ok
}

// WorkflowStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s WorkflowStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid WorkflowStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// WorkflowStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *WorkflowStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("workflow status unmarshal json: %w", err)
	}
	v, ok := workflowStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown WorkflowStatus: %q", str)
	}
	*s = v
	return nil
}

// AllWorkflowStatuses returns all valid WorkflowStatus values.
func AllWorkflowStatuses() []WorkflowStatus {
	return []WorkflowStatus{
		WorkflowStatusPending, WorkflowStatusRunning, WorkflowStatusPauseRequested,
		WorkflowStatusPaused, WorkflowStatusNeedsReplan, WorkflowStatusSucceeded,
		WorkflowStatusFailed, WorkflowStatusCancelled, WorkflowStatusExpired,
		WorkflowStatusBudgetExceeded,
	}
}

// IsTerminal returns true for terminal workflow statuses.
func (s WorkflowStatus) IsTerminal() bool {
	switch s {
	case WorkflowStatusSucceeded, WorkflowStatusFailed, WorkflowStatusCancelled,
		WorkflowStatusExpired, WorkflowStatusBudgetExceeded:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Node statuses
// ---------------------------------------------------------------------------

// NodeStatus represents the lifecycle status of a pipeline node/stage.
type NodeStatus int

const (
	NodeStatusPending        NodeStatus = iota // 0
	NodeStatusReady                            // 1
	NodeStatusLaunching                        // 2
	NodeStatusRunning                          // 3
	NodeStatusPauseRequested                   // 4
	NodeStatusPaused                           // 5
	NodeStatusNeedsReplan                      // 6
	NodeStatusSucceeded                        // 7
	NodeStatusFailed                           // 8
	NodeStatusCancelled                        // 9
	NodeStatusSkipped                          // 10
)

var nodeStatusNames = map[NodeStatus]string{
	NodeStatusPending:        "PENDING",
	NodeStatusReady:          "READY",
	NodeStatusLaunching:      "LAUNCHING",
	NodeStatusRunning:        "RUNNING",
	NodeStatusPauseRequested: "PAUSE_REQUESTED",
	NodeStatusPaused:         "PAUSED",
	NodeStatusNeedsReplan:    "NEEDS_REPLAN",
	NodeStatusSucceeded:      "SUCCEEDED",
	NodeStatusFailed:         "FAILED",
	NodeStatusCancelled:      "CANCELLED",
	NodeStatusSkipped:        "SKIPPED",
}

var nodeStatusValues = func() map[string]NodeStatus {
	m := make(map[string]NodeStatus, len(nodeStatusNames))
	for k, v := range nodeStatusNames {
		m[v] = k
	}
	return m
}()

// NodeStatus.String returns the string representation.
func (s NodeStatus) String() string {
	if name, ok := nodeStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// NodeStatus.Valid reports whether the node status value is valid.
func (s NodeStatus) Valid() bool {
	_, ok := nodeStatusNames[s]
	return ok
}

// NodeStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s NodeStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid NodeStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// NodeStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *NodeStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("node status unmarshal json: %w", err)
	}
	v, ok := nodeStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown NodeStatus: %q", str)
	}
	*s = v
	return nil
}

// AllNodeStatuses returns all valid NodeStatus values.
func AllNodeStatuses() []NodeStatus {
	return []NodeStatus{
		NodeStatusPending, NodeStatusReady, NodeStatusLaunching,
		NodeStatusRunning, NodeStatusPauseRequested, NodeStatusPaused,
		NodeStatusNeedsReplan, NodeStatusSucceeded, NodeStatusFailed,
		NodeStatusCancelled, NodeStatusSkipped,
	}
}

// ---------------------------------------------------------------------------
// Service statuses
// ---------------------------------------------------------------------------

// ServiceStatus represents the lifecycle status of an MCP service binding.
type ServiceStatus int

const (
	ServiceStatusDeclared  ServiceStatus = iota // 0
	ServiceStatusStarting                       // 1
	ServiceStatusReady                          // 2
	ServiceStatusUnhealthy                      // 3
	ServiceStatusFenced                         // 4
	ServiceStatusStopping                       // 5
	ServiceStatusStopped                        // 6
	ServiceStatusFailed                         // 7
)

var serviceStatusNames = map[ServiceStatus]string{
	ServiceStatusDeclared:  "DECLARED",
	ServiceStatusStarting:  "STARTING",
	ServiceStatusReady:     "READY",
	ServiceStatusUnhealthy: "UNHEALTHY",
	ServiceStatusFenced:    "FENCED",
	ServiceStatusStopping:  "STOPPING",
	ServiceStatusStopped:   "STOPPED",
	ServiceStatusFailed:    "FAILED",
}

var serviceStatusValues = func() map[string]ServiceStatus {
	m := make(map[string]ServiceStatus, len(serviceStatusNames))
	for k, v := range serviceStatusNames {
		m[v] = k
	}
	return m
}()

// ServiceStatus.String returns the string representation.
func (s ServiceStatus) String() string {
	if name, ok := serviceStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// ServiceStatus.Valid reports whether the service status value is valid.
func (s ServiceStatus) Valid() bool {
	_, ok := serviceStatusNames[s]
	return ok
}

// ServiceStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s ServiceStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid ServiceStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// ServiceStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *ServiceStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("service status unmarshal json: %w", err)
	}
	v, ok := serviceStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown ServiceStatus: %q", str)
	}
	*s = v
	return nil
}

// AllServiceStatuses returns all valid ServiceStatus values.
func AllServiceStatuses() []ServiceStatus {
	return []ServiceStatus{
		ServiceStatusDeclared, ServiceStatusStarting, ServiceStatusReady,
		ServiceStatusUnhealthy, ServiceStatusFenced, ServiceStatusStopping,
		ServiceStatusStopped, ServiceStatusFailed,
	}
}

// ---------------------------------------------------------------------------
// Child batch statuses
// ---------------------------------------------------------------------------

// ChildBatchStatus represents the lifecycle status of a child batch.
type ChildBatchStatus int

const (
	ChildBatchIntent         ChildBatchStatus = iota // 0
	ChildBatchAllocated                              // 1
	ChildBatchRunning                                // 2
	ChildBatchPauseRequested                         // 3
	ChildBatchPaused                                 // 4
	ChildBatchJoining                                // 5
	ChildBatchStopping                               // 6
	ChildBatchStopped                                // 7
	ChildBatchSucceeded                              // 8
	ChildBatchFailed                                 // 9
	ChildBatchCancelled                              // 10
)

var childBatchStatusNames = map[ChildBatchStatus]string{
	ChildBatchIntent:         "INTENT",
	ChildBatchAllocated:      "ALLOCATED",
	ChildBatchRunning:        "RUNNING",
	ChildBatchPauseRequested: "PAUSE_REQUESTED",
	ChildBatchPaused:         "PAUSED",
	ChildBatchJoining:        "JOINING",
	ChildBatchStopping:       "STOPPING",
	ChildBatchStopped:        "STOPPED",
	ChildBatchSucceeded:      "SUCCEEDED",
	ChildBatchFailed:         "FAILED",
	ChildBatchCancelled:      "CANCELLED",
}

var childBatchStatusValues = func() map[string]ChildBatchStatus {
	m := make(map[string]ChildBatchStatus, len(childBatchStatusNames))
	for k, v := range childBatchStatusNames {
		m[v] = k
	}
	return m
}()

// ChildBatchStatus.String returns the string representation.
func (s ChildBatchStatus) String() string {
	if name, ok := childBatchStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// ChildBatchStatus.Valid reports whether the child batch status value is valid.
func (s ChildBatchStatus) Valid() bool {
	_, ok := childBatchStatusNames[s]
	return ok
}

// ChildBatchStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s ChildBatchStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid ChildBatchStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// ChildBatchStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *ChildBatchStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("child batch status unmarshal json: %w", err)
	}
	v, ok := childBatchStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown ChildBatchStatus: %q", str)
	}
	*s = v
	return nil
}

// AllChildBatchStatuses returns all valid ChildBatchStatus values.
func AllChildBatchStatuses() []ChildBatchStatus {
	return []ChildBatchStatus{
		ChildBatchIntent, ChildBatchAllocated, ChildBatchRunning,
		ChildBatchPauseRequested, ChildBatchPaused, ChildBatchJoining,
		ChildBatchStopping, ChildBatchStopped, ChildBatchSucceeded,
		ChildBatchFailed, ChildBatchCancelled,
	}
}

// ---------------------------------------------------------------------------
// Attempt statuses
// ---------------------------------------------------------------------------

// AttemptStatus represents the lifecycle status of an attempt.
type AttemptStatus int

const (
	AttemptStatusPending     AttemptStatus = iota // 0
	AttemptStatusRunning                          // 1
	AttemptStatusNeedsReplan                      // 2
	AttemptStatusSucceeded                        // 3
	AttemptStatusFailed                           // 4
	AttemptStatusFenced                           // 5
	AttemptStatusCancelled                        // 6
)

var attemptStatusNames = map[AttemptStatus]string{
	AttemptStatusPending:     "PENDING",
	AttemptStatusRunning:     "RUNNING",
	AttemptStatusNeedsReplan: "NEEDS_REPLAN",
	AttemptStatusSucceeded:   "SUCCEEDED",
	AttemptStatusFailed:      "FAILED",
	AttemptStatusFenced:      "FENCED",
	AttemptStatusCancelled:   "CANCELLED",
}

var attemptStatusValues = func() map[string]AttemptStatus {
	m := make(map[string]AttemptStatus, len(attemptStatusNames))
	for k, v := range attemptStatusNames {
		m[v] = k
	}
	return m
}()

// AttemptStatus.String returns the string representation.
func (s AttemptStatus) String() string {
	if name, ok := attemptStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// AttemptStatus.Valid reports whether the attempt status value is valid.
func (s AttemptStatus) Valid() bool {
	_, ok := attemptStatusNames[s]
	return ok
}

// AttemptStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s AttemptStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid AttemptStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// AttemptStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *AttemptStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("attempt status unmarshal json: %w", err)
	}
	v, ok := attemptStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown AttemptStatus: %q", str)
	}
	*s = v
	return nil
}

// AllAttemptStatuses returns all valid AttemptStatus values.
func AllAttemptStatuses() []AttemptStatus {
	return []AttemptStatus{
		AttemptStatusPending, AttemptStatusRunning, AttemptStatusNeedsReplan,
		AttemptStatusSucceeded, AttemptStatusFailed, AttemptStatusFenced,
		AttemptStatusCancelled,
	}
}

// IsTerminal returns true for terminal attempt statuses.
func (s AttemptStatus) IsTerminal() bool {
	switch s {
	case AttemptStatusSucceeded, AttemptStatusFailed,
		AttemptStatusFenced, AttemptStatusCancelled:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Failure reasons
// ---------------------------------------------------------------------------

// FailureReason categorises why an attempt or run failed.
type FailureReason int

const (
	FailureModelTimeout              FailureReason = iota // 0
	FailureModelConnectionFailed                          // 1
	FailureModelRateLimited                               // 2
	FailureModelServiceError                              // 3
	FailureModelContextLimit                              // 4
	FailureModelOutputLimit                               // 5
	FailureModelMalformedJSON                             // 6
	FailureModelIdentityMismatch                          // 7
	FailureModelAuthUnavailable                           // 8
	FailureModelQuotaExhausted                            // 9
	FailureNoEligibleTarget                               // 10
	FailureAttemptTimeExhausted                           // 11
	FailureStallTimeout                                   // 12
	FailureNoProgressGuardrail                            // 13
	FailureRepeatedActionGuardrail                        // 14
	FailureLLMBudgetExhausted                             // 15
	FailureActiveTimeExhausted                            // 16
	FailurePolicyDenied                                   // 17
	FailureExternalDependencyFailed                       // 18
	FailureAgentException                                 // 19
	FailureCheckpointUnavailable                          // 20
	FailureDaemonRestarted                                // 21
	FailureUserCancelled                                  // 22
	FailureMCPServiceUnavailable                          // 23
	FailureMCPProtocolError                               // 24
	FailureHandoffMissing                                 // 25
	FailureHandoffInvalid                                 // 26
	FailureChildSpawnDenied                               // 27
	FailureChildBatchFailed                               // 28
	FailureWorkflowResourceExhausted                      // 29
	FailurePauseBoundaryUnavailable                       // 30
)

var failureReasonNames = map[FailureReason]string{
	FailureModelTimeout:              "MODEL_TIMEOUT",
	FailureModelConnectionFailed:     "MODEL_CONNECTION_FAILED",
	FailureModelRateLimited:          "MODEL_RATE_LIMITED",
	FailureModelServiceError:         "MODEL_SERVICE_ERROR",
	FailureModelContextLimit:         "MODEL_CONTEXT_LIMIT",
	FailureModelOutputLimit:          "MODEL_OUTPUT_LIMIT",
	FailureModelMalformedJSON:        "MODEL_MALFORMED_JSON",
	FailureModelIdentityMismatch:     "MODEL_IDENTITY_MISMATCH",
	FailureModelAuthUnavailable:      "MODEL_AUTH_UNAVAILABLE",
	FailureModelQuotaExhausted:       "MODEL_QUOTA_EXHAUSTED",
	FailureNoEligibleTarget:          "NO_ELIGIBLE_TARGET",
	FailureAttemptTimeExhausted:      "ATTEMPT_TIME_EXHAUSTED",
	FailureStallTimeout:              "STALL_TIMEOUT",
	FailureNoProgressGuardrail:       "NO_PROGRESS_GUARDRAIL",
	FailureRepeatedActionGuardrail:   "REPEATED_ACTION_GUARDRAIL",
	FailureLLMBudgetExhausted:        "LLM_BUDGET_EXHAUSTED",
	FailureActiveTimeExhausted:       "ACTIVE_TIME_EXHAUSTED",
	FailurePolicyDenied:              "POLICY_DENIED",
	FailureExternalDependencyFailed:  "EXTERNAL_DEPENDENCY_FAILED",
	FailureAgentException:            "AGENT_EXCEPTION",
	FailureCheckpointUnavailable:     "CHECKPOINT_UNAVAILABLE",
	FailureDaemonRestarted:           "DAEMON_RESTARTED",
	FailureUserCancelled:             "USER_CANCELLED",
	FailureMCPServiceUnavailable:     "MCP_SERVICE_UNAVAILABLE",
	FailureMCPProtocolError:          "MCP_PROTOCOL_ERROR",
	FailureHandoffMissing:            "HANDOFF_MISSING",
	FailureHandoffInvalid:            "HANDOFF_INVALID",
	FailureChildSpawnDenied:          "CHILD_SPAWN_DENIED",
	FailureChildBatchFailed:          "CHILD_BATCH_FAILED",
	FailureWorkflowResourceExhausted: "WORKFLOW_RESOURCE_EXHAUSTED",
	FailurePauseBoundaryUnavailable:  "PAUSE_BOUNDARY_UNAVAILABLE",
}

var failureReasonValues = func() map[string]FailureReason {
	m := make(map[string]FailureReason, len(failureReasonNames))
	for k, v := range failureReasonNames {
		m[v] = k
	}
	return m
}()

// FailureReason.String returns the string representation.
func (r FailureReason) String() string {
	if name, ok := failureReasonNames[r]; ok {
		return name
	}
	return "UNKNOWN"
}

// FailureReason.Valid reports whether the failure reason value is valid.
func (r FailureReason) Valid() bool {
	_, ok := failureReasonNames[r]
	return ok
}

// FailureReason.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (r FailureReason) MarshalJSON() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("invalid FailureReason: %d", r)
	}
	return json.Marshal(r.String())
}

// FailureReason.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (r *FailureReason) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("failure reason unmarshal json: %w", err)
	}
	v, ok := failureReasonValues[str]
	if !ok {
		return fmt.Errorf("unknown FailureReason: %q", str)
	}
	*r = v
	return nil
}

// AllFailureReasons returns all valid FailureReason values.
func AllFailureReasons() []FailureReason {
	return []FailureReason{
		FailureModelTimeout, FailureModelConnectionFailed,
		FailureModelRateLimited, FailureModelServiceError,
		FailureModelContextLimit, FailureModelOutputLimit,
		FailureModelMalformedJSON, FailureModelIdentityMismatch,
		FailureModelAuthUnavailable, FailureModelQuotaExhausted,
		FailureNoEligibleTarget, FailureAttemptTimeExhausted,
		FailureStallTimeout, FailureNoProgressGuardrail,
		FailureRepeatedActionGuardrail, FailureLLMBudgetExhausted,
		FailureActiveTimeExhausted, FailurePolicyDenied,
		FailureExternalDependencyFailed, FailureAgentException,
		FailureCheckpointUnavailable, FailureDaemonRestarted,
		FailureUserCancelled, FailureMCPServiceUnavailable,
		FailureMCPProtocolError, FailureHandoffMissing,
		FailureHandoffInvalid, FailureChildSpawnDenied,
		FailureChildBatchFailed, FailureWorkflowResourceExhausted,
		FailurePauseBoundaryUnavailable,
	}
}

// ---------------------------------------------------------------------------
// Failure scope
// ---------------------------------------------------------------------------

// FailureScope categorises where a failure originated.
type FailureScope int

const (
	FailureScopeModelCall  FailureScope = iota // 0
	FailureScopeWorker                         // 1
	FailureScopeBudget                         // 2
	FailureScopePolicy                         // 3
	FailureScopeCredential                     // 4
	FailureScopeExternal                       // 5
	FailureScopePlatform                       // 6
	FailureScopeWorkflow                       // 7
	FailureScopeMCPService                     // 8
	FailureScopeHandoff                        // 9
	FailureScopeChildBatch                     // 10
)

var failureScopeNames = map[FailureScope]string{
	FailureScopeModelCall:  "model_call",
	FailureScopeWorker:     "worker",
	FailureScopeBudget:     "budget",
	FailureScopePolicy:     "policy",
	FailureScopeCredential: "credential",
	FailureScopeExternal:   "external",
	FailureScopePlatform:   "platform",
	FailureScopeWorkflow:   "workflow",
	FailureScopeMCPService: "mcp_service",
	FailureScopeHandoff:    "handoff",
	FailureScopeChildBatch: "child_batch",
}

var failureScopeValues = func() map[string]FailureScope {
	m := make(map[string]FailureScope, len(failureScopeNames))
	for k, v := range failureScopeNames {
		m[v] = k
	}
	return m
}()

// FailureScope.String returns the string representation.
func (s FailureScope) String() string {
	if name, ok := failureScopeNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// FailureScope.Valid reports whether the failure scope value is valid.
func (s FailureScope) Valid() bool {
	_, ok := failureScopeNames[s]
	return ok
}

// FailureScope.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s FailureScope) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid FailureScope: %d", s)
	}
	return json.Marshal(s.String())
}

// FailureScope.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *FailureScope) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("failure scope unmarshal json: %w", err)
	}
	v, ok := failureScopeValues[str]
	if !ok {
		return fmt.Errorf("unknown FailureScope: %q", str)
	}
	*s = v
	return nil
}

// AllFailureScopes returns all valid FailureScope values.
func AllFailureScopes() []FailureScope {
	return []FailureScope{
		FailureScopeModelCall, FailureScopeWorker, FailureScopeBudget,
		FailureScopePolicy, FailureScopeCredential, FailureScopeExternal,
		FailureScopePlatform, FailureScopeWorkflow, FailureScopeMCPService,
		FailureScopeHandoff, FailureScopeChildBatch,
	}
}

// ---------------------------------------------------------------------------
// Recovery disposition
// ---------------------------------------------------------------------------

// RecoveryDisposition describes whether and how an attempt may be recovered.
type RecoveryDisposition int

const (
	RecoveryNotNeeded     RecoveryDisposition = iota // 0
	RecoveryAutoRecovered                            // 1
	RecoveryNeedsReplan                              // 2
	RecoveryTerminal                                 // 3
)

var recoveryDispositionNames = map[RecoveryDisposition]string{
	RecoveryNotNeeded:     "not_needed",
	RecoveryAutoRecovered: "auto_recovered",
	RecoveryNeedsReplan:   "needs_replan",
	RecoveryTerminal:      "terminal",
}

var recoveryDispositionValues = func() map[string]RecoveryDisposition {
	m := make(map[string]RecoveryDisposition, len(recoveryDispositionNames))
	for k, v := range recoveryDispositionNames {
		m[v] = k
	}
	return m
}()

// RecoveryDisposition.String returns the string representation.
func (d RecoveryDisposition) String() string {
	if name, ok := recoveryDispositionNames[d]; ok {
		return name
	}
	return "UNKNOWN"
}

// RecoveryDisposition.Valid reports whether the recovery disposition value is valid.
func (d RecoveryDisposition) Valid() bool {
	_, ok := recoveryDispositionNames[d]
	return ok
}

// RecoveryDisposition.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (d RecoveryDisposition) MarshalJSON() ([]byte, error) {
	if !d.Valid() {
		return nil, fmt.Errorf("invalid RecoveryDisposition: %d", d)
	}
	return json.Marshal(d.String())
}

// RecoveryDisposition.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (d *RecoveryDisposition) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("recovery disposition unmarshal json: %w", err)
	}
	v, ok := recoveryDispositionValues[str]
	if !ok {
		return fmt.Errorf("unknown RecoveryDisposition: %q", str)
	}
	*d = v
	return nil
}

// ---------------------------------------------------------------------------
// Data classification
// ---------------------------------------------------------------------------

// DataClassification represents the sensitivity level of data.
type DataClassification string

const (
	ClassificationPublic       DataClassification = "public"
	ClassificationInternal     DataClassification = "internal"
	ClassificationConfidential DataClassification = "confidential"
	ClassificationRestricted   DataClassification = "restricted"
)

// AllDataClassifications returns all valid DataClassification values in order.
func AllDataClassifications() []DataClassification {
	return []DataClassification{
		ClassificationPublic,
		ClassificationInternal,
		ClassificationConfidential,
		ClassificationRestricted,
	}
}

// DataClassification.String returns the string representation.
func (c DataClassification) String() string {
	return string(c)
}

// Valid returns true if c is a known DataClassification.
func (c DataClassification) Valid() bool {
	switch c {
	case ClassificationPublic, ClassificationInternal,
		ClassificationConfidential, ClassificationRestricted:
		return true
	}
	return false
}

// Level returns a numeric level for ordering: public=0, internal=1,
// confidential=2, restricted=3.
func (c DataClassification) Level() int {
	switch c {
	case ClassificationPublic:
		return 0
	case ClassificationInternal:
		return 1
	case ClassificationConfidential:
		return 2
	case ClassificationRestricted:
		return 3
	default:
		return -1
	}
}

// MaxClassification returns the more restrictive of a and b.
func MaxClassification(a, b DataClassification) DataClassification {
	if a.Level() >= b.Level() {
		return a
	}
	return b
}

// DataClassification.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (c DataClassification) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("invalid DataClassification: %q", string(c))
	}
	return json.Marshal(string(c))
}

// DataClassification.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (c *DataClassification) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("data classification unmarshal json: %w", err)
	}
	dc := DataClassification(s)
	if !dc.Valid() {
		return fmt.Errorf("unknown DataClassification: %q", s)
	}
	*c = dc
	return nil
}

// ---------------------------------------------------------------------------
// Resume capability
// ---------------------------------------------------------------------------

// ResumeCapability describes what resume strategies are available.
type ResumeCapability int

const (
	ResumeCapSafeCheckpoint ResumeCapability = iota // 0
	ResumeCapRestartOnly                            // 1
	ResumeCapNone                                   // 2
)

var resumeCapabilityNames = map[ResumeCapability]string{
	ResumeCapSafeCheckpoint: "safe_checkpoint",
	ResumeCapRestartOnly:    "restart_only",
	ResumeCapNone:           "none",
}

var resumeCapabilityValues = func() map[string]ResumeCapability {
	m := make(map[string]ResumeCapability, len(resumeCapabilityNames))
	for k, v := range resumeCapabilityNames {
		m[v] = k
	}
	return m
}()

// ResumeCapability.String returns the string representation.
func (rc ResumeCapability) String() string {
	if name, ok := resumeCapabilityNames[rc]; ok {
		return name
	}
	return "UNKNOWN"
}

// ResumeCapability.Valid reports whether the resume capability value is valid.
func (rc ResumeCapability) Valid() bool {
	_, ok := resumeCapabilityNames[rc]
	return ok
}

// ResumeCapability.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (rc ResumeCapability) MarshalJSON() ([]byte, error) {
	if !rc.Valid() {
		return nil, fmt.Errorf("invalid ResumeCapability: %d", rc)
	}
	return json.Marshal(rc.String())
}

// ResumeCapability.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (rc *ResumeCapability) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("resume capability unmarshal json: %w", err)
	}
	v, ok := resumeCapabilityValues[str]
	if !ok {
		return fmt.Errorf("unknown ResumeCapability: %q", str)
	}
	*rc = v
	return nil
}

// ---------------------------------------------------------------------------
// Deployment status
// ---------------------------------------------------------------------------

// DeploymentStatus represents the lifecycle status of a deployment.
type DeploymentStatus int

const (
	DeploymentActive   DeploymentStatus = iota // 0
	DeploymentInactive                         // 1
)

var deploymentStatusNames = map[DeploymentStatus]string{
	DeploymentActive:   "ACTIVE",
	DeploymentInactive: "INACTIVE",
}

var deploymentStatusValues = func() map[string]DeploymentStatus {
	m := make(map[string]DeploymentStatus, len(deploymentStatusNames))
	for k, v := range deploymentStatusNames {
		m[v] = k
	}
	return m
}()

// DeploymentStatus.String returns the string representation.
func (s DeploymentStatus) String() string {
	if name, ok := deploymentStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// DeploymentStatus.Valid reports whether the deployment status value is valid.
func (s DeploymentStatus) Valid() bool {
	_, ok := deploymentStatusNames[s]
	return ok
}

// DeploymentStatus.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s DeploymentStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid DeploymentStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// DeploymentStatus.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *DeploymentStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("deployment status unmarshal json: %w", err)
	}
	v, ok := deploymentStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown DeploymentStatus: %q", str)
	}
	*s = v
	return nil
}

// ---------------------------------------------------------------------------
// Admission outcome
// ---------------------------------------------------------------------------

// AdmissionOutcome represents the result of attempting to admit an invocation.
type AdmissionOutcome int

const (
	AdmissionAccepted            AdmissionOutcome = iota // 0
	AdmissionIdempotentReplay                            // 1
	AdmissionAlreadyRunning                              // 2
	AdmissionIdempotencyConflict                         // 3
	AdmissionDeploymentInactive                          // 4
)

var admissionOutcomeNames = map[AdmissionOutcome]string{
	AdmissionAccepted:            "ACCEPTED",
	AdmissionIdempotentReplay:    "IDEMPOTENT_REPLAY",
	AdmissionAlreadyRunning:      "ALREADY_RUNNING",
	AdmissionIdempotencyConflict: "IDEMPOTENCY_CONFLICT",
	AdmissionDeploymentInactive:  "DEPLOYMENT_INACTIVE",
}

var admissionOutcomeValues = func() map[string]AdmissionOutcome {
	m := make(map[string]AdmissionOutcome, len(admissionOutcomeNames))
	for k, v := range admissionOutcomeNames {
		m[v] = k
	}
	return m
}()

// AdmissionOutcome.String returns the string representation.
func (o AdmissionOutcome) String() string {
	if name, ok := admissionOutcomeNames[o]; ok {
		return name
	}
	return "UNKNOWN"
}

// AdmissionOutcome.Valid reports whether the admission outcome value is valid.
func (o AdmissionOutcome) Valid() bool {
	_, ok := admissionOutcomeNames[o]
	return ok
}

// AdmissionOutcome.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (o AdmissionOutcome) MarshalJSON() ([]byte, error) {
	if !o.Valid() {
		return nil, fmt.Errorf("invalid AdmissionOutcome: %d", o)
	}
	return json.Marshal(o.String())
}

// AdmissionOutcome.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (o *AdmissionOutcome) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("admission outcome unmarshal json: %w", err)
	}
	v, ok := admissionOutcomeValues[str]
	if !ok {
		return fmt.Errorf("unknown AdmissionOutcome: %q", str)
	}
	*o = v
	return nil
}

// ---------------------------------------------------------------------------
// Control command
// ---------------------------------------------------------------------------

// ControlCommand represents an operator command for a run/workflow.
type ControlCommand int

const (
	ControlCancel      ControlCommand = iota // 0
	ControlPause                             // 1
	ControlResume                            // 2
	ControlRestart                           // 3
	ControlContinue                          // 4
	ControlAmendLimits                       // 5
)

var controlCommandNames = map[ControlCommand]string{
	ControlCancel:      "CANCEL",
	ControlPause:       "PAUSE",
	ControlResume:      "RESUME",
	ControlRestart:     "RESTART",
	ControlContinue:    "CONTINUE",
	ControlAmendLimits: "AMEND_LIMITS",
}

var controlCommandValues = func() map[string]ControlCommand {
	m := make(map[string]ControlCommand, len(controlCommandNames))
	for k, v := range controlCommandNames {
		m[v] = k
	}
	return m
}()

// ControlCommand.String returns the string representation.
func (c ControlCommand) String() string {
	if name, ok := controlCommandNames[c]; ok {
		return name
	}
	return "UNKNOWN"
}

// ControlCommand.Valid reports whether the control command value is valid.
func (c ControlCommand) Valid() bool {
	_, ok := controlCommandNames[c]
	return ok
}

// ControlCommand.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (c ControlCommand) MarshalJSON() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("invalid ControlCommand: %d", c)
	}
	return json.Marshal(c.String())
}

// ControlCommand.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (c *ControlCommand) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("control command unmarshal json: %w", err)
	}
	v, ok := controlCommandValues[str]
	if !ok {
		return fmt.Errorf("unknown ControlCommand: %q", str)
	}
	*c = v
	return nil
}

// ---------------------------------------------------------------------------
// Authority scope
// ---------------------------------------------------------------------------

// AuthorityScope represents an administrative authority scope.
type AuthorityScope string

const (
	AuthScopeDefault     AuthorityScope = "default"
	AuthScopeControl     AuthorityScope = "runs:control"
	AuthScopeAmendLimits AuthorityScope = "runs:amend_limits"
)

// AuthorityScope.String returns the string representation.
func (s AuthorityScope) String() string {
	return string(s)
}

// AuthorityScope.Valid reports whether the authority scope value is valid.
func (s AuthorityScope) Valid() bool {
	switch s {
	case AuthScopeDefault, AuthScopeControl, AuthScopeAmendLimits:
		return true
	}
	return false
}

// AuthorityScope.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s AuthorityScope) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid AuthorityScope: %q", string(s))
	}
	return json.Marshal(string(s))
}

// AuthorityScope.UnmarshalJSON unmarshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *AuthorityScope) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("authority scope unmarshal json: %w", err)
	}
	as := AuthorityScope(str)
	if !as.Valid() {
		return fmt.Errorf("unknown AuthorityScope: %q", str)
	}
	*s = as
	return nil
}
