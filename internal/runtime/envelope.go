package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Message role constants for the normalized model-call envelope.
const (
	RoleSystem    = "system"
	RoleDeveloper = "developer"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ReasoningEffort controls how much reasoning effort a model expends.
type ReasoningEffort string

const (
	// ReasoningEffortNone disables reasoning.
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortLow requests low reasoning effort.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium requests medium reasoning effort.
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh requests high reasoning effort.
	ReasoningEffortHigh ReasoningEffort = "high"
)

// validRoles is the set of accepted message roles.
var validRoles = map[string]struct{}{
	RoleSystem:    {},
	RoleDeveloper: {},
	RoleUser:      {},
	RoleAssistant: {},
	RoleTool:      {},
}

// validReasoningEfforts is the set of accepted reasoning efforts.
var validReasoningEfforts = map[ReasoningEffort]struct{}{
	ReasoningEffortNone:   {},
	ReasoningEffortLow:    {},
	ReasoningEffortMedium: {},
	ReasoningEffortHigh:   {},
}

// Message is a single role/content pair in the normalized envelope.
type Message struct {
	Role    string
	Content string
}

// StructuredOutputSpec requests a structured output conforming to a JSON schema.
type StructuredOutputSpec struct {
	// JSONSchema is a JSON Schema object describing the desired output shape.
	JSONSchema map[string]any
}

// ToolCallPart is a single tool-call part inside the envelope.
type ToolCallPart struct {
	ID        string
	Name      string
	Arguments string
}

// Usage accounts for token consumption on a model call.
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	ReasoningTokens  int64
	TotalTokens      int64
}

// ModelCallEnvelope is the normalized multi-role model-call envelope. It
// supports structured messages, structured output, tool-call parts, reasoning
// controls and usage, cancellation, deadlines, and namespaced provider
// extension metadata.
type ModelCallEnvelope struct {
	// Messages is the ordered list of multi-role messages. Must be non-empty.
	Messages []Message
	// StructuredOutput optionally requests a structured JSON-schema output.
	StructuredOutput *StructuredOutputSpec
	// ToolCalls carries tool-call parts (e.g. assistant-tool results).
	ToolCalls []ToolCallPart
	// ReasoningEffort controls reasoning effort. Empty means unspecified.
	ReasoningEffort ReasoningEffort
	// MaxReasoningTokens bounds reasoning token spend. Zero means unspecified.
	MaxReasoningTokens int64
	// ReasoningSummary is a provider-approved summary. It must never contain
	// raw chain-of-thought; Validate rejects obvious CoT markers.
	ReasoningSummary *string
	// CancelCtx carries cancellation for the call. May be nil.
	CancelCtx context.Context
	// Deadline bounds the call. May be nil.
	Deadline *time.Time
	// ProviderExtensions carries namespaced, policy-reviewed provider
	// extension metadata. Keys must be namespaced (e.g. "x-vendor-foo").
	ProviderExtensions map[string]any
	// Usage accounts for token consumption. May be nil before the call.
	Usage *Usage
}

// cotMarkers are substrings that strongly indicate raw chain-of-thought.
// Matching is case-insensitive. A provider-approved summary must not contain
// any of these.
var cotMarkers = []string{
	"<thought>",
	"</thought>",
	"<thinking>",
	"</thinking>",
	"chain of thought:",
	"chain-of-thought:",
	"step 1:",
	"step 1 :",
}

// containsRawCoT reports whether s appears to contain raw chain-of-thought.
func containsRawCoT(s string) bool {
	lower := strings.ToLower(s)
	for _, marker := range cotMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// isNamespacedExtensionKey reports whether k is a namespaced provider
// extension key. Namespaced keys start with "x-" (the conventional HTTP
// extension header prefix) followed by a vendor/namespace segment and a
// final key segment, e.g. "x-vendor-foo".
func isNamespacedExtensionKey(k string) bool {
	if !strings.HasPrefix(k, "x-") {
		return false
	}
	rest := strings.TrimPrefix(k, "x-")
	// Require at least "namespace-key" (two segments).
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	return true
}

// Validate checks the envelope is well-formed:
//   - messages non-empty;
//   - every role is valid;
//   - reasoning effort, if set, is valid;
//   - reasoning summary, if set, does not contain raw chain-of-thought;
//   - provider extension keys are namespaced;
//   - deadline, if set, is in the future.
func (e *ModelCallEnvelope) Validate() error {
	if len(e.Messages) == 0 {
		return errors.New("model call envelope: messages must be non-empty")
	}
	for i, m := range e.Messages {
		if _, ok := validRoles[m.Role]; !ok {
			return fmt.Errorf("model call envelope: message %d has invalid role %q", i, m.Role)
		}
	}
	if e.ReasoningEffort != "" {
		if _, ok := validReasoningEfforts[e.ReasoningEffort]; !ok {
			return fmt.Errorf("model call envelope: invalid reasoning effort %q", e.ReasoningEffort)
		}
	}
	if e.ReasoningSummary != nil {
		if containsRawCoT(*e.ReasoningSummary) {
			return errors.New("model call envelope: reasoning summary must not contain raw chain-of-thought")
		}
	}
	for k := range e.ProviderExtensions {
		if !isNamespacedExtensionKey(k) {
			return fmt.Errorf("model call envelope: provider extension key %q must be namespaced (e.g. x-vendor-foo)", k)
		}
	}
	if e.Deadline != nil {
		if e.Deadline.Before(time.Now()) {
			return errors.New("model call envelope: deadline must be in the future")
		}
	}
	return nil
}
