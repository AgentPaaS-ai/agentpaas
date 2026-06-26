# B13 BUG 7d Step 4: Ingest Harness Audit JSONL into Daemon Chain

## Objective
After a container stops, read the harness audit JSONL file from the host-mounted
audit directory and append each record to the daemon's audit chain.

## Context
- Step 3 (merged) added `AuditDir` to `trackedRun` and the Run handler creates
  `{State}/runs/{runID}/harness-audit/` and mounts it as `/audit` in the container.
- The harness writes audit events (egress_allowed, egress_denied) to
  `/audit/harness-audit.jsonl` inside the container, which maps to
  `{AuditDir}/harness-audit.jsonl` on the host.
- The daemon already has `readAuditJSONL(path)` (control_handlers.go:734) which
  parses a JSONL file into `[]audit.AuditRecord`.
- The daemon has `s.auditWriter.Append(record)` which adds a record to the
  daemon's hash-chained audit JSONL, and `s.auditIndex.Rebuild(path)` which
  refreshes the SQLite index.

## Files to Edit

### 1. `internal/daemon/control_handlers.go` — Stop handler (~line 228)

The Stop handler currently:
1. Stops the container
2. Removes the container + network
3. Untracks the run
4. Publishes events
5. Records audit "run_stop"

Add a new step BETWEEN step 2 (remove) and step 3 (untrack):
- Look up the trackedRun to get AuditDir
- If AuditDir is non-empty, read `{AuditDir}/harness-audit.jsonl`
- For each record, append it to the daemon's audit chain via `s.auditWriter.Append(record)`
- Rebuild the audit index once after all appends
- Log errors to stderr (don't fail the Stop operation if audit ingestion fails)

IMPORTANT: You must call `s.lookupRun(runID)` BEFORE `s.untrackRun(runID)`,
because untrackRun deletes the entry. The current code calls lookupRun at line 234
to get containerID + netID. Modify it to also capture the AuditDir.

Current Stop handler code (lines 228-272):
```go
func (s *stubControlServer) Stop(ctx context.Context, req *controlv1.StopRequest) (*controlv1.StopResponse, error) {
	runID := req.GetRunId()
	...
	containerID, netID := s.lookupRun(runID)
	...
	// Stop container, remove container, remove network
	...
	s.untrackRun(runID)
	...
}
```

Change `s.lookupRun(runID)` to also return the AuditDir. Either:
a) Modify `lookupRun` to return 3 values (containerID, netID, auditDir), OR
b) Add a new method `lookupRunAuditDir(runID) string` that reads under the same lock.

Option (a) is cleaner. Update lookupRun signature and all call sites.

### New method: `ingestHarnessAudit(runID, auditDir string)`

Add this method to control_handlers.go:

```go
// ingestHarnessAudit reads the harness audit JSONL from the host audit
// directory and appends each record to the daemon's audit chain.
// Errors are logged but do not fail the Stop operation — the container
// is already stopped, and missing audit data is a best-effort concern.
func (s *stubControlServer) ingestHarnessAudit(runID, auditDir string) {
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
	for _, record := range records {
		// Ensure run_id is present in payload for audit queries.
		if record.Payload == nil {
			record.Payload = make(map[string]interface{})
		}
		if _, ok := record.Payload["run_id"]; !ok {
			record.Payload["run_id"] = runID
		}
		if err := s.auditWriter.Append(record); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: append harness audit record (%s): %v\n", runID, err)
		}
	}
	// Refresh the SQLite index so dashboard queries see the new records.
	if s.auditIndex != nil && s.homePaths != nil {
		_ = s.auditIndex.Rebuild(filepath.Join(s.homePaths.State, "audit.jsonl"))
	}
}
```

### Modified Stop handler flow:

```go
containerID, netID, auditDir := s.lookupRun(runID)
// ... stop, remove container, remove network ...

// Ingest harness audit records before untracking.
s.ingestHarnessAudit(runID, auditDir)

s.untrackRun(runID)
// ... events, audit ...
```

### 2. `internal/daemon/control_handlers.go` — lookupRun (~line 549)

Change from returning 2 values to 3:
```go
func (s *stubControlServer) lookupRun(runID string) (runtime.ContainerID, string, string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		return "", "", ""
	}
	tracked, ok := s.runs[runID]
	if !ok {
		return "", "", ""
	}
	return tracked.Container, tracked.Network, tracked.AuditDir
}
```

Update ALL call sites of `lookupRun` to handle the third return value.

### 3. Test in `internal/daemon/control_handlers_test.go`

Add `TestStop_IngestsHarnessAudit` that:
1. Creates a test server with a mock runtime driver
2. Creates a harness-audit.jsonl file with 2 test records (egress_denied + egress_allowed)
3. Calls Run (to create the tracked run + audit dir)
4. Calls Stop
5. Queries the daemon's audit chain (via AuditQuery or by reading the audit JSONL)
6. Verifies the harness records appear in the daemon's audit chain with run_id set

## Build + Lint
```sh
cd /tmp/b13-audit-ingest
go build ./...
go test ./internal/daemon/... -count=1 -timeout 60s
golangci-lint run ./internal/daemon/...
```

## Constraints
- Do NOT modify the Run handler (Step 3 is done)
- Do NOT modify the harness code
- Errors in audit ingestion must NOT fail the Stop operation
- The `bufio`, `fmt`, `os`, `path/filepath`, `json`, `strings` packages are already imported
- Keep changes minimal
