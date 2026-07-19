package trigger

// B29-T08 ADVERSARY TEST — terminal cleanup removes all run resources.
//
// Attack: an adversary invokes a run, lets it reach terminal, then inspects
// the system for orphaned resources: leaked in-memory run entries, leaked
// subscriber channels, leaked WAL files (if cleanup purges them), and
// lingering state that could be reused to resurrect a terminal run.
//
// Invariant under test:
//   - After a terminal state, the durable event store can still replay
//     events for audit (Read returns the committed events).
//   - No live subscriber channels remain for the run after Close.
//   - No leaked in-memory run state survives a Close + reopen cycle: the
//     reopened store reads the same events from disk (no double-counting,
//     no phantom runs).
//   - A terminal run is NOT re-executed when re-subscribed (the synthetic
//     admission path does not flip a terminal run back to RUNNING).

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
)

// TestAdversary_B29_TerminalCleanupRemovesLiveResources verifies that
// after a run reaches terminal state and the store is closed:
//   - No orphaned subscriber channels remain (the in-memory runState map
//     is cleared on Close).
//   - The durable WAL can still be replayed from disk for audit.
//   - Reopening the store at the same state dir reconstructs the same
//     events (no phantom or duplicate runs).
func TestAdversary_B29_TerminalCleanupRemovesLiveResources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	ctx := context.Background()
	tenant := "tenant-cleanup"
	runID := "run-cleanup"

	// Append lifecycle events including a terminal.
	for _, et := range []string{string(EventRunCreated), string(EventRunStarted), "progress", string(EventRunSucceeded)} {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, et, []byte(et))); err != nil {
			t.Fatalf("Append %s: %v", et, err)
		}
	}

	// Attach a live subscriber to prove Close tears it down.
	ch, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Drain replayed events so the subscriber is not stalled.
	var seen []string
	for len(seen) < 4 {
		select {
		case e := <-ch:
			seen = append(seen, e.Type)
		case <-time.After(time.Second):
			t.Fatalf("ADVERSARY BREAK: subscriber did not receive replay event %d (got %d: %v)", len(seen), len(seen), seen)
		}
	}

	// Close the store — this must tear down ALL live subscriber channels
	// and clear the in-memory runState map. After Close, no live
	// resources remain for the run.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Adversary assertion: the subscriber channel must be CLOSED after
	// Close. An open channel here would be a leaked live resource.
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("ADVERSARY BREAK: subscriber channel still open after Close — leaked live resource")
		}
		// Correct: channel is closed.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ADVERSARY BREAK: subscriber channel did not close after Close — leaked live resource (deadlock)")
	}

	// Audit contract: the durable WAL still exists on disk and can be
	// replayed. Reopening the store at the same state dir must
	// reconstruct the same events — no phantom runs, no duplicates.
	store2, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore 2: %v", err)
	}
	defer func() { _ = store2.Close() }()

	events, err := store2.Read(ctx, tenant, runID, 0, 100)
	if err != nil {
		t.Fatalf("Read after reopen: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("ADVERSARY BREAK: after reopen Read = %d events; want 4 (durable audit must survive cleanup)", len(events))
	}
	// Sequences must be contiguous 1..4 — no duplicates from the reopen.
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("ADVERSARY BREAK: events[%d].Sequence = %d; want %d (no phantom/duplicate runs after reopen)", i, e.Sequence, i+1)
		}
	}
	// The terminal event must still be the last one.
	if events[len(events)-1].Type != string(EventRunSucceeded) {
		t.Fatalf("ADVERSARY BREAK: last event after reopen = %q; want %q (terminal must survive)", events[len(events)-1].Type, string(EventRunSucceeded))
	}
}

// TestAdversary_B29_TerminalRunNotResurrectedOnResubscribe verifies that
// re-subscribing to a terminal run replays the committed events and does
// NOT re-execute the run (the runStore entry stays terminal).
func TestAdversary_B29_TerminalRunNotResurrectedOnResubscribe(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestDurableService(t)
	defer cleanup()

	stream := &captureInvokeStream{ctx: context.Background()}
	if err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "test-agent"}, stream); err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}
	if len(stream.responses) != 2 {
		t.Fatalf("InvokeStream responses = %d; want 2", len(stream.responses))
	}
	runID := stream.responses[0].GetRun().GetRunId()

	// The run must be terminal (SUCCEEDED) in the runStore.
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatal("ADVERSARY BREAK: run not found in runStore")
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("ADVERSARY BREAK: run status = %s; want SUCCEEDED (terminal)", entry.Status)
	}

	// Adversary: re-subscribe with cursor 0. This must replay events
	// (audit) and NOT re-execute the run. The runStore entry must stay
	// SUCCEEDED — no resurrection to PENDING/RUNNING.
	ch, err := store.Subscribe(context.Background(), defaultTriggerTenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe replay: %v", err)
	}
	var replayed []string
	for len(replayed) < 2 {
		select {
		case e := <-ch:
			replayed = append(replayed, e.Type)
		case <-time.After(time.Second):
			t.Fatalf("ADVERSARY BREAK: replay did not deliver 2 events (got %d: %v)", len(replayed), replayed)
		}
	}
	// The re-subscribe must NOT have flipped the runStore entry back to
	// a non-terminal state.
	entry2, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatal("ADVERSARY BREAK: run vanished from runStore after resubscribe")
	}
	if entry2.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("ADVERSARY BREAK: run resurrected after resubscribe: status = %s; want SUCCEEDED (no re-execution)", entry2.Status)
	}
}

// TestAdversary_B29_TerminalCleanupNoOrphanedWAL verifies that the WAL
// file for a terminal run is a regular file (not a symlink), has safe
// permissions (0600), and is the ONLY file under the tenant dir — no
// orphaned temp or partial files leak after the terminal commit.
func TestAdversary_B29_TerminalCleanupNoOrphanedWAL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state", "events")
	store, err := NewDurableEventStore(stateDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	tenant := "tenant-wal"
	runID := "run-wal"
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	// Append a terminal event.
	if _, err := store.Append(ctx, mkEvent(tenant, runID, string(EventRunSucceeded), []byte("done"))); err != nil {
		t.Fatalf("Append terminal: %v", err)
	}

	// The tenant dir must contain exactly ONE .wal file — no orphaned
	// temp files, no partial writes.
	tenantDir := filepath.Join(stateDir, tenant)
	entries, err := os.ReadDir(tenantDir)
	if err != nil {
		t.Fatalf("ReadDir tenant: %v", err)
	}
	var walFiles []string
	for _, e := range entries {
		if e.IsDir() {
			t.Fatalf("ADVERSARY BREAK: unexpected subdirectory in tenant dir: %s (orphaned resource)", e.Name())
		}
		if e.Name()[len(e.Name())-4:] == walSuffix {
			walFiles = append(walFiles, e.Name())
		} else {
			t.Fatalf("ADVERSARY BREAK: orphaned non-WAL file in tenant dir: %s (cleanup must remove stray files)", e.Name())
		}
	}
	if len(walFiles) != 1 {
		t.Fatalf("ADVERSARY BREAK: tenant dir has %d WAL files; want 1 (no orphans)", len(walFiles))
	}
	// The single WAL file must be a regular file with 0600 perms.
	walPath := filepath.Join(tenantDir, walFiles[0])
	fi, err := os.Lstat(walPath)
	if err != nil {
		t.Fatalf("Lstat WAL: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("ADVERSARY BREAK: WAL file is a symlink (orphaned/unsafe resource)")
	}
	if !fi.Mode().IsRegular() {
		t.Fatal("ADVERSARY BREAK: WAL file is not a regular file")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("ADVERSARY BREAK: WAL file mode = %#o; want 0600 (no group/other bits)", fi.Mode().Perm())
	}
}
