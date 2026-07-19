package runtime

import (
	"strings"
	"testing"
	"time"
)

func TestValidateStreamEvent_AcceptsValidEvent(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventResponseStarted,
		Payload:   []byte("hello"),
	}
	if err := ValidateStreamEvent(ev); err != nil {
		t.Fatalf("valid event must be accepted: %v", err)
	}
}

func TestValidateStreamEvent_AcceptsAllKinds(t *testing.T) {
	kinds := []StreamEventKind{
		StreamEventResponseStarted,
		StreamEventOutputDelta,
		StreamEventToolCallDelta,
		StreamEventUsageUpdate,
		StreamEventResponseCompleted,
		StreamEventResponseFailed,
	}
	for _, k := range kinds {
		ev := StreamEvent{
			CallID:    "call-1",
			RequestID: "req-1",
			Sequence:  1,
			Timestamp: time.Now().UTC(),
			Kind:      k,
			Payload:   []byte("x"),
		}
		if err := ValidateStreamEvent(ev); err != nil {
			t.Fatalf("kind %q must be accepted: %v", k, err)
		}
	}
}

func TestValidateStreamEvent_RejectsEmptyCallID(t *testing.T) {
	ev := StreamEvent{
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventResponseStarted,
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("empty call_id must be rejected")
	}
}

func TestValidateStreamEvent_RejectsEmptyRequestID(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventResponseStarted,
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("empty request_id must be rejected")
	}
}

func TestValidateStreamEvent_RejectsZeroSequence(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventResponseStarted,
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("zero sequence must be rejected")
	}
}

func TestValidateStreamEvent_RejectsZeroTimestamp(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Kind:      StreamEventResponseStarted,
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("zero timestamp must be rejected")
	}
}

func TestValidateStreamEvent_RejectsUnknownKind(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventKind("bogus"),
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("unknown kind must be rejected")
	}
}

func TestValidateStreamEvent_RejectsPayloadOver64KB(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventOutputDelta,
		Payload:   make([]byte, MaxStreamPayloadBytes+1),
	}
	if err := ValidateStreamEvent(ev); err == nil {
		t.Fatal("payload > 64KB must be rejected")
	}
}

func TestValidateStreamEvent_AcceptsPayloadAt64KB(t *testing.T) {
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventOutputDelta,
		Payload:   make([]byte, MaxStreamPayloadBytes),
	}
	if err := ValidateStreamEvent(ev); err != nil {
		t.Fatalf("payload exactly 64KB must be accepted: %v", err)
	}
}

func TestValidateStreamEvent_RejectsRawCredentials(t *testing.T) {
	cases := []string{
		"Authorization: Bearer sk-abc123",
		"api-key: sk-abc123",
		"api_key=sk-abc123",
		"secret=hunter2",
		"password=hunter2",
		"-----BEGIN PRIVATE KEY-----\nMIIE...",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
		"-----BEGIN EC PRIVATE KEY-----\nMIIE...",
	}
	for _, c := range cases {
		ev := StreamEvent{
			CallID:    "call-1",
			RequestID: "req-1",
			Sequence:  1,
			Timestamp: time.Now().UTC(),
			Kind:      StreamEventOutputDelta,
			Payload:   []byte(c),
		}
		if err := ValidateStreamEvent(ev); err == nil {
			t.Fatalf("payload containing raw credential must be rejected: %q", c)
		}
	}
}

func TestValidateStreamEvent_RejectsRawCoT(t *testing.T) {
	cases := []string{
		"<thought>let me think</thought>",
		"<thinking>step 1: ...</thinking>",
		"Chain of thought: first I consider...",
		"chain-of-thought: ...",
		"Step 1: I need to reason",
	}
	for _, c := range cases {
		ev := StreamEvent{
			CallID:    "call-1",
			RequestID: "req-1",
			Sequence:  1,
			Timestamp: time.Now().UTC(),
			Kind:      StreamEventOutputDelta,
			Payload:   []byte(c),
		}
		if err := ValidateStreamEvent(ev); err == nil {
			t.Fatalf("payload containing raw CoT must be rejected: %q", c)
		}
	}
}

func TestIsTerminalStreamEvent(t *testing.T) {
	if !IsTerminalStreamEvent(StreamEventResponseCompleted) {
		t.Fatal("response_completed is terminal")
	}
	if !IsTerminalStreamEvent(StreamEventResponseFailed) {
		t.Fatal("response_failed is terminal")
	}
	if IsTerminalStreamEvent(StreamEventOutputDelta) {
		t.Fatal("output_delta is not terminal")
	}
}

func TestValidateStreamEvent_AcceptsUsageDimensions(t *testing.T) {
	// Usage dimensions (token counts) are approved summaries and must not be
	// rejected as credentials or CoT.
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventUsageUpdate,
		Payload:   []byte(`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`),
	}
	if err := ValidateStreamEvent(ev); err != nil {
		t.Fatalf("usage dimensions must be accepted: %v", err)
	}
}

func TestValidateStreamEvent_AcceptsApprovedReasoningSummary(t *testing.T) {
	// An approved reasoning summary (no CoT markers) is permitted in a payload.
	ev := StreamEvent{
		CallID:    "call-1",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventOutputDelta,
		Payload:   []byte("The model evaluated three candidates and selected the best."),
	}
	if err := ValidateStreamEvent(ev); err != nil {
		t.Fatalf("approved reasoning summary must be accepted: %v", err)
	}
}

func TestValidateStreamEvent_RejectsEmptyWhitespaceCallID(t *testing.T) {
	ev := StreamEvent{
		CallID:    "   ",
		RequestID: "req-1",
		Sequence:  1,
		Timestamp: time.Now().UTC(),
		Kind:      StreamEventResponseStarted,
	}
	err := ValidateStreamEvent(ev)
	if err == nil {
		t.Fatal("whitespace-only call_id must be rejected")
	}
	if !strings.Contains(err.Error(), "call_id") {
		t.Fatalf("error must mention call_id; got %v", err)
	}
}
