package trigger

// B29-T08 ADVERSARY TEST — slow consumer cannot block provider cancellation
// or terminal commit.
//
// Attack: a streaming run is consumed slowly (the subscriber sleeps between
// reads). While the consumer is behind, the run is cancelled. The adversary
// expects the cancellation to back-pressure or deadlock — preventing the
// terminal commit so the run never reaches a terminal state.
//
// Invariant under test: the provider's terminal commit (Append of a terminal
// event) must NOT be blocked by a slow consumer. The durable store's Append
// fans out to subscribers with a bounded overflow timeout (100ms); a stalled
// subscriber is closed and removed, so the publisher (Append) proceeds and the
// terminal event is durably committed. The cancellation therefore completes
// and commits a terminal event even if the slow consumer hasn't drained.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// TestAdversary_B29_SlowConsumerCannotBlockTerminalCommit verifies that a
// slow consumer (one that sleeps between reads) cannot prevent a terminal
// event from being appended and committed. We model a "streaming run" as a
// sequence of appends to a durable event store, with a slow subscriber that
// does not drain its channel. While the subscriber is behind, the "provider"
// (test goroutine) appends a terminal event. The terminal event must be
// durably committed within a bounded time, regardless of the slow consumer.
func TestAdversary_B29_SlowConsumerCannotBlockTerminalCommit(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-slow"
	runID := "run-slow"

	// Adversary: a slow consumer subscribes but sleeps between reads. It
	// deliberately falls behind to try to stall the publisher.
	slowCh, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	slowDone := make(chan struct{})
	var slowEvents []port.Event
	var slowMu sync.Mutex
	go func() {
		defer close(slowDone)
		for {
			select {
			case e, open := <-slowCh:
				if !open {
					return
				}
				// ADVERSARY: sleep between reads to fall behind.
				time.Sleep(50 * time.Millisecond)
				slowMu.Lock()
				slowEvents = append(slowEvents, e)
				slowMu.Unlock()
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	// Publisher: append several events then a terminal event. The terminal
	// commit must NOT be blocked by the slow consumer. The durable store
	// closes a stalled subscriber after the overflow timeout (100ms), so
	// each Append completes in bounded time.
	terminalDeadline := time.Now().Add(3 * time.Second)
	for i := 0; i < 10; i++ {
		done := make(chan struct{})
		go func(i int) {
			defer close(done)
			if _, err := store.Append(ctx, mkEvent(tenant, runID, "progress", []byte("p"))); err != nil {
				t.Errorf("ADVERSARY BREAK: Append %d failed: %v", i, err)
			}
		}(i)
		select {
		case <-done:
		case <-time.After(time.Until(terminalDeadline)):
			t.Fatalf("ADVERSARY BREAK: Append %d blocked by slow consumer — provider terminal commit stalled", i)
		}
	}

	// Append the terminal event. This is the critical assertion: the
	// terminal commit must complete in bounded time even though the slow
	// consumer is behind.
	termDone := make(chan struct{})
	var termSeq int64
	go func() {
		defer close(termDone)
		seq, err := store.Append(ctx, mkEvent(tenant, runID, string(EventRunSucceeded), []byte("done")))
		if err != nil {
			t.Errorf("ADVERSARY BREAK: terminal Append failed: %v", err)
			return
		}
		termSeq = seq
	}()
	select {
	case <-termDone:
	case <-time.After(3 * time.Second):
		t.Fatal("ADVERSARY BREAK: terminal Append blocked by slow consumer — provider cancellation cannot commit")
	}

	// The terminal event must be durable: Read returns it.
	events, err := store.Read(ctx, tenant, runID, 0, 1000)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("ADVERSARY BREAK: no events committed — terminal event lost")
	}
	last := events[len(events)-1]
	if last.Type != string(EventRunSucceeded) {
		t.Fatalf("ADVERSARY BREAK: last event = %q; want %q (terminal commit must be durable)", last.Type, string(EventRunSucceeded))
	}
	if last.Sequence != termSeq {
		t.Fatalf("ADVERSARY BREAK: terminal sequence = %d; Read reports %d", termSeq, last.Sequence)
	}
}
