// Package routedrun defines the durable domain contracts for the AgentPaaS
// v0.3.0 Durable Routed Run: deployment, invocation, run, workflow, and
// their associated state machines, enums, and store interfaces.
//
// This package is contracts-only. It contains no store implementations,
// daemon wiring, or behavioral logic beyond state-transition validation.
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

func (id InvocationID) String() string { return string(id) }
func (id InvocationID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *InvocationID) UnmarshalText(b []byte) error { *id = InvocationID(string(b)); return nil }
func (id InvocationID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ControlRequestID) String() string { return string(id) }
func (id ControlRequestID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ControlRequestID) UnmarshalText(b []byte) error { *id = ControlRequestID(string(b)); return nil }
func (id ControlRequestID) Value() (driver.Value, error) { return string(id), nil }
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

func (id LimitAmendmentID) String() string { return string(id) }
func (id LimitAmendmentID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *LimitAmendmentID) UnmarshalText(b []byte) error { *id = LimitAmendmentID(string(b)); return nil }
func (id LimitAmendmentID) Value() (driver.Value, error) { return string(id), nil }
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

func (id WorkflowID) String() string { return string(id) }
func (id WorkflowID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *WorkflowID) UnmarshalText(b []byte) error { *id = WorkflowID(string(b)); return nil }
func (id WorkflowID) Value() (driver.Value, error) { return string(id), nil }
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

func (id NodeID) String() string { return string(id) }
func (id NodeID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *NodeID) UnmarshalText(b []byte) error { *id = NodeID(string(b)); return nil }
func (id NodeID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ServiceID) String() string { return string(id) }
func (id ServiceID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ServiceID) UnmarshalText(b []byte) error { *id = ServiceID(string(b)); return nil }
func (id ServiceID) Value() (driver.Value, error) { return string(id), nil }
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

func (id HandoffID) String() string { return string(id) }
func (id HandoffID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *HandoffID) UnmarshalText(b []byte) error { *id = HandoffID(string(b)); return nil }
func (id HandoffID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ChildBatchID) String() string { return string(id) }
func (id ChildBatchID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ChildBatchID) UnmarshalText(b []byte) error { *id = ChildBatchID(string(b)); return nil }
func (id ChildBatchID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ChildResultID) String() string { return string(id) }
func (id ChildResultID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ChildResultID) UnmarshalText(b []byte) error { *id = ChildResultID(string(b)); return nil }
func (id ChildResultID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ArtifactID) String() string { return string(id) }
func (id ArtifactID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ArtifactID) UnmarshalText(b []byte) error { *id = ArtifactID(string(b)); return nil }
func (id ArtifactID) Value() (driver.Value, error) { return string(id), nil }
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

func (id RunID) String() string { return string(id) }
func (id RunID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *RunID) UnmarshalText(b []byte) error { *id = RunID(string(b)); return nil }
func (id RunID) Value() (driver.Value, error) { return string(id), nil }
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

func (id AttemptID) String() string { return string(id) }
func (id AttemptID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *AttemptID) UnmarshalText(b []byte) error { *id = AttemptID(string(b)); return nil }
func (id AttemptID) Value() (driver.Value, error) { return string(id), nil }
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

func (id LeaseID) String() string { return string(id) }
func (id LeaseID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *LeaseID) UnmarshalText(b []byte) error { *id = LeaseID(string(b)); return nil }
func (id LeaseID) Value() (driver.Value, error) { return string(id), nil }
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

func (id CheckpointID) String() string { return string(id) }
func (id CheckpointID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *CheckpointID) UnmarshalText(b []byte) error { *id = CheckpointID(string(b)); return nil }
func (id CheckpointID) Value() (driver.Value, error) { return string(id), nil }
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

func (id ModelCallID) String() string { return string(id) }
func (id ModelCallID) MarshalText() ([]byte, error) { return []byte(id), nil }
func (id *ModelCallID) UnmarshalText(b []byte) error { *id = ModelCallID(string(b)); return nil }
func (id ModelCallID) Value() (driver.Value, error) { return string(id), nil }
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

// ---------------------------------------------------------------------------
// JSON helpers for typed enums
// ---------------------------------------------------------------------------

// jsonEnumString is a helper for enums that serialize as their string value.
type jsonEnumString string

func (e jsonEnumString) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(e))
}

func (e *jsonEnumString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*e = jsonEnumString(s)
	return nil
}

// jsonEnumStringRejectUnknown is like jsonEnumString but rejects unknown values.
type jsonEnumStringRejectUnknown struct {
	value string
	valid func(string) bool
}

func (e jsonEnumStringRejectUnknown) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.value)
}

func (e *jsonEnumStringRejectUnknown) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if !e.valid(s) {
		return fmt.Errorf("unknown enum value: %q", s)
	}
	e.value = s
	return nil
}