package trigger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDurableCrossTenantReadReturnsEmpty verifies that tenant A cannot read
// tenant B's events. Cross-tenant reads return empty (not an error).
func TestDurableCrossTenantReadReturnsEmpty(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	// Tenant A writes events.
	if _, err := store.Append(ctx, mkEvent("tenant-a", "run-x", "e", []byte("secret-a"))); err != nil {
		t.Fatalf("Append A: %v", err)
	}
	if _, err := store.Append(ctx, mkEvent("tenant-a", "run-x", "e2", []byte("secret-a2"))); err != nil {
		t.Fatalf("Append A2: %v", err)
	}

	// Tenant B reads tenant A's run — must get empty.
	events, err := store.Read(ctx, "tenant-b", "run-x", 0, 100)
	if err != nil {
		t.Fatalf("Read cross-tenant returned error: %v (want nil err, empty slice)", err)
	}
	if len(events) != 0 {
		t.Fatalf("cross-tenant Read returned %d events; want 0 (silent-empty)", len(events))
	}

	// Tenant B's LatestSequence for tenant A's run must be 0.
	latest, err := store.LatestSequence(ctx, "tenant-b", "run-x")
	if err != nil {
		t.Fatalf("LatestSequence cross-tenant returned error: %v", err)
	}
	if latest != 0 {
		t.Fatalf("cross-tenant LatestSequence = %d; want 0", latest)
	}

	// Tenant B subscribes to tenant A's run. Because the durable store keys
	// subscribers by (tenant, run), tenant B's subscriber is registered under
	// (tenant-b, run-x) and will never receive events appended to
	// (tenant-a, run-x). The channel is open and blocking (not an error) —
	// the "returns empty" semantic means "no events delivered", which a
	// blocking open channel satisfies for a subscriber that attached before
	// any events exist for its own key.
	ch, err := store.Subscribe(ctx, "tenant-b", "run-x", 0)
	if err != nil {
		t.Fatalf("Subscribe cross-tenant returned error: %v", err)
	}
	select {
	case e, open := <-ch:
		if open {
			t.Fatalf("cross-tenant Subscribe delivered an event: %+v", e)
		}
		t.Fatal("cross-tenant Subscribe channel closed — should be open (no events for tenant-b/run-x)")
	case <-defaultShortTimer():
		// No event delivered within the window — correct. Tenant B has no
		// events for (tenant-b, run-x), so its subscriber correctly sees
		// nothing.
	}
}

// TestDurableCrossTenantSubscribeIsolated verifies that a tenant-A subscriber
// never receives an event appended for tenant B even if the runID matches.
func TestDurableCrossTenantSubscribeIsolated(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	// Both tenants use the same runID string — isolation must be by tenant.
	chA, err := store.Subscribe(ctx, "tenant-a", "shared-run", 0)
	if err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}

	// Tenant B appends an event. Tenant A must NOT receive it.
	if _, err := store.Append(ctx, mkEvent("tenant-b", "shared-run", "from-b", []byte("b-secret"))); err != nil {
		t.Fatalf("Append B: %v", err)
	}
	select {
	case e, open := <-chA:
		if open {
			t.Fatalf("tenant A received tenant B's event: %+v", e)
		}
		t.Fatal("tenant A channel closed unexpectedly")
	case <-defaultShortTimer():
		// No event delivered — correct.
	}

	// Tenant A appends its own event — that one must arrive.
	if _, err := store.Append(ctx, mkEvent("tenant-a", "shared-run", "from-a", []byte("a"))); err != nil {
		t.Fatalf("Append A: %v", err)
	}
	select {
	case e := <-chA:
		if e.TenantID != "tenant-a" {
			t.Fatalf("tenant A received event with TenantID %q; want %q", e.TenantID, "tenant-a")
		}
		if e.Type != "from-a" {
			t.Fatalf("tenant A received wrong event type %q; want %q", e.Type, "from-a")
		}
	case <-defaultShortTimer():
		t.Fatal("tenant A did not receive its own event")
	}
}

// TestDurableResubscribeNoDuplicates verifies that re-subscribing with the
// same after_sequence does not create duplicate event deliveries.
func TestDurableResubscribeNoDuplicates(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-dedupe"
	runID := "run-dedupe"
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// First subscription from cursor 0.
	ch1, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe 1: %v", err)
	}
	var seqs1 []int64
	for i := 0; i < 3; i++ {
		e := <-ch1
		seqs1 = append(seqs1, e.Sequence)
	}

	// Second subscription from cursor 0 again — must receive the same 3
	// events exactly once each (no duplication of the WAL replay).
	ch2, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe 2: %v", err)
	}
	var seqs2 []int64
	for i := 0; i < 3; i++ {
		select {
		case e := <-ch2:
			seqs2 = append(seqs2, e.Sequence)
		case <-defaultShortTimer():
			t.Fatalf("timed out waiting for replay event %d", i)
		}
	}
	if len(seqs2) != 3 {
		t.Fatalf("re-subscribe delivered %d events; want 3", len(seqs2))
	}
	for i := range seqs1 {
		if seqs1[i] != seqs2[i] {
			t.Fatalf("re-subscribe order differs at %d: %d vs %d", i, seqs1[i], seqs2[i])
		}
	}
}

// TestDurableWALFilePermissions verifies the WAL file is created at the
// expected path with mode 0600 and the tenant directory with 0700.
func TestDurableWALFilePermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	tenant := "tenant-perm"
	runID := "run-perm"
	if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
		t.Fatalf("Append: %v", err)
	}

	walPath := filepath.Join(stateDir, "tenant-perm", "run-perm"+walSuffix)
	fi, err := os.Lstat(walPath)
	if err != nil {
		t.Fatalf("Lstat WAL: %v", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("WAL file mode = %#o; want 0600 (no group/other bits)", fi.Mode().Perm())
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("WAL file is a symlink — must be a regular file")
	}
	if !fi.Mode().IsRegular() {
		t.Fatal("WAL file is not a regular file")
	}

	tenantDir := filepath.Join(stateDir, "tenant-perm")
	di, err := os.Lstat(tenantDir)
	if err != nil {
		t.Fatalf("Lstat tenant dir: %v", err)
	}
	if di.Mode().Perm()&0o077 != 0 {
		t.Fatalf("tenant dir mode = %#o; want 0700 (no group/other bits)", di.Mode().Perm())
	}
}

// TestDurableEmptyTenantRunReturnsEmpty verifies that reading or subscribing
// to an unknown tenant/run returns empty (not an error).
func TestDurableEmptyTenantRunReturnsEmpty(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	events, err := store.Read(ctx, "unknown-tenant", "unknown-run", 0, 100)
	if err != nil {
		t.Fatalf("Read unknown: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("Read unknown returned %d events; want 0", len(events))
	}
	latest, err := store.LatestSequence(ctx, "unknown-tenant", "unknown-run")
	if err != nil {
		t.Fatalf("LatestSequence unknown: %v", err)
	}
	if latest != 0 {
		t.Fatalf("LatestSequence unknown = %d; want 0", latest)
	}
	ch, err := store.Subscribe(ctx, "unknown-tenant", "unknown-run", 0)
	if err != nil {
		t.Fatalf("Subscribe unknown: %v", err)
	}
	// A subscriber to a (tenant, run) with no events gets an open, blocking
	// channel — it will receive live events if any are appended to that key.
	// Since no events are appended here, the channel correctly blocks.
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("Subscribe unknown delivered an unexpected event")
		}
		// A closed channel is acceptable if the store was closed; here it
		// should be open and blocking, so reaching this branch is a failure.
		t.Fatal("Subscribe unknown channel is closed — should be open and blocking (no events yet)")
	case <-defaultShortTimer():
		// No event within the window — correct for a run with no events.
	}
}

// defaultShortTimer returns a 1-second timer for test select branches.
func defaultShortTimer() <-chan time.Time {
	return time.After(time.Second)
}
