package daemon

import (
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB30T03PartB_DaemonInvokeTimeout_DerivedFromTimeEnvelope verifies
// that when a trackedRun carries a TimeEnvelope (set by the durable
// admission path), the daemon's invoke-context timeout is derived from
// the envelope's active-time remaining and attempt-lease remaining only
// — NOT the per-operation StallTimeoutMs, which is a harness-level
// concern. This ensures the daemon's auto-invoke goroutine survives for
// the full session duration, not just one operation.
//
// This directly exercises the compute function used by the invoke goroutine
// (control_handlers.go invokeContextTimeout). The full Run flow requires
// Docker; the timeout derivation is the behavior under test.
func TestB30T03PartB_DaemonInvokeTimeout_DerivedFromTimeEnvelope(t *testing.T) {
	// active=60000, lease=60000 → effective = min(60000,60000) = 60000ms (60s).
	// StallTimeoutMs=30000 is NOT used — it's a per-operation harness concern.
	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 30_000, 30_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	s := newTestControlServer(t)
	tr := &trackedRun{TimeEnvelope: &env}
	got := s.invokeContextTimeout(tr)
	want := 60 * time.Second
	if got != want {
		t.Fatalf("invokeContextTimeout = %v, want %v (active-remaining-derived, not stall-capped)", got, want)
	}

	// lease < active: lease=5000, active=60000 → min(60000,5000) = 5000ms.
	env2, ok2 := routedrun.TimeEnvelopeFromCeilings(60_000, 5_000, 30_000, 30_000)
	if !ok2 {
		t.Fatal("expected envelope")
	}
	tr2 := &trackedRun{TimeEnvelope: &env2}
	got2 := s.invokeContextTimeout(tr2)
	want2 := 5 * time.Second
	if got2 != want2 {
		t.Fatalf("invokeContextTimeout (lease-bound) = %v, want %v", got2, want2)
	}

	// active < lease: active=10000 consumed=8000 remaining=2000, lease=60000 → min=2000ms.
	env3, ok3 := routedrun.TimeEnvelopeFromCeilings(10_000, 60_000, 30_000, 30_000)
	if !ok3 {
		t.Fatal("expected envelope")
	}
	env3.ConsumedActiveDurationMs = 8_000
	tr3 := &trackedRun{TimeEnvelope: &env3}
	got3 := s.invokeContextTimeout(tr3)
	want3 := 2 * time.Second
	if got3 != want3 {
		t.Fatalf("invokeContextTimeout (active-bound) = %v, want %v", got3, want3)
	}
}

// TestB30T03PartB_DaemonInvokeTimeout_LegacyFallback2Min verifies the v0.2.3
// compat path: with no TimeEnvelope on the trackedRun (legacy trigger path),
// the invoke-context timeout falls back to the legacy 2-minute constant.
func TestB30T03PartB_DaemonInvokeTimeout_LegacyFallback2Min(t *testing.T) {
	s := newTestControlServer(t)
	// No envelope on trackedRun → legacy fallback.
	tr := &trackedRun{}
	if got := s.invokeContextTimeout(tr); got != legacyInvokeContextTimeout {
		t.Fatalf("invokeContextTimeout = %v, want %v (legacy 2-min fallback)", got, legacyInvokeContextTimeout)
	}
	// nil trackedRun → legacy fallback.
	if got := s.invokeContextTimeout(nil); got != legacyInvokeContextTimeout {
		t.Fatalf("invokeContextTimeout(nil) = %v, want %v", got, legacyInvokeContextTimeout)
	}
	if legacyInvokeContextTimeout != 2*time.Minute {
		t.Fatalf("legacyInvokeContextTimeout = %v, want 2m", legacyInvokeContextTimeout)
	}
}

// TestB30T03PartB_DaemonInvokeTimeout_EnvelopeExhaustedClampsLow verifies
// that an exhausted envelope yields a tiny timeout rather than the legacy
// 2-min fallback — the invoke cannot exceed remaining active time.
func TestB30T03PartB_DaemonInvokeTimeout_EnvelopeExhaustedClampsLow(t *testing.T) {
	env, ok := routedrun.TimeEnvelopeFromCeilings(10_000, 10_000, 30_000, 30_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	env.ConsumedActiveDurationMs = 10_000
	s := newTestControlServer(t)
	tr := &trackedRun{TimeEnvelope: &env}
	got := s.invokeContextTimeout(tr)
	if got != 1*time.Millisecond {
		t.Fatalf("invokeContextTimeout = %v, want 1ms (exhausted envelope)", got)
	}
}

// TestB30T03PartB_SetRunTimeEnvelope_Seam verifies the durable-path seam:
// setRunTimeEnvelope attaches the envelope to an existing tracked run, and
// returns false when the run does not exist (the durable path must have
// started the run first).
func TestB30T03PartB_SetRunTimeEnvelope_Seam(t *testing.T) {
	s := newTestControlServer(t)
	// Track a run with no envelope.
	tr := &trackedRun{AgentName: "demo"}
	s.trackRunPtr("run-seam-1", tr)

	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 30_000, 30_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	if !s.setRunTimeEnvelope("run-seam-1", env) {
		t.Fatal("setRunTimeEnvelope returned false for existing run")
	}
	if tr.TimeEnvelope == nil {
		t.Fatal("expected envelope set on trackedRun")
	}
	if tr.TimeEnvelope.CurrentMaxActiveDurationMs != 60_000 {
		t.Errorf("envelope max = %d, want 60000", tr.TimeEnvelope.CurrentMaxActiveDurationMs)
	}
	// Unknown run → false.
	if s.setRunTimeEnvelope("run-nope", env) {
		t.Error("setRunTimeEnvelope returned true for unknown run")
	}
}

// TestB30T03PartB_LegacyPath_StillUses120sFallback verifies the end-to-end
// legacy compat contract: the daemon's payload for the legacy trigger path
// carries NO time_envelope (the harness falls back to its 120s / 300s
// documented legacy defaults). This proves backward compat for the v0.2.3
// synchronous path.
func TestB30T03PartB_LegacyPath_StillUses120sFallback(t *testing.T) {
	// The legacy trigger path passes nil TimeEnvelope through invokeAgent.
	// invokeContextTimeout(nil env) must yield the 2-min legacy fallback.
	s := newTestControlServer(t)
	tr := &trackedRun{TimeEnvelope: nil}
	if got := s.invokeContextTimeout(tr); got != legacyInvokeContextTimeout {
		t.Fatalf("legacy path invokeContextTimeout = %v, want %v", got, legacyInvokeContextTimeout)
	}
	// The harness legacy fallback constants are exercised in
	// b30_t03_partb_model_client_test.go and budget_test.go (120s) and
	// b30_t03_partb_invoke_timeout_test.go (300s). This test pins the daemon
	// side of the legacy contract.
}
