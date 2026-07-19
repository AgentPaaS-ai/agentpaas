package trigger

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestDurableSubscribeFromZero receives all events from a cursor of 0.
func TestDurableSubscribeFromZero(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-sub"
	runID := "run-sub0"
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	ch, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	for i := 0; i < 3; i++ {
		select {
		case e, open := <-ch:
			if !open {
				t.Fatalf("channel closed at event %d", i)
			}
			if e.Sequence != int64(i+1) {
				t.Fatalf("event %d sequence = %d; want %d", i, e.Sequence, i+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

// TestDurableSubscribeFromCursorN receives only events after N.
func TestDurableSubscribeFromCursorN(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-sub-n"
	runID := "run-subN"
	for i := 0; i < 5; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Subscribe from sequence 2 — should receive events 3, 4, 5.
	ch, err := store.Subscribe(ctx, tenant, runID, 2)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	for i := 0; i < 3; i++ {
		select {
		case e, open := <-ch:
			if !open {
				t.Fatalf("channel closed at event %d", i)
			}
			want := int64(i + 3)
			if e.Sequence != want {
				t.Fatalf("event %d sequence = %d; want %d", i, e.Sequence, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

// TestDurableSubscribeReceivesLiveEvents verifies that after replaying
// existing events, a live Append is delivered to the subscriber.
func TestDurableSubscribeReceivesLiveEvents(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-live"
	runID := "run-live"
	if _, err := store.Append(ctx, mkEvent(tenant, runID, "e1", []byte("p1"))); err != nil {
		t.Fatalf("Append 1: %v", err)
	}

	ch, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Drain replay.
	select {
	case e := <-ch:
		if e.Sequence != 1 {
			t.Fatalf("replay sequence = %d; want 1", e.Sequence)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replay")
	}

	// Live event must arrive.
	if _, err := store.Append(ctx, mkEvent(tenant, runID, "e2", []byte("p2"))); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	select {
	case e := <-ch:
		if e.Sequence != 2 {
			t.Fatalf("live sequence = %d; want 2", e.Sequence)
		}
		if string(e.Payload) != "p2" {
			t.Fatalf("live payload = %q; want %q", string(e.Payload), "p2")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live event")
	}
}

// TestDurableRestartPreservesEvents verifies that a new DurableEventStore
// pointing at the same state dir can read all previously-appended events.
func TestDurableRestartPreservesEvents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")

	store1, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore 1: %v", err)
	}
	ctx := context.Background()
	tenant := "tenant-restart"
	runID := "run-restart"
	for i := 0; i < 4; i++ {
		if _, err := store1.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := store1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	// "Restart" — new store at same path.
	store2, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore 2: %v", err)
	}
	defer func() { _ = store2.Close() }()

	events, err := store2.Read(ctx, tenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read after restart: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("after restart len(events) = %d; want 4", len(events))
	}
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("events[%d].Sequence = %d; want %d", i, e.Sequence, i+1)
		}
	}
	latest, err := store2.LatestSequence(ctx, tenant, runID)
	if err != nil {
		t.Fatalf("LatestSequence after restart: %v", err)
	}
	if latest != 4 {
		t.Fatalf("latest after restart = %d; want 4", latest)
	}

	// A new append after restart continues the sequence.
	seq5, err := store2.Append(ctx, mkEvent(tenant, runID, "e", []byte("p5")))
	if err != nil {
		t.Fatalf("Append after restart: %v", err)
	}
	if seq5 != 5 {
		t.Fatalf("seq5 = %d; want 5", seq5)
	}
}
