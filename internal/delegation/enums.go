package delegation

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Task status
// ---------------------------------------------------------------------------

// TaskStatus represents the lifecycle status of a delegated task.
type TaskStatus int

const (
	TaskStatusPending   TaskStatus = iota // 0
	TaskStatusAdmitted                    // 1
	TaskStatusRunning                     // 2
	TaskStatusSucceeded                   // 3
	TaskStatusFailed                      // 4
	TaskStatusCancelled                   // 5
	TaskStatusExpired                     // 6
	TaskStatusDenied                      // 7
)

var taskStatusNames = map[TaskStatus]string{
	TaskStatusPending:   "PENDING",
	TaskStatusAdmitted:  "ADMITTED",
	TaskStatusRunning:   "RUNNING",
	TaskStatusSucceeded: "SUCCEEDED",
	TaskStatusFailed:    "FAILED",
	TaskStatusCancelled: "CANCELLED",
	TaskStatusExpired:   "EXPIRED",
	TaskStatusDenied:    "DENIED",
}

var taskStatusValues = func() map[string]TaskStatus {
	m := make(map[string]TaskStatus, len(taskStatusNames))
	for k, v := range taskStatusNames {
		m[v] = k
	}
	return m
}()

// String returns the string representation.
func (s TaskStatus) String() string {
	if name, ok := taskStatusNames[s]; ok {
		return name
	}
	return "UNKNOWN"
}

// Valid returns true if s is a known TaskStatus.
func (s TaskStatus) Valid() bool {
	_, ok := taskStatusNames[s]
	return ok
}

// MarshalJSON implements json.Marshaler.
func (s TaskStatus) MarshalJSON() ([]byte, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("invalid TaskStatus: %d", s)
	}
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *TaskStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("task status unmarshal json: %w", err)
	}
	v, ok := taskStatusValues[str]
	if !ok {
		return fmt.Errorf("unknown TaskStatus: %q", str)
	}
	*s = v
	return nil
}

// IsTerminal returns true for terminal task statuses.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusSucceeded, TaskStatusFailed, TaskStatusCancelled,
		TaskStatusExpired, TaskStatusDenied:
		return true
	}
	return false
}

// AllTaskStatuses returns all valid TaskStatus values.
func AllTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskStatusPending, TaskStatusAdmitted, TaskStatusRunning,
		TaskStatusSucceeded, TaskStatusFailed, TaskStatusCancelled,
		TaskStatusExpired, TaskStatusDenied,
	}
}

// ---------------------------------------------------------------------------
// Message role
// ---------------------------------------------------------------------------

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser   MessageRole = "user"
	RoleAgent  MessageRole = "agent"
	RoleSystem MessageRole = "system"
	RoleTool   MessageRole = "tool"
)

// Valid returns true if r is a known MessageRole.
func (r MessageRole) Valid() bool {
	switch r {
	case RoleUser, RoleAgent, RoleSystem, RoleTool:
		return true
	}
	return false
}

// AllMessageRoles returns all valid MessageRole values.
func AllMessageRoles() []MessageRole {
	return []MessageRole{RoleUser, RoleAgent, RoleSystem, RoleTool}
}

// MarshalJSON implements json.Marshaler.
func (r MessageRole) MarshalJSON() ([]byte, error) {
	if !r.Valid() {
		return nil, fmt.Errorf("invalid MessageRole: %q", string(r))
	}
	return json.Marshal(string(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *MessageRole) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("message role unmarshal json: %w", err)
	}
	mr := MessageRole(s)
	if !mr.Valid() {
		return fmt.Errorf("unknown MessageRole: %q", s)
	}
	*r = mr
	return nil
}

// ---------------------------------------------------------------------------
// Part kind
// ---------------------------------------------------------------------------

// PartKind represents the kind of a message part.
type PartKind string

const (
	PartKindText        PartKind = "text"
	PartKindJSON        PartKind = "json"
	PartKindArtifactRef PartKind = "artifact_ref"
	PartKindError       PartKind = "error"
)

// Valid returns true if k is a known PartKind.
func (k PartKind) Valid() bool {
	switch k {
	case PartKindText, PartKindJSON, PartKindArtifactRef, PartKindError:
		return true
	}
	return false
}

// AllPartKinds returns all valid PartKind values.
func AllPartKinds() []PartKind {
	return []PartKind{PartKindText, PartKindJSON, PartKindArtifactRef, PartKindError}
}

// MarshalJSON implements json.Marshaler.
func (k PartKind) MarshalJSON() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("invalid PartKind: %q", string(k))
	}
	return json.Marshal(string(k))
}

// UnmarshalJSON implements json.Unmarshaler.
func (k *PartKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("part kind unmarshal json: %w", err)
	}
	pk := PartKind(s)
	if !pk.Valid() {
		return fmt.Errorf("unknown PartKind: %q", s)
	}
	*k = pk
	return nil
}

// ---------------------------------------------------------------------------
// Event type
// ---------------------------------------------------------------------------

// EventType represents the type of a task event.
type EventType string

const (
	EventTaskAdmitted       EventType = "TASK_ADMITTED"
	EventTaskDenied         EventType = "TASK_DENIED"
	EventTaskStarted        EventType = "TASK_STARTED"
	EventTaskMessage        EventType = "TASK_MESSAGE"
	EventTaskProgress       EventType = "TASK_PROGRESS"
	EventTaskSucceeded      EventType = "TASK_SUCCEEDED"
	EventTaskFailed         EventType = "TASK_FAILED"
	EventTaskCancelled      EventType = "TASK_CANCELLED"
	EventTaskExpired        EventType = "TASK_EXPIRED"
	EventArtifactCommitted  EventType = "ARTIFACT_COMMITTED"
)

// Valid returns true if t is a known EventType.
func (t EventType) Valid() bool {
	switch t {
	case EventTaskAdmitted, EventTaskDenied, EventTaskStarted,
		EventTaskMessage, EventTaskProgress, EventTaskSucceeded,
		EventTaskFailed, EventTaskCancelled, EventTaskExpired,
		EventArtifactCommitted:
		return true
	}
	return false
}

// AllEventTypes returns all valid EventType values.
func AllEventTypes() []EventType {
	return []EventType{
		EventTaskAdmitted, EventTaskDenied, EventTaskStarted,
		EventTaskMessage, EventTaskProgress, EventTaskSucceeded,
		EventTaskFailed, EventTaskCancelled, EventTaskExpired,
		EventArtifactCommitted,
	}
}

// MarshalJSON implements json.Marshaler.
func (t EventType) MarshalJSON() ([]byte, error) {
	if !t.Valid() {
		return nil, fmt.Errorf("invalid EventType: %q", string(t))
	}
	return json.Marshal(string(t))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *EventType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("event type unmarshal json: %w", err)
	}
	et := EventType(s)
	if !et.Valid() {
		return fmt.Errorf("unknown EventType: %q", s)
	}
	*t = et
	return nil
}

// ---------------------------------------------------------------------------
// WriterKind – used to gate system-role messages to trusted runtime writers
// ---------------------------------------------------------------------------

// WriterKind discriminates who is writing a message.
type WriterKind string

const (
	WriterAgent   WriterKind = "agent"   // Agent SDK
	WriterRuntime WriterKind = "runtime" // Trusted runtime / gateway
)

// Valid returns true if w is a known WriterKind.
func (w WriterKind) Valid() bool {
	switch w {
	case WriterAgent, WriterRuntime:
		return true
	}
	return false
}