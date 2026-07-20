package routedrun

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Clock interface (B30-T03 A3)
// ---------------------------------------------------------------------------

// Clock is the injectable clock abstraction. Now() returns a UTC wall-clock
// time for evidence timestamps; NowMonotonic() returns a monotonic clock
// reading for duration decisions (immune to wall-clock jumps). The split
// lets tests inject a FakeClock with independent wall and monotonic axes
// (b30-summary.md:382: "Timezone/wall-clock jump does not change monotonic
// duration behavior").
type Clock interface {
	// Now returns the current wall-clock time in UTC.
	Now() time.Time
	// NowMonotonic returns a monotonic clock reading used for duration
	// decisions. On most platforms this is time.Now(); the interface
	// allows a fake clock in tests to keep the monotonic axis independent
	// of the wall axis.
	NowMonotonic() time.Time
}

// Timer is the injectable timer abstraction.
type Timer interface {
	// After fires once after d, sending the current time on the returned
	// channel.
	After(d time.Duration) <-chan time.Time
	// NewTimer returns a handle that can be stopped and reset.
	NewTimer(d time.Duration) TimerHandle
}

// TimerHandle is a stoppable, resettable timer.
type TimerHandle interface {
	// Stop stops the timer. It returns false if the timer has already
	// expired or been stopped.
	Stop() bool
	// Reset resets the timer to d. It returns false if the timer had
	// already expired or been stopped.
	Reset(d time.Duration) bool
	// C returns the channel the timer fires on.
	C() <-chan time.Time
}

// ---------------------------------------------------------------------------
// SystemClock / SystemTimer (production)
// ---------------------------------------------------------------------------

// SystemClock is the production Clock implementation using time.Now.
type SystemClock struct{}

// Now returns the current wall-clock time in UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// NowMonotonic returns the current monotonic reading. time.Now() carries a
// monotonic component on most platforms, so the same call suffices for
// duration arithmetic via Sub.
func (SystemClock) NowMonotonic() time.Time { return time.Now() }

// SystemTimer is the production Timer implementation using time.After /
// time.NewTimer.
type SystemTimer struct{}

// After fires once after d.
func (SystemTimer) After(d time.Duration) <-chan time.Time { return time.After(d) }

// NewTimer returns a handle backed by time.NewTimer.
func (SystemTimer) NewTimer(d time.Duration) TimerHandle {
	return systemTimerHandle{t: time.NewTimer(d)}
}

// systemTimerHandle wraps a *time.Timer so it satisfies TimerHandle.
type systemTimerHandle struct {
	t *time.Timer
}

func (h systemTimerHandle) Stop() bool                  { return h.t.Stop() }
func (h systemTimerHandle) Reset(d time.Duration) bool   { return h.t.Reset(d) }
func (h systemTimerHandle) C() <-chan time.Time          { return h.t.C }

// ---------------------------------------------------------------------------
// FakeClock (tests)
// ---------------------------------------------------------------------------

// FakeClock is a test clock with independent wall and monotonic axes and
// manual timer advancement. Now() returns the wall axis (settable via
// SetWall); NowMonotonic() returns the monotonic axis (advanceable via
// AdvanceMonotonic). Timers scheduled via After/NewTimer fire in
// declaration-independent order as the monotonic axis advances.
//
// The wall and monotonic axes are deliberately decoupled so that tests can
// simulate wall-clock jumps (forward or backward) without affecting duration
// arithmetic (b30-summary.md:382). This is what the T08 fake-clock 24h /
// 100-turn test relies on.
type FakeClock struct {
	mu      sync.Mutex
	wall    time.Time
	mono    time.Time
	timers  []*fakeTimer
	monoNs  int64 // cumulative monotonic nanoseconds for stable ordering
}

// NewFakeClock constructs a FakeClock whose wall and monotonic axes both
// start at initial. The two axes are independent thereafter: SetWall moves
// only the wall axis, AdvanceMonotonic moves only the monotonic axis.
func NewFakeClock(initial time.Time) *FakeClock {
	return &FakeClock{
		wall:  initial.UTC(),
		mono:  initial,
	}
}

// Now returns the wall axis (UTC).
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}

// NowMonotonic returns the monotonic axis.
func (c *FakeClock) NowMonotonic() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono
}

// NowMonotonicUnixMs returns the monotonic axis as a Unix millisecond
// timestamp, which is what TimeEnvelope segment accounting expects.
func (c *FakeClock) NowMonotonicUnixMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono.UnixMilli()
}

// SetWall jumps the wall axis to t (UTC-normalized). The monotonic axis is
// unaffected — this is the wall-clock-jump safety seam.
func (c *FakeClock) SetWall(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = t.UTC()
}

// AdvanceMonotonic advances the monotonic axis by d and fires any timers
// whose deadline has been reached, in deadline order. Timers fire exactly
// once. Negative d is a no-op (monotonic time does not move backward).
func (c *FakeClock) AdvanceMonotonic(d time.Duration) {
	if d < 0 {
		return
	}
	c.mu.Lock()
	c.mono = c.mono.Add(d)
	c.monoNs += int64(d)
	// Collect fired timers in deadline order.
	type pendingFire struct {
		idx int
		t   *fakeTimer
	}
	var fired []pendingFire
	for i, t := range c.timers {
		if t.fired {
			continue
		}
		if t.deadlineNs <= c.monoNs {
			fired = append(fired, pendingFire{i, t})
		}
	}
	// Sort by deadline ascending, then by schedule order (already stable
	// via slice index) for ties — guarantees "fire in order" semantics.
	sort.SliceStable(fired, func(i, j int) bool {
		return fired[i].t.deadlineNs < fired[j].t.deadlineNs
	})
	c.mu.Unlock()
	// Send outside the lock to avoid blocking a receiver that re-enters.
	for _, f := range fired {
		c.mu.Lock()
		if f.t.fired {
			c.mu.Unlock()
			continue
		}
		f.t.fired = true
		fireTime := c.mono
		c.mu.Unlock()
		select {
		case f.t.c <- fireTime:
		default:
		}
	}
}

// After schedules a one-shot timer that fires after d on the monotonic axis.
func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	h := c.NewTimer(d)
	return h.C()
}

// NewTimer schedules a one-shot timer handle on the monotonic axis.
func (c *FakeClock) NewTimer(d time.Duration) TimerHandle {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	deadline := c.monoNs + int64(d)
	ft := &fakeTimer{
		c:          ch,
		deadlineNs: deadline,
	}
	c.timers = append(c.timers, ft)
	return fakeTimerHandle{clock: c, t: ft}
}

// fakeTimer is an internal FakeClock timer entry.
type fakeTimer struct {
	c          chan time.Time
	deadlineNs int64
	fired      bool
	stopped    bool
}

// fakeTimerHandle wraps a fakeTimer to satisfy TimerHandle.
type fakeTimerHandle struct {
	clock *FakeClock
	t     *fakeTimer
}

func (h fakeTimerHandle) C() <-chan time.Time { return h.t.c }

func (h fakeTimerHandle) Stop() bool {
	h.clock.mu.Lock()
	defer h.clock.mu.Unlock()
	if h.t.fired || h.t.stopped {
		return false
	}
	h.t.stopped = true
	return true
}

func (h fakeTimerHandle) Reset(d time.Duration) bool {
	h.clock.mu.Lock()
	defer h.clock.mu.Unlock()
	wasActive := !h.t.fired && !h.t.stopped
	h.t.fired = false
	h.t.stopped = false
	h.t.deadlineNs = h.clock.monoNs + int64(d)
	return wasActive
}

// ---------------------------------------------------------------------------
// Active-time segment accounting (B30-T03 A4)
// ---------------------------------------------------------------------------

// ErrSegmentAlreadyOpen is returned by StartActiveSegment when a segment is
// already open. Per b30-summary.md:357-358 at most one segment may be open
// per workflow; a second start is rejected rather than silently closing the
// first, so callers must explicitly close before restarting.
var ErrSegmentAlreadyOpen = errors.New("time envelope: active segment already open")

// StartActiveSegment begins a new active segment at nowMs. It returns a NEW
// TimeEnvelope with RunningSegmentStartMs = nowMs. If a segment is already
// open it returns ErrSegmentAlreadyOpen without modifying the envelope.
//
// nowMs MUST be a monotonic millisecond timestamp (see Clock.NowMonotonic).
// Atomic with observed workflow state: RUNNING or PAUSE_REQUESTED accrue;
// PAUSED and NEEDS_REPLAN do not (b30-summary.md:353-359). The caller is
// responsible for only starting a segment in an accruing state.
func StartActiveSegment(env TimeEnvelope, nowMs int64) (TimeEnvelope, error) {
	if env.RunningSegmentStartMs != nil {
		return env, ErrSegmentAlreadyOpen
	}
	out := env
	start := nowMs
	out.RunningSegmentStartMs = &start
	return out, nil
}

// CloseActiveSegment closes an open segment and accrues the elapsed active
// time to ConsumedActiveDurationMs. It is idempotent: if no segment is open
// it returns the envelope unchanged (a daemon restart conservatively closes
// an interrupted active segment exactly once — b30-summary.md:355-356).
//
// nowMs MUST be a monotonic millisecond timestamp. If nowMs precedes the
// segment start (e.g. a backward monotonic jump, which should not happen
// but is defended against), the elapsed is clamped to 0 so consumed time
// never goes negative.
func CloseActiveSegment(env TimeEnvelope, nowMs int64) TimeEnvelope {
	if env.RunningSegmentStartMs == nil {
		return env
	}
	elapsed := nowMs - *env.RunningSegmentStartMs
	if elapsed < 0 {
		elapsed = 0
	}
	out := env
	// Overflow-safe addition.
	var newConsumed int64
	if out.ConsumedActiveDurationMs > maxInt64Minus(elapsed) {
		newConsumed = maxInt64
	} else {
		newConsumed = out.ConsumedActiveDurationMs + elapsed
	}
	out.ConsumedActiveDurationMs = newConsumed
	out.RunningSegmentStartMs = nil
	return out
}

// FreezeActiveSegment closes the open segment (accruing elapsed) AND records
// FrozenConsumedMs for PAUSED / NEEDS_REPLAN. While frozen the envelope does
// not accrue active time. Returns a NEW TimeEnvelope.
func FreezeActiveSegment(env TimeEnvelope, nowMs int64) TimeEnvelope {
	closed := CloseActiveSegment(env, nowMs)
	closed.FrozenConsumedMs = closed.ConsumedActiveDurationMs
	return closed
}

// UnfreezeActiveSegment clears the frozen state and allows a new segment to
// start. ConsumedActiveDurationMs is preserved (the frozen interval was not
// charged). Returns a NEW TimeEnvelope.
func UnfreezeActiveSegment(env TimeEnvelope, nowMs int64) TimeEnvelope {
	out := env
	out.FrozenConsumedMs = 0
	// nowMs is accepted for API symmetry; unfreezing does not itself start a
	// segment — the caller must StartActiveSegment to resume accrual.
	_ = nowMs // intentionally ignored (reviewed)
	return out
}

// ---------------------------------------------------------------------------
// Ceiling-update seam (B30-T03 A7 — reserved for B39)
// ---------------------------------------------------------------------------

// WithAmendedCeiling returns a NEW TimeEnvelope with the updated maximum
// active duration and lifecycle/authority generation, preserving
// ConsumedActiveDurationMs (no time lost on amendment). T03 reserves this
// seam but does NOT expose amendment behavior to callers — B39 owns it
// (b30-summary.md:371-373). The seam is atomic: the ceiling and generation
// advance together in a single immutable update.
func WithAmendedCeiling(env TimeEnvelope, newMaxActiveMs, newAuthorityGeneration int64) TimeEnvelope {
	out := env
	out.CurrentMaxActiveDurationMs = newMaxActiveMs
	out.LifecycleAuthorityGeneration = newAuthorityGeneration
	// ConsumedActiveDurationMs is deliberately preserved.
	return out
}

// ---------------------------------------------------------------------------
// Termination precedence (B30-T03 A5)
// ---------------------------------------------------------------------------

// TerminationReason is the deterministic reason a workflow terminated. The
// numeric values define the precedence order: lower numbers win when events
// coincide (b30-summary.md:360-366):
//
//  1. user cancellation
//  2. active-time exhaustion
//  3. lease expiry
//  4. stall
//  5. process/provider failure
type TerminationReason int

const (
	// TerminationUnknown is the zero value and must not be emitted.
	TerminationUnknown TerminationReason = iota
	TerminationUserCancel
	TerminationActiveTimeExhausted
	TerminationLeaseExpired
	TerminationStall
	TerminationProcessFailure
)

// String returns the stable name for a termination reason.
func (r TerminationReason) String() string {
	switch r {
	case TerminationUserCancel:
		return "USER_CANCEL"
	case TerminationActiveTimeExhausted:
		return "ACTIVE_TIME_EXHAUSTED"
	case TerminationLeaseExpired:
		return "LEASE_EXPIRED"
	case TerminationStall:
		return "STALL"
	case TerminationProcessFailure:
		return "PROCESS_FAILURE"
	default:
		return "UNKNOWN"
	}
}

// TerminationEvent is a simultaneously-observed termination signal.
type TerminationEvent struct {
	Kind       TerminationReason
	ObservedAt time.Time
}

// ResolveTermination applies the deterministic precedence rules from
// b30-summary.md:360-366 to a set of simultaneously-observed events and
// returns the winner. It is a pure function — no side effects. If events is
// empty it returns TerminationUnknown. Ties between equal-precedence events
// (impossible by construction since each Kind is distinct) resolve to the
// earliest ObservedAt for stability.
func ResolveTermination(env TimeEnvelope, events []TerminationEvent) TerminationReason {
	if len(events) == 0 {
		return TerminationUnknown
	}
	best := TerminationUnknown
	var bestObserved time.Time
	first := true
	for _, ev := range events {
		if ev.Kind == TerminationUnknown {
			continue
		}
		if first {
			best = ev.Kind
			bestObserved = ev.ObservedAt
			first = false
			continue
		}
		if ev.Kind < best {
			best = ev.Kind
			bestObserved = ev.ObservedAt
		} else if ev.Kind == best && ev.ObservedAt.Before(bestObserved) {
			bestObserved = ev.ObservedAt
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// maxInt64Minus returns math.MaxInt64 - x, clamped to 0 if x > math.MaxInt64.
// Used for overflow-safe "is there room to add x to y without exceeding
// math.MaxInt64" checks without importing math in this file.
func maxInt64Minus(x int64) int64 {
	const maxInt64 = int64(1<<63 - 1)
	if x < 0 {
		return maxInt64
	}
	if x > maxInt64 {
		return 0
	}
	return maxInt64 - x
}

const maxInt64 = int64(1<<63 - 1)
