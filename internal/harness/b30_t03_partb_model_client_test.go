package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB30T03PartB_ModelClientTimeout_DerivedFromTimeEnvelope verifies ceiling
// 5: when a TimeEnvelope is in the invoke state, the LLM HTTP client timeout
// is derived from EffectiveOperationDeadlineMs(nowMs, ModelCallTimeoutMs),
// not the legacy 120s constant.
func TestB30T03PartB_ModelClientTimeout_DerivedFromTimeEnvelope(t *testing.T) {
	// ModelCallTimeoutMs=5000, lease=30000, active=30000 → min=5000.
	env, ok := routedrun.TimeEnvelopeFromCeilings(30_000, 30_000, 10_000, 5_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{
		nowMonotonicMs: func() int64 { return nowMs },
	}
	state := &rpcInvokeState{
		payload:      map[string]any{},
		budget:       NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		timeEnvelope: &env,
	}
	want := 5 * time.Second
	if got := s.modelClientTimeout(state); got != want {
		t.Fatalf("modelClientTimeout = %v, want %v (envelope-derived)", got, want)
	}
}

// TestB30T03PartB_ModelClientTimeout_LegacyFallback120s verifies the v0.2.3
// compat path: with no TimeEnvelope in the invoke state, the timeout falls
// back to the legacy 120s constant.
func TestB30T03PartB_ModelClientTimeout_LegacyFallback120s(t *testing.T) {
	s := &harnessRPCServer{}
	state := &rpcInvokeState{
		payload: map[string]any{},
		budget:  NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	want := 120 * time.Second
	if got := s.modelClientTimeout(state); got != want {
		t.Fatalf("modelClientTimeout = %v, want %v (legacy fallback)", got, want)
	}
}

// TestB30T03PartB_ModelClientTimeout_EnvelopeExhaustedClampsLow verifies
// that an exhausted envelope yields a sub-second (effectively zero) timeout
// rather than the legacy 120s — the model call cannot exceed remaining time.
func TestB30T03PartB_ModelClientTimeout_EnvelopeExhaustedClampsLow(t *testing.T) {
	env, ok := routedrun.TimeEnvelopeFromCeilings(10_000, 10_000, 10_000, 10_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	// consumed == max → remaining = 0 → deadline 0.
	env.ConsumedActiveDurationMs = 10_000
	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{
		nowMonotonicMs: func() int64 { return nowMs },
	}
	state := &rpcInvokeState{
		payload:      map[string]any{},
		budget:       NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		timeEnvelope: &env,
	}
	got := s.modelClientTimeout(state)
	if got <= 0 || got > 1*time.Millisecond {
		t.Fatalf("modelClientTimeout = %v, want (0,1ms] (exhausted envelope)", got)
	}
}

// TestB30T03PartB_ModelClient_RealCall_UsesEnvelopeTimeout is an end-to-end
// smoke test: a slow upstream that exceeds the envelope-derived timeout is
// killed, while the call surfaces a structured error rather than hanging.
func TestB30T03PartB_ModelClient_RealCall_UsesEnvelopeTimeout(t *testing.T) {
	// Upstream sleeps 2s; envelope gives a 200ms model-call timeout.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "late"}},
			},
			"usage": map[string]any{"total_tokens": 1},
			"model": "gpt-4o",
		})
	}))
	defer ts.Close()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 10_000, 200)
	if !ok {
		t.Fatal("expected envelope")
	}
	nowMs := routedrun.NowMonotonicMs(nil)
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{
		audit:         recorder,
		nowMonotonicMs: func() int64 { return nowMs },
	}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "openai",
				"model":      "gpt-4o",
				"credential": testCredID,
			},
		},
		credentials: map[string]rpcCredential{
			testCredID: {Header: "Authorization", Value: testSecret},
		},
		budget:       NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate:    nil,
		timeEnvelope: &env,
	}
	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "Say hello"}}
	start := time.Now()
	resp := s.handleLLM(req, state)
	elapsed := time.Since(start)
	if resp.OK {
		t.Fatalf("expected error (client timeout), got OK")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("elapsed = %v, want < 1.5s (envelope timeout 200ms)", elapsed)
	}
}
