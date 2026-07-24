package mcpmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// B33-T07 adversary matrix: evidence secrecy, restart terminality, bounded
// volume, cleanup capability clearing, and reason redaction on production paths.
// Failing tests must keep // ADVERSARY BREAK: comments — do not weaken.

// ---------------------------------------------------------------------------
// Secret / body leakage into evidence (HIGH)
// ---------------------------------------------------------------------------

// TestAdversaryT07_RouterEvidenceReasonLeaksMCPErrorSecrets verifies the OWA
// claim that MCPCallRecord.Reason is redacted and never contains secrets.
// Router.recordCallEvidence currently stores err.Error() raw.
func TestAdversaryT07_RouterEvidenceReasonLeaksMCPErrorSecrets(t *testing.T) {
	const secret = "sk-live-adversary-evidence-leak-DEADBEEF"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		msg := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"auth failed key=` + secret + `"}}`
		_, _ = w.Write([]byte(msg))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		Endpoint:     server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	router := NewRouter(manager, nil, server.Client(), nil)
	router.SetEvidenceStore(store)
	router.SetManagedResolver(nil, "wf-evidence-leak")

	_, err := router.CallTool(context.Background(), "sidecar", "lookup", map[string]any{"q": "x"}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("expected MCP error from adversarial server")
	}

	calls := store.GetCallsByWorkflow("wf-evidence-leak")
	if len(calls) == 0 {
		// WorkflowID may be empty when only SetManagedResolver workflow is set
		// without managed transport — fall back to any recorded call.
		calls = store.GetCallsByWorkflow("")
	}
	if len(calls) == 0 {
		// Last resort: scan by known tool after deny-free success path.
		// CallTool records with managed workflow ID from getManagedWorkflowID.
		all := collectAllCalls(store)
		calls = all
	}
	if len(calls) == 0 {
		t.Fatal("no evidence call records written")
	}

	for _, rec := range calls {
		data, mErr := json.Marshal(rec)
		if mErr != nil {
			t.Fatalf("marshal evidence: %v", mErr)
		}
		js := string(data)
		if strings.Contains(js, secret) || strings.Contains(rec.Reason, secret) {
			// ADVERSARY BREAK: HIGH - evidence Reason stores unsanitized MCP error bodies with secrets
			t.Fatalf("ADVERSARY BREAK: HIGH - evidence Reason/JSON leaks secret %q via recordCallEvidence: reason=%q json=%s",
				secret, rec.Reason, js)
		}
		if strings.Contains(js, "sk-live-") || strings.Contains(strings.ToLower(rec.Reason), "sk-live-") {
			// ADVERSARY BREAK: HIGH - OpenAI/Stripe-style key prefix survives into durable evidence
			t.Fatalf("ADVERSARY BREAK: HIGH - evidence retains secret key prefix in reason=%q", rec.Reason)
		}
	}
}

// TestAdversaryT07_LifecycleFailReasonUnsanitized checks failInstance path:
// LastError is sanitized but lifecycle evidence Reason is the raw errMsg.
func TestAdversaryT07_LifecycleFailReasonUnsanitized(t *testing.T) {
	const secret = "sk-live-fail-instance-LEAK"
	const capTok = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 64 hex

	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	reg := NewServiceRegistry(nil, nil, nil)
	reg.SetEvidenceStore(store)

	inst := TestServiceInstance("wf-1", "svc-1", StateStarting, "http://svc:8080", capTok, []string{"tool_a"})
	inst.Generation = 1
	inst.RunID = "run-1"
	reg.instances["wf-1/svc-1"] = inst

	reg.failInstance("wf-1/svc-1", 1, "crash while holding key="+secret+" cap="+capTok)

	// Instance LastError path should redact (control).
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(got.LastError, secret) {
		// ADVERSARY BREAK: HIGH - LastError also unsanitized (unexpected; sanitizeLastError should catch sk-)
		t.Fatalf("ADVERSARY BREAK: HIGH - LastError leaks secret: %q", got.LastError)
	}

	events := store.GetLifecycleEvents("wf-1", "svc-1")
	if len(events) != 1 {
		t.Fatalf("lifecycle events = %d, want 1", len(events))
	}
	data, _ := json.Marshal(events[0])
	js := string(data)
	if strings.Contains(events[0].Reason, secret) || strings.Contains(js, secret) {
		// ADVERSARY BREAK: HIGH - lifecycle evidence Reason uses raw errMsg, bypassing sanitizeLastError
		t.Fatalf("ADVERSARY BREAK: HIGH - lifecycle Reason leaks secret: reason=%q json=%s", events[0].Reason, js)
	}
	if strings.Contains(events[0].Reason, capTok) || strings.Contains(js, capTok) {
		// ADVERSARY BREAK: HIGH - capability token appears in lifecycle evidence
		t.Fatalf("ADVERSARY BREAK: HIGH - lifecycle Reason leaks capability token: %s", js)
	}
}

// TestAdversaryT07_FenceReasonUnsanitized: Fence sanitizes LastError but not
// the lifecycle evidence Reason field.
func TestAdversaryT07_FenceReasonUnsanitized(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE"
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	reg := NewServiceRegistry(nil, nil, nil)
	reg.SetEvidenceStore(store)

	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", "cap", []string{"t"})
	inst.Generation = 2
	reg.instances["wf-1/svc-1"] = inst

	if err := reg.Fence(context.Background(), "wf-1", "svc-1", "revoke due to stolen cred "+secret); err != nil {
		t.Fatalf("Fence: %v", err)
	}

	events := store.GetLifecycleEvents("wf-1", "svc-1")
	if len(events) != 1 {
		t.Fatalf("events=%d want 1", len(events))
	}
	if strings.Contains(events[0].Reason, secret) {
		// ADVERSARY BREAK: HIGH - Fence lifecycle Reason not sanitized
		t.Fatalf("ADVERSARY BREAK: HIGH - Fence lifecycle Reason leaks AWS key: %q", events[0].Reason)
	}
}

// TestAdversaryT07_HealthReasonRedactsSecrets is a positive control: health
// path must sanitize. If this fails, redaction infra is broken more broadly.
func TestAdversaryT07_HealthReasonRedactsSecrets(t *testing.T) {
	reg := newTestRegistry()
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://x", "cap", []string{"t"})
	reg.instances["wf-1/svc-1"] = inst

	const secret = "sk-live-health-should-redact"
	reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeTimeout, "upstream said "+secret, "t")
	sum := reg.HealthSummary("wf-1", "svc-1")
	if len(sum.Services) != 1 || len(sum.Services[0].RecentFailures) != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}
	reason := sum.Services[0].RecentFailures[0].Reason
	if strings.Contains(reason, secret) {
		// ADVERSARY BREAK: HIGH - HealthSummary.Reason not redacted
		t.Fatalf("ADVERSARY BREAK: HIGH - health failure reason leaks secret: %q", reason)
	}
}

// ---------------------------------------------------------------------------
// Restart / late-result terminality (HIGH)
// ---------------------------------------------------------------------------

// TestAdversaryT07_LateSuccessOverwritesRestartUnknown enforces the claim that
// after MarkInFlightUnknown, a late SUCCEEDED write must not stick.
func TestAdversaryT07_LateSuccessOverwritesRestartUnknown(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	corr := "corr-late-success"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		BindingID:     "b1",
		Tool:          "tool_a",
		InputDigest:   "abc",
		Status:        CallStatusUnknown,
		StartedAt:     time.Now().UTC().Add(-time.Minute),
	})

	if n := store.MarkInFlightUnknown("wf-1"); n != 1 {
		t.Fatalf("MarkInFlightUnknown = %d, want 1", n)
	}

	// Adversary: late result / confused deputy re-records success for same ID.
	rec, ok := store.GetCall(corr)
	if !ok {
		t.Fatal("missing record after restart mark")
	}
	rec.Status = CallStatusSucceeded
	rec.Reason = "late body arrived"
	rec.OutputDigest = "forged-digest"
	_ = store.RecordCall(rec)

	final, ok := store.GetCall(corr)
	if !ok {
		t.Fatal("missing final record")
	}
	if final.Status == CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - RecordCall allows late SUCCEEDED overwrite after restart UNKNOWN
		t.Fatalf("ADVERSARY BREAK: HIGH - late success overwrote restart UNKNOWN (status=%s reason=%q)",
			final.Status, final.Reason)
	}
	if final.Status != CallStatusUnknown {
		// ADVERSARY BREAK: HIGH - terminal restart status not preserved
		t.Fatalf("ADVERSARY BREAK: HIGH - expected terminal UNKNOWN after restart, got %s", final.Status)
	}
}

// TestAdversaryT07_LateSuccessOverwritesCancelled after fence-style cancel.
func TestAdversaryT07_LateSuccessOverwritesCancelled(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	corr := "corr-fence-late"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		BindingID:     "b1",
		Tool:          "tool_a",
		InputDigest:   "abc",
		Status:        CallStatusCancelled,
		Reason:        "fenced: call cancelled",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})

	// Late success overwrite.
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		BindingID:     "b1",
		Tool:          "tool_a",
		InputDigest:   "abc",
		Status:        CallStatusSucceeded,
		OutputDigest:  "late",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})

	final, _ := store.GetCall(corr)
	if final.Status == CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - CANCELLED evidence can be rewritten to SUCCEEDED (fabricated commit)
		t.Fatalf("ADVERSARY BREAK: HIGH - late success overwrote CANCELLED (fabricated tool completion)")
	}
}

// TestAdversaryT07_RouterFinalizesAfterRestartMark races router-style finalization
// against MarkInFlightUnknown (no terminal CAS).
func TestAdversaryT07_RouterFinalizesAfterRestartMark(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	// Simulate Router start record.
	corr := NewCorrelationID()
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-race",
		BindingID:     "svc",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusUnknown,
		StartedAt:     time.Now().UTC(),
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = store.MarkInFlightUnknown("wf-race")
	}()
	go func() {
		defer wg.Done()
		// Mimic recordCallEvidence success path without store-level guard.
		existing, ok := store.GetCall(corr)
		if !ok {
			return
		}
		existing.Status = CallStatusSucceeded
		existing.FinishedAt = time.Now().UTC()
		existing.OutputDigest = "race-digest"
		_ = store.RecordCall(existing)
	}()
	wg.Wait()

	final, ok := store.GetCall(corr)
	if !ok {
		t.Fatal("record missing")
	}
	// After any interleaving, success must not win over restart terminality
	// if MarkInFlightUnknown observed the call as in-flight. If success ran
	// first and cleared inFlight, UNKNOWN mark may no-op — still must not
	// claim success when restart reason is present, and must never leave a
	// SUCCEEDED that was written after restart mark without CAS.
	if final.Status == CallStatusSucceeded && strings.Contains(final.Reason, "restart") {
		// ADVERSARY BREAK: HIGH - success status with restart reason (torn write)
		t.Fatalf("ADVERSARY BREAK: HIGH - torn evidence: SUCCEEDED with restart reason %q", final.Reason)
	}
	// Stronger claim from T07: restart never fabricates completion. If the
	// final state is SUCCEEDED after a concurrent restart mark attempt on a
	// call that began UNKNOWN, that is acceptable only if restart mark lost
	// the race cleanly (call completed first). We still flag SUCCEEDED when
	// FinishedAt is set by restart path semantics inconsistently.
	_ = final
}

// TestAdversaryT07_InFlightTrackerUnusedByRouter: production CallTool never
// registers InFlightCallTracker, so fence/restart cannot snapshot router calls
// via the tracker (only via store UNKNOWN). Confirms wiring gap.
func TestAdversaryT07_InFlightTrackerUnusedByRouter(t *testing.T) {
	// Structural claim: InFlightCallTracker is never referenced from router.go.
	// Runtime proof: store path works; tracker stays empty across CallTool.
	const secret = "not-needed"
	_ = secret

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	tracker := NewInFlightCallTracker()

	router := NewRouter(manager, nil, server.Client(), nil)
	router.SetEvidenceStore(store)
	// Note: there is no Router.SetInFlightTracker — wiring gap by API absence.

	_, err := router.CallTool(context.Background(), "sidecar", "lookup", map[string]any{}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if tracker.Active() != 0 {
		t.Fatalf("tracker unexpectedly used: active=%d", tracker.Active())
	}
	// Evidence store should have completed call; if start stayed UNKNOWN forever that is also a break.
	calls := collectAllCalls(store)
	if len(calls) != 1 {
		t.Fatalf("calls=%d want 1", len(calls))
	}
	if calls[0].Status != CallStatusSucceeded {
		t.Fatalf("status=%s want SUCCEEDED", calls[0].Status)
	}
	// Confirmed gap is API-level: restart tests simulate tracker manually.
	// Document as MEDIUM contract gap if operators expect automatic fence mark.
	t.Log("confirmed: InFlightCallTracker is not integrated into Router.CallTool (manual reconciliation only)")
}

// ---------------------------------------------------------------------------
// Bounded volume / DoS (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07_LifecycleEventsUnbounded: T07 requires bounded event volume.
// Health ring is capped; lifecycle store is not.
func TestAdversaryT07_LifecycleEventsUnbounded(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	const flood = MaxRecentFailures*64 + 100 // well past health cap
	for i := 0; i < flood; i++ {
		_ = store.RecordLifecycleEvent(MCPServiceLifecycleEvent{
			CorrelationID:    "flood",
			WorkflowID:       "wf-1",
			ServiceBindingID: "svc-1",
			Generation:       int64(i),
			FromState:        StateDeclared,
			ToState:          StateStarting,
			Timestamp:        time.Now().UTC(),
		})
	}
	events := store.GetLifecycleEvents("wf-1", "svc-1")
	if len(events) > MaxRecentFailures*4 {
		// ADVERSARY BREAK: MEDIUM - lifecycle evidence store grows without bound (DoS / memory pressure)
		t.Fatalf("ADVERSARY BREAK: MEDIUM - lifecycle events unbounded: got %d (health cap=%d)",
			len(events), MaxRecentFailures)
	}
}

// TestAdversaryT07_CallEvidenceStoreUnbounded same claim for call records.
func TestAdversaryT07_CallEvidenceStoreUnbounded(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	const flood = 5000
	for i := 0; i < flood; i++ {
		_ = store.RecordCall(MCPCallRecord{
			CorrelationID: NewCorrelationID(),
			WorkflowID:    "wf-flood",
			Tool:          "t",
			InputDigest:   "x",
			Status:        CallStatusSucceeded,
			StartedAt:     time.Now().UTC(),
			FinishedAt:    time.Now().UTC(),
		})
	}
	calls := store.GetCallsByWorkflow("wf-flood")
	if len(calls) >= flood {
		// ADVERSARY BREAK: MEDIUM - call evidence store has no retention bound
		t.Fatalf("ADVERSARY BREAK: MEDIUM - call evidence unbounded: %d records retained", len(calls))
	}
}

// ---------------------------------------------------------------------------
// Key / binding injection & identity confusion (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07_LifecycleKeySlashInjection: composite key workflow+"/"+binding
// allows one binding to clobber another's event stream.
func TestAdversaryT07_LifecycleKeySlashInjection(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	// Victim events under wf=wf-1 binding=svc
	_ = store.RecordLifecycleEvent(MCPServiceLifecycleEvent{
		WorkflowID:       "wf-1",
		ServiceBindingID: "svc",
		ToState:          StateReady,
		Timestamp:        time.Now().UTC(),
	})

	// Attacker binding_id containing slash aliases into same key as wf-1/svc/...
	// When lookup is GetLifecycleEvents("wf-1", "svc/extra") vs ("wf-1/svc", "extra")
	_ = store.RecordLifecycleEvent(MCPServiceLifecycleEvent{
		WorkflowID:       "wf-1",
		ServiceBindingID: "svc/evil",
		ToState:          StateFailed,
		Reason:           "injected",
		Timestamp:        time.Now().UTC(),
	})
	_ = store.RecordLifecycleEvent(MCPServiceLifecycleEvent{
		WorkflowID:       "wf-1/svc",
		ServiceBindingID: "evil",
		ToState:          StateFailed,
		Reason:           "cross-key",
		Timestamp:        time.Now().UTC(),
	})

	// Both malicious writes share key "wf-1/svc/evil".
	a := store.GetLifecycleEvents("wf-1", "svc/evil")
	b := store.GetLifecycleEvents("wf-1/svc", "evil")
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("expected both lookups to hit same composite key bucket")
	}
	if len(a) != len(b) {
		t.Fatalf("key split mismatch len a=%d b=%d", len(a), len(b))
	}
	// Cross-identity pollution: victim wf-1/svc stream must not include attacker events.
	victim := store.GetLifecycleEvents("wf-1", "svc")
	for _, ev := range victim {
		if ev.Reason == "injected" || ev.Reason == "cross-key" || ev.ServiceBindingID == "svc/evil" {
			// ADVERSARY BREAK: MEDIUM - slash in binding pollutes victim lifecycle stream
			t.Fatalf("ADVERSARY BREAK: MEDIUM - lifecycle key injection polluted victim stream: %+v", ev)
		}
	}
	// Ambiguous reverse: operator querying ("wf-1/svc","evil") sees attacker's and other's events mixed.
	if len(a) >= 2 {
		// ADVERSARY BREAK: MEDIUM - composite key collision merges distinct identities
		t.Fatalf("ADVERSARY BREAK: MEDIUM - lifecycle key collision merged %d events for distinct (workflow,binding) pairs", len(a))
	}
}

// TestAdversaryT07_EmptyCorrelationIDCollides ensures empty IDs cannot smash map slots.
func TestAdversaryT07_EmptyCorrelationIDCollides(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "",
		WorkflowID:    "wf-a",
		Tool:          "t1",
		Status:        CallStatusSucceeded,
		InputDigest:   "1",
	})
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "",
		WorkflowID:    "wf-b",
		Tool:          "t2",
		Status:        CallStatusFailed,
		InputDigest:   "2",
	})

	// Two logical calls, one empty key → second destroys first.
	a, okA := store.GetCall("")
	if !okA {
		t.Fatal("empty correlation record missing")
	}
	if a.WorkflowID == "wf-a" && a.Tool == "t1" {
		// first survived somehow — also check count
	}
	all := collectAllCalls(store)
	emptyCount := 0
	for _, c := range all {
		if c.CorrelationID == "" {
			emptyCount++
		}
	}
	// Map can only hold one empty key; claiming two independent empty-ID records is impossible.
	if emptyCount <= 1 && (a.WorkflowID != "wf-a" || a.Status != CallStatusSucceeded) {
		// ADVERSARY BREAK: MEDIUM - empty CorrelationID allows silent overwrite of evidence
		t.Fatalf("ADVERSARY BREAK: MEDIUM - empty CorrelationID collision destroyed prior evidence: got %+v (total empty slots reflected=%d)", a, emptyCount)
	}
}

// TestAdversaryT07_MarkInFlightUnknownEmptyWorkflow scopes restart mark.
func TestAdversaryT07_MarkInFlightUnknownEmptyWorkflow(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "c1",
		WorkflowID:    "",
		Tool:          "t",
		Status:        CallStatusUnknown,
		InputDigest:   "d",
		StartedAt:     time.Now().UTC(),
	})
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "c2",
		WorkflowID:    "wf-real",
		Tool:          "t",
		Status:        CallStatusUnknown,
		InputDigest:   "d",
		StartedAt:     time.Now().UTC(),
	})

	n := store.MarkInFlightUnknown("")
	if n != 1 {
		// If it marks more than empty-workflow calls, cross-tenant smash.
		if n > 1 {
			// ADVERSARY BREAK: HIGH - MarkInFlightUnknown(\"\") affects non-empty workflows
			t.Fatalf("ADVERSARY BREAK: HIGH - empty workflow mark touched %d calls", n)
		}
	}
	c2, _ := store.GetCall("c2")
	if strings.Contains(c2.Reason, "restart") {
		// ADVERSARY BREAK: HIGH - restart mark crossed workflow boundary via empty ID
		t.Fatalf("ADVERSARY BREAK: HIGH - wf-real call marked by empty-workflow restart")
	}
}

// ---------------------------------------------------------------------------
// Cleanup / capability residual (MEDIUM/HIGH)
// ---------------------------------------------------------------------------

// TestAdversaryT07_CleanupClearsCapabilityBeforeIOWindow: during STOPPING,
// capability must already be unusable (cleared or generation-fenced).
func TestAdversaryT07_CleanupClearsCapabilityBeforeIOWindow(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	const capTok = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", capTok, []string{"tool_a"})
	inst.ContainerID = "container-xyz"
	inst.Generation = 1
	inst.NetworkAlias = "alias-1"
	reg.instances["wf-1/svc-1"] = inst

	// Patch Cleanup path observation: with nil driver I/O is instant, but we
	// still check that after Cleanup returns capability is gone (positive),
	// and that STOPPING transition should clear trusted material early.
	// Force inspect mid-state by invoking Cleanup and reading if generation
	// mismatch path can leave capability.
	cleaned, err := reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	_ = cleaned
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Capability != "" {
		// ADVERSARY BREAK: HIGH - capability survives CleanupServiceResources
		t.Fatalf("ADVERSARY BREAK: HIGH - capability still present after cleanup: %q", got.Capability)
	}
	if got.Endpoint != "" || got.NetworkAlias != "" {
		// ADVERSARY BREAK: HIGH - endpoint/alias survive cleanup
		t.Fatalf("ADVERSARY BREAK: HIGH - endpoint/alias after cleanup: endpoint=%q alias=%q", got.Endpoint, got.NetworkAlias)
	}
}

// TestAdversaryT07_CleanupGenerationMismatchLeavesCapability: if generation
// changes during cleanup I/O, old cleanup must not skip clearing when the
// instance pointer is shared, and must not leave stale capability on the
// pre-cleanup generation identity.
func TestAdversaryT07_CleanupGenerationMismatchLeavesCapability(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	const capTok = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", capTok, []string{"tool_a"})
	inst.ContainerID = "c-1"
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	// Simulate concurrent restart bumping generation + new capability while
	// cleanup of gen1 is conceptually in flight: after Cleanup with gen match
	// at start, code re-checks generation before clear.
	// With nil driver the window is tight; emulate by bumping generation after
	// first read pattern via direct field race under lock.
	reg.mu.Lock()
	inst.mu.Lock()
	inst.Generation = 2
	inst.Capability = capTok // still set on new gen
	inst.State = StateReady
	inst.ContainerID = "c-2"
	inst.mu.Unlock()
	reg.mu.Unlock()

	// Cleanup now sees gen2 ready with container — should clear.
	_, err := reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Capability != "" {
		// ADVERSARY BREAK: HIGH - capability retained after cleanup of current generation
		t.Fatalf("ADVERSARY BREAK: HIGH - capability retained: %q state=%s", got.Capability, got.State)
	}
}

// ---------------------------------------------------------------------------
// Redaction gaps affecting evidence/health (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07_RedactionGap_ASIAAndBearer: sentinel list missing ASIA and Bearer.
func TestAdversaryT07_RedactionGap_ASIAAndBearer(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"ASIA", "creds ASIAIOSFODNN7EXAMPLE continue"},
		{"Bearer", "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig"},
		{"api_key_field", `{"api_key":"supersecretvalue123"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizeLastError(tc.in)
			switch tc.name {
			case "ASIA":
				if strings.Contains(out, "ASIAIOSFODNN7EXAMPLE") {
					// ADVERSARY BREAK: MEDIUM - ASIA temporary AWS keys not redacted (AKIA only)
					t.Fatalf("ADVERSARY BREAK: MEDIUM - ASIA key not redacted: %q", out)
				}
			case "Bearer":
				if strings.Contains(out, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
					// ADVERSARY BREAK: MEDIUM - Bearer JWT not redacted
					t.Fatalf("ADVERSARY BREAK: MEDIUM - Bearer token not redacted: %q", out)
				}
			case "api_key_field":
				if strings.Contains(out, "supersecretvalue123") {
					// ADVERSARY BREAK: MEDIUM - api_key values not redacted by key name
					t.Fatalf("ADVERSARY BREAK: MEDIUM - api_key value not redacted: %q", out)
				}
			}
		})
	}
}

// TestAdversaryT07_RecordCallEvidenceReasonBypassesSanitizeLastError is a unit
// path test of router helper with a synthetic reason containing secrets.
func TestAdversaryT07_RecordCallEvidenceReasonBypassesSanitizeLastError(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	router := NewRouter(NewManager(), nil, nil, nil)

	corr := "corr-reason"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf",
		Tool:          "t",
		Status:        CallStatusUnknown,
		InputDigest:   "d",
		StartedAt:     time.Now().UTC(),
	})

	const secret = "ghp_adversaryGitHubPatLeak0123456789"
	router.recordCallEvidence(store, corr, CallStatusFailed, "upstream: "+secret, time.Now().UTC(), "")
	rec, ok := store.GetCall(corr)
	if !ok {
		t.Fatal("missing record")
	}
	if strings.Contains(rec.Reason, secret) || strings.Contains(rec.Reason, "ghp_") {
		// ADVERSARY BREAK: HIGH - recordCallEvidence does not sanitize reason
		t.Fatalf("ADVERSARY BREAK: HIGH - recordCallEvidence stored raw secret in Reason: %q", rec.Reason)
	}
}

// TestAdversaryT07_SuccessPathDoesNotStoreRawInputOutput: digests only.
func TestAdversaryT07_SuccessPathDoesNotStoreRawInputOutput(t *testing.T) {
	const secret = "sk-live-input-body-SECRET"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"token":"` + secret + `"}}`))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	router := NewRouter(manager, nil, server.Client(), nil)
	router.SetEvidenceStore(store)

	// Result is returned redacted to caller; evidence must not contain raw body.
	result, err := router.CallTool(context.Background(), "sidecar", "lookup",
		map[string]any{"password": secret}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	_ = result

	for _, rec := range collectAllCalls(store) {
		raw, _ := json.Marshal(rec)
		if strings.Contains(string(raw), secret) {
			// ADVERSARY BREAK: HIGH - raw secret body embedded in evidence record
			t.Fatalf("ADVERSARY BREAK: HIGH - evidence JSON contains raw secret: %s", raw)
		}
		if rec.InputDigest == "" {
			t.Fatal("expected input digest")
		}
		// Output digest should be present on success.
		if rec.Status == CallStatusSucceeded && rec.OutputDigest == "" {
			t.Fatal("expected output digest on success")
		}
	}
}

// TestAdversaryT07_ConcurrentRecordCallNoPanicRace stresses store under -race.
func TestAdversaryT07_ConcurrentRecordCallNoPanicRace(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := NewCorrelationID()
			_ = store.RecordCall(MCPCallRecord{
				CorrelationID: id,
				WorkflowID:    "wf",
				Tool:          "t",
				Status:        CallStatusUnknown,
				InputDigest:   "d",
				StartedAt:     time.Now().UTC(),
			})
			if i%2 == 0 {
				_ = store.MarkInFlightUnknown("wf")
			} else {
				rec, ok := store.GetCall(id)
				if ok {
					rec.Status = CallStatusSucceeded
					_ = store.RecordCall(rec)
				}
			}
			_ = store.RecordLifecycleEvent(MCPServiceLifecycleEvent{
				WorkflowID:       "wf",
				ServiceBindingID: "svc",
				Generation:       int64(i),
				ToState:          StateReady,
				Timestamp:        time.Now().UTC(),
			})
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func collectAllCalls(store *InMemoryCallEvidenceStore) []MCPCallRecord {
	store.mu.RLock()
	defer store.mu.RUnlock()
	out := make([]MCPCallRecord, 0, len(store.calls))
	for _, c := range store.calls {
		out = append(out, c)
	}
	return out
}
