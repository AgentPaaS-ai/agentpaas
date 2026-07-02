# Block 14B-T03: Real-Time Egress Visibility

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. The harness emits
egress audit events (egress_allowed, egress_denied) via a FileAuditAppender that
writes to a JSONL file inside the container. The file is bind-mounted to the host
at `$HOME/.local/state/agentpaas/runs/<runID>/harness-audit/harness-audit.jsonl`.

Currently, the daemon ONLY reads this file AFTER the run completes (in Stop() →
ingestHarnessAudit). During a long-running agent, egress attempts are invisible
to the dashboard.

## What to Implement

### Part 1: Create a real-time audit tailer

Create `internal/daemon/audit_tailer.go`:

```go
package daemon

// auditTailer tails the harness audit JSONL file during a run and
// feeds each new record to the daemon's audit chain + event bus.
// This enables real-time egress visibility in the dashboard.
type auditTailer struct {
    path       string    // path to harness-audit.jsonl
    runID      string
    writer     *audit.AuditWriter
    index      *audit.SQLiteIndexer
    eventBus   *trigger.EventBus
    otelStore  *otel.Store
    stopCh     chan struct{}
    done       chan struct{}
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
func (t *auditTailer) stop() {
    close(t.stopCh)
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

// readNewRecords reads any new lines appended since lastOffset,
// verifies the hash chain, appends to the daemon audit chain,
// and publishes events.
func (t *auditTailer) readNewRecords() {
    // ... implementation details below
}
```

### Part 2: readNewRecords implementation

```go
func (t *auditTailer) readNewRecords() {
    if t.path == "" {
        return
    }

    // Open the file, seek to lastOffset, read new lines.
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
        // Ensure run_id is present
        if record.Payload == nil {
            record.Payload = make(map[string]interface{})
        }
        if _, ok := record.Payload["run_id"]; !ok {
            record.Payload["run_id"] = t.runID
        }
        newRecords = append(newRecords, record)
    }

    // Update offset to current position.
    if offset, err := f.Seek(0, io.SeekCurrent); err == nil {
        t.lastOffset = offset
    }

    if len(newRecords) == 0 {
        return
    }

    // Append to daemon audit chain.
    for _, record := range newRecords {
        if err := t.writer.Append(record); err != nil {
            fmt.Fprintf(os.Stderr, "audit tailer: append (%s): %v\n", t.runID, err)
        }

        // Publish to event bus for dashboard timeline.
        if t.eventBus != nil {
            // Map egress events to run timeline events.
            t.eventBus.Publish(t.runID, record.EventType, record.Payload)
        }
    }

    // Refresh SQLite index periodically (not every poll — too expensive).
    // The index is refreshed on Stop anyway. For real-time, we can skip
    // or use a throttled refresh.
}
```

### Part 3: Start/stop tailer in Run/Stop handlers

In `internal/daemon/control_handlers.go`, Run handler:

After creating the trackedRun and before auto-invoke:

```go
// Start real-time audit tailer for live egress visibility.
auditPath := filepath.Join(hostAuditDir, "harness-audit.jsonl")
tracked.Tailer = newAuditTailer(auditPath, runID, s.auditWriter, s.auditIndex, s.eventBus)
tracked.Tailer.start()
```

In Stop handler, BEFORE `s.ingestHarnessAudit(runID, auditDir)`:

```go
// Stop the real-time audit tailer (does a final read).
if tracked.Tailer != nil {
    tracked.Tailer.stop()
}
```

Then `ingestHarnessAudit` still runs for the chain verification and final
ingestion (the tailer may have already ingested some records, but the tailer
does NOT verify the hash chain — that's ingestHarnessAudit's job. The tailer
just publishes events in real-time; ingestHarnessAudit does the authoritative
post-run ingestion).

IMPORTANT: Do NOT double-append records. The tailer appends records to the
daemon audit chain as they arrive. But ingestHarnessAudit ALSO reads the file
and appends all records. This would create DUPLICATES.

Solution: Change ingestHarnessAudit to SKIP records that are already in the
daemon chain. OR: the tailer only publishes to the event bus (for real-time
dashboard) but does NOT append to the audit chain — ingestHarnessAudit remains
the single source of truth for the audit chain.

PREFERRED: The tailer ONLY publishes to the event bus (for real-time dashboard
updates). It does NOT append to the daemon audit chain. The audit chain is
ingested post-run by ingestHarnessAudit (with hash chain verification). This
avoids duplicates and keeps the chain authoritative.

Update readNewRecords to ONLY publish events, not append:
```go
// Do NOT append to audit chain — ingestHarnessAudit does that post-run
// with hash chain verification. The tailer only publishes to event bus
// for real-time dashboard visibility.
if t.eventBus != nil {
    for _, record := range newRecords {
        t.eventBus.Publish(t.runID, record.EventType, record.Payload)
    }
}
```

### Part 4: Add Tailer field to trackedRun

In `internal/daemon/stub_handlers.go`:

```go
type trackedRun struct {
    // ... existing fields ...
    Tailer *auditTailer // real-time audit tailer (nil if not running)
}
```

## Tests

Create `internal/daemon/audit_tailer_test.go`:

1. `TestAuditTailer_PublishesNewRecords` — create a test JSONL file,
   write an egress_allowed record, verify the event bus receives it.

2. `TestAuditTailer_HandlesMissingFile` — tailer for non-existent file
   should not panic or error.

3. `TestAuditTailer_HandlesMalformedLines` — write a malformed JSON line,
   verify it's skipped, not fatal.

4. `TestAuditTailer_StopFinalRead` — write records after tailer starts,
   call stop(), verify all records were published.

5. `TestAuditTailer_DoesNotAppendToAuditChain` — verify the tailer
   publishes events but does NOT call auditWriter.Append (to prevent
   duplicates with ingestHarnessAudit).

## Constraints

- The tailer MUST NOT append to the daemon audit chain. It only publishes
  events for real-time dashboard visibility. ingestHarnessAudit remains
  the single source of truth for the audit chain.
- The tailer poll interval is 500ms (not configurable for P1).
- The tailer MUST handle missing files gracefully (file may not exist
  until the harness writes its first audit record).
- Run `make lint` and `go test ./internal/daemon/... -race -count=1` — both must pass.
- ALL existing tests MUST still pass.

## What NOT to Do

- Do NOT change ingestHarnessAudit (it stays as the authoritative post-run ingestion).
- Do NOT add streaming/chunked reads (simple poll + seek is fine for P1).
- Do NOT change the harness (it already writes audit events correctly).
- Do NOT change the dashboard (the event bus + timeline SSE already works).
