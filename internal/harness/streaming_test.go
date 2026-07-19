package harness

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// fakeStreamProvider is a configurable ModelStreamProvider for tests. It
// pushes a scripted list of deltas into a channel, supports cancellation via
// a context, and records Close calls.
type fakeStreamProvider struct {
	mu          sync.Mutex
	deltas      []ModelStreamDelta // scripted deltas to deliver
	startErr    error
	closeCalls  int
	startedCtx  context.Context
	closeCh     chan struct{} // closed when Close is called
	closedCount atomic.Int32
}

func newFakeStreamProvider(deltas []ModelStreamDelta) *fakeStreamProvider {
	return &fakeStreamProvider{
		deltas:  deltas,
		closeCh: make(chan struct{}),
	}
}

func (p *fakeStreamProvider) Start(ctx context.Context, _ runtime.ModelCallEnvelope) (<-chan ModelStreamDelta, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.startErr != nil {
		return nil, p.startErr
	}
	p.startedCtx = ctx
	ch := make(chan ModelStreamDelta, len(p.deltas)+1)
	go func() {
		defer close(ch)
		for _, d := range p.deltas {
			select {
			case <-ctx.Done():
				return
			case ch <- d:
			}
		}
		// Wait for Close or context cancellation before returning so the
		// pump observes a clean EOF after all deltas are delivered.
		select {
		case <-ctx.Done():
		case <-p.closeCh:
		}
	}()
	return ch, nil
}

func (p *fakeStreamProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	if p.closeCh != nil && p.closedCount.Add(1) == 1 {
		close(p.closeCh)
	}
	return nil
}

// deliveringClosingProvider emits the scripted deltas then closes the channel
// (EOF) without waiting for Close. Used to simulate a provider that returns
// its full response and finishes.
type deliveringClosingProvider struct {
	mu        sync.Mutex
	deltas    []ModelStreamDelta
	closeCalls int
}

func newDeliveringClosingProvider(deltas []ModelStreamDelta) *deliveringClosingProvider {
	return &deliveringClosingProvider{deltas: deltas}
}

func (p *deliveringClosingProvider) Start(ctx context.Context, _ runtime.ModelCallEnvelope) (<-chan ModelStreamDelta, error) {
	ch := make(chan ModelStreamDelta, len(p.deltas)+1)
	go func() {
		defer close(ch)
		for _, d := range p.deltas {
			select {
			case <-ctx.Done():
				return
			case ch <- d:
			}
		}
	}()
	return ch, nil
}

func (p *deliveringClosingProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	return nil
}

func validEnvelope() runtime.ModelCallEnvelope {
	return runtime.ModelCallEnvelope{
		Messages: []runtime.Message{{Role: runtime.RoleUser, Content: "hello"}},
	}
}

// drainEvents collects all events from ch until it closes, with a timeout.
func drainEvents(t *testing.T, ch <-chan runtime.StreamEvent) []runtime.StreamEvent {
	t.Helper()
	var out []runtime.StreamEvent
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, open := <-ch:
			if !open {
				return out
			}
			out = append(out, ev)
		case <-timer.C:
			t.Fatalf("drainEvents: timed out after receiving %d events", len(out))
			return out
		}
	}
}

func eventKinds(evts []runtime.StreamEvent) []runtime.StreamEventKind {
	out := make([]runtime.StreamEventKind, 0, len(evts))
	for _, e := range evts {
		out = append(out, e.Kind)
	}
	return out
}

func TestStreamingAdapter_BufferedRelease_NoDeltasUntilCompleted(t *testing.T) {
	// Use a deliveringClosingProvider that emits two deltas then closes the
	// channel (EOF). Under buffered_release, the adapter buffers both and
	// emits them as committed output before response_completed.
	provider := newDeliveringClosingProvider([]ModelStreamDelta{
		{Text: "Hello"},
		{Text: " world"},
	})
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailBufferedRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	// Must start with response_started.
	if len(kinds) == 0 || kinds[0] != runtime.StreamEventResponseStarted {
		t.Fatalf("expected response_started first; got %v", kinds)
	}
	// buffered_release: the buffered output is emitted as output_delta(s)
	// before response_completed. No deltas are emitted incrementally before
	// the terminal.
	var deltasEmitted int
	for _, k := range kinds {
		if k == runtime.StreamEventOutputDelta {
			deltasEmitted++
		}
	}
	if deltasEmitted == 0 {
		t.Fatalf("buffered_release must emit the committed output before response_completed; kinds=%v", kinds)
	}
	// Last event must be response_completed.
	if kinds[len(kinds)-1] != runtime.StreamEventResponseCompleted {
		t.Fatalf("expected response_completed last; got %v", kinds)
	}
	// All events must validate (no raw creds/CoT, bounded payload).
	for _, ev := range evts {
		if err := runtime.ValidateStreamEvent(ev); err != nil {
			t.Fatalf("event %q failed validation: %v", ev.Kind, err)
		}
	}
	// Sequences must be monotonically increasing from 1.
	for i, ev := range evts {
		if ev.Sequence != int64(i+1) {
			t.Fatalf("event %d sequence = %d; want %d", i, ev.Sequence, i+1)
		}
	}
}

func TestStreamingAdapter_IncrementalRelease_EmitsDeltasUncommittedUntilCompleted(t *testing.T) {
	// Use a provider that emits two deltas then waits for Close. The pump
	// only sees EOF when the provider closes its channel; to get a clean
	// completion we use a closingProvider-style: deliver deltas then close.
	deltas := []ModelStreamDelta{
		{Text: "Hello"},
		{Text: " world"},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Allow the provider goroutine to deliver deltas, then close to signal EOF.
	// Give it a moment to push deltas into the buffered channel.
	time.Sleep(50 * time.Millisecond)
	_ = provider.Close()
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	if len(kinds) < 4 {
		t.Fatalf("expected at least 4 events (started, delta, delta, completed); got %v", kinds)
	}
	if kinds[0] != runtime.StreamEventResponseStarted {
		t.Fatalf("expected response_started first; got %v", kinds)
	}
	// Must contain output_delta events.
	hasDelta := false
	for _, k := range kinds {
		if k == runtime.StreamEventOutputDelta {
			hasDelta = true
		}
	}
	if !hasDelta {
		t.Fatalf("incremental_release must emit output_delta; kinds=%v", kinds)
	}
	if kinds[len(kinds)-1] != runtime.StreamEventResponseCompleted {
		t.Fatalf("expected response_completed last; got %v", kinds)
	}
	// All events must validate.
	for _, ev := range evts {
		if err := runtime.ValidateStreamEvent(ev); err != nil {
			t.Fatalf("event %q failed validation: %v", ev.Kind, err)
		}
	}
}

func TestStreamingAdapter_BudgetExhaustion_ClosesAndCommitsFailed(t *testing.T) {
	// Provider emits a usage update that exceeds budget, then more deltas.
	// The adapter must close upstream, commit response_failed, and reject
	// late deltas.
	deltas := []ModelStreamDelta{
		{Usage: &runtime.Usage{PromptTokens: 100, TotalTokens: 100}},
		{Text: "late delta after budget"},
	}
	provider := newFakeStreamProvider(deltas)
	store := &recordingEventStore{}
	budget := NewStaticBudget(0) // already exhausted
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, budget)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	// Must end with response_failed (budget exhaustion).
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed terminal on budget exhaustion; got %v", kinds)
	}
	// The late "late delta after budget" text must NOT appear in any event.
	for _, ev := range evts {
		if strings.Contains(string(ev.Payload), "late delta after budget") {
			t.Fatalf("late delta after budget exhaustion must not be emitted: %q", ev.Payload)
		}
	}
	// Provider must be closed (upstream request closed).
	if provider.closeCalls == 0 {
		t.Fatal("provider must be closed on budget exhaustion")
	}
	// A terminal response_failed event must be appended to the durable store.
	hasFailed := false
	for _, e := range store.events {
		if e.Type == "model.stream.response_failed" {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Fatalf("durable store must record response_failed; got %+v", store.events)
	}
}

func TestStreamingAdapter_Cancellation_CommitsFailedAndRejectsLateDeltas(t *testing.T) {
	provider := newFakeStreamProvider([]ModelStreamDelta{
		{Text: "first"},
		{Text: "second"},
		{Text: "third"},
	})
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := adapter.Stream(ctx, validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Cancel almost immediately.
	time.Sleep(20 * time.Millisecond)
	cancel()
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	// Must end with response_failed (cancellation).
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed terminal on cancellation; got %v", kinds)
	}
	// No response_completed must be emitted.
	for _, k := range kinds {
		if k == runtime.StreamEventResponseCompleted {
			t.Fatalf("cancellation must not emit response_completed; kinds=%v", kinds)
		}
	}
	// Provider must be closed.
	if provider.closeCalls == 0 {
		t.Fatal("provider must be closed on cancellation")
	}
	// Durable store must record a response_failed terminal.
	hasFailed := false
	for _, e := range store.events {
		if e.Type == "model.stream.response_failed" {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Fatalf("durable store must record response_failed on cancellation; got %+v", store.events)
	}
}

func TestStreamingAdapter_Backpressure_SlowConsumerCausesBackpressureError(t *testing.T) {
	// Override the backpressure timeout to keep the test fast.
	oldTimeout := BackpressureTimeout
	BackpressureTimeout = 100 * time.Millisecond
	defer func() { BackpressureTimeout = oldTimeout }()

	// Provider emits many deltas quickly; NO consumer drains ch, so the bounded
	// 128-cap channel fills and the adapter hits backpressure. We detect the
	// backpressure terminal via the durable store (persistTerminal appends a
	// response_failed event regardless of whether the channel emit succeeded)
	// and via the channel closing.
	deltas := make([]ModelStreamDelta, StreamChannelBufferSize+50)
	for i := range deltas {
		deltas[i] = ModelStreamDelta{Text: "x"}
	}
	provider := newFakeStreamProvider(deltas)
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(provider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// No consumer drains ch: out (128-cap) fills, the 129th emit blocks for
	// BackpressureTimeout, finishTerminal emits a response_failed terminal
	// (which also blocks then times out), persists it, and the pump closes the
	// channel. We detect via the durable store (persistTerminal appends a
	// response_failed regardless of channel-emit success) and then confirm the
	// channel closed.
	deadline := time.Now().Add(5 * time.Second)
	var hasBackpressure bool
	for time.Now().Before(deadline) {
		store.mu.Lock()
		for _, e := range store.events {
			if e.Type == "model.stream.response_failed" && strings.Contains(string(e.Payload), ErrStreamBackpressure.Error()) {
				hasBackpressure = true
				break
			}
		}
		store.mu.Unlock()
		if hasBackpressure {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hasBackpressure {
		t.Fatal("durable store did not record backpressure response_failed")
	}

	// The channel must eventually close. Drain remaining events (the pump has
	// already committed the terminal; we just wait for close).
	closed := false
	closeDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(closeDeadline) && !closed {
		select {
		case _, open := <-ch:
			if !open {
				closed = true
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !closed {
		t.Fatal("channel did not close after backpressure terminal")
	}
}

func TestStreamingAdapter_RawCredentialsAbsentFromEvents(t *testing.T) {
	// If a provider delta carries a credential marker (a PEM private key
	// header), the adapter must fail closed and never emit it. We assert on
	// the marker header (not a real secret value) so the test does not embed
	// any credential.
	credText := "-----BEGIN PRIVATE KEY----- MIIBVwIBADANBgkqhkiG9w0BAQE"
	deltas := []ModelStreamDelta{
		{Text: credText},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = provider.Close()
	evts := drainEvents(t, ch)
	// No event payload may carry the private key header.
	for _, ev := range evts {
		if strings.Contains(string(ev.Payload), "BEGIN PRIVATE KEY") {
			t.Fatalf("raw credential marker must not appear in any event: kind=%q payload=%q", ev.Kind, ev.Payload)
		}
	}
	// The stream must terminate with response_failed (validation rejected the
	// credential-bearing delta).
	kinds := eventKinds(evts)
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed terminal after credential rejection; got %v", kinds)
	}
}

func TestStreamingAdapter_RawCoTAbsentFromEvents(t *testing.T) {
	cotText := "<thought>let me reason step 1: about this</thought>"
	deltas := []ModelStreamDelta{
		{Text: cotText},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = provider.Close()
	evts := drainEvents(t, ch)
	for _, ev := range evts {
		if strings.Contains(string(ev.Payload), "<thought>") {
			t.Fatalf("raw CoT must not appear in any event: kind=%q payload=%q", ev.Kind, ev.Payload)
		}
	}
	kinds := eventKinds(evts)
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed terminal after CoT rejection; got %v", kinds)
	}
}

func TestStreamingAdapter_PayloadBounded_RejectsOver64KB(t *testing.T) {
	// A single delta whose text exceeds 64KB must be rejected (validation
	// fails), failing the stream closed.
	big := strings.Repeat("a", runtime.MaxStreamPayloadBytes+1)
	deltas := []ModelStreamDelta{
		{Text: big},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = provider.Close()
	evts := drainEvents(t, ch)
	for _, ev := range evts {
		if len(ev.Payload) > runtime.MaxStreamPayloadBytes {
			t.Fatalf("event payload exceeds 64KB: kind=%q len=%d", ev.Kind, len(ev.Payload))
		}
	}
	kinds := eventKinds(evts)
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed terminal after oversized payload; got %v", kinds)
	}
}

func TestStreamingAdapter_UsageUpdateEmitted(t *testing.T) {
	deltas := []ModelStreamDelta{
		{Usage: &runtime.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, NewStaticBudget(100))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = provider.Close()
	evts := drainEvents(t, ch)
	hasUsage := false
	for _, ev := range evts {
		if ev.Kind == runtime.StreamEventUsageUpdate {
			hasUsage = true
			if !strings.Contains(string(ev.Payload), "prompt_tokens") {
				t.Fatalf("usage_update must carry approved dimensions; got %q", ev.Payload)
			}
			if strings.Contains(strings.ToLower(string(ev.Payload)), "secret") {
				t.Fatalf("usage_update must not carry secrets; got %q", ev.Payload)
			}
		}
	}
	if !hasUsage {
		t.Fatalf("usage_update event must be emitted; kinds=%v", eventKinds(evts))
	}
}

func TestStreamingAdapter_InvalidEnvelopeRejected(t *testing.T) {
	provider := newFakeStreamProvider(nil)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	_, err := adapter.Stream(context.Background(), runtime.ModelCallEnvelope{}, runtime.GuardrailIncrementalRelease, nil)
	if err == nil {
		t.Fatal("invalid envelope must be rejected")
	}
}

func TestStreamingAdapter_NilProviderRejected(t *testing.T) {
	adapter := NewStreamingAdapter(nil, nil, "", "", "")
	_, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err == nil {
		t.Fatal("nil provider must be rejected")
	}
}

func TestStreamingAdapter_PartialOutputUncommittedUntilCompleted(t *testing.T) {
	// Under buffered_release, no output_delta is emitted until
	// response_completed. Under incremental_release, deltas are emitted but
	// the committed flag only flips at completion. We assert the wire-level
	// contract: no response_completed means no committed output is observable.
	deltas := []ModelStreamDelta{
		{Text: "partial"},
	}
	provider := newFakeStreamProvider(deltas)
	adapter := NewStreamingAdapter(provider, nil, "", "", "")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := adapter.Stream(ctx, validEnvelope(), runtime.GuardrailBufferedRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Cancel before completion so the stream terminates with response_failed.
	time.Sleep(10 * time.Millisecond)
	cancel()
	evts := drainEvents(t, ch)
	for _, ev := range evts {
		if ev.Kind == runtime.StreamEventResponseCompleted {
			t.Fatalf("partial output must not become a committed response_completed; kinds=%v", eventKinds(evts))
		}
		if ev.Kind == runtime.StreamEventOutputDelta {
			// buffered_release must not emit deltas before completion.
			t.Fatalf("buffered_release must not emit output_delta before completion; kinds=%v", eventKinds(evts))
		}
	}
}

func TestStreamingAdapter_ProviderErrorCommitsFailed(t *testing.T) {
	providerErr := errors.New("provider 500")
	// A provider that returns an error delta.
	errProvider := &errStreamProvider{err: providerErr}
	store := &recordingEventStore{}
	adapter := NewStreamingAdapter(errProvider, store, "tenant", "run", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	if len(kinds) == 0 || kinds[len(kinds)-1] != runtime.StreamEventResponseFailed {
		t.Fatalf("expected response_failed on provider error; got %v", kinds)
	}
}

// errStreamProvider emits a single error delta then closes.
type errStreamProvider struct {
	err       error
	closeCalls int
	mu        sync.Mutex
}

func (p *errStreamProvider) Start(_ context.Context, _ runtime.ModelCallEnvelope) (<-chan ModelStreamDelta, error) {
	ch := make(chan ModelStreamDelta, 1)
	ch <- ModelStreamDelta{Err: p.err}
	close(ch)
	return ch, nil
}

func (p *errStreamProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	return nil
}

// recordingEventStore is a minimal port.EventStore that records Append calls.
type recordingEventStore struct {
	mu     sync.Mutex
	events []port.Event
}

func (r *recordingEventStore) Append(_ context.Context, e port.Event) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e.Sequence = int64(len(r.events) + 1)
	r.events = append(r.events, e)
	return e.Sequence, nil
}

func (r *recordingEventStore) Subscribe(context.Context, string, string, int64) (<-chan port.Event, error) {
	ch := make(chan port.Event)
	close(ch)
	return ch, nil
}

func (r *recordingEventStore) Read(context.Context, string, string, int64, int) ([]port.Event, error) {
	return nil, nil
}

func (r *recordingEventStore) LatestSequence(context.Context, string, string) (int64, error) {
	return 0, nil
}

var _ port.EventStore = (*recordingEventStore)(nil)
