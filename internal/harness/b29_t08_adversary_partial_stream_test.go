package harness

// B29-T08 ADVERSARY TEST — partial-stream failure: stream fails
// mid-response.
//
// Attack: a streaming model call begins, emits some deltas, then the
// provider fails mid-stream (after some deltas but before
// response_completed). The adversary expects the partial output to
// become checkpoint input, recovery replay context, or a successful
// result — i.e. the partial output is incorrectly committed.
//
// Invariant under test:
//   - The terminal event is response_failed, NOT response_completed.
//   - No response_completed event is emitted.
//   - The durable event store receives a response_failed terminal marker,
//     NOT a response_completed marker.
//   - The committed flag never flips to true for partial output — there
//     is no observable "committed partial output" path.
//   - The provider is closed (upstream request released).

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// partialFailureProvider emits a few deltas, then an error delta, then
// closes. This simulates a provider that fails mid-stream after partial
// output.
type partialFailureProvider struct {
	closeCalls int32
	err        error
}

func newPartialFailureProvider(failErr error) *partialFailureProvider {
	return &partialFailureProvider{err: failErr}
}

func (p *partialFailureProvider) Start(ctx context.Context, _ runtime.ModelCallEnvelope) (<-chan ModelStreamDelta, error) {
	ch := make(chan ModelStreamDelta, 4)
	go func() {
		defer close(ch)
		// Emit a couple of partial deltas first.
		for _, d := range []ModelStreamDelta{
			{Text: "partial-output-1"},
			{Text: "partial-output-2"},
		} {
			select {
			case <-ctx.Done():
				return
			case ch <- d:
			}
		}
		// Then fail mid-stream before response_completed.
		select {
		case <-ctx.Done():
			return
		case ch <- ModelStreamDelta{Err: p.err}:
		}
	}()
	return ch, nil
}

func (p *partialFailureProvider) Close() error {
	atomic.AddInt32(&p.closeCalls, 1)
	return nil
}

// TestAdversary_B29_PartialStreamFailureCommitsFailedNotCompleted verifies
// that a provider failure mid-stream commits a response_failed terminal,
// NOT response_completed. The partial output must not become a committed
// result.
func TestAdversary_B29_PartialStreamFailureCommitsFailedNotCompleted(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("provider mid-stream failure")
	provider := newPartialFailureProvider(providerErr)
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)

	// Adversary assertion: the terminal event MUST be response_failed,
	// not response_completed. If the stream committed a partial output
	// as response_completed, that would let partial output become a
	// checkpoint/recovery result.
	if len(kinds) == 0 {
		t.Fatal("ADVERSARY BREAK: stream emitted no events — provider failure dropped the terminal")
	}
	if kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("ADVERSARY BREAK: terminal = %q; want response_failed (partial output must not commit)", kinds[len(kinds)-1])
	}
	// No response_completed must appear anywhere.
	for i, k := range kinds {
		if k == runtime.StreamEventResponseCompleted {
			t.Fatalf("ADVERSARY BREAK: response_completed emitted at position %d (partial output became committed result); kinds=%v", i, kinds)
		}
	}

	// The durable store must record a response_failed terminal marker,
	// NOT a response_completed marker.
	store.mu.Lock()
	storeEvents := append([]port.Event(nil), store.events...)
	store.mu.Unlock()
	if len(storeEvents) == 0 {
		t.Fatal("ADVERSARY BREAK: durable store recorded no terminal event — partial failure has no audit trail")
	}
	lastStore := storeEvents[len(storeEvents)-1]
	if lastStore.Type != "model.stream.response_failed" {
		t.Fatalf("ADVERSARY BREAK: durable terminal = %q; want model.stream.response_failed", lastStore.Type)
	}
	for _, e := range storeEvents {
		if e.Type == "model.stream.response_completed" {
			t.Fatalf("ADVERSARY BREAK: durable store recorded response_completed — partial output committed as success")
		}
	}

	// The provider must be closed (upstream request released). A leaked
	// upstream request would be a live resource leak on partial failure.
	if atomic.LoadInt32(&provider.closeCalls) == 0 {
		t.Fatal("ADVERSARY BREAK: provider not closed after partial failure — leaked upstream request")
	}

	// The error reason must appear in the terminal payload (audit
	// surface) but must NOT contain raw credentials (defense-in-depth).
	// We assert the reason string is present.
	hasReason := false
	for _, ev := range evts {
		if ev.Kind == runtime.StreamEventResponseFailed {
			if strings.Contains(string(ev.Payload), providerErr.Error()) {
				hasReason = true
			}
		}
	}
	if !hasReason {
		t.Fatalf("ADVERSARY BREAK: response_failed payload %q does not carry the failure reason %q (audit must show failure)", string(evts[len(evts)-1].Payload), providerErr.Error())
	}
}

// TestAdversary_B29_PartialStreamBufferedReleaseCommitsFailedNotCompleted
// verifies the same invariant under buffered_release: when the provider
// fails mid-stream, NO buffered output_delta is emitted (partial output
// stays uncommitted) and the terminal is response_failed.
func TestAdversary_B29_PartialStreamBufferedReleaseCommitsFailedNotCompleted(t *testing.T) {
	t.Parallel()

	providerErr := errors.New("buffered provider mid-stream failure")
	provider := newPartialFailureProvider(providerErr)
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailBufferedRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)

	if len(kinds) == 0 {
		t.Fatal("ADVERSARY BREAK: buffered stream emitted no events")
	}
	if kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("ADVERSARY BREAK: buffered terminal = %q; want response_failed", kinds[len(kinds)-1])
	}
	for i, k := range kinds {
		if k == runtime.StreamEventResponseCompleted {
			t.Fatalf("ADVERSARY BREAK: buffered response_completed at position %d (partial committed); kinds=%v", i, kinds)
		}
		// Under buffered_release, no output_delta may be emitted before
		// the terminal — partial output stays in the buffer and is
		// discarded on failure.
		if k == runtime.StreamEventOutputDelta {
			t.Fatalf("ADVERSARY BREAK: buffered_release emitted output_delta at position %d on partial failure (partial output leaked); kinds=%v", i, kinds)
		}
	}

	// Durable store must record response_failed, not response_completed.
	store.mu.Lock()
	storeEvents := append([]port.Event(nil), store.events...)
	store.mu.Unlock()
	for _, e := range storeEvents {
		if e.Type == "model.stream.response_completed" {
			t.Fatal("ADVERSARY BREAK: durable store recorded response_completed under buffered_release on partial failure")
		}
	}
	if len(storeEvents) == 0 || storeEvents[len(storeEvents)-1].Type != "model.stream.response_failed" {
		t.Fatalf("ADVERSARY BREAK: durable terminal under buffered_release = %+v; want model.stream.response_failed", storeEvents)
	}
}

// TestAdversary_B29_PartialStreamFailureRejectsLateDelta verifies that
// after a mid-stream failure commits response_failed, the stream is
// terminal and a new stream on the same adapter gets a fresh call ID —
// the terminal state does NOT leak into the new call. This prevents a
// late delta or a new call from resurrecting a partial-failure stream
// into a "committed" state.
func TestAdversary_B29_PartialStreamFailureRejectsLateDelta(t *testing.T) {
	t.Parallel()

	// Provider that emits one delta then an error.
	providerErr := errors.New("late-delta provider failure")
	provider := newPartialFailureProvider(providerErr)
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	if len(evts) == 0 {
		t.Fatal("ADVERSARY BREAK: stream emitted no events on partial failure")
	}
	if evts[len(evts)-1].Kind != runtime.StreamEventResponseFailed {
		t.Fatalf("ADVERSARY BREAK: terminal = %q; want response_failed", evts[len(evts)-1].Kind)
	}
	firstCallID := evts[0].CallID

	// Adversary: a NEW stream on the same adapter must NOT reuse the
	// terminal state from the failed call. It must get a fresh call ID
	// and a fresh sequence counter (starting at 1). If the terminal
	// state leaked, the new stream's first event would carry the old
	// call ID or a sequence > 1.
	ch2, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream 2: %v", err)
	}
	evts2 := drainEvents(t, ch2)
	if len(evts2) == 0 {
		t.Fatal("ADVERSARY BREAK: second stream emitted no events — terminal state leaked across calls")
	}
	if evts2[0].CallID == firstCallID {
		t.Fatalf("ADVERSARY BREAK: second stream reused call ID %q from the failed call — terminal state leaked", firstCallID)
	}
	if evts2[0].Sequence != 1 {
		t.Fatalf("ADVERSARY BREAK: second stream first sequence = %d; want 1 (fresh sequence counter)", evts2[0].Sequence)
	}
	// The new stream must also terminate with response_failed (the
	// provider always fails) — NOT response_completed. A leak that
	// committed the partial output as response_completed would be the
	// adversary break.
	if evts2[len(evts2)-1].Kind != runtime.StreamEventResponseFailed {
		t.Fatalf("ADVERSARY BREAK: second stream terminal = %q; want response_failed (no resurrection to completed)", evts2[len(evts2)-1].Kind)
	}
}
