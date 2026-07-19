package runtime

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// StreamEventKind classifies a model streaming event.
type StreamEventKind string

// Stream event kinds. The kinds are ordered roughly by lifecycle: a stream
// begins with response_started, zero or more output_delta/tool_call_delta/
// usage_update events, and ends with exactly one terminal event
// (response_completed or response_failed).
const (
	// StreamEventResponseStarted marks the beginning of a stream. It is the
	// first event emitted and is emitted exactly once.
	StreamEventResponseStarted StreamEventKind = "response_started"
	// StreamEventOutputDelta carries an incremental output token/chunk delta.
	// Only emitted under incremental_release guardrail mode.
	StreamEventOutputDelta StreamEventKind = "output_delta"
	// StreamEventToolCallDelta carries an incremental tool-call argument delta.
	// Only emitted under incremental_release guardrail mode.
	StreamEventToolCallDelta StreamEventKind = "tool_call_delta"
	// StreamEventUsageUpdate carries an incremental usage/budget update. It
	// contains only approved usage dimensions, never raw credentials or CoT.
	StreamEventUsageUpdate StreamEventKind = "usage_update"
	// StreamEventResponseCompleted is the terminal success event. Partial
	// output is committed only when this event is emitted.
	StreamEventResponseCompleted StreamEventKind = "response_completed"
	// StreamEventResponseFailed is the terminal failure event (budget
	// exhaustion, cancellation, backpressure, or provider error). Late deltas
	// are rejected after this event.
	StreamEventResponseFailed StreamEventKind = "response_failed"
)

// IsTerminalStreamEvent reports whether kind is a terminal stream event
// (response_completed or response_failed). After a terminal event, no further
// events may be emitted for the same call.
func IsTerminalStreamEvent(kind StreamEventKind) bool {
	return kind == StreamEventResponseCompleted || kind == StreamEventResponseFailed
}

// MaxStreamPayloadBytes caps a single stream event payload. A delta payload
// is bounded so a slow observer cannot exhaust trusted memory. 64 KiB.
const MaxStreamPayloadBytes = 64 * 1024

// StreamEvent is one event in a governed model stream. Events are ordered by
// Sequence within a call. Payload is bounded and must never contain raw
// credentials or raw chain-of-thought.
type StreamEvent struct {
	// CallID identifies the logical model call (stable across retries of the
	// same call). Must be non-empty.
	CallID string
	// RequestID identifies the specific upstream provider request. Must be
	// non-empty.
	RequestID string
	// Sequence is the per-call monotonic event sequence, starting at 1.
	Sequence int64
	// Timestamp is when the event was produced. Must be non-zero.
	Timestamp time.Time
	// TargetIdentity is the package/runtime identity the stream is addressed
	// to. May be empty for anonymous/ad-hoc streams.
	TargetIdentity string
	// Kind is the event kind. Must be a valid StreamEventKind.
	Kind StreamEventKind
	// Payload is the bounded event payload. Must be <= MaxStreamPayloadBytes
	// and must not contain raw credentials or raw chain-of-thought.
	Payload []byte
}

// credentialMarkers are substrings that, if present in a stream payload,
// strongly suggest leaked raw credentials. Matching is case-insensitive.
// This is a defense-in-depth check: the trusted path never puts credentials
// in payloads, but we reject them at the boundary too.
var credentialMarkers = []string{
	"authorization: bearer ",
	"api-key: ",
	"api_key=",
	"secret=",
	"password=",
	"-----begin private key-----",
	"-----begin rsa private key-----",
	"-----begin ec private key-----",
}

// containsRawCredential reports whether s appears to contain a raw credential.
func containsRawCredential(s string) bool {
	lower := strings.ToLower(s)
	for _, marker := range credentialMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// ValidateStreamEvent checks a StreamEvent is well-formed and safe:
//   - CallID and RequestID non-empty;
//   - Kind is a recognized StreamEventKind;
//   - Sequence > 0;
//   - Timestamp non-zero;
//   - Payload bounded (<= MaxStreamPayloadBytes);
//   - Payload contains no raw credentials;
//   - Payload contains no raw chain-of-thought.
//
// This is the trusted boundary: events that fail validation MUST NOT be
// emitted to observers or persisted as durable events.
func ValidateStreamEvent(ev StreamEvent) error {
	if strings.TrimSpace(ev.CallID) == "" {
		return errors.New("stream event: call_id must be non-empty")
	}
	if strings.TrimSpace(ev.RequestID) == "" {
		return errors.New("stream event: request_id must be non-empty")
	}
	if ev.Sequence <= 0 {
		return fmt.Errorf("stream event: sequence must be > 0; got %d", ev.Sequence)
	}
	if ev.Timestamp.IsZero() {
		return errors.New("stream event: timestamp must be non-zero")
	}
	switch ev.Kind {
	case StreamEventResponseStarted,
		StreamEventOutputDelta,
		StreamEventToolCallDelta,
		StreamEventUsageUpdate,
		StreamEventResponseCompleted,
		StreamEventResponseFailed:
		// ok
	default:
		return fmt.Errorf("stream event: unknown kind %q", ev.Kind)
	}
	if len(ev.Payload) > MaxStreamPayloadBytes {
		return fmt.Errorf("stream event: payload exceeds %d bytes (got %d)", MaxStreamPayloadBytes, len(ev.Payload))
	}
	if containsRawCredential(string(ev.Payload)) {
		return errors.New("stream event: payload must not contain raw credentials")
	}
	if containsRawCoT(string(ev.Payload)) {
		return errors.New("stream event: payload must not contain raw chain-of-thought")
	}
	return nil
}
