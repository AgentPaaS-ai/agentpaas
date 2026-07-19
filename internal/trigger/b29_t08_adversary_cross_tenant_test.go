package trigger

// B29-T08 ADVERSARY TEST — cross-tenant subscription: tenant A subscribes
// to tenant B's run.
//
// Attack: tenant A appends events to its own run. Tenant B tries to
// Subscribe or Read tenant A's run, expecting to receive tenant A's events
// (data leak across tenant boundary).
//
// Invariant under test:
//   - Tenant B's Subscribe returns an open channel that NEVER delivers
//     tenant A's events.
//   - Tenant B's Read returns empty (not an error).
//   - Tenant B's LatestSequence for tenant A's run returns 0.
//   - Cross-tenant access is denied silently (no error that confirms the
//     run exists — the store does not leak existence either).

import (
	"context"
	"testing"
	"time"
)

// TestAdversary_B29_CrossTenantSubscribeDeniesAccess verifies that tenant B
// cannot Subscribe to tenant A's run and receive tenant A's events. The
// durable store keys subscribers by (tenant, run), so a tenant-B subscriber
// for tenant-A's runID receives nothing — cross-tenant access is denied.
func TestAdversary_B29_CrossTenantSubscribeDeniesAccess(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenantA := "tenant-a"
	tenantB := "tenant-b"
	runID := "shared-run-id"

	// Tenant A subscribes to its own run BEFORE any events exist (the
	// lazy run-state creation lets pre-event subscribers receive live
	// appends).
	chA, err := store.Subscribe(ctx, tenantA, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}

	// Adversary: tenant B subscribes to the SAME runID under tenant B's
	// namespace. The store must register this subscriber under
	// (tenant-b, runID), NOT (tenant-a, runID). Tenant B's subscriber
	// must NEVER receive tenant A's events.
	chB, err := store.Subscribe(ctx, tenantB, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}

	// Tenant A appends events to its own run.
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, mkEvent(tenantA, runID, "secret-event", []byte("tenant-a-secret"))); err != nil {
			t.Fatalf("Append A %d: %v", i, err)
		}
	}

	// Tenant A must receive its 3 events.
	for i := 0; i < 3; i++ {
		select {
		case e := <-chA:
			if e.TenantID != tenantA {
				t.Fatalf("ADVERSARY BREAK: tenant A received event with TenantID %q; want %q", e.TenantID, tenantA)
			}
			if string(e.Payload) != "tenant-a-secret" {
				t.Fatalf("ADVERSARY BREAK: tenant A received wrong payload %q", string(e.Payload))
			}
		case <-time.After(time.Second):
			t.Fatalf("ADVERSARY BREAK: tenant A did not receive event %d", i)
		}
	}

	// Adversary assertion: tenant B must NOT receive tenant A's events.
	select {
	case e, open := <-chB:
		if open {
			t.Fatalf("ADVERSARY BREAK: tenant B received tenant A's event: %+v (cross-tenant leak)", e)
		}
		// A closed channel is acceptable here only if tenant B had no
		// events of its own; the contract is "no events delivered to
		// the wrong tenant". Closing the channel without delivering
		// cross-tenant data is fine. But we expect an open, blocking
		// channel since tenant B has no events.
		t.Log("tenant B channel closed (acceptable: no cross-tenant delivery)")
	case <-time.After(200 * time.Millisecond):
		// No event delivered to tenant B within the window — correct.
		// Cross-tenant access is denied.
	}
}

// TestAdversary_B29_CrossTenantReadReturnsEmpty verifies that tenant B's
// Read of tenant A's run returns empty (not an error, not tenant A's
// events).
func TestAdversary_B29_CrossTenantReadReturnsEmpty(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenantA := "tenant-a"
	tenantB := "tenant-b"
	runID := "run-x"

	// Tenant A writes events.
	for i := 0; i < 5; i++ {
		if _, err := store.Append(ctx, mkEvent(tenantA, runID, "secret", []byte("tenant-a-secret"))); err != nil {
			t.Fatalf("Append A %d: %v", i, err)
		}
	}

	// Adversary: tenant B reads tenant A's runID. Must return empty.
	events, err := store.Read(ctx, tenantB, runID, 0, 100)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: cross-tenant Read returned error: %v (want nil err, empty slice)", err)
	}
	if len(events) != 0 {
		t.Fatalf("ADVERSARY BREAK: cross-tenant Read returned %d events; want 0 (no leak)", len(events))
	}
	for _, e := range events {
		if e.TenantID == tenantA {
			t.Fatalf("ADVERSARY BREAK: cross-tenant Read returned tenant A event %+v", e)
		}
	}

	// LatestSequence for tenant B reading tenant A's run must be 0 (no
	// existence leak).
	latest, err := store.LatestSequence(ctx, tenantB, runID)
	if err != nil {
		t.Fatalf("ADVERSARY BREAK: cross-tenant LatestSequence returned error: %v", err)
	}
	if latest != 0 {
		t.Fatalf("ADVERSARY BREAK: cross-tenant LatestSequence = %d; want 0 (no existence leak)", latest)
	}

	// Sanity: tenant A still reads its own 5 events (no corruption).
	allA, err := store.Read(ctx, tenantA, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read A: %v", err)
	}
	if len(allA) != 5 {
		t.Fatalf("Read A = %d events; want 5 (cross-tenant read must not corrupt index)", len(allA))
	}
}

// TestAdversary_B29_CrossTenantSameRunIDIsolated verifies that two tenants
// using the SAME runID string are fully isolated — events from one never
// leak to the other even on the live Subscribe channel.
func TestAdversary_B29_CrossTenantSameRunIDIsolated(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenantA := "tenant-a"
	tenantB := "tenant-b"
	runID := "collision-run"

	chA, err := store.Subscribe(ctx, tenantA, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}
	chB, err := store.Subscribe(ctx, tenantB, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}

	// Tenant A appends a sentinel-bearing event.
	sentinel := []byte("ADVERSARY_B29_TENANT_A_SECRET_DO_NOT_LEAK")
	if _, err := store.Append(ctx, mkEvent(tenantA, runID, "a-event", sentinel)); err != nil {
		t.Fatalf("Append A: %v", err)
	}
	// Tenant B appends its own event.
	if _, err := store.Append(ctx, mkEvent(tenantB, runID, "b-event", []byte("b"))); err != nil {
		t.Fatalf("Append B: %v", err)
	}

	// Tenant A receives exactly its own event (not tenant B's).
	select {
	case e := <-chA:
		if e.TenantID != tenantA {
			t.Fatalf("ADVERSARY BREAK: tenant A received event with TenantID %q; want %q", e.TenantID, tenantA)
		}
		if string(e.Payload) != string(sentinel) {
			t.Fatalf("ADVERSARY BREAK: tenant A received wrong payload %q; want its own sentinel", string(e.Payload))
		}
	case <-time.After(time.Second):
		t.Fatal("ADVERSARY BREAK: tenant A did not receive its own event")
	}
	// Tenant A must NOT receive tenant B's event next.
	select {
	case e, open := <-chA:
		if open {
			t.Fatalf("ADVERSARY BREAK: tenant A received a second event %+v — likely tenant B's (cross-tenant leak)", e)
		}
	case <-time.After(200 * time.Millisecond):
		// Correct — no second event for tenant A.
	}

	// Tenant B receives exactly its own event (not tenant A's sentinel).
	select {
	case e := <-chB:
		if e.TenantID != tenantB {
			t.Fatalf("ADVERSARY BREAK: tenant B received event with TenantID %q; want %q", e.TenantID, tenantB)
		}
		if string(e.Payload) == string(sentinel) {
			t.Fatalf("ADVERSARY BREAK: tenant B received tenant A's sentinel payload (cross-tenant leak)")
		}
	case <-time.After(time.Second):
		t.Fatal("ADVERSARY BREAK: tenant B did not receive its own event")
	}
}
