package harness

import (
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB30T03PartB_HarnessBudget_DerivedFromTimeEnvelope verifies ceiling 4:
// when a TimeEnvelope is attached to the budget config, the wall-clock budget
// is derived from the envelope's ActiveTimeRemainingMs, not the legacy 120s
// default constant.
func TestB30T03PartB_HarnessBudget_DerivedFromTimeEnvelope(t *testing.T) {
	// max=30000ms, consumed=10000ms → remaining=20000ms.
	env, ok := routedrun.TimeEnvelopeFromCeilings(30_000, 60_000, 120_000, 120_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	env.ConsumedActiveDurationMs = 10_000
	nowMs := routedrun.NowMonotonicMs(nil)

	cfg := BudgetConfig{
		MaxTokens:    10000,
		MaxIterations: 1000,
		TimeEnvelope: &env,
		NowMonotonicMs: func() int64 { return nowMs },
	}
	enforcer := newBudgetEnforcer(cfg, "run-env", "inv-env", nil, time.Now)

	want := time.Duration(20_000) * time.Millisecond
	if got := enforcer.WallClockBudget(); got != want {
		t.Fatalf("WallClockBudget = %v, want %v (envelope remaining)", got, want)
	}
	if got := enforcer.WallClockBudgetMs(); got != 20_000 {
		t.Errorf("WallClockBudgetMs = %d, want 20000", got)
	}
}

// TestB30T03PartB_HarnessBudget_LegacyFallback120s verifies the v0.2.3 compat
// path: with no TimeEnvelope and no explicit WallClockSeconds, the budget
// falls back to the legacy 120s defaultWallClockBudget.
func TestB30T03PartB_HarnessBudget_LegacyFallback120s(t *testing.T) {
	enforcer := newBudgetEnforcer(BudgetConfig{MaxTokens: 10000}, "run-leg", "inv-leg", nil, time.Now)
	want := 120 * time.Second
	if got := enforcer.WallClockBudget(); got != want {
		t.Fatalf("WallClockBudget = %v, want %v (legacy fallback)", got, want)
	}
}

// TestB30T03PartB_HarnessBudget_ExplicitOverrideBeatsEnvelope verifies the
// policy override semantics: BudgetConfig.WallClockSeconds (explicit policy
// override) wins over both the envelope default and the legacy fallback.
func TestB30T03PartB_HarnessBudget_ExplicitOverrideBeatsEnvelope(t *testing.T) {
	env, ok := routedrun.TimeEnvelopeFromCeilings(30_000, 60_000, 120_000, 120_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	cfg := BudgetConfig{
		WallClockSeconds: 45,
		MaxTokens:        10000,
		TimeEnvelope:     &env,
	}
	enforcer := newBudgetEnforcer(cfg, "run-ovr", "inv-ovr", nil, time.Now)
	want := 45 * time.Second
	if got := enforcer.WallClockBudget(); got != want {
		t.Fatalf("WallClockBudget = %v, want %v (explicit override)", got, want)
	}
}

// TestB30T03PartB_HarnessBudget_ExpiredEnvelopeClampsToZero verifies that a
// TimeEnvelope whose active time is exhausted yields a zero wall-clock budget
// (the run has no remaining active time).
func TestB30T03PartB_HarnessBudget_ExpiredEnvelopeClampsToZero(t *testing.T) {
	env, ok := routedrun.TimeEnvelopeFromCeilings(10_000, 60_000, 120_000, 120_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	// consumed == max → remaining = 0.
	env.ConsumedActiveDurationMs = 10_000
	nowMs := routedrun.NowMonotonicMs(nil)

	cfg := BudgetConfig{
		MaxTokens:    10000,
		TimeEnvelope: &env,
		NowMonotonicMs: func() int64 { return nowMs },
	}
	enforcer := newBudgetEnforcer(cfg, "run-exp", "inv-exp", nil, time.Now)
	if got := enforcer.WallClockBudget(); got != 0 {
		t.Fatalf("WallClockBudget = %v, want 0 (exhausted envelope)", got)
	}
}
