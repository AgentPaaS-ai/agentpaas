package harness

import (
	"testing"
)

func TestSanitizeAgentPayload_StripsReservedKeys(t *testing.T) {
	payload := map[string]any{
		"credentials":   []map[string]any{{"id": "k1", "value": "secret"}},
		"llm":           map[string]any{"provider": "openai"},
		"mcp":           map[string]any{"server": "s1"},
		"mcp_servers":   []map[string]any{{"server_id": "s1"}},
		"__agentpaas_foo": "internal",
		"question":      "What is the weather?",
		"city":          "Folsom",
	}
	result := sanitizeAgentPayload(payload)
	if _, ok := result["credentials"]; ok {
		t.Errorf("credentials key should be stripped")
	}
	if _, ok := result["llm"]; ok {
		t.Errorf("llm key should be stripped")
	}
	if _, ok := result["mcp"]; ok {
		t.Errorf("mcp key should be stripped")
	}
	if _, ok := result["mcp_servers"]; ok {
		t.Errorf("mcp_servers key should be stripped")
	}
	if _, ok := result["__agentpaas_foo"]; ok {
		t.Errorf("__agentpaas_ prefixed key should be stripped")
	}
	if result["question"] != "What is the weather?" {
		t.Errorf("question = %q, want 'What is the weather?'", result["question"])
	}
	if result["city"] != "Folsom" {
		t.Errorf("city = %q, want 'Folsom'", result["city"])
	}
}

func TestSanitizeAgentPayload_AdditionalReservedKeys(t *testing.T) {
	// Verify other __agentpaas_ prefixes are stripped.
	payload := map[string]any{
		"__agentpaas_token":   "abc123",
		"__agentpaas_internal": map[string]any{"x": 1},
		"user_input":          "hello",
	}
	result := sanitizeAgentPayload(payload)
	if _, ok := result["__agentpaas_token"]; ok {
		t.Errorf("__agentpaas_token should be stripped")
	}
	if _, ok := result["__agentpaas_internal"]; ok {
		t.Errorf("__agentpaas_internal should be stripped")
	}
	if result["user_input"] != "hello" {
		t.Errorf("user_input = %q, want 'hello'", result["user_input"])
	}
}

func TestSanitizeAgentPayload_PassesUserKeys(t *testing.T) {
	payload := map[string]any{
		"question":   "What is 2+2?",
		"input":      "some input",
		"message":    "a message",
		"custom_key": 42,
	}
	result := sanitizeAgentPayload(payload)
	for k, expected := range payload {
		if got, ok := result[k]; !ok || got != expected {
			t.Errorf("key %q: got %v, want %v", k, got, expected)
		}
	}
	if len(result) != len(payload) {
		t.Errorf("result has %d keys, want %d: %v", len(result), len(payload), result)
	}
}

func TestSanitizeAgentPayload_NilPayload(t *testing.T) {
	result := sanitizeAgentPayload(nil)
	if result != nil {
		t.Errorf("nil payload should return nil, got %v", result)
	}
}

func TestSanitizeAgentPayload_EmptyPayload(t *testing.T) {
	result := sanitizeAgentPayload(map[string]any{})
	if len(result) != 0 {
		t.Errorf("empty payload should return empty map, got %v", result)
	}
}

func TestSanitizeAgentPayload_MixedKeys(t *testing.T) {
	payload := map[string]any{
		"llm":           map[string]any{"provider": "openai"},
		"credentials":   []any{},
		"mcp_servers":   []any{},
		"__agentpaas_x": "hidden",
		"good_key":      "visible",
	}
	result := sanitizeAgentPayload(payload)
	if len(result) != 1 || result["good_key"] != "visible" {
		t.Errorf("expected only good_key=visible, got %v", result)
	}
}