package routedrun

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// A6.1 EffectiveOperationDeadlineMs = min(operation_timeout, lease, active)
// ---------------------------------------------------------------------------

func TestTimeEnvelope_EffectiveDeadline_MinOfThree(t *testing.T) {
	// operation_timeout is the smallest.
	leaseRemaining := int64(60_000)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		ConsumedActiveDurationMs:    0,
		AttemptLeaseRemainingMs:     &leaseRemaining,
		StallTimeoutMs:              10_000,
		ModelCallTimeoutMs:          20_000,
		LifecycleAuthorityGeneration: 1,
	}
	nowMs := int64(0)
	// operation_timeout (10_000) < lease (60_000) < active (300_000)
	if got := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs); got != 10_000 {
		t.Errorf("stall smallest: got %d, want 10_000", got)
	}

	// lease is smallest: lease=5_000 < stall=10_000 < active.
	smallLease := int64(5_000)
	env.AttemptLeaseRemainingMs = &smallLease
	if got := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs); got != 5_000 {
		t.Errorf("lease smallest: got %d, want 5_000", got)
	}

	// active is smallest: max=8_000 consumed=4_000 → remaining=4_000.
	env.CurrentMaxActiveDurationMs = 8_000
	env.ConsumedActiveDurationMs = 4_000
	if got := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs); got != 4_000 {
		t.Errorf("active smallest: got %d, want 4_000", got)
	}

	// Boundary minus-one: active remaining = 3_999 (consumed 4_001).
	env.ConsumedActiveDurationMs = 4_001
	if got := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs); got != 3_999 {
		t.Errorf("active minus-one: got %d, want 3_999", got)
	}

	// Boundary plus-one: active remaining = 4_001 (consumed 3_999).
	// min(4_001, 5_000, 10_000) = 4_001.
	env.ConsumedActiveDurationMs = 3_999
	if got := env.EffectiveOperationDeadlineMs(nowMs, env.StallTimeoutMs); got != 4_001 {
		t.Errorf("active plus-one: got %d, want 4_001 (min of 4_001,5_000,10_000)", got)
	}
}

// ---------------------------------------------------------------------------
// A6.2 ActiveTimeRemaining includes running segment elapsed
// ---------------------------------------------------------------------------

func TestTimeEnvelope_ActiveTimeRemaining_IncludesRunningSegment(t *testing.T) {
	start := int64(100_000)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		ConsumedActiveDurationMs:    50_000,
		RunningSegmentStartMs:       &start,
		LifecycleAuthorityGeneration: 1,
	}
	// now = 175_000 → elapsed = 75_000 → remaining = 300_000 - (50_000 + 75_000) = 175_000
	if got := env.ActiveTimeRemainingMs(175_000); got != 175_000 {
		t.Errorf("remaining with running segment: got %d, want 175_000", got)
	}

	// No running segment: remaining = max - consumed.
	env.RunningSegmentStartMs = nil
	if got := env.ActiveTimeRemainingMs(999_000); got != 250_000 {
		t.Errorf("remaining no segment: got %d, want 250_000", got)
	}
}

// ---------------------------------------------------------------------------
// A6.3 IsExpired when active time exhausted
// ---------------------------------------------------------------------------

func TestTimeEnvelope_IsExpired_WhenActiveTimeExhausted(t *testing.T) {
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  60_000,
		ConsumedActiveDurationMs:    60_000,
		LifecycleAuthorityGeneration: 1,
	}
	if !env.IsExpired(0) {
		t.Error("consumed == max should be expired")
	}
	env.ConsumedActiveDurationMs = 61_000
	if !env.IsExpired(0) {
		t.Error("consumed > max should be expired")
	}
}

// ---------------------------------------------------------------------------
// A6.4 IsExpired when lease expired
// ---------------------------------------------------------------------------

func TestTimeEnvelope_IsExpired_WhenLeaseExpired(t *testing.T) {
	zeroLease := int64(0)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		ConsumedActiveDurationMs:    0,
		AttemptLeaseRemainingMs:     &zeroLease,
		LifecycleAuthorityGeneration: 1,
	}
	if !env.IsExpired(0) {
		t.Error("lease == 0 should be expired")
	}
	negLease := int64(-1)
	env.AttemptLeaseRemainingMs = &negLease
	if !env.IsExpired(0) {
		t.Error("lease < 0 should be expired")
	}
	// Not expired when both healthy.
	posLease := int64(10_000)
	env.AttemptLeaseRemainingMs = &posLease
	if env.IsExpired(0) {
		t.Error("healthy envelope should not be expired")
	}
}

// ---------------------------------------------------------------------------
// A6.5 SystemClock.Now is UTC
// ---------------------------------------------------------------------------

func TestClock_SystemNowUTC(t *testing.T) {
	c := SystemClock{}
	now := c.Now()
	if now.Location() != time.UTC {
		t.Errorf("SystemClock.Now location = %v, want UTC", now.Location())
	}
	// NowMonotonic should be a real time (not zero).
	mono := c.NowMonotonic()
	if mono.IsZero() {
		t.Error("SystemClock.NowMonotonic should not be zero")
	}
}

// ---------------------------------------------------------------------------
// A6.6 FakeClock advance, no drift
// ---------------------------------------------------------------------------

func TestClock_FakeClockAdvance(t *testing.T) {
	c := NewFakeClock(time.Unix(1_000_000, 0).UTC())
	if got := c.Now(); got.Unix() != 1_000_000 {
		t.Errorf("initial now = %d, want 1_000_000", got.Unix())
	}
	c.AdvanceMonotonic(5 * time.Second)
	// Monotonic advanced by 5s.
	if got := c.NowMonotonic(); got.Unix() != 1_000_005 {
		t.Errorf("mono after 5s = %d, want 1_000_005", got.Unix())
	}
	// Wall clock is independent of monotonic advance (no drift in wall).
	// Wall only changes via SetWall.
	c.SetWall(time.Unix(2_000_000, 0).UTC())
	if got := c.Now(); got.Unix() != 2_000_000 {
		t.Errorf("wall after SetWall = %d, want 2_000_000", got.Unix())
	}
	// Monotonic unaffected by wall jump.
	if got := c.NowMonotonic(); got.Unix() != 1_000_005 {
		t.Errorf("mono after wall jump = %d, want 1_000_005 (no drift)", got.Unix())
	}
}

// ---------------------------------------------------------------------------
// A6.7 FakeClock timers fire in order
// ---------------------------------------------------------------------------

func TestClock_FakeClockTimersFireInOrder(t *testing.T) {
	c := NewFakeClock(time.Unix(0, 0).UTC())
	// Schedule timers at 100ms, 300ms, 200ms (out of order declaration).
	ch1 := c.After(100 * time.Millisecond)
	ch3 := c.After(300 * time.Millisecond)
	ch2 := c.After(200 * time.Millisecond)

	// Advance to 150ms: only ch1 fires.
	c.AdvanceMonotonic(150 * time.Millisecond)
	select {
	case <-ch1:
	default:
		t.Error("ch1 (100ms) should fire by 150ms")
	}
	if fired(ch2) || fired(ch3) {
		t.Error("ch2/ch3 should not fire by 150ms")
	}

	// Advance to 250ms: ch2 fires next.
	c.AdvanceMonotonic(100 * time.Millisecond)
	if !fired(ch2) {
		t.Error("ch2 (200ms) should fire by 250ms")
	}
	if fired(ch3) {
		t.Error("ch3 (300ms) should not fire by 250ms")
	}

	// Advance to 350ms: ch3 fires last.
	c.AdvanceMonotonic(100 * time.Millisecond)
	if !fired(ch3) {
		t.Error("ch3 (300ms) should fire by 350ms")
	}
}

func fired(ch <-chan time.Time) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// A6.8 Start then Close accrues elapsed
// ---------------------------------------------------------------------------

func TestActiveSegment_StartThenClose_AccruesElapsed(t *testing.T) {
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		LifecycleAuthorityGeneration: 1,
	}
	started, err := StartActiveSegment(env, 100)
	if err != nil {
		t.Fatalf("StartActiveSegment: %v", err)
	}
	if started.RunningSegmentStartMs == nil || *started.RunningSegmentStartMs != 100 {
		t.Fatalf("RunningSegmentStartMs = %v, want 100", started.RunningSegmentStartMs)
	}
	closed := CloseActiveSegment(started, 200)
	if closed.ConsumedActiveDurationMs != 100 {
		t.Errorf("ConsumedActiveDurationMs = %d, want 100", closed.ConsumedActiveDurationMs)
	}
	if closed.RunningSegmentStartMs != nil {
		t.Error("RunningSegmentStartMs should be nil after close")
	}
	// Immutability: original env unchanged.
	if env.ConsumedActiveDurationMs != 0 || env.RunningSegmentStartMs != nil {
		t.Error("original env was mutated")
	}
}

// ---------------------------------------------------------------------------
// A6.9 Close is idempotent
// ---------------------------------------------------------------------------

func TestActiveSegment_CloseIsIdempotent(t *testing.T) {
	start := int64(100)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		ConsumedActiveDurationMs:    0,
		RunningSegmentStartMs:       &start,
		LifecycleAuthorityGeneration: 1,
	}
	closed := CloseActiveSegment(env, 200) // +100
	closedAgain := CloseActiveSegment(closed, 300) // no-op
	if closedAgain.ConsumedActiveDurationMs != 100 {
		t.Errorf("double close ConsumedActiveDurationMs = %d, want 100 (no double-charge)",
			closedAgain.ConsumedActiveDurationMs)
	}
}

// ---------------------------------------------------------------------------
// A6.10 Freeze and Unfreeze: frozen interval not charged
// ---------------------------------------------------------------------------

func TestActiveSegment_FreezeAndUnfreeze(t *testing.T) {
	start := int64(100)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		ConsumedActiveDurationMs:    50,
		RunningSegmentStartMs:       &start,
		LifecycleAuthorityGeneration: 1,
	}
	// Freeze at t=200 (accrues 100 for the running segment).
	frozen := FreezeActiveSegment(env, 200)
	if frozen.ConsumedActiveDurationMs != 150 {
		t.Errorf("after freeze consumed = %d, want 150", frozen.ConsumedActiveDurationMs)
	}
	if frozen.FrozenConsumedMs != 150 {
		t.Errorf("FrozenConsumedMs = %d, want 150", frozen.FrozenConsumedMs)
	}
	if frozen.RunningSegmentStartMs != nil {
		t.Error("RunningSegmentStartMs should be nil while frozen")
	}
	// Advance wall clock — frozen interval must NOT accrue.
	unfrozen := UnfreezeActiveSegment(frozen, 1_000_000)
	if unfrozen.ConsumedActiveDurationMs != 150 {
		t.Errorf("after unfreeze consumed = %d, want 150 (frozen interval not charged)",
			unfrozen.ConsumedActiveDurationMs)
	}
	if unfrozen.FrozenConsumedMs != 0 {
		t.Errorf("FrozenConsumedMs = %d, want 0 after unfreeze", unfrozen.FrozenConsumedMs)
	}
}

// ---------------------------------------------------------------------------
// A6.11 Start on already-open is an error
// ---------------------------------------------------------------------------

func TestActiveSegment_StartOnAlreadyOpen_IsError(t *testing.T) {
	start := int64(100)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  300_000,
		RunningSegmentStartMs:       &start,
		LifecycleAuthorityGeneration: 1,
	}
	_, err := StartActiveSegment(env, 200)
	if err == nil {
		t.Error("StartActiveSegment on open segment should error")
	}
	if !errors.Is(err, ErrSegmentAlreadyOpen) {
		t.Errorf("err = %v, want ErrSegmentAlreadyOpen", err)
	}
}

// ---------------------------------------------------------------------------
// A6.12-16 Termination precedence
// ---------------------------------------------------------------------------

func TestTermination_Precedence_UserCancelWins(t *testing.T) {
	env := TimeEnvelope{LifecycleAuthorityGeneration: 1}
	events := []TerminationEvent{
		{Kind: TerminationActiveTimeExhausted, ObservedAt: time.Now()},
		{Kind: TerminationLeaseExpired, ObservedAt: time.Now()},
		{Kind: TerminationUserCancel, ObservedAt: time.Now()},
	}
	if got := ResolveTermination(env, events); got != TerminationUserCancel {
		t.Errorf("got %v, want TerminationUserCancel", got)
	}
}

func TestTermination_Precedence_ActiveTimeOverLease(t *testing.T) {
	env := TimeEnvelope{LifecycleAuthorityGeneration: 1}
	events := []TerminationEvent{
		{Kind: TerminationLeaseExpired, ObservedAt: time.Now()},
		{Kind: TerminationActiveTimeExhausted, ObservedAt: time.Now()},
	}
	if got := ResolveTermination(env, events); got != TerminationActiveTimeExhausted {
		t.Errorf("got %v, want TerminationActiveTimeExhausted", got)
	}
}

func TestTermination_Precedence_LeaseOverStall(t *testing.T) {
	env := TimeEnvelope{LifecycleAuthorityGeneration: 1}
	events := []TerminationEvent{
		{Kind: TerminationStall, ObservedAt: time.Now()},
		{Kind: TerminationLeaseExpired, ObservedAt: time.Now()},
	}
	if got := ResolveTermination(env, events); got != TerminationLeaseExpired {
		t.Errorf("got %v, want TerminationLeaseExpired", got)
	}
}

func TestTermination_Precedence_StallOverProcessFailure(t *testing.T) {
	env := TimeEnvelope{LifecycleAuthorityGeneration: 1}
	events := []TerminationEvent{
		{Kind: TerminationProcessFailure, ObservedAt: time.Now()},
		{Kind: TerminationStall, ObservedAt: time.Now()},
	}
	if got := ResolveTermination(env, events); got != TerminationStall {
		t.Errorf("got %v, want TerminationStall", got)
	}
}

func TestTermination_Precedence_AllFive(t *testing.T) {
	env := TimeEnvelope{LifecycleAuthorityGeneration: 1}
	now := time.Now()
	events := []TerminationEvent{
		{Kind: TerminationProcessFailure, ObservedAt: now},
		{Kind: TerminationStall, ObservedAt: now},
		{Kind: TerminationLeaseExpired, ObservedAt: now},
		{Kind: TerminationActiveTimeExhausted, ObservedAt: now},
		{Kind: TerminationUserCancel, ObservedAt: now},
	}
	if got := ResolveTermination(env, events); got != TerminationUserCancel {
		t.Errorf("all five: got %v, want TerminationUserCancel", got)
	}
}

// ---------------------------------------------------------------------------
// A6.17 AuthorityGeneration starts at 1
// ---------------------------------------------------------------------------

func TestTimeEnvelope_AuthorityGeneration_StartsAtOne(t *testing.T) {
	env := NewTimeEnvelope(60_000, 30_000, 5_000, 10_000)
	if env.LifecycleAuthorityGeneration != 1 {
		t.Errorf("LifecycleAuthorityGeneration = %d, want 1", env.LifecycleAuthorityGeneration)
	}
}

// ---------------------------------------------------------------------------
// A6.18 CancellationGeneration starts at 0
// ---------------------------------------------------------------------------

func TestTimeEnvelope_CancellationGeneration_StartsAtZero(t *testing.T) {
	env := NewTimeEnvelope(60_000, 30_000, 5_000, 10_000)
	if env.CancellationGeneration != 0 {
		t.Errorf("CancellationGeneration = %d, want 0", env.CancellationGeneration)
	}
}

// ---------------------------------------------------------------------------
// A6.19 JSON round trip
// ---------------------------------------------------------------------------

func TestTimeEnvelope_JSONRoundTrip(t *testing.T) {
	start := int64(123)
	lease := int64(456)
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  600_000,
		ConsumedActiveDurationMs:    1_000,
		RunningSegmentStartMs:       &start,
		AttemptLeaseRemainingMs:     &lease,
		StallTimeoutMs:              5_000,
		ModelCallTimeoutMs:          10_000,
		LifecycleAuthorityGeneration: 3,
		CancellationGeneration:       2,
		FrozenConsumedMs:             0,
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got TimeEnvelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Compare field-by-field (struct contains pointers; == compares addresses).
	if got.SchemaVersion != env.SchemaVersion ||
		got.CurrentMaxActiveDurationMs != env.CurrentMaxActiveDurationMs ||
		got.ConsumedActiveDurationMs != env.ConsumedActiveDurationMs ||
		got.StallTimeoutMs != env.StallTimeoutMs ||
		got.ModelCallTimeoutMs != env.ModelCallTimeoutMs ||
		got.LifecycleAuthorityGeneration != env.LifecycleAuthorityGeneration ||
		got.CancellationGeneration != env.CancellationGeneration ||
		got.FrozenConsumedMs != env.FrozenConsumedMs {
		t.Errorf("round-trip scalar mismatch:\n got  %+v\n want %+v", got, env)
	}
	if (got.RunningSegmentStartMs == nil) != (env.RunningSegmentStartMs == nil) {
		t.Fatalf("RunningSegmentStartMs nil mismatch: got %v want %v",
			got.RunningSegmentStartMs, env.RunningSegmentStartMs)
	}
	if got.RunningSegmentStartMs != nil && *got.RunningSegmentStartMs != *env.RunningSegmentStartMs {
		t.Errorf("RunningSegmentStartMs = %d, want %d",
			*got.RunningSegmentStartMs, *env.RunningSegmentStartMs)
	}
	if (got.AttemptLeaseRemainingMs == nil) != (env.AttemptLeaseRemainingMs == nil) {
		t.Fatalf("AttemptLeaseRemainingMs nil mismatch: got %v want %v",
			got.AttemptLeaseRemainingMs, env.AttemptLeaseRemainingMs)
	}
	if got.AttemptLeaseRemainingMs != nil && *got.AttemptLeaseRemainingMs != *env.AttemptLeaseRemainingMs {
		t.Errorf("AttemptLeaseRemainingMs = %d, want %d",
			*got.AttemptLeaseRemainingMs, *env.AttemptLeaseRemainingMs)
	}
}

// ---------------------------------------------------------------------------
// A7.20 Amended ceiling preserves consumed time
// ---------------------------------------------------------------------------

func TestTimeEnvelope_AmendedCeiling_PreservesConsumedTime(t *testing.T) {
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  60_000,
		ConsumedActiveDurationMs:    20_000,
		LifecycleAuthorityGeneration: 1,
	}
	amended := WithAmendedCeiling(env, 120_000, 2)
	if amended.CurrentMaxActiveDurationMs != 120_000 {
		t.Errorf("ceiling = %d, want 120_000", amended.CurrentMaxActiveDurationMs)
	}
	if amended.ConsumedActiveDurationMs != 20_000 {
		t.Errorf("consumed = %d, want 20_000 (no time lost on amendment)",
			amended.ConsumedActiveDurationMs)
	}
	if amended.LifecycleAuthorityGeneration != 2 {
		t.Errorf("generation = %d, want 2", amended.LifecycleAuthorityGeneration)
	}
	// Original unchanged.
	if env.CurrentMaxActiveDurationMs != 60_000 || env.LifecycleAuthorityGeneration != 1 {
		t.Error("original env was mutated")
	}
}

// ---------------------------------------------------------------------------
// A8.21 Long duration no overflow
// ---------------------------------------------------------------------------

func TestTimeEnvelope_LongDurationNoOverflow(t *testing.T) {
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  math.MaxInt64,
		ConsumedActiveDurationMs:    math.MaxInt64 - 1,
		LifecycleAuthorityGeneration: 1,
	}
	// remaining = MaxInt64 - (MaxInt64 - 1) = 1; must not panic or wrap negative.
	remaining := env.ActiveTimeRemainingMs(0)
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
	if remaining < 0 {
		t.Error("remaining wrapped negative")
	}
	// Even with a running segment that adds elapsed, subtraction must be safe.
	start := int64(0)
	env.RunningSegmentStartMs = &start
	// now=0 → elapsed=0 → remaining still 1.
	if got := env.ActiveTimeRemainingMs(0); got != 1 {
		t.Errorf("remaining with 0 elapsed = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// A9.22 Wall-clock jump backward does not affect monotonic duration
// ---------------------------------------------------------------------------

func TestTimeEnvelope_WallClockJumpDoesNotAffectMonotonicDuration(t *testing.T) {
	c := NewFakeClock(time.Unix(10_000_000, 0).UTC())
	// Start a segment using monotonic time.
	env := TimeEnvelope{
		SchemaVersion:               timeEnvelopeSchemaVersionV1,
		CurrentMaxActiveDurationMs:  600_000,
		LifecycleAuthorityGeneration: 1,
	}
	startMs := c.NowMonotonicUnixMs()
	started, err := StartActiveSegment(env, startMs)
	if err != nil {
		t.Fatalf("StartActiveSegment: %v", err)
	}
	// Advance monotonic clock by 100ms (real elapsed).
	c.AdvanceMonotonic(100 * time.Millisecond)
	// Jump wall clock BACKWARD by 1 hour (must not affect monotonic).
	c.SetWall(time.Unix(10_000_000-3600, 0).UTC())
	// Close using monotonic time, not wall.
	closeMs := c.NowMonotonicUnixMs()
	closed := CloseActiveSegment(started, closeMs)
	if closed.ConsumedActiveDurationMs != 100 {
		t.Errorf("consumed = %d, want 100 (monotonic, wall jump ignored)",
			closed.ConsumedActiveDurationMs)
	}
	if closed.ConsumedActiveDurationMs < 0 {
		t.Error("consumed went negative from wall-clock jump")
	}
}
