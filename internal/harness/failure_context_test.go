package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailureContextHTTPRedactsBodyCredentialAndURLQuery(t *testing.T) {
	const (
		secret   = "SENTINEL_HTTP_CREDENTIAL"
		bodyText = "SENTINEL_HTTP_BODY"
	)
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{Audit: recorder}, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return agent.http_with_credential(
        "api-key",
        "POST",
        payload["url"],
        body=payload["body"],
        headers={"X-Body-Echo": payload["body"]},
    )
`)

	// Inject credentials via the side-channel (new T01/T02 flow).
	srv.worker.rpc.SetCredentialsForTest(map[string]rpcCredential{
		"api-key": {Header: "Authorization", Value: secret},
	})

	errResp := invokeSDKAgentError(t, srv, `{
		"run_id":"run-http-redact",
		"invoke_id":"invoke-http-redact",
		"url":"http://127.0.0.1:1/submit?token=`+secret+`",
		"body":"`+bodyText+`",
		"credentials":[{"id":"api-key","header":"Authorization","value":"`+secret+`"}]
	}`)
	ctx := failureContextFromError(t, errResp)

	if ctx.Category != FailureCategorySaaSFailed && ctx.Category != FailureCategoryToolFailed {
		t.Fatalf("category = %q, want saas/tool failure", ctx.Category)
	}
	encoded := encodeJSON(t, ctx)
	for _, forbidden := range []string{secret, bodyText, "?token="} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("failure context leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, "[REDACTED:body]") || !strings.Contains(encoded, "[REDACTED:credential]") {
		t.Fatalf("failure context = %s, want body and credential redaction markers", encoded)
	}
	if ctx.UpstreamEvidence == nil || ctx.UpstreamEvidence.Availability != AvailabilityUnavailable {
		t.Fatalf("upstream evidence = %#v, want unavailable", ctx.UpstreamEvidence)
	}

	event := recorder.lastFailureContextEvent(t)
	if event.EventType != "failure_context" {
		t.Fatalf("event type = %q, want failure_context", event.EventType)
	}
	if eventText := encodeJSON(t, event.Payload); strings.Contains(eventText, secret) || strings.Contains(eventText, bodyText) {
		t.Fatalf("audit failure context leaked sentinel: %s", eventText)
	}
}

func TestFailureContextMCPHashesBodiesOnly(t *testing.T) {
	// This test verifies the audit hashes the MCP input body. It exercises the
	// no-router synthetic MCP path, which is gated behind AGENTPAAS_TEST_FAKE_MCP
	// (B30-T00) so the synthetic success + mcp_call audit event is recorded.
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "1")
	const sentinel = "SENTINEL_MCP_BODY"
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{Audit: recorder}, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    return agent.mcp("declared", "search", {"q": payload["sentinel"]})
`)

	got := invokeSDKAgent(t, srv, `{
		"sentinel":"`+sentinel+`",
		"mcp_servers":[{"server_id":"declared","tools":["search"]}]
	}`)
	if got.Status != "OK" {
		t.Fatalf("invoke status = %q, want OK", got.Status)
	}

	events := recorder.events()
	encoded := encodeJSON(t, events)
	if strings.Contains(encoded, sentinel) {
		t.Fatalf("audit leaked raw MCP body: %s", encoded)
	}
	wantHash := sha256Hex(`{"q":"` + sentinel + `"}`)
	if !strings.Contains(encoded, wantHash) {
		t.Fatalf("audit = %s, want MCP input hash %s", encoded, wantHash)
	}
}

func TestFailureContextBudgetExceededRedactsPayload(t *testing.T) {
	const secret = "SENTINEL_BUDGET_SECRET"
	srv := newReadyServer(t, `from agentpaas_sdk import agent

@agent.on_invoke
def handle(payload):
    while True:
        agent.record_iteration()
`)

	errResp := invokeSDKAgentError(t, srv, `{
		"secret":"`+secret+`",
		"budget":{"max_iterations":1}
	}`)
	ctx := failureContextFromError(t, errResp)
	if ctx.Category != FailureCategoryBudgetExceeded {
		t.Fatalf("category = %q, want budget_exceeded", ctx.Category)
	}
	if ctx.RunID == "" || ctx.InvokeID == "" {
		t.Fatalf("ids missing from failure context: %#v", ctx)
	}
	if strings.Contains(encodeJSON(t, ctx), secret) {
		t.Fatalf("failure context leaked payload secret: %#v", ctx)
	}
}

func TestFailureContextImportCrashUsesStderrRefNotStderrContent(t *testing.T) {
	const stderrSentinel = "SENTINEL_STDERR_CONTENT"
	agentPath := writeAgent(t, `import sys
print("`+stderrSentinel+`", file=sys.stderr)
raise RuntimeError("boom during import")
`)
	srv := NewServer(Config{AgentPath: agentPath})
	t.Cleanup(func() { _ = srv.Close() })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal readyz error response: %v", err)
	}
	ctx := failureContextFromError(t, got)
	if ctx.Category != FailureCategoryImportFailed {
		t.Fatalf("category = %q, want import_failed", ctx.Category)
	}
	if ctx.StderrRef == "" {
		t.Fatal("stderr_ref missing from import failure context")
	}
	if strings.Contains(encodeJSON(t, ctx), stderrSentinel) {
		t.Fatalf("failure context leaked stderr content: %#v", ctx)
	}
}

func TestFailureContextInvokeIDsAreUniqueAndPolicyDigestStable(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    raise RuntimeError("boom")
`)

	first := failureContextFromError(t, invokeSDKAgentError(t, srv, `{"policy":{"allow":["a"]}}`))
	second := failureContextFromError(t, invokeSDKAgentError(t, srv, `{"policy":{"allow":["a"]}}`))
	third := failureContextFromError(t, invokeSDKAgentError(t, srv, `{"policy":{"allow":["b"]}}`))

	if first.RunID == "" || first.InvokeID == "" || second.RunID == "" || second.InvokeID == "" {
		t.Fatalf("missing generated IDs: first=%#v second=%#v", first, second)
	}
	if first.RunID == second.RunID || first.InvokeID == second.InvokeID {
		t.Fatalf("IDs are not unique: first=%#v second=%#v", first, second)
	}
	if first.PolicyDigest == "" || first.PolicyDigest != second.PolicyDigest {
		t.Fatalf("policy digest unstable for same policy: %q vs %q", first.PolicyDigest, second.PolicyDigest)
	}
	if first.PolicyDigest == third.PolicyDigest {
		t.Fatalf("policy digest did not change for different policy: %q", first.PolicyDigest)
	}
}

func TestRedactFailureDetailPatterns(t *testing.T) {
	detail := redactFailureDetail(`Authorization: Bearer abc.def-123
url=https://user:pass@example.test/path?token=secret
api_key = sk-test-secret
password=cleartext
-----BEGIN PRIVATE KEY-----
secret
-----END PRIVATE KEY-----`)

	for _, forbidden := range []string{"abc.def-123", "token=secret", "sk-test-secret", "cleartext", "BEGIN PRIVATE KEY"} {
		if strings.Contains(detail, forbidden) {
			t.Fatalf("redacted detail leaked %q: %s", forbidden, detail)
		}
	}
	for _, marker := range []string{"[REDACTED:bearer_token]", "[REDACTED:api_key]", "[REDACTED:password]", "[REDACTED:private_key]"} {
		if !strings.Contains(detail, marker) {
			t.Fatalf("redacted detail = %q, want marker %s", detail, marker)
		}
	}
}

func failureContextFromError(t *testing.T, errResp ErrorResponse) FailureContext {
	t.Helper()
	if errResp.FailureContext != nil {
		return *errResp.FailureContext
	}
	var ctx FailureContext
	if err := json.Unmarshal([]byte(errResp.Detail), &ctx); err != nil {
		t.Fatalf("unmarshal failure context from detail %q: %v", errResp.Detail, err)
	}
	return ctx
}

func encodeJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (r *recordingAuditAppender) lastFailureContextEvent(t *testing.T) auditRecordForFailureContext {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.records) - 1; i >= 0; i-- {
		if r.records[i].EventType == "failure_context" {
			return auditRecordForFailureContext{
				EventType: r.records[i].EventType,
				Payload:   r.records[i].Payload,
			}
		}
	}
	t.Fatal("no failure_context audit records emitted")
	return auditRecordForFailureContext{}
}

type auditRecordForFailureContext struct {
	EventType string
	Payload   map[string]interface{}
}
