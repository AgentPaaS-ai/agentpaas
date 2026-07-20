package harness

import (
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
