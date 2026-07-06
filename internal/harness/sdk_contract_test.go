package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSDKOnInvokeDecoratorReturnsResult(t *testing.T) {
	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return {"decorated": payload["value"]}
`)

	got := invokeSDKAgent(t, srv, `{"value":"called"}`)
	if got.Status != "OK" || got.Result["decorated"] != "called" {
		t.Fatalf("invoke response = %#v, want decorated result", got)
	}
}

func TestSDKLLMRecordsTokensAndEnforcesBudget(t *testing.T) {
	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    first = agent.llm("one two three")
    if payload.get("second"):
        agent.llm("four five six")
    return {"tokens": first["tokens"]}
`)

	got := invokeSDKAgent(t, srv, `{"budget":{"max_tokens":3}}`)
	if got.Result["tokens"] != float64(3) {
		t.Fatalf("tokens = %#v, want 3", got.Result["tokens"])
	}

	errResp := invokeSDKAgentError(t, srv, `{"second":true,"budget":{"max_tokens":3}}`)
	if errResp.Status != StatusBudgetExceeded {
		t.Fatalf("budget response = %#v, want budget exceeded", errResp)
	}
}

func TestSDKHTTPReturnsStatusAndBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("plain-body"))
	}))
	defer upstream.Close()

	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return agent.http("GET", payload["url"])
`)

	got := invokeSDKAgent(t, srv, `{"url":`+quoteJSON(upstream.URL)+`}`)
	if got.Result["status"] != float64(http.StatusCreated) || got.Result["body"] != "plain-body" {
		t.Fatalf("http response = %#v, want status/body", got.Result)
	}
}

func TestSDKHTTPWithCredentialDoesNotExposeSecret(t *testing.T) {
	const secret = "sentinel-secret-never-return"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != secret {
			t.Fatalf("authorization header = %q, want secret", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-Upstream-Token", secret)
		_, _ = w.Write([]byte("credential accepted"))
	}))
	defer upstream.Close()

	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return agent.http_with_credential("api-key", "GET", payload["url"])
`)
	defer func() { _ = srv.Close() }()

	// Inject credentials via the side-channel (new T01/T02 flow).
	srv.worker.rpc.SetCredentialsForTest(map[string]rpcCredential{
		"api-key": {Header: "Authorization", Value: secret},
	})

	payload := `{"url":` + quoteJSON(upstream.URL) + `,"credentials":[{"id":"api-key","header":"Authorization","value":` + quoteJSON(secret) + `}]}`
	got := invokeSDKAgent(t, srv, payload)
	encoded, err := json.Marshal(got.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("sdk-visible response leaked credential: %s", encoded)
	}
}

func TestSDKMCPAllowlistAndDeniedAudit(t *testing.T) {
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{Audit: recorder}, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    if payload.get("deny"):
        return agent.mcp("blocked", "read", {})
    return agent.mcp("declared", "search", {"q": "x"})
`)

	got := invokeSDKAgent(t, srv, `{"mcp_servers":[{"server_id":"declared","tools":["search"]}]}`)
	if got.Result["server_id"] != "declared" || got.Result["tool"] != "search" {
		t.Fatalf("mcp result = %#v, want declared tool result", got.Result)
	}

	errResp := invokeSDKAgentError(t, srv, `{"deny":true,"mcp_servers":[{"server_id":"declared","tools":["search"]}]}`)
	if errResp.Reason != "invoke_failed" {
		t.Fatalf("mcp denied response = %#v, want invoke failure", errResp)
	}
	event := recorder.lastEvent(t)
	if event.EventType != "mcp_denied" || event.Payload["server_id"] != "blocked" || event.Payload["tool"] != "read" {
		t.Fatalf("audit event = %#v, want mcp_denied blocked/read", event)
	}
}

func TestSDKMCPDeniedFailureContextCategoryFullInvokePath(t *testing.T) {
	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return agent.mcp("undeclared", "tool", {})
`)

	errResp := invokeSDKAgentError(t, srv, `{"mcp_servers":[{"server_id":"declared","tools":["tool"]}]}`)
	if errResp.FailureContext == nil {
		t.Fatalf("failure context is nil; error response = %#v", errResp)
	}
	if errResp.FailureContext.Category != FailureCategoryMCPDenied {
		t.Fatalf("failure context category = %q, want %q; detail %s", errResp.FailureContext.Category, FailureCategoryMCPDenied, errResp.FailureContext.RedactedDetail)
	}
}

func TestSDKIterationBudgetStopsInvoke(t *testing.T) {
	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    while True:
        agent.record_iteration()
`)

	errResp := invokeSDKAgentError(t, srv, `{"budget":{"max_iterations":2}}`)
	if errResp.Status != StatusBudgetExceeded {
		t.Fatalf("iteration response = %#v, want budget exceeded", errResp)
	}
}

func invokeSDKAgent(t *testing.T, srv *Server, payload string) InvokeResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got InvokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal invoke response: %v", err)
	}
	return got
}

func invokeSDKAgentError(t *testing.T, srv *Server, payload string) ErrorResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	return got
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
