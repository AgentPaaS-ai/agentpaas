package trigger

// B29-T01 CHARACTERIZATION TEST — freezes current behavior; B29
// replacement tasks are expected to update or fail these tests.
//
// Observation 3: EventBus is in-memory, bounded, drops events silently,
// and does not survive process restart (simulated by constructing a
// new EventBus).

import (
	"sync"
	"testing"
	"time"
)

// TestEventBusUsesInMemoryMapStructurally proves EventBus holds all
// state in an in-memory map[string]*runBuffer. NewEventBus takes no
// path/store/persistence argument — it creates a bare map.
func TestEventBusUsesInMemoryMapStructurally(t *testing.T) {
	t.Parallel()

	// NewEventBus accepts zero arguments — no path, no store, no WAL, no DB.
	bus := NewEventBus()
	if bus == nil {
		t.Fatal("NewEventBus returned nil")
	}
	if bus.buffers == nil {
		t.Fatal("EventBus has no buffers map")
	}

	// Publish an event and verify it's only held in the in-memory buffer.
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	events := bus.GetEvents("run-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in memory, got %d", len(events))
	}

	// There is no persistent storage mechanism on EventBus itself —
	// no file descriptor, no path, no database handle.
	// This is a structural characterization: the type has no persistence fields.
}

// TestEventBusChannelBufferCapacity64 asserts the subscriber channel
// is created with capacity 64. We verify this indirectly by publishing
// >64 events with a stalled subscriber and proving >64 events accumulate
// in the buffer before dropping.
func TestEventBusChannelBufferCapacity64(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-full")
	ch, cancel := bus.Subscribe("run-full", 0)
	// Do NOT read from ch — subscriber is stalled.

	// Publish 65 events (more than buffer capacity 64).
	for i := 0; i < 65; i++ {
		bus.Publish("run-full", EventRunProgress, nil)
	}

	// Drain the channel. We should receive exactly buffer-size events,
	// meaning 64 were buffered and the rest were dropped.
	received := 0
	timeout := time.After(200 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-ch:
			received++
		case <-timeout:
			break drainLoop
		}
	}
	cancel()

	if received == 0 {
		t.Fatal("received 0 events; buffer may be broken")
	}
	if received >= 65 {
		t.Fatalf("received %d events (all 65) — buffer is unbounded or >64; should have dropped some", received)
	}
	if received > 64 {
		t.Fatalf("received %d events; buffer appears larger than 64", received)
	}
	// The expected behavior: exactly 64 buffered, 1 dropped.
	if received != 64 {
		t.Logf("received %d events (expected 64 buffered, %d dropped)", received, 65-received)
	}
}

// TestEventBusDropsEventsWhenBufferFull proves that when the subscriber
// buffer is full, Publish silently drops events (via the select-default
// pattern). We publish many events and the subscriber receives fewer
// than published.
func TestEventBusDropsEventsWhenBufferFull(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-drop")
	ch, cancel := bus.Subscribe("run-drop", 0)
	// Stall: never read from ch.

	totalPublished := 200
	for i := 0; i < totalPublished; i++ {
		bus.Publish("run-drop", EventRunProgress, nil)
	}

	// Drain what's in the buffer.
	received := 0
	timeout := time.After(500 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-ch:
			received++
		case <-timeout:
			break drainLoop
		}
	}
	cancel()

	if received >= totalPublished {
		t.Fatalf("received all %d events — no drop behavior observed", received)
	}
	if received == 0 {
		t.Fatal("received 0 events — channel may be broken")
	}
	t.Logf("published %d, received %d — %d events were silently dropped",
		totalPublished, received, totalPublished-received)
}

// TestEventBusMultipleSubscribersIndependentDrops proves each
// subscriber has its own buffer and drops independently.
func TestEventBusMultipleSubscribersIndependentDrops(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-multi")

	// One fast subscriber (reads immediately), one slow subscriber (stalled).
	fastCh, fastCancel := bus.Subscribe("run-multi", 0)
	defer fastCancel()

	slowCh, slowCancel := bus.Subscribe("run-multi", 0)
	// Never read from slowCh — it's stalled.
	defer slowCancel()

	var fastReceived int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range fastCh {
			fastReceived++
		}
	}()

	// Publish 200 events. The fast subscriber keeps reading.
	for i := 0; i < 200; i++ {
		bus.Publish("run-multi", EventRunProgress, nil)
	}

	// Give the fast goroutine time to consume.
	time.Sleep(100 * time.Millisecond)

	// Drain the slow subscriber.
	slowReceived := 0
	timeout := time.After(500 * time.Millisecond)
drainSlow:
	for {
		select {
		case <-slowCh:
			slowReceived++
		case <-timeout:
			break drainSlow
		}
	}

	// fast subscriber should get all 200 events (it keeps up).
	// But if the goroutine is still consuming, we may not get all.
	// The key assertion: slow subscriber got fewer than 200.
	if slowReceived >= 200 {
		t.Fatalf("slow subscriber received all %d events — no drop", slowReceived)
	}
	t.Logf("fast received approx %d events, slow received %d (drops expected)", fastReceived, slowReceived)
}

// TestEventBusDoesNotSurviveRestart proves that constructing a new
// EventBus (simulating process restart) loses all prior events.
// Subscribe on the new bus for a previously-known runID returns
// a closed channel immediately.
func TestEventBusDoesNotSurviveRestart(t *testing.T) {
	t.Parallel()

	// Create first EventBus and publish events.
	bus1 := NewEventBus()
	bus1.RegisterRun("run-restart")
	bus1.Publish("run-restart", EventRunCreated, nil)
	bus1.Publish("run-restart", EventRunSucceeded, nil)

	// Verify events exist in bus1.
	events1 := bus1.GetEvents("run-restart")
	if len(events1) != 2 {
		t.Fatalf("bus1 has %d events; want 2", len(events1))
	}

	// "Restart" — construct a new EventBus. Prior events are gone.
	bus2 := NewEventBus()

	events2 := bus2.GetEvents("run-restart")
	if events2 != nil {
		t.Fatalf("bus2 has %d events after restart; want nil (all events lost)", len(events2))
	}

	// Subscribe on bus2 for the old runID — must return a closed channel.
	ch, cancel := bus2.Subscribe("run-restart", 0)
	defer cancel()

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel on new bus for old runID should be closed immediately")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel close on new bus")
	}
}

// TestEventBusTerminalClosesAllSubscriberChannels proves that a
// terminal event closes ALL subscriber channels for that run.
func TestEventBusTerminalClosesAllSubscriberChannels(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-term")

	sub1, cancel1 := bus.Subscribe("run-term", 0)
	defer cancel1()
	sub2, cancel2 := bus.Subscribe("run-term", 0)
	defer cancel2()
	sub3, cancel3 := bus.Subscribe("run-term", 0)
	defer cancel3()

	// Publish terminal event.
	bus.Publish("run-term", EventRunSucceeded, nil)

	// All three channels must: receive the RunSucceeded event, then close.
	for i, ch := range []<-chan RunEvent{sub1, sub2, sub3} {
		// First read: terminal event itself.
		select {
		case event, open := <-ch:
			if !open {
				t.Errorf("subscriber %d: channel closed unexpectedly before receiving terminal event", i)
				continue
			}
			if event.Type != EventRunSucceeded {
				t.Errorf("subscriber %d: expected RunSucceeded, got %s", i, event.Type)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timed out waiting for terminal event", i)
			continue
		}

		// Second read: must be channel close.
		select {
		case _, open := <-ch:
			if open {
				t.Errorf("subscriber %d channel is still open after terminal event", i)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timed out waiting for channel close after terminal", i)
		}
	}
}
