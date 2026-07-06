package daemon

import (
	"context"
	"strings"
	"testing"
)

// TestBuildInvokePayload_MergesUserPayload is the regression test for the
// P0 bug where trigger invoke --payload was dropped. The user's trigger
// payload JSON must be merged into the invoke payload that reaches the
// agent's handle_invoke(), so the agent receives its input data.
//
// Without LLM config, the payload should still carry the user's keys.
func TestBuildInvokePayload_MergesUserPayload(t *testing.T) {
	server := testControlServer(t)

	// No deployed agent, no LLM config — buildInvokePayload returns the
	// base payload. With a user payload, the user's keys must appear.
	userPayload := []byte(`{"lat": 38.677, "lon": -121.176, "city": "Folsom"}`)
	result, err := server.buildInvokePayload(context.Background(), "test-agent", userPayload)
	if err != nil {
		t.Fatalf("buildInvokePayload error: %v", err)
	}

	// User keys must be present at the top level.
	if got := result["lat"]; got == nil {
		t.Errorf("user payload key 'lat' missing from invoke payload; agent would receive empty payload. got=%v", result)
	}
	if got := result["lon"]; got == nil {
		t.Errorf("user payload key 'lon' missing from invoke payload; got=%v", result)
	}
	if got := result["city"]; got == nil {
		t.Errorf("user payload key 'city' missing from invoke payload; got=%v", result)
	}
}

// TestBuildInvokePayload_NoUserPayload verifies backward compatibility:
// with no user payload (auto-invoke on run start, or trigger without
// --payload), the payload is the base (empty or LLM-config-only).
func TestBuildInvokePayload_NoUserPayload(t *testing.T) {
	server := testControlServer(t)

	result, err := server.buildInvokePayload(context.Background(), "test-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload error: %v", err)
	}
	if result == nil {
		t.Fatal("buildInvokePayload returned nil map")
	}
	// No user keys, no LLM config → empty map (backward compat).
	if len(result) != 0 {
		t.Errorf("expected empty payload with no user payload and no LLM config, got=%v", result)
	}
}

// TestBuildInvokePayload_InvalidUserPayloadFailClosed verifies that invalid
// JSON in the user payload returns an error (fail-closed) instead of silently
// ignoring. The error must contain guidance about the JSON being invalid.
func TestBuildInvokePayload_InvalidUserPayloadFailClosed(t *testing.T) {
	server := testControlServer(t)

	_, err := server.buildInvokePayload(context.Background(), "test-agent", []byte("not json"))
	if err == nil {
		t.Fatal("buildInvokePayload should return error on invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid trigger payload JSON") {
		t.Errorf("error = %q, want 'invalid trigger payload JSON'", err.Error())
	}
}

// TestBuildInvokePayload_ReservedKeysProtected verifies that the reserved
// keys (llm, credentials) injected by the daemon are not overwritten by
// user payload keys with the same name.
func TestBuildInvokePayload_ReservedKeysProtected(t *testing.T) {
	server := testControlServer(t)

	// User tries to override reserved keys — should be ignored.
	userPayload := []byte(`{"llm": "evil", "credentials": "stolen", "lat": 1.0}`)
	result, err := server.buildInvokePayload(context.Background(), "test-agent", userPayload)
	if err != nil {
		t.Fatalf("buildInvokePayload error: %v", err)
	}
	// 'lat' should be present (legit user key).
	if result["lat"] == nil {
		t.Errorf("user key 'lat' missing: %v", result)
	}
	// 'llm'/'credentials' should NOT be the user's string values (no LLM
	// config here, so they should be absent entirely, not "evil"/"stolen").
	if got, ok := result["llm"].(string); ok && got == "evil" {
		t.Errorf("user payload overwrote reserved 'llm' key: %v", result)
	}
	if got, ok := result["credentials"].(string); ok && got == "stolen" {
		t.Errorf("user payload overwrote reserved 'credentials' key: %v", result)
	}
}
