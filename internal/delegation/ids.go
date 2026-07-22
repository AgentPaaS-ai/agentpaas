package delegation

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// ID prefixes.
const (
	PrefixTask    = "task-"
	PrefixMessage = "msg-"
	PrefixResult  = "tres-"
	PrefixEvent   = "tevt-"
)

// idEntropyBytes is the number of random bytes in each generated ID.
const idEntropyBytes = 16

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateID returns prefix + base32(random bytes), lowercase.
func generateID(prefix string) (string, error) {
	var b [idEntropyBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("delegation: generate id: %w", err)
	}
	encoded := strings.ToLower(base32NoPad.EncodeToString(b[:]))
	return prefix + encoded, nil
}

// ---------------------------------------------------------------------------
// Stable ID types
// ---------------------------------------------------------------------------

// TaskID identifies a task.
type TaskID string

// String returns the string representation.
func (id TaskID) String() string { return string(id) }

// Validate returns true if the ID is non-empty and has the correct prefix.
func (id TaskID) Validate() bool {
	return ValidateIDPrefix(string(id), PrefixTask)
}

// MessageID identifies a message.
type MessageID string

// String returns the string representation.
func (id MessageID) String() string { return string(id) }

// Validate returns true if the ID is non-empty and has the correct prefix.
func (id MessageID) Validate() bool {
	return ValidateIDPrefix(string(id), PrefixMessage)
}

// ResultID identifies a task result.
type ResultID string

// String returns the string representation.
func (id ResultID) String() string { return string(id) }

// Validate returns true if the ID is non-empty and has the correct prefix.
func (id ResultID) Validate() bool {
	return ValidateIDPrefix(string(id), PrefixResult)
}

// EventID identifies a task event.
type EventID string

// String returns the string representation.
func (id EventID) String() string { return string(id) }

// Validate returns true if the ID is non-empty and has the correct prefix.
func (id EventID) Validate() bool {
	return ValidateIDPrefix(string(id), PrefixEvent)
}

// ValidateIDPrefix returns true if id is non-empty and starts with prefix.
func ValidateIDPrefix(id, prefix string) bool {
	return id != "" && strings.HasPrefix(id, prefix) && len(id) > len(prefix)
}

// ---------------------------------------------------------------------------
// ID constructors
// ---------------------------------------------------------------------------

// NewTaskID generates a cryptographically random task ID.
func NewTaskID() (TaskID, error) {
	s, err := generateID(PrefixTask)
	return TaskID(s), err
}

// NewMessageID generates a cryptographically random message ID.
func NewMessageID() (MessageID, error) {
	s, err := generateID(PrefixMessage)
	return MessageID(s), err
}

// NewResultID generates a cryptographically random result ID.
func NewResultID() (ResultID, error) {
	s, err := generateID(PrefixResult)
	return ResultID(s), err
}

// NewEventID generates a cryptographically random event ID.
func NewEventID() (EventID, error) {
	s, err := generateID(PrefixEvent)
	return EventID(s), err
}