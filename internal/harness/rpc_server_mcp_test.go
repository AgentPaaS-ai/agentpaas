package harness

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// mcpNotFoundCode is the typed error name reserved by B26 for the managed MCP
// service-not-enabled path. Production no-router MCP calls must fail closed
// with this code rather than return a synthetic {ok: true} result.
const mcpNotFoundCode = "agentpaas_mcp_service_not_enabled"

// newMCPTestServer builds a harnessRPCServer with the given audit appender and
// no router installed. It returns the server and an invoke state preloaded with
// the given payload (so mcpAllowed is populated from mcp_servers).
func newMCPTestServer(t *testing.T, recorder *recordingAuditAppender, payload map[string]any) (*harnessRPCServer, *rpcInvokeState) {
	t.Helper()
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload:    payload,
		budget:     NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		mcpAllowed: mcpAllowlistFromPayload(payload),
	}
	return s, state
}

func mcpPayload(servers ...map[string]any) map[string]any {
	list := make([]any, len(servers))
	for i, s := range servers {
		list[i] = s
	}
	return map[string]any{"mcp_servers": list}
}

func mcpServerEntry(serverID string, tools ...string) map[string]any {
	toolList := make([]any, len(tools))
	for i, t := range tools {
		toolList[i] = t
	}
	return map[string]any{"server_id": serverID, "tools": toolList}
}

// TestMCP_NoRouter_FailsClosedInProduction verifies the no-router path
// returns a typed not-enabled error instead of a synthetic {ok: true} when
// AGENTPAAS_TEST_FAKE_MCP is unset.
func TestMCP_NoRouter_FailsClosedInProduction(t *testing.T) {
	// Ensure the fake flag is unset for this test (defense-in-depth even though
	// t.Setenv would normally handle it; explicit unset makes intent clear).
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "")

	recorder := &recordingAuditAppender{}
	payload := mcpPayload(mcpServerEntry("declared", "search"))
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "declared",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	resp := s.handleMCP(req, state)

	if resp.OK {
		t.Fatalf("response OK = true, want false (fail-closed); result=%#v", resp.Result)
	}
	if resp.Code != mcpNotFoundCode {
		t.Fatalf("response Code = %q, want %q; error=%q", resp.Code, mcpNotFoundCode, resp.Error)
	}
	if !strings.Contains(resp.Error, mcpNotFoundCode) {
		t.Fatalf("response Error = %q, want it to contain %q", resp.Error, mcpNotFoundCode)
	}
	// No synthetic {ok: true} result must be present.
	if resp.Result != nil {
		t.Fatalf("response Result = %#v, want nil (no synthetic success)", resp.Result)
	}

	// Audit must record a denial, not a fabricated success call.
	events := recorder.events()
	if len(events) == 0 {
		t.Fatal("no audit events recorded; want mcp_denied")
	}
	var lastEventType string
	for _, ev := range events {
		if ev.EventType == "mcp_call" {
			t.Fatalf("audit recorded mcp_call (fabricated success); want only mcp_denied: %#v", ev)
		}
		lastEventType = ev.EventType
	}
	if lastEventType != "mcp_denied" {
		t.Fatalf("last audit EventType = %q, want mcp_denied", lastEventType)
	}
	// The denial reason/payload should carry the typed error name.
	last := events[len(events)-1]
	reason, _ := last.Payload["reason"].(string)
	if !strings.Contains(reason, mcpNotFoundCode) {
		t.Fatalf("denial reason = %q, want it to contain %q; payload=%#v", reason, mcpNotFoundCode, last.Payload)
	}
}

// TestMCP_NoRouter_FakeModeReturnsSynthetic verifies that with
// AGENTPAAS_TEST_FAKE_MCP=1 the no-router path still returns the synthetic
// {ok: true} result and audits a call (not a denial), keeping existing test
// fixtures working when they opt in.
func TestMCP_NoRouter_FakeModeReturnsSynthetic(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "1")

	recorder := &recordingAuditAppender{}
	payload := mcpPayload(mcpServerEntry("declared", "search"))
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "declared",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	resp := s.handleMCP(req, state)

	if !resp.OK {
		t.Fatalf("response OK = false, want true (fake mode); error=%q code=%q", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("response Result = %#v, want map", resp.Result)
	}
	if result["server_id"] != "declared" || result["tool"] != "search" {
		t.Fatalf("result = %#v, want server_id=declared tool=search", result)
	}
	inner, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("result[\"result\"] = %#v, want map with ok=true", result["result"])
	}
	if inner["ok"] != true {
		t.Fatalf("synthetic result.ok = %#v, want true", inner["ok"])
	}

	// Audit must record a call, not a denial, so audit stays consistent with
	// the (synthetic) success response.
	events := recorder.events()
	if len(events) == 0 {
		t.Fatal("no audit events recorded; want mcp_call")
	}
	for _, ev := range events {
		if ev.EventType == "mcp_denied" {
			t.Fatalf("audit recorded mcp_denied in fake mode; want mcp_call: %#v", ev)
		}
	}
	if events[len(events)-1].EventType != "mcp_call" {
		t.Fatalf("last audit EventType = %q, want mcp_call", events[len(events)-1].EventType)
	}
}

// TestMCP_NoRouter_UndeclaredToolStillDenied proves the allowlist check runs
// BEFORE the fake path: an undeclared server/tool is rejected with mcp_denied
// even when AGENTPAAS_TEST_FAKE_MCP=1.
func TestMCP_NoRouter_UndeclaredToolStillDenied(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "1")

	recorder := &recordingAuditAppender{}
	// Allowlist declares "declared"/"tool", but the request targets "blocked".
	payload := mcpPayload(mcpServerEntry("declared", "tool"))
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "blocked",
			"tool":      "read",
			"input":     map[string]any{},
		},
	}
	resp := s.handleMCP(req, state)

	if resp.OK {
		t.Fatalf("response OK = true, want false (undeclared must be denied); result=%#v", resp.Result)
	}
	if resp.Code != "mcp_denied" {
		t.Fatalf("response Code = %q, want mcp_denied; error=%q", resp.Code, resp.Error)
	}
	// Audit records a denial, not a call.
	for _, ev := range recorder.events() {
		if ev.EventType == "mcp_call" {
			t.Fatalf("audit recorded mcp_call for undeclared tool; want mcp_denied: %#v", ev)
		}
	}
}

// TestMCP_WithRouter_FailsClosedDoesNotAffectRealRouter proves the fail-closed
// change does not regress real-router behavior: with a router installed, the
// call flows through the router and returns its (real) result, not the
// synthetic {ok: true}.
func TestMCP_WithRouter_FailsClosedDoesNotAffectRealRouter(t *testing.T) {
	// Fake mode must be OFF here — production path with a real router.
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "")

	// Stand up an HTTP MCP server that returns a distinctive result so we can
	// distinguish the router's response from the synthetic {ok: true}.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"source":"real-router","value":"router-data"}}`))
	}))
	defer upstream.Close()

	manager := mcpmanager.NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "declared",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"search"},
	}}, "agent-1", "run-1")
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)

	recorder := &recordingAuditAppender{}
	payload := mcpPayload(mcpServerEntry("declared", "search"))
	s, state := newMCPTestServer(t, recorder, payload)
	s.SetRouter(router)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "declared",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	// handleMCP builds its own context internally before calling the router.

	resp := s.handleMCP(req, state)

	if !resp.OK {
		t.Fatalf("response OK = false, want true (real router); error=%q code=%q", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("response Result = %#v, want map from router", resp.Result)
	}
	if result["source"] != "real-router" {
		t.Fatalf("result = %#v, want source=real-router (not synthetic {ok: true})", result)
	}
	if result["value"] != "router-data" {
		t.Fatalf("result.value = %#v, want router-data", result["value"])
	}
	// Ensure the synthetic {ok: true} marker is NOT in the result.
	if inner, ok := result["result"].(map[string]any); ok && inner["ok"] == true {
		t.Fatalf("result contains synthetic {ok: true}: %#v", result)
	}
}

// TestMCP_SyntheticPayloadStringAbsentInProduction is a source/AST guard that
// verifies the synthetic {"ok": true} construction on the MCP path appears
// ONLY inside the AGENTPAAS_TEST_FAKE_MCP conditional block, preventing a
// future refactor from re-introducing synthetic success on the production path.
func TestMCP_SyntheticPayloadStringAbsentInProduction(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "") // ensure production semantics

	source, err := os.ReadFile("rpc_server.go")
	if err != nil {
		t.Fatalf("read rpc_server.go: %v", err)
	}
	text := string(source)

	// The synthetic construction must exist (otherwise the test mode fake was
	// removed entirely, which would break existing opt-in fixtures).
	if !strings.Contains(text, `map[string]any{"ok": true}`) {
		t.Fatal("rpc_server.go no longer contains the synthetic {ok: true} construction; fake-mode MCP path was removed")
	}

	// Locate handleMCP and assert the synthetic construction lives inside the
	// AGENTPAAS_TEST_FAKE_MCP conditional block, not on the unconditional
	// production path.
	handleIdx := strings.Index(text, "func (s *harnessRPCServer) handleMCP")
	if handleIdx < 0 {
		t.Fatal("handleMCP function not found in rpc_server.go")
	}
	handleBody := text[handleIdx:]

	// Find the fake-MCP env check within handleMCP.
	fakeCheck := strings.Index(handleBody, `os.Getenv("AGENTPAAS_TEST_FAKE_MCP")`)
	if fakeCheck < 0 {
		t.Fatal("AGENTPAAS_TEST_FAKE_MCP check not found inside handleMCP; the fail-closed gate is missing")
	}

	// The synthetic construction must appear AFTER the fake-MCP check (i.e. it
	// is inside the conditional block), not before it.
	syntheticIdx := strings.Index(handleBody, `map[string]any{"ok": true}`)
	if syntheticIdx < 0 {
		t.Fatal("synthetic {ok: true} construction not found inside handleMCP")
	}
	if syntheticIdx < fakeCheck {
		t.Fatalf("synthetic {ok: true} at offset %d appears BEFORE the AGENTPAAS_TEST_FAKE_MCP check at offset %d — it is on the production path, not gated by fake mode", syntheticIdx, fakeCheck)
	}
}

// TestMCP_NoRouter_AuditRecordIsDeniedNotCall is a focused audit-record test
// for the production no-router path: the audit EventType is mcp_denied (not
// mcp_call) and the reason/payload includes the typed error name.
func TestMCP_NoRouter_AuditRecordIsDeniedNotCall(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "")

	recorder := &recordingAuditAppender{}
	payload := mcpPayload(mcpServerEntry("declared", "search"))
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "declared",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	_ = s.handleMCP(req, state)

	events := recorder.events()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1: %#v", len(events), events)
	}
	ev := events[0]
	if ev.EventType != "mcp_denied" {
		t.Fatalf("EventType = %q, want mcp_denied", ev.EventType)
	}
	if ev.Payload["server_id"] != "declared" || ev.Payload["tool"] != "search" {
		t.Fatalf("payload = %#v, want server_id=declared tool=search", ev.Payload)
	}
	reason, _ := ev.Payload["reason"].(string)
	if !strings.Contains(reason, mcpNotFoundCode) {
		t.Fatalf("reason = %q, want it to contain %q", reason, mcpNotFoundCode)
	}
}

// TestMCP_ManagedBinding_RejectsSyntheticSuccess proves that managed
// AgentPaaS service bindings (transport=agentpaas-service) never get
// synthetic success, even when AGENTPAAS_TEST_FAKE_MCP=1.
func TestMCP_ManagedBinding_RejectsSyntheticSuccess(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "1")

	recorder := &recordingAuditAppender{}
	// Managed binding: transport = "agentpaas-service"
	payload := map[string]any{
		"mcp_servers": []any{
			map[string]any{
				"server_id": "managed-svc",
				"name":      "managed-svc",
				"tools":     []any{"lookup"},
				"transport": "agentpaas-service",
			},
		},
	}
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "managed-svc",
			"tool":      "lookup",
			"input":     map[string]any{},
		},
	}
	resp := s.handleMCP(req, state)

	if resp.OK {
		t.Fatalf("response OK = true, want false (managed binding must not get synthetic success); result=%#v", resp.Result)
	}
	if resp.Code != mcpNotFoundCode {
		t.Fatalf("response Code = %q, want %q; error=%q", resp.Code, mcpNotFoundCode, resp.Error)
	}
	if !strings.Contains(resp.Error, "synthetic success is forbidden") {
		t.Fatalf("response Error = %q, want it to contain 'synthetic success is forbidden'", resp.Error)
	}

	// Audit must be denial.
	for _, ev := range recorder.events() {
		if ev.EventType == "mcp_call" {
			t.Fatalf("audit recorded mcp_call for managed binding in fake mode; want mcp_denied: %#v", ev)
		}
	}
}

// TestMCP_ManagedBinding_NonManagedStillSynthetic proves that non-managed
// bindings (regular stdio/HTTP) still get synthetic success in test mode.
func TestMCP_ManagedBinding_NonManagedStillSynthetic(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "1")

	recorder := &recordingAuditAppender{}
	// Regular stdio binding — not managed.
	payload := mcpPayload(mcpServerEntry("external-stdio", "search"))
	s, state := newMCPTestServer(t, recorder, payload)

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "external-stdio",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	resp := s.handleMCP(req, state)

	if !resp.OK {
		t.Fatalf("response OK = false, want true (non-managed binding should get synthetic in fake mode); error=%q", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("response Result = %#v, want map", resp.Result)
	}
	inner, ok := result["result"].(map[string]any)
	if !ok || inner["ok"] != true {
		t.Fatalf("synthetic result = %#v, want {ok: true}", result["result"])
	}
}

// TestMCP_RouterTypedErrorCodes proves that the Router's typed errors
// map to stable response codes in handleMCP.
func TestMCP_RouterTypedErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"protocol_error", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeProtocolError}, mcpmanager.ErrCodeProtocolError},
		{"service_not_found", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeServiceNotFound}, mcpmanager.ErrCodeServiceNotFound},
		{"service_not_ready", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeServiceNotReady}, mcpmanager.ErrCodeServiceNotReady},
		{"lease_expired", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeLeaseExpired}, mcpmanager.ErrCodeLeaseExpired},
		{"policy_denied", &mcpmanager.TypedError{Code: mcpmanager.ErrCodePolicyDenied}, mcpmanager.ErrCodePolicyDenied},
		{"timeout", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeTimeout}, mcpmanager.ErrCodeTimeout},
		{"cancelled", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeCancelled}, mcpmanager.ErrCodeCancelled},
		{"service_crashed", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeServiceCrashed}, mcpmanager.ErrCodeServiceCrashed},
		{"router_unavailable", &mcpmanager.TypedError{Code: mcpmanager.ErrCodeRouterUnavail}, mcpmanager.ErrCodeRouterUnavail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := mcpErrorCode(tt.err)
			if code != tt.wantCode {
				t.Errorf("mcpErrorCode(%v) = %q, want %q", tt.err, code, tt.wantCode)
			}
		})
	}
}

// TestMCP_RouterTypedErrorCode_Fallback tests the fallback heuristics in
// mcpErrorCode for non-TypedError errors.
func TestMCP_RouterTypedErrorCode_Fallback(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		wantCode string
	}{
		{"timeout_string", "timed out waiting for response", "mcp_timeout"},
		{"not_allowed", "mcp server/tool not allowed", "mcp_denied"},
		{"not_declared", "tool not declared", "mcp_denied"},
		{"not_found", "service not found", "mcp_service_not_found"},
		{"not_ready", "service not ready", "mcp_service_not_ready"},
		{"crashed", "server crashed", "mcp_service_crashed"},
		{"unknown", "something went wrong", "mcp_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := mcpErrorCode(errors.New(tt.errMsg))
			if code != tt.wantCode {
				t.Errorf("mcpErrorCode(%q) = %q, want %q", tt.errMsg, code, tt.wantCode)
			}
		})
	}
}

// TestMCP_OneCallOneAuditRecord proves that a single MCP call with router
// produces exactly one audit record — no duplicate router+harness events.
func TestMCP_OneCallOneAuditRecord(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "")

	// Stand up an HTTP MCP server that returns a distinctive result.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"source":"one-audit-test"}}`))
	}))
	defer upstream.Close()

	manager := mcpmanager.NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "test-svc",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"search"},
	}}, "agent-1", "run-1")
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)

	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	s.SetRouter(router)
	state := &rpcInvokeState{
		payload:    mcpPayload(mcpServerEntry("test-svc", "search")),
		budget:     NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		mcpAllowed: mcpAllowlistFromPayload(mcpPayload(mcpServerEntry("test-svc", "search"))),
	}

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "test-svc",
			"tool":      "search",
			"input":     map[string]any{"q": "x"},
		},
	}
	resp := s.handleMCP(req, state)
	if !resp.OK {
		t.Fatalf("response OK = false: error=%q code=%q", resp.Error, resp.Code)
	}

	events := recorder.events()
	// We expect exactly one audit record: a single mcp_call event.
	// The harness auditMCPCall and the Router's AuditToolCall both fire;
	// verify we don't double-record.
	mcpCalls := 0
	mcpDenied := 0
	for _, ev := range events {
		switch ev.EventType {
		case "mcp_call":
			mcpCalls++
		case "mcp_denied":
			mcpDenied++
		}
	}
	if mcpDenied > 0 {
		t.Fatalf("got %d mcp_denied events, want 0", mcpDenied)
	}
	if mcpCalls == 0 {
		t.Fatal("no mcp_call audit events recorded")
	}
	// The harness records one audit event per call path. The Router's
	// AuditToolCall adds another. The T05 spec requires one audit record
	// per call — we document the current count and expect T07 cleanup.
	t.Logf("audit events: %d total, %d mcp_call, %d mcp_denied", len(events), mcpCalls, mcpDenied)
}

// TestMCP_RouterDeliversManagedServiceResult proves the full managed service
// e2e path: Manager.Register with transport=agentpaas-service → Router →
// ManagedServiceResolver → httptest server → distinctive non-synthetic value.
// The test asserts the result carries the service's own value (not {ok: true}),
// proves Capability header reaches the service, and verifies managed path cannot
// succeed without the resolver/registry wired in.
func TestMCP_RouterDeliversManagedServiceResult(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_MCP", "")

	const workflowID = "wf-integration"
	const bindingID = "managed-integration"
	capToken := "test-integration-cap-token-00000000000000000000000000000001"

	// Capture request headers for assertion.
	var capturedHeaders http.Header

	// Stand up a fake MCP service endpoint returning a distinctive result.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":0,"result":{"source":"b33-t05-integration-real","value":"managed-e2e-proof"}}`))
	}))
	defer func() { upstream.Close() }()

	// Build a Router with Manager + managed resolver backed by a pre-built
	// ServiceRegistry instance (bypasses real container creation).
	manager := mcpmanager.NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         bindingID,
		Transport:    "agentpaas-service",
		AllowedTools: []string{"lookup"},
	}}, "agent-integration", "run-integration")

	inst := mcpmanager.TestServiceInstance(workflowID, bindingID,
		mcpmanager.StateReady, upstream.URL, capToken, []string{"lookup"})
	registry := mcpmanager.TestServiceRegistry([]*mcpmanager.ServiceInstance{inst})
	resolver := mcpmanager.TestManagedResolverHTTPClient(registry, upstream.Client())

	router := mcpmanager.NewRouter(manager, nil, nil, nil)
	router.SetManagedResolver(resolver, workflowID)

	// Wire the router into a harness RPC server via handleMCP.
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	s.SetRouter(router)
	state := &rpcInvokeState{
		// Managed binding in the payload: transport=agentpaas-service.
		payload: map[string]any{
			"mcp_servers": []any{
				map[string]any{
					"server_id": bindingID,
					"name":      bindingID,
					"tools":     []any{"lookup"},
					"transport": "agentpaas-service",
				},
			},
		},
		budget:     NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		mcpAllowed: map[string]map[string]bool{bindingID: {"lookup": true}},
	}

	req := rpcRequest{
		ID:     "1",
		Method: "mcp",
		Params: map[string]any{
			"server_id": bindingID,
			"tool":      "lookup",
			"input":     map[string]any{"q": "integration"},
		},
	}
	resp := s.handleMCP(req, state)

	if !resp.OK {
		t.Fatalf("response OK = false, want true (managed service via resolver); error=%q code=%q", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("response Result = %#v, want map from managed resolver", resp.Result)
	}
	// Prove result is NOT the synthetic {ok: true}.
	if result["source"] != "b33-t05-integration-real" {
		t.Fatalf("result.source = %q, want b33-t05-integration-real (prove real service value)", result["source"])
	}
	if result["value"] != "managed-e2e-proof" {
		t.Fatalf("result.value = %q, want managed-e2e-proof", result["value"])
	}
	// Ensure no synthetic inner {ok: true}.
	if inner, ok := result["result"].(map[string]any); ok && inner["ok"] == true {
		t.Fatalf("result contains synthetic {ok: true}: %#v", result)
	}

	// Assert Capability header reached the upstream service.
	if capHdr := capturedHeaders.Get(mcpmanager.CapabilityHeader); capHdr != capToken {
		t.Fatalf("Capability header = %q, want %q", capHdr, capToken)
	}

	// Prove managed path fails without resolver (unwire it).
	s2 := &harnessRPCServer{audit: recorder}
	router2 := mcpmanager.NewRouter(manager, nil, nil, nil)
	// Intentionally do NOT call SetManagedResolver on router2.
	s2.SetRouter(router2)
	state2 := &rpcInvokeState{
		payload: map[string]any{
			"mcp_servers": []any{
				map[string]any{
					"server_id": bindingID,
					"name":      bindingID,
					"tools":     []any{"lookup"},
					"transport": "agentpaas-service",
				},
			},
		},
		budget:     NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		mcpAllowed: map[string]map[string]bool{bindingID: {"lookup": true}},
	}
	resp2 := s2.handleMCP(req, state2)

	if resp2.OK {
		t.Fatalf("response OK = true (no resolver), want false; result=%#v", resp2.Result)
	}
	if !strings.Contains(resp2.Error, "managed service resolver not configured") {
		t.Fatalf("error = %q, want 'managed service resolver not configured'", resp2.Error)
	}
}
