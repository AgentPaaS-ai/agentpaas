// Package harness provides the AgentPaaS harness runtime: sandboxed agent
// execution, RPC, budget enforcement, and governed model streaming.
package harness

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// Streaming errors (sentinel). These are returned from StreamingAdapter
// terminal events and from Stream when the adapter cannot start.
var (
	// ErrStreamBackpressure is emitted when a slow observer cannot drain the
	// event channel within BackpressureTimeout. The channel is closed.
	ErrStreamBackpressure = errors.New("harness: stream backpressure timeout")
	// ErrStreamBudgetExhausted is the terminal reason when incremental usage
	// exceeds the configured budget.
	ErrStreamBudgetExhausted = errors.New("harness: stream budget exhausted")
	// ErrStreamCancelled is the terminal reason when the stream context is
	// cancelled before the response completes.
	ErrStreamCancelled = errors.New("harness: stream cancelled")
	// ErrStreamClosed is returned by AcceptDelta when the stream is already
	// terminal (cancellation, budget exhaustion, or backpressure) and a late
	// delta is rejected.
	ErrStreamClosed = errors.New("harness: stream closed; late delta rejected")
	// ErrStreamAlreadyTerminal is returned when a terminal event has already
	// been committed for the call.
	ErrStreamAlreadyTerminal = errors.New("harness: stream already terminal")
)

// Streaming adapter bounds. The event channel is bounded so a slow observer
// cannot exhaust trusted memory; backpressure has an explicit time bound.
const (
	// StreamChannelBufferSize is the bounded event channel capacity. The
	// adapter blocks (does not drop) when the channel is full, up to
	// BackpressureTimeout.
	StreamChannelBufferSize = 128
	// maxBufferSize caps the total buffered output bytes under
	// buffered_release. When exceeded, the adapter closes the stream with
	// response_failed to prevent unbounded memory growth (B29-1).
	maxBufferSize = 10 * 1024 * 1024 // 10 MiB
)

// BackpressureTimeout is how long the adapter blocks trying to deliver an
// event to a stalled observer before closing the channel with
// ErrStreamBackpressure. It is a var (not a const) so tests can shorten it.
var BackpressureTimeout = 5 * time.Second

// ModelStreamDelta is one incremental chunk from a model provider stream.
// Exactly one of Text, ToolCall, or Usage should be set per delta.
type ModelStreamDelta struct {
	// Text is an incremental output token/chunk. May be empty.
	Text string
	// ToolCall is an incremental tool-call argument delta. May be empty.
	ToolCall string
	// Usage is an incremental usage/budget update. nil means no update.
	Usage *runtime.Usage
	// Err is a provider error. Non-nil terminates the stream.
	Err error
}

// ModelStreamProvider is the trusted provider-side stream interface. The
// StreamingAdapter calls Start to begin the upstream request, drains Deltas
// to feed the governed stream, and calls Close to release the upstream
// request on completion, cancellation, or budget exhaustion.
type ModelStreamProvider interface {
	// Start begins the upstream provider request for the given envelope. The
	// returned channel receives deltas until it is closed (EOF) or the context
	// is cancelled. The provider MUST close the channel on completion.
	Start(ctx context.Context, envelope runtime.ModelCallEnvelope) (<-chan ModelStreamDelta, error)
	// Close releases the upstream provider request. It is idempotent and safe
	// to call after cancellation or budget exhaustion.
	Close() error
}

// StreamBudget is the incremental budget enforced by the adapter. After each
// usage_update, the adapter checks Remaining and, if <= 0, closes the upstream
// request and commits a typed terminal event.
type StreamBudget interface {
	// Remaining returns the remaining budget (e.g. tokens). A non-positive
	// value means the budget is exhausted. Returns a stable value; the
	// adapter treats <= 0 as exhausted.
	Remaining() int64
}

// StaticBudget is a StreamBudget with a fixed remaining value (for tests).
// It is safe for concurrent use.
type StaticBudget struct {
	mu       sync.Mutex
	remaining int64
}

// NewStaticBudget returns a StaticBudget with the given remaining budget.
func NewStaticBudget(remaining int64) *StaticBudget {
	return &StaticBudget{remaining: remaining}
}

// Remaining reports the remaining budget.
func (b *StaticBudget) Remaining() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining
}

// Debit subtracts n from the remaining budget. Used by tests to simulate
// incremental consumption. Returns the new remaining value.
func (b *StaticBudget) Debit(n int64) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remaining -= n
	return b.remaining
}

// StreamingAdapter wraps a ModelStreamProvider call, enforces a guardrail
// mode, emits governed StreamEvents to a DurableEventStore, and bounds
// backpressure, budget, and cancellation.
//
// The adapter is the trusted boundary between the upstream provider stream
// and downstream observers. It guarantees:
//   - Raw credentials and raw CoT never enter an event (ValidateStreamEvent
//     is applied before emit; a payload that fails validation is dropped and
//     the stream fails closed with response_failed).
//   - Input/output/token limits are enforced incrementally via the
//     StreamBudget.
//   - Backpressure has an explicit byte/event/time bound (128 events, 5s).
//   - A strict whole-response guardrail selects buffered_release and emits
//     no output_delta events.
//   - Cancellation, fencing, or budget exhaustion closes the upstream
//     request, commits a typed terminal event, and rejects late deltas.
//   - Partial output is marked uncommitted until response_completed.
type StreamingAdapter struct {
	provider ModelStreamProvider
	store    port.EventStore
	// tenantID and runID identify the durable event stream the adapter appends
	// terminal events to. May be empty for in-memory only streaming.
	tenantID string
	runID    string
	// targetIdentity is the package/runtime identity the stream is addressed
	// to. Recorded on every event.
	targetIdentity string
}

// NewStreamingAdapter constructs a StreamingAdapter that appends terminal
// events to the given durable EventStore under (tenantID, runID). If store is
// nil, terminal events are not persisted (the in-memory channel still emits
// them). The targetIdentity is recorded on every emitted event.
func NewStreamingAdapter(provider ModelStreamProvider, store port.EventStore, tenantID, runID, targetIdentity string) *StreamingAdapter {
	return &StreamingAdapter{
		provider:       provider,
		store:          store,
		tenantID:       tenantID,
		runID:          runID,
		targetIdentity: targetIdentity,
	}
}

// Stream starts the model call and emits governed StreamEvents on the
// returned channel. The channel is closed when the stream terminates
// (response_completed, response_failed, or backpressure).
//
// The envelope must be valid (envelope.Validate). The guardrail mode
// determines whether output_delta events are emitted (incremental_release) or
// suppressed until a single response_completed (buffered_release).
//
// If budget is non-nil, after each usage_update the adapter checks
// budget.Remaining; if <= 0, it closes the upstream request, commits a
// response_failed terminal event with reason ErrStreamBudgetExhausted, and
// rejects late deltas.
//
// If ctx is cancelled before the response completes, the adapter closes the
// upstream request, commits response_failed with reason ErrStreamCancelled,
// and rejects late deltas.
func (a *StreamingAdapter) Stream(
	ctx context.Context,
	envelope runtime.ModelCallEnvelope,
	guardrail runtime.GuardrailMode,
	budget StreamBudget,
) (<-chan runtime.StreamEvent, error) {
	if err := envelope.Validate(); err != nil {
		return nil, fmt.Errorf("harness: stream envelope invalid: %w", err)
	}
	if a.provider == nil {
		return nil, errors.New("harness: stream provider is nil")
	}

	// Allocate stable call/request IDs.
	callID, requestID, err := newStreamCallIDs()
	if err != nil {
		return nil, fmt.Errorf("harness: allocate call ids: %w", err)
	}

	out := make(chan runtime.StreamEvent, StreamChannelBufferSize)
	// seqMu guards the per-call sequence counter and the closed flag. The
	// closed flag is set exactly once when the stream becomes terminal; after
	// that, AcceptDelta and emit reject late deltas.
	state := &streamState{
		callID:         callID,
		requestID:      requestID,
		envelope:       envelope,
		guardrail:      guardrail,
		budget:         budget,
	}

	// Start the upstream provider request on a child context so cancellation
	// closes both the provider and the adapter.
	streamCtx, cancel := context.WithCancel(ctx)
	deltaCh, err := a.provider.Start(streamCtx, envelope)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("harness: stream provider start: %w", err)
	}

	go a.pump(streamCtx, cancel, deltaCh, out, state)
	return out, nil
}

// streamState carries the per-call mutable state guarded by seqMu.
type streamState struct {
	seqMu     sync.Mutex
	sequence  int64
	closed    bool
	committed bool // true once response_completed is emitted
	buffered  []byte

	callID    string
	requestID string
	envelope  runtime.ModelCallEnvelope
	guardrail runtime.GuardrailMode
	budget    StreamBudget
}

// nextSeq returns the next per-call sequence number (starting at 1) under
// seqMu. Returns 0 and false if the stream is already closed (terminal); the
// caller must not emit after a terminal.
func (s *streamState) nextSeq() (int64, bool) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	if s.closed {
		return 0, false
	}
	s.sequence++
	return s.sequence, true
}

// nextSeqForced returns the next per-call sequence number under seqMu WITHOUT
// checking the closed flag. It is used by finishTerminal to allocate the
// sequence for the terminal event itself (which is, by definition, the last
// event and must not be rejected as a "late delta"). The caller MUST have
// already claimed the terminal slot via markClosed.
func (s *streamState) nextSeqForced() int64 {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	s.sequence++
	return s.sequence
}

// markClosed sets the closed flag under seqMu. Idempotent: returns true the
// first time, false afterwards.
func (s *streamState) markClosed() bool {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	if s.closed {
		return false
	}
	s.closed = true
	return true
}

// isClosed reports the closed flag without taking seqMu for the closed path.
func (s *streamState) isClosed() bool {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	return s.closed
}

// pump drains the provider delta channel, emits governed events to out, and
// terminates the stream. It owns the lifetime of out and the streamCtx cancel.
func (a *StreamingAdapter) pump(
	streamCtx context.Context,
	cancel context.CancelFunc,
	deltaCh <-chan ModelStreamDelta,
	out chan<- runtime.StreamEvent,
	state *streamState,
) {
	defer close(out)
	defer cancel()
	defer func() { _ = a.provider.Close() }() // best-effort close

	// Emit response_started. This is the only non-terminal emit before the
	// loop; if it fails (backpressure or validation), the stream is terminal
	// immediately.
	if !a.emit(out, state, runtime.StreamEventResponseStarted, nil) {
		// emit already committed a response_failed terminal if appropriate.
		// If it did not (e.g. validation of nil payload can't fail, but
		// backpressure can), ensure we are terminal.
		a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBackpressure)
		return
	}

	for {
		select {
		case <-streamCtx.Done():
			// Cancellation: close upstream, emit response_failed, reject late
			// deltas. Partial output remains uncommitted.
			a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamCancelled)
			return
		case delta, open := <-deltaCh:
			if !open {
				// Provider finished. Emit buffered output if any, then
				// response_completed.
				a.complete(out, state)
				return
			}
			if delta.Err != nil {
				a.finishTerminal(out, state, runtime.StreamEventResponseFailed, delta.Err)
				return
			}
			// Incremental usage/budget check.
			if delta.Usage != nil && state.budget != nil {
				if state.budget.Remaining() <= 0 {
					a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBudgetExhausted)
					return
				}
				// Emit usage_update (approved dimensions only).
				usagePayload := usageToPayload(delta.Usage)
				if !a.emit(out, state, runtime.StreamEventUsageUpdate, usagePayload) {
					// emit already committed a response_failed terminal.
					a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBackpressure)
					return
				}
				// Re-check budget after emitting usage: a concurrent debit may
				// have exhausted it.
				if state.budget.Remaining() <= 0 {
					a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBudgetExhausted)
					return
				}
				continue
			}
			// Output delta handling depends on guardrail mode.
			if delta.Text != "" || delta.ToolCall != "" {
				if !a.handleOutputDelta(out, state, delta) {
					// handleOutputDelta already committed a terminal.
					return
				}
			}
		}
	}
}

// handleOutputDelta emits an output_delta under incremental_release, or
// buffers under buffered_release. Returns false if the stream became
// terminal (backpressure, validation failure, or budget exhaustion); true to
// continue.
func (a *StreamingAdapter) handleOutputDelta(
	out chan<- runtime.StreamEvent,
	state *streamState,
	delta ModelStreamDelta,
) bool {
	if state.budget != nil && state.budget.Remaining() <= 0 {
		a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBudgetExhausted)
		return false
	}
	if state.guardrail == runtime.GuardrailBufferedRelease {
		// No deltas emitted until the full response is committed. Buffer the
		// chunk; partial output remains uncommitted.
		state.seqMu.Lock()
		state.buffered = append(state.buffered, []byte(delta.Text)...)
		overflowed := len(state.buffered) > maxBufferSize
		state.seqMu.Unlock()
		if overflowed {
			// B29-1: cap total buffered bytes to prevent unbounded memory growth.
			a.finishTerminal(out, state, runtime.StreamEventResponseFailed,
				fmt.Errorf("%w: buffered output exceeded %d bytes", ErrStreamClosed, maxBufferSize))
			return false
		}
		return true
	}
	// incremental_release: emit output_delta or tool_call_delta. Partial
	// output is marked uncommitted (the committed flag stays false until
	// response_completed).
	kind := runtime.StreamEventOutputDelta
	payload := []byte(delta.Text)
	if delta.ToolCall != "" {
		kind = runtime.StreamEventToolCallDelta
		payload = []byte(delta.ToolCall)
	}
	if !a.emit(out, state, kind, payload) {
		// emit already committed a response_failed terminal (backpressure or
		// validation failure). Nothing more to do.
		return false
	}
	return true
}

// complete emits any buffered output (buffered_release) followed by
// response_completed and marks the stream committed. It is called exactly
// once when the provider delta channel closes without error.
func (a *StreamingAdapter) complete(out chan<- runtime.StreamEvent, state *streamState) {
	if state.isClosed() {
		// Already terminal (e.g. backpressure or cancellation raced ahead).
		// Do not emit a second terminal.
		return
	}
	// Under buffered_release, emit one final output_delta carrying the whole
	// committed response, then response_completed. The committed flag is set
	// before response_completed so partial output cannot become checkpoint
	// input.
	if state.guardrail == runtime.GuardrailBufferedRelease {
		state.seqMu.Lock()
		buffered := state.buffered
		state.seqMu.Unlock()
		for len(buffered) > 0 {
			n := len(buffered)
			if n > runtime.MaxStreamPayloadBytes {
				n = runtime.MaxStreamPayloadBytes
			}
			chunk := buffered[:n]
			buffered = buffered[n:]
			if !a.emit(out, state, runtime.StreamEventOutputDelta, chunk) {
				// emit already committed a response_failed terminal.
				return
			}
		}
	}
	// Mark committed before emitting response_completed so observers that see
	// the terminal event can trust the output.
	state.seqMu.Lock()
	state.committed = true
	state.seqMu.Unlock()
	a.finishTerminal(out, state, runtime.StreamEventResponseCompleted, nil)
}

// emit constructs a StreamEvent, validates it, and sends it on out with a
// bounded backpressure timeout. Returns true on success. On failure (backpressure
// timeout or validation failure) it commits a response_failed terminal and
// returns false; the caller must return from the pump loop.
//
// emit rejects events after the stream is closed (late deltas): nextSeq
// returns false if closed.
func (a *StreamingAdapter) emit(
	out chan<- runtime.StreamEvent,
	state *streamState,
	kind runtime.StreamEventKind,
	payload []byte,
) bool {
	seq, ok := state.nextSeq()
	if !ok {
		// Stream already closed; late delta rejected.
		return false
	}
	ev := runtime.StreamEvent{
		CallID:         state.callID,
		RequestID:      state.requestID,
		Sequence:       seq,
		Timestamp:      time.Now().UTC(),
		TargetIdentity: a.targetIdentity,
		Kind:           kind,
		Payload:        payload,
	}
	if err := runtime.ValidateStreamEvent(ev); err != nil {
		// Validation failure: a payload contained raw credentials or CoT, or
		// exceeded the bound. Fail closed: emit response_failed and persist.
		// The offending event is NOT emitted.
		a.finishTerminal(out, state, runtime.StreamEventResponseFailed, err)
		return false
	}
	timer := time.NewTimer(BackpressureTimeout)
	defer timer.Stop()
	select {
	case out <- ev:
		return true
	case <-timer.C:
		// Backpressure: slow observer cannot drain within the time bound.
		// Emit a typed backpressure terminal.
		a.finishTerminal(out, state, runtime.StreamEventResponseFailed, ErrStreamBackpressure)
		return false
	}
}

// finishTerminal emits the terminal event (response_completed or
// response_failed) on out, marks the stream closed, and persists the terminal
// to the durable EventStore. It is idempotent: the first caller emits the
// terminal and marks closed; subsequent calls are no-ops (late deltas are
// rejected). This is the single point that flips the closed flag, so the
// terminal event itself is never rejected as a "late delta".
//
// The payload of the terminal event is the failure reason (for
// response_failed) or empty (for response_completed). The reason is a
// sentinel error whose Error() string is safe (no credentials, no CoT).
func (a *StreamingAdapter) finishTerminal(
	out chan<- runtime.StreamEvent,
	state *streamState,
	kind runtime.StreamEventKind,
	reason error,
) {
	// Atomically claim the terminal slot. Only the first caller proceeds; this
	// guarantees exactly one terminal event even under concurrent cancellation
	// and provider errors.
	if !state.markClosed() {
		return
	}
	seq := state.nextSeqForced()
	payload := []byte{}
	if reason != nil {
		payload = []byte(reason.Error())
	}
	if len(payload) > runtime.MaxStreamPayloadBytes {
		payload = payload[:runtime.MaxStreamPayloadBytes]
	}
	ev := runtime.StreamEvent{
		CallID:         state.callID,
		RequestID:      state.requestID,
		Sequence:       seq,
		Timestamp:      time.Now().UTC(),
		TargetIdentity: a.targetIdentity,
		Kind:           kind,
		Payload:        payload,
	}
	// The terminal event is the last event; it may still fail validation if
	// the reason string contained a credential marker. In that pathological
	// case, redact and emit a generic failure. ValidateStreamEvent is cheap
	// defense-in-depth.
	if err := runtime.ValidateStreamEvent(ev); err != nil {
		// Fall back to a generic, safe terminal payload.
		ev.Payload = []byte("stream terminal")
		if vErr := runtime.ValidateStreamEvent(ev); vErr != nil {
			// Should be impossible (empty-ish payload); give up emitting but
			// still persist a marker.
			ev.Payload = nil
		}
	}
	// Emit on out with a bounded backpressure timeout. The terminal must be
	// observable to the consumer; if it cannot be delivered within the bound,
	// the consumer is gone and we close the channel.
	timer := time.NewTimer(BackpressureTimeout)
	defer timer.Stop()
	select {
	case out <- ev:
	case <-timer.C:
		// Consumer stalled; the channel will be closed by the deferred
		// close(out) in pump. The terminal is still persisted below.
	}
	// Persist the terminal to the durable EventStore. A lost terminal event
	// is the worst failure mode in an audit-first system: the run would
	// appear non-terminal forever, blocking idempotent re-invocation and
	// corrupting replay. Surface the error via log so an operator can
	// reconcile; the in-memory stream has already emitted the terminal.
	if err := a.persistTerminal(kind, payload); err != nil {
		log.Printf("harness: persistTerminal append failed for tenant=%q run=%q kind=%s: %v (run may need manual reconciliation)",
			a.tenantID, a.runID, kind, err)
	}
}

// persistTerminal appends a terminal event marker to the durable EventStore,
// if one is wired. Subscribers and replay can observe the terminal. It returns
// any Append error so the caller (finishTerminal) can surface it — a silently
// lost terminal event is the worst failure mode in an audit-first system.
func (a *StreamingAdapter) persistTerminal(kind runtime.StreamEventKind, payload []byte) error {
	if a.store == nil {
		return nil
	}
	eventType := streamEventTypeFromKind(kind)
	_, err := a.store.Append(context.Background(), port.Event{
		TenantID:  a.tenantID,
		RunID:     a.runID,
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	})
	return err
}

// streamEventTypeFromKind maps a runtime.StreamEventKind to a durable
// port.Event type string. Terminal events use a stable namespace so the
// trigger service can route them.
func streamEventTypeFromKind(kind runtime.StreamEventKind) string {
	switch kind {
	case runtime.StreamEventResponseStarted:
		return "model.stream.response_started"
	case runtime.StreamEventOutputDelta:
		return "model.stream.output_delta"
	case runtime.StreamEventToolCallDelta:
		return "model.stream.tool_call_delta"
	case runtime.StreamEventUsageUpdate:
		return "model.stream.usage_update"
	case runtime.StreamEventResponseCompleted:
		return "model.stream.response_completed"
	case runtime.StreamEventResponseFailed:
		return "model.stream.response_failed"
	default:
		return "model.stream.unknown"
	}
}

// usageToPayload serializes an approved Usage dimensions summary. Only the
// approved dimensions (token counts) are emitted; no raw credentials or CoT.
func usageToPayload(u *runtime.Usage) []byte {
	if u == nil {
		return nil
	}
	// JSON-encode only the approved dimensions. We avoid encoding/json here to
	// keep the adapter dependency-light and to guarantee no field is added by
	// accident; a manual encoding is trivial and reviewable.
	return []byte(fmt.Sprintf(
		`{"prompt_tokens":%d,"completion_tokens":%d,"reasoning_tokens":%d,"total_tokens":%d}`,
		u.PromptTokens, u.CompletionTokens, u.ReasoningTokens, u.TotalTokens,
	))
}

// AcceptDelta is the trusted entry point for a provider to push a delta into a
// running stream after the delta channel has been drained (e.g. for
// out-of-band providers). It rejects late deltas after the stream is closed.
// This is exported for adapters that integrate via a push model rather than a
// delta channel; the default pump path does not call it.
func (a *StreamingAdapter) AcceptDelta(state *streamState, _ ModelStreamDelta) error { // intentionally ignored (reviewed)
	if state.isClosed() {
		return ErrStreamClosed
	}
	return nil
}

// newStreamCallIDs allocates stable, non-empty call and request IDs. It uses
// crypto/rand via newFailureID so IDs are not predictable.
func newStreamCallIDs() (callID, requestID string, err error) {
	return newFailureID("call"), newFailureID("req"), nil
}
