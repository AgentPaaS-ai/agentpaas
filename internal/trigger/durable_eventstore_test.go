package trigger

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// newTestDurableStore creates a DurableEventStore rooted in a temp dir and
// returns the store, the state dir, and a cleanup function.
func newTestDurableStore(t *testing.T) (*DurableEventStore, string, func()) {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	s, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	cleanup := func() {
		_ = s.Close()
	}
	return s, stateDir, cleanup
}

func mkEvent(tenantID, runID, eventType string, payload []byte) port.Event {
	return port.Event{
		TenantID:  tenantID,
		RunID:     runID,
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}
}

// TestDurableAppendAndRead verifies that appended events are read back in
// order with correct sequence numbers.
func TestDurableAppendAndRead(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-a"
	runID := "run-1"

	seq1, err := store.Append(ctx, mkEvent(tenant, runID, "created", []byte("c1")))
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if seq1 != 1 {
		t.Fatalf("seq1 = %d; want 1", seq1)
	}
	seq2, err := store.Append(ctx, mkEvent(tenant, runID, "started", []byte("s2")))
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if seq2 != 2 {
		t.Fatalf("seq2 = %d; want 2", seq2)
	}
	seq3, err := store.Append(ctx, mkEvent(tenant, runID, "succeeded", []byte("x3")))
	if err != nil {
		t.Fatalf("Append 3: %v", err)
	}
	if seq3 != 3 {
		t.Fatalf("seq3 = %d; want 3", seq3)
	}

	events, err := store.Read(ctx, tenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d; want 3", len(events))
	}
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("events[%d].Sequence = %d; want %d", i, e.Sequence, i+1)
		}
	}
	if string(events[0].Payload) != "c1" {
		t.Fatalf("events[0].Payload = %q; want %q", string(events[0].Payload), "c1")
	}

	latest, err := store.LatestSequence(ctx, tenant, runID)
	if err != nil {
		t.Fatalf("LatestSequence: %v", err)
	}
	if latest != 3 {
		t.Fatalf("latest = %d; want 3", latest)
	}
}

// TestDurableReadFromCursor verifies Read returns only events after the
// given sequence.
func TestDurableReadFromCursor(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-a"
	runID := "run-cursor"
	for i := 0; i < 5; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("payload"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	events, err := store.Read(ctx, tenant, runID, 2, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(events) = %d; want 3 (after seq 2)", len(events))
	}
	if events[0].Sequence != 3 {
		t.Fatalf("events[0].Sequence = %d; want 3", events[0].Sequence)
	}
}
