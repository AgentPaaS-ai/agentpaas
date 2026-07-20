package harness

import (
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB30T03PartB_HarnessInvokeTimeout_DerivedFromTimeEnvelope verifies
// ceiling 3: when the invoke payload carries a TimeEnvelope, the harness
// /invoke context timeout is derived from EffectiveOperationDeadlineMs(
// nowMs, env.StallTimeoutMs), not the legacy 300s default.
func TestB30T03PartB_HarnessInvokeTimeout_DerivedFromTimeEnvelope(t *testing.T) {
	// StallTimeoutMs=30000, lease=60000, active=60000 → min=30000 (30s).
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
	want := 30 * time.Second
	if got := srv.invokeTimeoutForPayload(payload); got != want {
		t.Fatalf("invokeTimeoutForPayload = %v, want %v (envelope-derived)", got, want)
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
