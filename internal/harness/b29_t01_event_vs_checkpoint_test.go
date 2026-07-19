package harness

// B29-T01 CHARACTERIZATION TEST — freezes current behavior; B29
// replacement tasks are expected to update or fail these tests.
//
// Observation 4: External event subscription (EventBus) is NOT the
// durable source of truth for run state. The B27 progress journal
// (file-backed JSONL with HMAC) IS the durable store. After process
// restart (simulated by creating new instances):
//   - Events in EventBus are gone.
//   - Progress journal records on disk survive re-open.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// TestProgressJournalSurvivesRestart proves that progress journal records
// written to disk survive closing and re-opening the journal file.
// This characterizes the B27 checkpoint as the durable source of truth.
func TestProgressJournalSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journals", "attempt1.jsonl")
	key := []byte("test-key-32-bytes-long-enough!!")
	identity := progressIdentity{
		WorkflowID:  "wf-survive",
		NodeID:      "node1",
		RunID:       "run-survive",
		AttemptID:   "attempt-survive",
		LeaseID:     "lease1",
		LeaseExpiry: time.Now().Add(time.Hour),
	}

	// Phase 1: Write to journal (simulating agent runtime).
	w1, err := newProgressJournalWriter(journalPath, key, identity)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	_, err = w1.append("evt-survive-1", "phase1", []string{"work done"}, nil, nil, "committed", false, "", "")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := w1.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Phase 2: "Restart" — verify the file still exists on disk.
	if _, err := os.Stat(journalPath); os.IsNotExist(err) {
		t.Fatal("journal file does not exist on disk after close (simulated process crash)")
	}

	// Phase 3: Re-open and verify the record survives.
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		t.Fatal("journal is empty after restart")
	}
	if len(lines) != 1 {
		t.Fatalf("journal has %d lines after restart; want 1", len(lines))
	}

	var rec progressJournalRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Phase != "phase1" {
		t.Fatalf("restored phase = %q; want phase1", rec.Phase)
	}
	if !verifyJournalRecord(&rec, key) {
		t.Fatal("HMAC verification failed on restored record")
	}
}

// TestEventBusEventsDoNotSurviveRestart proves that EventBus events
// are lost when a new EventBus is created (equivalent to process restart).
// This is the counterpart to the journal survival test above.
func TestEventBusEventsDoNotSurviveRestart(t *testing.T) {
	t.Parallel()

	bus1 := trigger.NewEventBus()
	bus1.RegisterRun("run-event-loss")
	bus1.Publish("run-event-loss", trigger.EventRunCreated, nil)
	bus1.Publish("run-event-loss", trigger.EventRunSucceeded, nil)

	// Verify events exist in bus1.
	if events := bus1.GetEvents("run-event-loss"); len(events) != 2 {
		t.Fatalf("bus1 has %d events; want 2", len(events))
	}

	// "Restart" — new EventBus.
	bus2 := trigger.NewEventBus()
	if events := bus2.GetEvents("run-event-loss"); events != nil {
		t.Fatalf("bus2 has %d events after restart; want nil", len(events))
	}

	ch, cancel := bus2.Subscribe("run-event-loss", 0)
	defer cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel on new bus for old run should be closed immediately")
		}
	}
}

// TestJournalIsDurableSourceOfTruth characterizes that the B27
// progress journal IS the durable source of truth for run state,
// while the EventBus is ephemeral/in-memory only.
//
// The journal is file-backed (disk), survives close/re-open.
// The EventBus is in-memory only, does NOT survive restart.
func TestJournalIsDurableSourceOfTruth(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "truth", "j.jsonl")
	key := []byte("durable-truth-key-32-bytes-here!")
	identity := progressIdentity{
		WorkflowID:  "wf-truth",
		NodeID:      "n1",
		RunID:       "run-truth",
		AttemptID:   "a1",
		LeaseID:     "l1",
		LeaseExpiry: time.Now().Add(time.Hour),
	}

	// Write a journal record (durable).
	w, err := newProgressJournalWriter(journalPath, key, identity)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	_, err = w.append("evt-truth", "executing", []string{"task A"}, []string{"task B"}, nil, "done A", true, "d1", "d2")
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := w.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Verify journal record survives.
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("read journal after close: %v", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Fatal("journal record did NOT survive close")
	}

	// Meanwhile, publish to EventBus (ephemeral).
	bus := trigger.NewEventBus()
	bus.RegisterRun("run-truth")
	bus.Publish("run-truth", trigger.EventRunProgress, nil)

	// New EventBus = events gone.
	busNew := trigger.NewEventBus()
	if events := busNew.GetEvents("run-truth"); events != nil {
		t.Fatalf("EventBus events survived restart; they should not")
	}

	// Conclusion: the journal (disk) is the durable source of truth.
	// The EventBus (in-memory) is NOT durable across restarts.
}

// TestProductionJournalPathCharacterization documents where the
// production B27 journal is expected to live. In tests we use
// t.TempDir(), but the production code writes to a path under
// ~/.agentpaas/state/. This test is purely documentary — it
// asserts the journal writer API and the fact that it writes to
// a real OS file.
func TestProductionJournalWritesToDisk(t *testing.T) {
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "prod", "journal.jsonl")

	w, err := newProgressJournalWriter(journalPath,
		[]byte("prod-key-32-bytes-long-enough!!"),
		progressIdentity{RunID: "r1", AttemptID: "a1"},
	)
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	defer func() { _ = w.close() }()

	_, err = w.append("evt-prod", "phase", nil, nil, nil, "", false, "", "")
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	// Verify the file exists and has content.
	stat, err := os.Stat(journalPath)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatal("journal file is empty")
	}

	// The journal Path method returns the disk path.
	if got := w.JournalPath(); got != journalPath {
		t.Fatalf("JournalPath = %q; want %q", got, journalPath)
	}
}
