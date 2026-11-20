package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialInvisibility_EchoedUpstreamBody(t *testing.T) {
	const sentinel = "ZERO-VIS-SENTINEL-2026-DO-NOT-LEAK"
	body := `{"headers":{"Authorization":"` + sentinel + `"}}`
	got := redactCredentialValue(body, sentinel)
	if strings.Contains(got, sentinel) {
		t.Fatalf("upstream response leaked credential: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:credential]") {
		t.Fatalf("upstream response missing credential redaction: %s", got)
	}
}

// TestCredentialInvisibility_NoSecretInPayload verifies that credentials
// injected via the invoke payload are stripped before reaching the agent
// handler, and do not appear in the response.
func TestCredentialInvisibility_NoSecretInPayload(t *testing.T) {
	const sentinel = "SENTINEL_SECRET_NEVER_RETURN"

	agent := `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    # Echo the payload back — credentials should be absent.
    return {"received": payload}
`
	srv := newReadyServer(t, agent)
	defer func() { _ = srv.Close() }()

	payload := `{"question":"hello","credentials":[{"id":"k","value":"` + sentinel + `"}],"llm":{"provider":"openai"}}`
	got := invokeSDKAgent(t, srv, payload)

	encoded, err := json.Marshal(got.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), sentinel) {
		t.Fatalf("result leaked sentinel: %s", encoded)
	}
	// The "credentials" key should also be absent from the received payload.
	if received, ok := got.Result["received"].(map[string]any); ok {
		if _, hasCreds := received["credentials"]; hasCreds {
			t.Errorf("received payload should not have credentials key: %v", received)
		}
		if _, hasLLM := received["llm"]; hasLLM {
			t.Errorf("received payload should not have llm key: %v", received)
		}
	}
}

// TestCredentialInvisibility_NoSecretInStdout verifies that credentials
// do not appear in the agent's stdout.
func TestCredentialInvisibility_NoSecretInStdout(t *testing.T) {
	const sentinel = "SENTINEL_STDOUT_SECRET"

	agent := `import json, sys
from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    # Print the payload to stdout — credentials should be absent.
    print(json.dumps(payload), file=sys.stdout)
    return {"done": True}
`
	srv := newReadyServer(t, agent)
	defer func() { _ = srv.Close() }()

	payload := `{"question":"hello","credentials":[{"id":"k","value":"` + sentinel + `"}]}`
	got := invokeSDKAgent(t, srv, payload)

	if strings.Contains(got.Stdout, sentinel) {
		t.Fatalf("stdout leaked sentinel: %s", got.Stdout)
	}
	if strings.Contains(got.Stderr, sentinel) {
		t.Fatalf("stderr leaked sentinel: %s", got.Stderr)
	}
}

// TestCredentialInvisibility_Regression_UserKeys verifies that normal
// trigger payload fields still reach the agent handler after sanitization
// strips the reserved keys.
func TestCredentialInvisibility_Regression_UserKeys(t *testing.T) {
	agent := `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return {"question": payload.get("question"), "city": payload.get("city")}
`
	srv := newReadyServer(t, agent)
	defer func() { _ = srv.Close() }()

	got := invokeSDKAgent(t, srv, `{"question":"What is the weather?","city":"Folsom","credentials":[{"id":"k","value":"secret"}]}`)
	if got.Status != "OK" || got.Result["question"] != "What is the weather?" || got.Result["city"] != "Folsom" {
		t.Fatalf("invoke response = %#v, want question and city preserved", got)
	}
}
