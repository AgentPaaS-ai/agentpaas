package harness

import (
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB30T03PartB_HarnessInvokeTimeout_DerivedFromTimeEnvelope verifies
// ceiling 3: when the invoke payload carries a TimeEnvelope, the harness
// /invoke context timeout is derived from the envelope's active-time
// remaining and attempt-lease remaining only — NOT the per-operation
// StallTimeoutMs, which is a per-operation stall-detection ceiling inside
// the worker, not a session-level ceiling.
func TestB30T03PartB_HarnessInvokeTimeout_DerivedFromTimeEnvelope(t *testing.T) {
	// Case 1: active=60s, lease=60s, stall=30s → want 60s (active bound by lease,
	// stall must NOT cap). With the old EffectiveOperationDeadlineMs this
	// would have been min(30s, 60s, 60s)=30s.
	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 30_000, 30_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	nowMs := routedrun.NowMonotonicMs(nil)
	srv := &Server{
		cfg: Config{InvokeTimeout: 300 * time.Second},
		nowMonotonicMs: func() int64 { return nowMs },
	}
	payload := map[string]any{
		"time_envelope": env.MarshalForPayload(),
	}
	want := 60 * time.Second
	if got := srv.invokeTimeoutForPayload(payload); got != want {
		t.Fatalf("Case 1: invokeTimeoutForPayload = %v, want %v (active+lease, not stall-capped)", got, want)
	}

	// Case 2: active=60s, lease=5s, stall=30s → want 5s (lease is the tighter bound).
	env2, ok2 := routedrun.TimeEnvelopeFromCeilings(60_000, 5_000, 30_000, 30_000)
	if !ok2 {
		t.Fatal("expected envelope case 2")
	}
	payload2 := map[string]any{
		"time_envelope": env2.MarshalForPayload(),
	}
	want2 := 5 * time.Second
	if got2 := srv.invokeTimeoutForPayload(payload2); got2 != want2 {
		t.Fatalf("Case 2: invokeTimeoutForPayload = %v, want %v (lease-bound)", got2, want2)
	}
}

// TestB30T03PartB_HarnessInvokeTimeout_LegacyFallback300s verifies the v0.2.3
// compat path: with no TimeEnvelope in the payload, the timeout falls back
// to the configured InvokeTimeout (legacy 300s default).
func TestB30T03PartB_HarnessInvokeTimeout_LegacyFallback300s(t *testing.T) {
	srv := &Server{cfg: Config{InvokeTimeout: 300 * time.Second}}
	// No time_envelope in payload → legacy fallback.
	got := srv.invokeTimeoutForPayload(map[string]any{"run_id": "r"})
	want := 300 * time.Second
	if got != want {
		t.Fatalf("invokeTimeoutForPayload = %v, want %v (legacy fallback)", got, want)
	}
	// Empty payload → legacy fallback.
	if got := srv.invokeTimeoutForPayload(map[string]any{}); got != want {
		t.Fatalf("empty payload: got %v, want %v", got, want)
	}
	// nil payload → legacy fallback.
	if got := srv.invokeTimeoutForPayload(nil); got != want {
		t.Fatalf("nil payload: got %v, want %v", got, want)
	}
}

// TestB30T03PartB_HarnessInvokeTimeout_EnvelopeExhaustedClampsLow verifies
// that an exhausted envelope yields a tiny timeout rather than the legacy
// 300s — the invoke cannot exceed remaining active time.
func TestB30T03PartB_HarnessInvokeTimeout_EnvelopeExhaustedClampsLow(t *testing.T) {
	env, ok := routedrun.TimeEnvelopeFromCeilings(10_000, 10_000, 30_000, 30_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	env.ConsumedActiveDurationMs = 10_000
	nowMs := routedrun.NowMonotonicMs(nil)
	srv := &Server{
		cfg:            Config{InvokeTimeout: 300 * time.Second},
		nowMonotonicMs: func() int64 { return nowMs },
	}
	payload := map[string]any{
		"time_envelope": env.MarshalForPayload(),
	}
	got := srv.invokeTimeoutForPayload(payload)
	if got <= 0 || got > 1*time.Millisecond {
		t.Fatalf("invokeTimeoutForPayload = %v, want (0,1ms] (exhausted envelope)", got)
	}
}
