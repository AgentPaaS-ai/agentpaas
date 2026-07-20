package routedrun

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// TimeEnvelope wiring helpers (B30-T03 Part B)
// ---------------------------------------------------------------------------

// DefaultStallTimeoutMs is the default per-operation stall timeout when a
// receipt does not carry one. It is the ceiling-1 / ceiling-3 default the
// durable path applies for stall detection absent an explicit policy value.
const DefaultStallTimeoutMs = int64(120_000) // 2 minutes

// DefaultModelCallTimeoutMs is the default per-operation model-call timeout
// when a receipt does not carry one. It matches the v0.2.3 ceiling-5 HTTP
// client timeout.
const DefaultModelCallTimeoutMs = int64(120_000) // 120 seconds

// TimeEnvelopeFromReceipt builds a fresh TimeEnvelope from the initial
// ceilings carried on an InvocationReceipt. The envelope starts with zero
// consumed active time, no open segment, lifecycle/authority generation = 1,
// and cancellation generation = 0 (b30-summary.md:337-340).
//
// maxActiveMs comes from receipt.InitialMaxActiveDurationMs; attemptLeaseMs
// from receipt.InitialAttemptLeaseMs. stallTimeoutMs and modelCallTimeoutMs
// default to DefaultStallTimeoutMs / DefaultModelCallTimeoutMs when the
// receipt carries no explicit value (the receipt schema is the initial
// aggregate ceilings; per-operation timeouts are policy-derived defaults until
// B39 amendments land).
//
// When the receipt carries zero max-active (the legacy v0.2.3 trigger path
// never admitted), this returns (TimeEnvelope{}, false) so callers can fall
// back to the legacy constant.
func TimeEnvelopeFromReceipt(receipt *InvocationReceipt) (TimeEnvelope, bool) {
	if receipt == nil || receipt.InitialMaxActiveDurationMs <= 0 {
		return TimeEnvelope{}, false
	}
	maxActive := receipt.InitialMaxActiveDurationMs
	lease := receipt.InitialAttemptLeaseMs
	stall := DefaultStallTimeoutMs
	model := DefaultModelCallTimeoutMs
	return NewTimeEnvelope(maxActive, lease, stall, model), true
}

// TimeEnvelopeFromCeilings builds a TimeEnvelope directly from explicit
// ceilings. Returns (TimeEnvelope{}, false) when maxActiveMs is non-positive
// (the legacy fallback signal).
func TimeEnvelopeFromCeilings(maxActiveMs, attemptLeaseMs, stallTimeoutMs, modelCallTimeoutMs int64) (TimeEnvelope, bool) {
	if maxActiveMs <= 0 {
		return TimeEnvelope{}, false
	}
	stall := stallTimeoutMs
	if stall <= 0 {
		stall = DefaultStallTimeoutMs
	}
	model := modelCallTimeoutMs
	if model <= 0 {
		model = DefaultModelCallTimeoutMs
	}
	return NewTimeEnvelope(maxActiveMs, attemptLeaseMs, stall, model), true
}

// MarshalForPayload returns a map[string]any representation of the envelope
// suitable for embedding in an invoke payload under a reserved key. The
// schema mirrors the JSON tags on TimeEnvelope so the harness can unmarshal
// it back with UnmarshalFromPayload.
func (e TimeEnvelope) MarshalForPayload() map[string]any {
	raw, err := json.Marshal(e)
	if err != nil {
		// TimeEnvelope is a plain struct with JSON tags; marshal cannot fail
		// in practice. Fall back to an empty map so the payload stays valid.
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// UnmarshalTimeEnvelopeFromPayload extracts a TimeEnvelope embedded under the
// "time_envelope" key of an invoke payload. Returns (TimeEnvelope{}, false)
// when absent or invalid, signaling the caller to use the legacy fallback.
func UnmarshalTimeEnvelopeFromPayload(payload map[string]any) (TimeEnvelope, bool) {
	if payload == nil {
		return TimeEnvelope{}, false
	}
	raw, ok := payload["time_envelope"]
	if !ok || raw == nil {
		return TimeEnvelope{}, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return TimeEnvelope{}, false
	}
	var env TimeEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return TimeEnvelope{}, false
	}
	if env.CurrentMaxActiveDurationMs <= 0 {
		return TimeEnvelope{}, false
	}
	return env, true
}

// NowMonotonicMs returns the monotonic millisecond timestamp for the given
// clock, or time.Now().UnixMilli() when clock is nil. This is the convenience
// helper call sites use to feed EffectiveOperationDeadlineMs / ActiveTime-
// RemainingMs.
func NowMonotonicMs(clock Clock) int64 {
	if clock != nil {
		return clock.NowMonotonic().UnixMilli()
	}
	return time.Now().UnixMilli()
}
