package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/trigger"
)

func newTestAuditTailer(t *testing.T, path, runID string, bus *trigger.EventBus) *auditTailer {
	t.Helper()
	auditPath := filepath.Join(t.TempDir(), "daemon-audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	tailer := newAuditTailer(path, runID, writer, nil, bus)
	return tailer
}

func waitForEvent(t *testing.T, bus *trigger.EventBus, runID string, eventType trigger.EventType, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, event := range bus.GetEvents(runID) {
			if event.Type == eventType {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for event %q on run %q", eventType, runID)
}

func countAuditRecords(t *testing.T, path string) int {
	t.Helper()
	records, err := readAuditJSONL(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("readAuditJSONL: %v", err)
	}
	return len(records)
}

func TestAuditTailer_PublishesNewRecords(t *testing.T) {
	runID := "run-tail-test"
	dir := t.TempDir()
	auditFile := filepath.Join(dir, "harness-audit.jsonl")

	bus := trigger.NewEventBus()
	bus.RegisterRun(runID)

	tailer := newTestAuditTailer(t, auditFile, runID, bus)
	tailer.start()
	t.Cleanup(tailer.stop)

	writeHarnessAuditChain(t, auditFile, []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "egress_allowed",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "api.example.com"},
		},
	})

	waitForEvent(t, bus, runID, "egress_allowed", 2*time.Second)

	events := bus.GetEvents(runID)
	var found bool
	for _, event := range events {
		if event.Type != "egress_allowed" {
			continue
		}
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event data type = %T, want map[string]interface{}", event.Data)
		}
		if data["destination"] != "api.example.com" {
			t.Fatalf("destination = %v, want api.example.com", data["destination"])
		}
		if data["run_id"] != runID {
			t.Fatalf("run_id = %v, want %q", data["run_id"], runID)
		}
		found = true
	}
	if !found {
		t.Fatal("egress_allowed event not published")
	}
}

func TestAuditTailer_HandlesMissingFile(t *testing.T) {
	runID := "run-missing-file"
	missingPath := filepath.Join(t.TempDir(), "does-not-exist", "harness-audit.jsonl")

	bus := trigger.NewEventBus()
	bus.RegisterRun(runID)

	tailer := newTestAuditTailer(t, missingPath, runID, bus)
	tailer.start()

	time.Sleep(600 * time.Millisecond)
	tailer.stop()

	if events := bus.GetEvents(runID); len(events) != 0 {
		t.Fatalf("events = %d, want 0 for missing file", len(events))
	}
}

func TestAuditTailer_HandlesMalformedLines(t *testing.T) {
	runID := "run-malformed"
	dir := t.TempDir()
	auditFile := filepath.Join(dir, "harness-audit.jsonl")

	if err := os.WriteFile(auditFile, []byte("not valid json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	writeHarnessAuditChain(t, auditFile, []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:06Z",
			EventType: "egress_denied",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "evil.com"},
		},
	})

	bus := trigger.NewEventBus()
	bus.RegisterRun(runID)

	tailer := newTestAuditTailer(t, auditFile, runID, bus)
	tailer.readNewRecords()

	events := bus.GetEvents(runID)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != "egress_denied" {
		t.Fatalf("event type = %q, want egress_denied", events[0].Type)
	}
}

func TestAuditTailer_StopFinalRead(t *testing.T) {
	runID := "run-stop-final"
	dir := t.TempDir()
	auditFile := filepath.Join(dir, "harness-audit.jsonl")

	bus := trigger.NewEventBus()
	bus.RegisterRun(runID)

	tailer := newTestAuditTailer(t, auditFile, runID, bus)
	tailer.start()

	writeHarnessAuditChain(t, auditFile, []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "egress_allowed",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "first.example.com"},
		},
		{
			Timestamp: "2026-01-02T03:04:06Z",
			EventType: "egress_denied",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "blocked.example.com"},
		},
	})

	tailer.stop()

	events := bus.GetEvents(runID)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "egress_allowed" {
		t.Fatalf("first event type = %q, want egress_allowed", events[0].Type)
	}
	if events[1].Type != "egress_denied" {
		t.Fatalf("second event type = %q, want egress_denied", events[1].Type)
	}
}

func TestAuditTailer_DoesNotAppendToAuditChain(t *testing.T) {
	runID := "run-no-append"
	dir := t.TempDir()
	harnessFile := filepath.Join(dir, "harness-audit.jsonl")
	daemonAuditPath := filepath.Join(dir, "daemon-audit.jsonl")

	writer, err := audit.NewAuditWriter(daemonAuditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	initialCount := countAuditRecords(t, daemonAuditPath)

	bus := trigger.NewEventBus()
	bus.RegisterRun(runID)

	tailer := newAuditTailer(harnessFile, runID, writer, nil, bus)
	tailer.start()
	t.Cleanup(tailer.stop)

	writeHarnessAuditChain(t, harnessFile, []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "egress_allowed",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "api.example.com"},
		},
	})

	waitForEvent(t, bus, runID, "egress_allowed", 2*time.Second)

	finalCount := countAuditRecords(t, daemonAuditPath)
	if finalCount != initialCount {
		t.Fatalf("daemon audit records = %d, want unchanged %d", finalCount, initialCount)
	}
}