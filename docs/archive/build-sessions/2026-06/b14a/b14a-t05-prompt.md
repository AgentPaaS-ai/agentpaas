# Task: 14A-T05 — Hash-chained harness audit (GAP-6, MEDIUM)

## Context

The harness `FileAuditAppender` (`internal/harness/file_appender.go`) writes flat JSONL
records with no hash chain — by design. The daemon's `AuditWriter.Append()` correctly
re-chains them (assigns seq, prev_hash, recomputes record_hash from canonical JSON).

The GAP-6 fix adds a **harness-side hash chain** so that the daemon can **verify** the
chain before re-chaining into its own audit trail. This detects tampering of the JSONL
file between when the harness writes it and when the daemon ingests it.

## Architecture

```
Harness (in container)          Daemon (on host)
─────────────────────           ──────────────────
FileAuditAppender               ingestHarnessAudit()
  writes JSONL with               reads JSONL
  prev_hash + record_hash         verifies harness chain
  (hash chain)                    if verification fails → log error, refuse ingestion
                                  if verification passes → re-chain via AuditWriter.Append()
```

The daemon does NOT trust the harness chain — it verifies it. The harness runs inside
the container which is the untrusted boundary.

## What to implement

### 1. Modify FileAuditAppender to maintain a hash chain

In `internal/harness/file_appender.go`:

Add fields to the struct:
```go
type FileAuditAppender struct {
    mu       sync.Mutex
    file     *os.File
    prevHash string  // hash of the last written record
}
```

Update `Append()` to compute and store the hash chain:
```go
func (a *FileAuditAppender) Append(record audit.AuditRecord) error {
    if a == nil || a.file == nil {
        return nil
    }
    if record.Timestamp == "" {
        record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
    }
    if record.DeploymentMode == "" {
        record.DeploymentMode = "local"
    }

    // Set prev_hash from the last record's hash
    record.PrevHash = a.prevHash

    // Compute record_hash from canonical JSON (without record_hash field)
    // We need to compute it BEFORE marshaling the full record
    recordHash, err := record.ComputeRecordHash()
    if err != nil {
        return fmt.Errorf("harness: compute record hash: %w", err)
    }
    record.RecordHash = recordHash

    // Marshal full record (with record_hash) for the JSONL line
    data, err := json.Marshal(record)
    if err != nil {
        return fmt.Errorf("harness: marshal audit record: %w", err)
    }
    data = append(data, '\n')

    a.mu.Lock()
    defer a.mu.Unlock()
    _, err = a.file.Write(data)
    if err != nil {
        return err
    }

    // Update prev_hash for the next record
    a.prevHash = recordHash
    return nil
}
```

**IMPORTANT:** The `audit.AuditRecord` struct has methods `CanonicalMarshal()` and
`computeRecordHash()` — but `computeRecordHash` is lowercase (unexported). You need
to either:
- Export it as `ComputeRecordHash()` — add a new exported method that calls the
  existing unexported one, OR
- Add the method directly in the audit package

Check `internal/audit/record.go` — the method is `computeRecordHash()` (unexported).
Add an exported wrapper:
```go
// ComputeRecordHash is an exported wrapper for computeRecordHash.
func (r *AuditRecord) ComputeRecordHash() (string, error) {
    return r.computeRecordHash()
}
```

### 2. Add chain verification on the daemon side

In `internal/daemon/control_handlers.go`, add a function to verify the harness chain:

```go
// verifyHarnessChain validates the hash chain of harness audit records.
// It checks:
// 1. Each record's prev_hash matches the previous record's record_hash
// 2. Each record's record_hash matches a recomputed hash from canonical JSON
// Returns nil if the chain is valid, or an error describing the break.
func verifyHarnessChain(records []audit.AuditRecord) error {
    if len(records) == 0 {
        return nil
    }
    for i, rec := range records {
        // Recompute record_hash
        computedHash, err := rec.ComputeRecordHash()
        if err != nil {
            return fmt.Errorf("harness chain: line %d: compute hash: %w", i+1, err)
        }
        if rec.RecordHash != computedHash {
            return fmt.Errorf("harness chain: line %d: record_hash mismatch: stored %q, recomputed %q",
                i+1, rec.RecordHash, computedHash)
        }
        if i > 0 {
            if rec.PrevHash != records[i-1].RecordHash {
                return fmt.Errorf("harness chain: line %d: prev_hash mismatch: got %q, expected %q",
                    i+1, rec.PrevHash, records[i-1].RecordHash)
            }
        }
    }
    return nil
}
```

### 3. Wire verification into ingestHarnessAudit

In `ingestHarnessAudit()`, after reading records and before appending to the daemon
chain:

```go
func (s *controlServer) ingestHarnessAudit(runID, auditDir string) {
    if auditDir == "" {
        return
    }
    auditPath := filepath.Join(auditDir, "harness-audit.jsonl")
    records, err := readAuditJSONL(auditPath)
    if err != nil {
        fmt.Fprintf(os.Stderr, "daemon: ingest harness audit (%s): %v\n", runID, err)
        return
    }
    if len(records) == 0 {
        return
    }

    // Verify the harness-side hash chain before ingesting
    if err := verifyHarnessChain(records); err != nil {
        fmt.Fprintf(os.Stderr, "daemon: harness audit chain verification failed (%s): %v\n", runID, err)
        // Log a tamper audit event to the daemon's own chain
        tamperRecord := audit.AuditRecord{
            Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
            EventType: "harness_audit_chain_broken",
            Actor:     "daemon",
            Payload: map[string]interface{}{
                "run_id":  runID,
                "error":   err.Error(),
                "action":  "audit_ingestion_refused",
            },
        }
        _ = s.auditWriter.Append(tamperRecord)
        // Do NOT ingest the tampered records
        return
    }

    for _, record := range records {
        // ... existing code ...
    }
    // ... existing index refresh ...
}
```

## Tests to write

### Harness-side tests (`internal/harness/file_appender_test.go`)

1. **TestFileAuditAppender_HashChain:** Write 3 records, read them back, verify
   each record's prev_hash matches the previous record's record_hash.
2. **TestFileAuditAppender_FirstRecordEmptyPrevHash:** First record has prev_hash="".
3. **TestFileAuditAppender_RecordHashMatches:** Each record's record_hash matches
   a recomputed hash from canonical JSON.
4. **TestFileAuditAppender_ConcurrentAppends:** Multiple goroutines appending —
   no panics, chain is still valid (each record links to exactly one predecessor).

### Daemon-side tests (`internal/daemon/control_handlers_test.go` or new file)

1. **TestVerifyHarnessChain_ValidChain:** Records with correct prev_hash and
   record_hash → verification passes.
2. **TestVerifyHarnessChain_TamperedRecord:** Modify a record's payload after
   writing → verification fails with "record_hash mismatch".
3. **TestVerifyHarnessChain_BrokenLink:** Modify a record's prev_hash →
   verification fails with "prev_hash mismatch".
4. **TestIngestHarnessAudit_RefusesTamperedChain:** Write valid chain, tamper with
   one record, call ingestHarnessAudit → daemon logs "chain verification failed"
   and does NOT ingest the records. Verify a "harness_audit_chain_broken" event
   is written to the daemon's own audit chain.

## Testing

```bash
cd /Users/pms88/projects/agentpaas
go test ./internal/harness/... -run TestFileAuditAppender -v -race
go test ./internal/daemon/... -run TestVerifyHarnessChain -v -race
go test ./internal/daemon/... -run TestIngestHarness -v -race
go test ./internal/audit/... -v -race  # ensure no regressions
```

## Commit message

```
feat(14a-t05): hash-chained harness audit (GAP-6)

FileAuditAppender now maintains a SHA-256 hash chain (prev_hash +
record_hash per record). The daemon verifies the harness chain before
ingestion — if the chain is broken (tampered), it refuses to ingest
and logs a harness_audit_chain_broken event to its own audit trail.

Exported ComputeRecordHash() on AuditRecord for cross-package use.
4 harness-side tests + 4 daemon-side tests.
```

## Branch

Create branch `feat/b14a-t05` from main.
