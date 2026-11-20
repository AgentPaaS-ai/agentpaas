// Package routedrun defines the durable domain model for the AgentPaaS
// Durable Routed Run: deployments, aliases, invocations, runs, attempts,
// workflows, time envelopes, artifacts, progress, and store interfaces.
//
// Core concerns:
//   - Stable typed IDs (DeploymentID, InvocationID, RunID, AttemptID, …)
//   - Enumerations and CAS-friendly record types with generation fields
//   - State-transition validation for run/attempt/workflow lifecycles
//   - Store interfaces (DeploymentStore, RunStore, WorkflowStore) plus
//     filesystem and in-memory implementations (LocalStore, MemoryStore)
//   - Write-ahead logging, control journal hooks, artifact workspaces,
//     resume/progress helpers, and time-envelope wiring
//
// Daemon gRPC handlers and the supervisor consume these types; this package
// does not open network listeners or drive containers directly.
package routedrun

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Stable ID types
// ---------------------------------------------------------------------------

// DeploymentID identifies an immutable deployment version.
type DeploymentID string

// DeploymentID.String returns the string representation.
func (id DeploymentID) String() string { return string(id) }

// MarshalText implements encoding.TextMarshaler.
func (id DeploymentID) MarshalText() ([]byte, error) {
	return []byte(id), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *DeploymentID) UnmarshalText(b []byte) error {
	*id = DeploymentID(string(b))
	return nil
}

// Value implements database/sql/driver.Valuer.
func (id DeploymentID) Value() (driver.Value, error) {
	return string(id), nil
}

// Scan implements database/sql.Scanner.
func (id *DeploymentID) Scan(src interface{}) error {
	if src == nil {
		*id = ""
		return nil
	}
	switch v := src.(type) {
	case string:
		*id = DeploymentID(v)
	case []byte:
		*id = DeploymentID(string(v))
	default:
		return fmt.Errorf("cannot scan %T into DeploymentID", src)
	}
	return nil
}

// InvocationID identifies a durable invocation.
type InvocationID string

// InvocationID.String returns the string representation.
func (id InvocationID) String() string { return string(id) }
// InvocationID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id InvocationID) MarshalText() ([]byte, error) { return []byte(id), nil }
// InvocationID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *InvocationID) UnmarshalText(b []byte) error { *id = InvocationID(string(b)); return nil }
// InvocationID.Value returns the database driver value for invocation id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id InvocationID) Value() (driver.Value, error) { return string(id), nil }
// InvocationID.Scan scans a database value into invocation id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *InvocationID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = InvocationID(v)
	case []byte: *id = InvocationID(string(v))
	default: return fmt.Errorf("cannot scan %T into InvocationID", src)
	}
	return nil
}

// ControlRequestID identifies a control request.
type ControlRequestID string

// ControlRequestID.String returns the string representation.
func (id ControlRequestID) String() string { return string(id) }
// ControlRequestID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ControlRequestID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ControlRequestID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ControlRequestID) UnmarshalText(b []byte) error { *id = ControlRequestID(string(b)); return nil }
// ControlRequestID.Value returns the database driver value for control request id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ControlRequestID) Value() (driver.Value, error) { return string(id), nil }
// ControlRequestID.Scan scans a database value into control request id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ControlRequestID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ControlRequestID(v)
	case []byte: *id = ControlRequestID(string(v))
	default: return fmt.Errorf("cannot scan %T into ControlRequestID", src)
	}
	return nil
}

// LimitAmendmentID identifies a limit amendment.
type LimitAmendmentID string

// LimitAmendmentID.String returns the string representation.
func (id LimitAmendmentID) String() string { return string(id) }
// LimitAmendmentID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id LimitAmendmentID) MarshalText() ([]byte, error) { return []byte(id), nil }
// LimitAmendmentID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *LimitAmendmentID) UnmarshalText(b []byte) error { *id = LimitAmendmentID(string(b)); return nil }
// LimitAmendmentID.Value returns the database driver value for limit amendment id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id LimitAmendmentID) Value() (driver.Value, error) { return string(id), nil }
// LimitAmendmentID.Scan scans a database value into limit amendment id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *LimitAmendmentID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = LimitAmendmentID(v)
	case []byte: *id = LimitAmendmentID(string(v))
	default: return fmt.Errorf("cannot scan %T into LimitAmendmentID", src)
	}
	return nil
}

// WorkflowID identifies a workflow.
type WorkflowID string

// WorkflowID.String returns the string representation.
func (id WorkflowID) String() string { return string(id) }
// WorkflowID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id WorkflowID) MarshalText() ([]byte, error) { return []byte(id), nil }
// WorkflowID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *WorkflowID) UnmarshalText(b []byte) error { *id = WorkflowID(string(b)); return nil }
// WorkflowID.Value returns the database driver value for workflow id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id WorkflowID) Value() (driver.Value, error) { return string(id), nil }
// WorkflowID.Scan scans a database value into workflow id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *WorkflowID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = WorkflowID(v)
	case []byte: *id = WorkflowID(string(v))
	default: return fmt.Errorf("cannot scan %T into WorkflowID", src)
	}
	return nil
}

// NodeID identifies a workflow node/stage.
type NodeID string

// NodeID.String returns the string representation.
func (id NodeID) String() string { return string(id) }
// NodeID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id NodeID) MarshalText() ([]byte, error) { return []byte(id), nil }
// NodeID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *NodeID) UnmarshalText(b []byte) error { *id = NodeID(string(b)); return nil }
// NodeID.Value returns the database driver value for node id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id NodeID) Value() (driver.Value, error) { return string(id), nil }
// NodeID.Scan scans a database value into node id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *NodeID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = NodeID(v)
	case []byte: *id = NodeID(string(v))
	default: return fmt.Errorf("cannot scan %T into NodeID", src)
	}
	return nil
}

// ServiceID identifies an MCP service binding.
type ServiceID string

// ServiceID.String returns the string representation.
func (id ServiceID) String() string { return string(id) }
// ServiceID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ServiceID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ServiceID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ServiceID) UnmarshalText(b []byte) error { *id = ServiceID(string(b)); return nil }
// ServiceID.Value returns the database driver value for service id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ServiceID) Value() (driver.Value, error) { return string(id), nil }
// ServiceID.Scan scans a database value into service id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ServiceID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ServiceID(v)
	case []byte: *id = ServiceID(string(v))
	default: return fmt.Errorf("cannot scan %T into ServiceID", src)
	}
	return nil
}

// HandoffID identifies a handoff between stages.
type HandoffID string

// HandoffID.String returns the string representation.
func (id HandoffID) String() string { return string(id) }
// HandoffID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id HandoffID) MarshalText() ([]byte, error) { return []byte(id), nil }
// HandoffID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *HandoffID) UnmarshalText(b []byte) error { *id = HandoffID(string(b)); return nil }
// HandoffID.Value returns the database driver value for handoff id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id HandoffID) Value() (driver.Value, error) { return string(id), nil }
// HandoffID.Scan scans a database value into handoff id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *HandoffID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = HandoffID(v)
	case []byte: *id = HandoffID(string(v))
	default: return fmt.Errorf("cannot scan %T into HandoffID", src)
	}
	return nil
}

// ChildBatchID identifies a child batch.
type ChildBatchID string

// ChildBatchID.String returns the string representation.
func (id ChildBatchID) String() string { return string(id) }
// ChildBatchID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ChildBatchID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ChildBatchID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ChildBatchID) UnmarshalText(b []byte) error { *id = ChildBatchID(string(b)); return nil }
// ChildBatchID.Value returns the database driver value for child batch id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ChildBatchID) Value() (driver.Value, error) { return string(id), nil }
// ChildBatchID.Scan scans a database value into child batch id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ChildBatchID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ChildBatchID(v)
	case []byte: *id = ChildBatchID(string(v))
	default: return fmt.Errorf("cannot scan %T into ChildBatchID", src)
	}
	return nil
}

// ChildResultID identifies a child run result.
type ChildResultID string

// ChildResultID.String returns the string representation.
func (id ChildResultID) String() string { return string(id) }
// ChildResultID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ChildResultID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ChildResultID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ChildResultID) UnmarshalText(b []byte) error { *id = ChildResultID(string(b)); return nil }
// ChildResultID.Value returns the database driver value for child result id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ChildResultID) Value() (driver.Value, error) { return string(id), nil }
// ChildResultID.Scan scans a database value into child result id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ChildResultID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ChildResultID(v)
	case []byte: *id = ChildResultID(string(v))
	default: return fmt.Errorf("cannot scan %T into ChildResultID", src)
	}
	return nil
}

// ArtifactID identifies an artifact.
type ArtifactID string

// ArtifactID.String returns the string representation.
func (id ArtifactID) String() string { return string(id) }
// ArtifactID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ArtifactID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ArtifactID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ArtifactID) UnmarshalText(b []byte) error { *id = ArtifactID(string(b)); return nil }
// ArtifactID.Value returns the database driver value for artifact id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ArtifactID) Value() (driver.Value, error) { return string(id), nil }
// ArtifactID.Scan scans a database value into artifact id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ArtifactID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ArtifactID(v)
	case []byte: *id = ArtifactID(string(v))
	default: return fmt.Errorf("cannot scan %T into ArtifactID", src)
	}
	return nil
}

// RunID identifies a run.
type RunID string

// RunID.String returns the string representation.
func (id RunID) String() string { return string(id) }
// RunID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id RunID) MarshalText() ([]byte, error) { return []byte(id), nil }
// RunID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *RunID) UnmarshalText(b []byte) error { *id = RunID(string(b)); return nil }
// RunID.Value returns the database driver value for run id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id RunID) Value() (driver.Value, error) { return string(id), nil }
// RunID.Scan scans a database value into run id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *RunID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = RunID(v)
	case []byte: *id = RunID(string(v))
	default: return fmt.Errorf("cannot scan %T into RunID", src)
	}
	return nil
}

// AttemptID identifies an attempt within a run.
type AttemptID string

// AttemptID.String returns the string representation.
func (id AttemptID) String() string { return string(id) }
// AttemptID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id AttemptID) MarshalText() ([]byte, error) { return []byte(id), nil }
// AttemptID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *AttemptID) UnmarshalText(b []byte) error { *id = AttemptID(string(b)); return nil }
// AttemptID.Value returns the database driver value for attempt id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id AttemptID) Value() (driver.Value, error) { return string(id), nil }
// AttemptID.Scan scans a database value into attempt id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *AttemptID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = AttemptID(v)
	case []byte: *id = AttemptID(string(v))
	default: return fmt.Errorf("cannot scan %T into AttemptID", src)
	}
	return nil
}

// AttemptIDValidate returns true if the attempt ID looks valid (non-empty, at- prefix).
func AttemptIDValidate(id AttemptID) bool {
	s := string(id)
	return s != "" && strings.HasPrefix(s, "at-") && len(s) > 3
}

// LeaseID identifies an opaque fencing token.
type LeaseID string

// LeaseID.String returns the string representation.
func (id LeaseID) String() string { return string(id) }
// LeaseID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id LeaseID) MarshalText() ([]byte, error) { return []byte(id), nil }
// LeaseID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *LeaseID) UnmarshalText(b []byte) error { *id = LeaseID(string(b)); return nil }
// LeaseID.Value returns the database driver value for lease id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id LeaseID) Value() (driver.Value, error) { return string(id), nil }
// LeaseID.Scan scans a database value into lease id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *LeaseID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = LeaseID(v)
	case []byte: *id = LeaseID(string(v))
	default: return fmt.Errorf("cannot scan %T into LeaseID", src)
	}
	return nil
}

// CheckpointID identifies a checkpoint.
type CheckpointID string

// CheckpointID.String returns the string representation.
func (id CheckpointID) String() string { return string(id) }
// CheckpointID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id CheckpointID) MarshalText() ([]byte, error) { return []byte(id), nil }
// CheckpointID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *CheckpointID) UnmarshalText(b []byte) error { *id = CheckpointID(string(b)); return nil }
// CheckpointID.Value returns the database driver value for checkpoint id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id CheckpointID) Value() (driver.Value, error) { return string(id), nil }
// CheckpointID.Scan scans a database value into checkpoint id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *CheckpointID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = CheckpointID(v)
	case []byte: *id = CheckpointID(string(v))
	default: return fmt.Errorf("cannot scan %T into CheckpointID", src)
	}
	return nil
}

// ModelCallID identifies a model call.
type ModelCallID string

// ModelCallID.String returns the string representation.
func (id ModelCallID) String() string { return string(id) }
// ModelCallID.MarshalText marshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ModelCallID) MarshalText() ([]byte, error) { return []byte(id), nil }
// ModelCallID.UnmarshalText unmarshals the value as text.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ModelCallID) UnmarshalText(b []byte) error { *id = ModelCallID(string(b)); return nil }
// ModelCallID.Value returns the database driver value for model call id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id ModelCallID) Value() (driver.Value, error) { return string(id), nil }
// ModelCallID.Scan scans a database value into model call id.
//
// It returns an error if the operation fails or inputs are invalid.
func (id *ModelCallID) Scan(src interface{}) error {
	if src == nil { *id = ""; return nil }
	switch v := src.(type) {
	case string: *id = ModelCallID(v)
	case []byte: *id = ModelCallID(string(v))
	default: return fmt.Errorf("cannot scan %T into ModelCallID", src)
	}
	return nil
}

// ---------------------------------------------------------------------------
// JSON convenience: MarshalCanonical and UnmarshalStrict
// ---------------------------------------------------------------------------

// MarshalCanonical marshals v to canonical JSON (sorted keys, no indentation).
func MarshalCanonical(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// UnmarshalStrict unmarshals JSON into v, rejecting unknown fields.
func UnmarshalStrict(data []byte, v interface{}) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}