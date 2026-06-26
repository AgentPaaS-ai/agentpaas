# B13 BUG 7d Step 3: Mount Audit Volume in Run Handler

## Objective
Wire the harness audit volume into the daemon's Run handler so that
the container can write audit JSONL to a host-mounted directory.

## Files to Edit
1. `internal/daemon/control_handlers.go` — Run() handler (~line 191, ContainerSpec.Create call)
2. `internal/daemon/stub_handlers.go` — trackedRun struct (add AuditDir field)

## Changes Required

### 1. trackedRun struct (stub_handlers.go ~line 21)
Add an `AuditDir` field to store the host audit directory path for Step 4 ingestion:

```go
type trackedRun struct {
	Container runtime.ContainerID
	Network   string
	AuditDir  string  // host path to harness-audit directory for post-run ingestion
}
```

### 2. trackRun function (control_handlers.go ~line 533)
Update the signature to accept auditDir and store it:

```go
func (s *stubControlServer) trackRun(runID string, containerID runtime.ContainerID, networkID, auditDir string) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runs == nil {
		s.runs = make(map[string]trackedRun)
	}
	s.runs[runID] = trackedRun{
		Container: containerID,
		Network:   networkID,
		AuditDir:  auditDir,
	}
}
```

### 3. Run handler (control_handlers.go ~line 191)
BEFORE the `rt.Create` call, add:

```go
// Create host audit directory for harness audit JSONL.
hostAuditDir := filepath.Join(s.homePaths.State, "runs", runID, "harness-audit")
if err := os.MkdirAll(hostAuditDir, 0o700); err != nil {
    _ = rt.RemoveNetwork(ctx, netID)
    return nil, status.Errorf(codes.Internal, "create audit dir: %v", err)
}
```

Then modify the ContainerSpec.Create call to include Binds and Env:

```go
containerID, err := rt.Create(ctx, runtime.ContainerSpec{
    Image:      imageRef,
    Labels:     runtime.Labels(runtime.ResourceTypeAgent, runID),
    NetworkIDs: []string{string(netID)},
    Binds:      []string{fmt.Sprintf("%s:/audit", hostAuditDir)},
    Env:        []string{"AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl"},
})
```

### 4. Update trackRun call site (~line 207)
Change from:
```go
s.trackRun(runID, containerID, string(netID))
```
To:
```go
s.trackRun(runID, containerID, string(netID), hostAuditDir)
```

## Test
Add a test in `internal/daemon/control_handlers_test.go` that verifies:
- The Run handler creates the audit directory on the host filesystem
- The ContainerSpec includes the correct Binds entry
- The ContainerSpec includes the AGENTPAAS_AUDIT_PATH env var

Use the existing test patterns. You can mock the runtime to capture the
ContainerSpec, or inspect the filesystem after calling Run.

## Build + Lint
```sh
cd /tmp/b13-audit-volume
go build ./...
go test ./internal/daemon/... -count=1
golangci-lint run ./internal/daemon/...
```

## Constraints
- Do NOT touch the Stop handler (that's Step 4, a separate micro-chunk)
- Do NOT modify the harness code (Steps 1-2 already done)
- The `os`, `filepath`, and `fmt` packages are already imported in control_handlers.go
- Keep changes minimal — only what's needed to mount the audit volume
