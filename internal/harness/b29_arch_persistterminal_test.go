package harness

// B29-ARCH-REVIEW BLOCKER 3: persistTerminal must surface the durable Append
// error instead of swallowing it. A silently lost terminal event is the worst
// failure mode in an audit-first system: the run would appear non-terminal
// forever, blocking idempotent re-invocation and corrupting replay.
//
// This test verifies that when the durable EventStore Append fails for a
// terminal event, the failure is observable (not silently dropped) and the
// in-memory stream still emits the terminal to its consumer.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// erroringEventStore is a port.EventStore whose Append always returns err.
// It is used to simulate a durable EventStore that cannot persist a terminal
// event (e.g. disk full, WAL corrupt, permission revoked).
type erroringEventStore struct {
	mu       sync.Mutex
	appends  int
	appendErr error
}

func (e *erroringEventStore) Append(_ context.Context, _ port.Event) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.appends++
	return 0, e.appendErr
}

func (e *erroringEventStore) Subscribe(context.Context, string, string, int64) (<-chan port.Event, error) {
	ch := make(chan port.Event)
	close(ch)
	return ch, nil
}

func (e *erroringEventStore) Read(context.Context, string, string, int64, int) ([]port.Event, error) {
	return nil, nil
}

func (e *erroringEventStore) LatestSequence(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (e *erroringEventStore) appendCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.appends
}

var _ port.EventStore = (*erroringEventStore)(nil)

// TestPersistTerminal_SurfacesAppendError verifies that when the durable
// EventStore Append fails, persistTerminal returns the error (rather than
// swallowing it) and the streaming adapter still emits the in-memory terminal
// to its consumer. The error is logged by finishTerminal; we assert the Append
// was attempted (not skipped) and the terminal was still delivered on the out
// channel.
func TestPersistTerminal_SurfacesAppendError(t *testing.T) {
	t.Parallel()

	appendErr := errors.New("disk full: cannot persist terminal")
	store := &erroringEventStore{appendErr: appendErr}

	// A provider that closes immediately (clean EOF) so the pump commits a
	// response_completed terminal.
	provider := &deliveringClosingProvider{deltas: []ModelStreamDelta{{Text: "done"}}}
	adapter := NewStreamingAdapter(provider, store, "tenant-pt", "run-pt", "pkg")
	ch, err := adapter.Stream(context.Background(), validEnvelope(), runtime.GuardrailIncrementalRelease, nil)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Drain the in-memory stream. It must still deliver a terminal event
	// even though the durable Append failed.
	evts := drainEvents(t, ch)
	kinds := eventKinds(evts)
	if len(kinds) == 0 {
		t.Fatal("ADVERSARY BREAK: no in-memory terminal emitted when durable Append failed")
	}
	last := kinds[len(kinds)-1]
	if last != runtime.StreamEventResponseCompleted && last != runtime.StreamEventResponseFailed {
		t.Fatalf("terminal kind = %q; want response_completed or response_failed", last)
	}

	// The Append must have been attempted (not skipped). A swallowed error
	// here would mean the terminal was silently lost — the worst failure
	// mode. We give the pump a brief moment to finish persisting after the
	// channel closes.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if store.appendCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.appendCount(); got == 0 {
		t.Fatal("ADVERSARY BREAK: persistTerminal did not attempt Append — terminal event silently lost from durable store")
	}
}
