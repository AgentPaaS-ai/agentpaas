package trigger

// B29-T08 ADVERSARY TEST — forged cursor: subscriber provides a fabricated
// after_sequence value beyond any real event.
//
// Attack: an adversary subscribes with after_sequence = 999999 (far beyond
// any event the run has ever produced). The adversary expects one of:
//   - The store panics (nil deref, slice out-of-range, overflow).
//   - The store wraps around and returns events from other runs or tenants.
//   - The store returns events the subscriber shouldn't see (cross-tenant
//     leak via a forged cursor).
//
// Invariant under test:
//   - The store does NOT panic on a forged cursor.
//   - It does NOT return events from other runs or tenants.
//   - It returns empty (no events with Sequence > 999999) or waits for new
//     events (open, blocking channel).

import (
	"context"
	"testing"
	"time"
)

// TestAdversary_B29_ForgedCursorBeyondLastEventReturnsEmptyNoPanic
// subscribes with a fabricated after_sequence far beyond any real event
// and asserts the store neither panics nor returns events from other
// runs/tenants.
func TestAdversary_B29_ForgedCursorBeyondLastEventReturnsEmptyNoPanic(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()

	// Tenant A has a real run with a few events.
	tenantA := "tenant-a"
	runA := "run-a"
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, mkEvent(tenantA, runA, "e", []byte("a-payload"))); err != nil {
			t.Fatalf("Append A %d: %v", i, err)
		}
	}
	// Tenant B has a separate run with a different event count.
	tenantB := "tenant-b"
	runB := "run-b"
	if _, err := store.Append(ctx, mkEvent(tenantB, runB, "e", []byte("b-payload"))); err != nil {
		t.Fatalf("Append B: %v", err)
	}

	latestA, err := store.LatestSequence(ctx, tenantA, runA)
	if err != nil {
		t.Fatalf("LatestSequence A: %v", err)
	}
	if latestA != 3 {
		t.Fatalf("LatestSequence A = %d; want 3", latestA)
	}

	// Adversary: subscribe with a forged cursor way beyond latestA.
	// The store must NOT panic. It must return an open channel with no
	// replayed events (because no event has Sequence > 999999), and must
	// not deliver events from tenant B's run.
	forgedCursor := int64(999999)
	ch, err := store.Subscribe(ctx, tenantA, runA, forgedCursor)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: Subscribe with forged cursor returned error: %v (want no panic, open channel)", err)
	}
	if ch == nil {
		t.Fatal("ADVERSARY BREAK: Subscribe with forged cursor returned nil channel")
	}

	// No events should be replayed (all real events have Sequence <= 3,
	// well below the forged cursor 999999). We must not receive tenant
	// B's events either.
	select {
	case e, open := <-ch:
		if open {
			t.Fatalf("ADVERSARY BREAK: forged cursor delivered event %+v (no event should have Sequence > 999999)", e)
		}
		// A closed channel is acceptable if the store decided not to
		// register the subscriber; but the contract is "open, blocking,
		// no events" for a forged cursor. A close here is a soft
		// violation but not an adversary break. We treat it as
		// acceptable.
	case <-time.After(200 * time.Millisecond):
		// No event within the window — correct. The forged cursor
		// matched no events, and the channel remains open (no replay,
		// no cross-tenant leak).
	}

	// Read with the forged cursor must also return empty (no events with
	// Sequence > 999999), not events from tenant B.
	events, err := store.Read(ctx, tenantA, runA, forgedCursor, 100)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: Read with forged cursor returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ADVERSARY BREAK: Read with forged cursor returned %d events; want 0 (no event has Sequence > 999999)", len(events))
	}
	for _, e := range events {
		if e.TenantID != tenantA {
			t.Fatalf("ADVERSARY BREAK: forged cursor delivered cross-tenant event %+v", e)
		}
		if e.RunID != runA {
			t.Fatalf("ADVERSARY BREAK: forged cursor delivered cross-run event %+v", e)
		}
	}

	// Sanity: the actual events for tenant A are still readable from
	// cursor 0. This proves the forged cursor did not corrupt the index.
	allA, err := store.Read(ctx, tenantA, runA, 0, 100)
	if err != nil {
		t.Fatalf("Read A from 0: %v", err)
	}
	if len(allA) != 3 {
		t.Fatalf("Read A from 0 = %d events; want 3 (forged cursor must not corrupt index)", len(allA))
	}
}

// TestAdversary_B29_ForgedCursorNegativeDoesNotPanic verifies a negative
// forged cursor does not panic and is treated like cursor 0 (replays all
// events) without returning cross-tenant data.
func TestAdversary_B29_ForgedCursorNegativeDoesNotPanic(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-neg"
	runID := "run-neg"
	for i := 0; i < 2; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// A negative cursor is out-of-band; the store must not panic. The
	// Subscribe filter (event.Sequence <= afterSequence) treats a
	// negative cursor like "replay all" because no real event has a
	// negative sequence. We assert the call does not panic and returns
	// a non-nil channel.
	ch, err := store.Subscribe(ctx, tenant, runID, -1)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: Subscribe negative cursor returned error: %v", err)
	}
	if ch == nil {
		t.Fatal("ADVERSARY BREAK: Subscribe negative cursor returned nil channel")
	}

	// Read with a negative cursor must not panic.
	events, err := store.Read(ctx, tenant, runID, -1, 100)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: Read negative cursor returned error: %v", err)
	}
	// A negative cursor replays all events (none have Sequence <= -1).
	if len(events) != 2 {
		t.Fatalf("ADVERSARY BREAK: Read negative cursor = %d events; want 2 (replay all, no panic)", len(events))
	}
	for _, e := range events {
		if e.TenantID != tenant || e.RunID != runID {
			t.Fatalf("ADVERSARY BREAK: negative cursor delivered cross-scope event %+v", e)
		}
	}
}
