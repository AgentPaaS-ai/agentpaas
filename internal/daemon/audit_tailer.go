package daemon

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// auditTailer tails the harness audit JSONL file during a run and
// feeds each new record to the daemon's event bus for real-time
// dashboard visibility. Post-run audit chain ingestion remains the
// responsibility of ingestHarnessAudit.
type auditTailer struct {
	path     string // path to harness-audit.jsonl
	runID    string
	writer   *audit.AuditWriter
	index    *audit.SQLiteIndexer
	eventBus *trigger.EventBus
	stopCh   chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	lastOffset int64
}

// newAuditTailer creates a tailer for the given audit file.
func newAuditTailer(path, runID string, writer *audit.AuditWriter,
	index *audit.SQLiteIndexer, bus *trigger.EventBus) *auditTailer {
	return &auditTailer{
		path:     path,
		runID:    runID,
		writer:   writer,
		index:    index,
		eventBus: bus,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// start begins tailing the audit file in a goroutine.
// It polls every 500ms for new content.
func (t *auditTailer) start() {
	go t.run()
}

// stop signals the tailer to stop and waits for it to finish.
// Safe to call multiple times — the cleanup path (cleanupRun) and Stop()
// can both call this on the same run.
func (t *auditTailer) stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
	<-t.done
}

// run is the main tail loop.
func (t *auditTailer) run() {
	defer close(t.done)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			// Final read before stopping.
			t.readNewRecords()
			return
		case <-ticker.C:
			t.readNewRecords()
		}
	}
}

// readNewRecords reads any new lines appended since lastOffset and
// publishes them to the event bus for real-time dashboard visibility.
func (t *auditTailer) readNewRecords() {
	if t.path == "" {
		return
	}

	f, err := os.Open(t.path)
	if err != nil {
		return // file may not exist yet
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(t.lastOffset, io.SeekStart); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	var newRecords []audit.AuditRecord
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record audit.AuditRecord
		if err := json.Unmarshal(line, &record); err != nil {
			continue // skip malformed lines
		}
		if record.Payload == nil {
			record.Payload = make(map[string]interface{})
		}
		if _, ok := record.Payload["run_id"]; !ok {
			record.Payload["run_id"] = t.runID
		}
		newRecords = append(newRecords, record)
	}

	if offset, err := f.Seek(0, io.SeekCurrent); err == nil {
		t.lastOffset = offset
	}

	if len(newRecords) == 0 {
		return
	}

	// Do NOT append to audit chain — ingestHarnessAudit does that post-run
	// with hash chain verification. The tailer only publishes to event bus
	// for real-time dashboard visibility.
	if t.eventBus != nil {
		for _, record := range newRecords {
			t.eventBus.Publish(t.runID, trigger.EventType(record.EventType), record.Payload)
		}
	}
}