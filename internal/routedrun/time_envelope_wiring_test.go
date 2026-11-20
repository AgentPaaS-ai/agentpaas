package routedrun

import (
	"testing"
)

func TestTimeEnvelopeFromReceipt_BuildsFromInitialCeilings(t *testing.T) {
	receipt := &InvocationReceipt{
		InitialMaxActiveDurationMs: 30_000,
		InitialAttemptLeaseMs:      20_000,
	}
	env, ok := TimeEnvelopeFromReceipt(receipt)
	if !ok {
		t.Fatal("expected ok, got false")
	}
	if env.CurrentMaxActiveDurationMs != 30_000 {
		t.Errorf("max = %d, want 30000", env.CurrentMaxActiveDurationMs)
	}
	if env.AttemptLeaseRemainingMs == nil || *env.AttemptLeaseRemainingMs != 20_000 {
		t.Errorf("lease = %v, want 20000", env.AttemptLeaseRemainingMs)
	}
	if env.StallTimeoutMs != DefaultStallTimeoutMs {
		t.Errorf("stall default = %d, want %d", env.StallTimeoutMs, DefaultStallTimeoutMs)
	}
	if env.ModelCallTimeoutMs != DefaultModelCallTimeoutMs {
		t.Errorf("model default = %d, want %d", env.ModelCallTimeoutMs, DefaultModelCallTimeoutMs)
	}
	if env.LifecycleAuthorityGeneration != 1 {
		t.Errorf("authority gen = %d, want 1", env.LifecycleAuthorityGeneration)
	}
	if env.CancellationGeneration != 0 {
		t.Errorf("cancel gen = %d, want 0", env.CancellationGeneration)
	}
}

func TestTimeEnvelopeFromReceipt_LegacyNoCeilingsReturnsFalse(t *testing.T) {
	// Legacy v0.2.3 trigger path has no admission receipt.
	env, ok := TimeEnvelopeFromReceipt(nil)
	if ok {
		t.Fatalf("expected ok=false for nil receipt, got %v", env)
	}
	// Zero max-active (unadmitted) → also false.
	env, ok = TimeEnvelopeFromReceipt(&InvocationReceipt{})
	if ok {
		t.Fatalf("expected ok=false for zero ceilings, got %v", env)
	}
}

func TestTimeEnvelopeFromCeilings_DefaultsOpTimeouts(t *testing.T) {
	env, ok := TimeEnvelopeFromCeilings(60_000, 30_000, 0, 0)
	if !ok {
		t.Fatal("expected ok")
	}
	if env.StallTimeoutMs != DefaultStallTimeoutMs {
		t.Errorf("stall = %d, want default %d", env.StallTimeoutMs, DefaultStallTimeoutMs)
	}
	if env.ModelCallTimeoutMs != DefaultModelCallTimeoutMs {
		t.Errorf("model = %d, want default %d", env.ModelCallTimeoutMs, DefaultModelCallTimeoutMs)
	}
	// Zero max-active → legacy fallback signal.
	if _, ok := TimeEnvelopeFromCeilings(0, 0, 0, 0); ok {
		t.Error("expected ok=false for zero max active")
	}
}

func TestTimeEnvelopePayloadRoundTrip(t *testing.T) {
	env, ok := TimeEnvelopeFromCeilings(30_000, 20_000, 10_000, 5_000)
	if !ok {
		t.Fatal("expected ok")
	}
	payload := map[string]any{"time_envelope": env.MarshalForPayload()}
	got, ok := UnmarshalTimeEnvelopeFromPayload(payload)
	if !ok {
		t.Fatal("expected round-trip ok")
	}
	if got.CurrentMaxActiveDurationMs != 30_000 {
		t.Errorf("max = %d, want 30000", got.CurrentMaxActiveDurationMs)
	}
	if got.StallTimeoutMs != 10_000 {
		t.Errorf("stall = %d, want 10000", got.StallTimeoutMs)
	}
	if got.ModelCallTimeoutMs != 5_000 {
		t.Errorf("model = %d, want 5000", got.ModelCallTimeoutMs)
	}
}

func TestUnmarshalTimeEnvelopeFromPayload_LegacyAbsentReturnsFalse(t *testing.T) {
	if _, ok := UnmarshalTimeEnvelopeFromPayload(nil); ok {
		t.Error("nil payload should return false")
	}
	if _, ok := UnmarshalTimeEnvelopeFromPayload(map[string]any{}); ok {
		t.Error("empty payload should return false")
	}
	if _, ok := UnmarshalTimeEnvelopeFromPayload(map[string]any{"time_envelope": map[string]any{}}); ok {
		t.Error("zero-max envelope should return false (legacy fallback)")
	}
}
