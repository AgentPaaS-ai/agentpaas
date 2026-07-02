# Worker Task: 14A0-T04 — Write TestE2E_PackRunInvokeStopAudit

## Context
AgentPaaS repo at /Users/pms88/projects/agentpaas, on main branch.
Create branch `feat/b14a0-t04-e2e`.

The Makefile `block14a0-gate` already has the conditional wiring:
```makefile
@if [ "$$AGENTPAAS_DOCKER_TESTS" = "1" ]; then \
    AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run TestE2E_PackRunInvokeStopAudit ./internal/daemon/... -timeout 300s; \
else \
    echo "(skipping Docker e2e — set AGENTPAAS_DOCKER_TESTS=1 to run)"; \
fi
```

You need to create the test file that this gate expects.

## Task: Create `internal/daemon/control_handlers_e2e_test.go`

Write a Go test file with `TestE2E_PackRunInvokeStopAudit` that exercises the full
pack → run → invoke → stop → audit query flow against real Docker (colima).

### Test Structure

```go
package daemon

import (
    "os"
    "testing"
    "path/filepath"
    "context"
    "time"
    "fmt"
    
    controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
    "github.com/parvezsyed/agentpaas/internal/audit"
    "github.com/parvezsyed/agentpaas/internal/home"
    "github.com/parvezsyed/agentpaas/internal/runtime"
)

func TestE2E_PackRunInvokeStopAudit(t *testing.T) {
    if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
        t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
    }
    // ... test body
}
```

### Test Steps

1. **Setup AGENTPAAS_HOME under `~/`** (NOT `/tmp` — colima mount limit):
   ```go
   homeDir := filepath.Join(os.Getenv("HOME"), "agentpaas-e2e-test-" + fmt.Sprint(time.Now().UnixNano()))
   defer os.RemoveAll(homeDir)
   ```

2. **Build binaries** (or find existing ones):
   - `bin/agentpaas` (CLI)
   - `bin/agentpaasd` (daemon) 
   - `bin/agentpaas-harness-linux` (harness for container)
   Use `exec.Command("go", "build", "-o", ...)` if not present.

3. **Start daemon** as a subprocess:
   ```go
   cmd := exec.Command(binPath, "daemon", "start")
   cmd.Env = append(os.Environ(), "AGENTPAAS_HOME="+homeDir)
   // Start in background, wait for socket
   ```

4. **Prepare test agent project**: use `demo/governed-weather/` as the agent project.
   Copy the SDK into it: `cp -r python/agentpaas_sdk <project>/python/`
   Then pack: `agentpaas pack <project-dir>`

5. **Run the agent**: `agentpaas run weather-agent` → capture runID

6. **Wait for invoke to complete**: poll container status or sleep ~30s

7. **Stop the run**: `agentpaas stop <runID>`

8. **Query audit**: `agentpaas audit query --run-id <runID> --json`

9. **Assert**:
   - Audit entries contain `egress_denied` events
   - At least one event has `destination` matching `api.weather.gov` or `evil-exfil.example.com`
   - The audit hash chain is intact (seq is sequential starting from 1, prev_hash matches)

### Alternative Approach (Simpler — In-Process)

Instead of CLI subprocesses, test the daemon's `controlServer` methods directly
with a real Docker runtime (not mock). This avoids subprocess management:

```go
func TestE2E_PackRunInvokeStopAudit(t *testing.T) {
    if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
        t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
    }

    // Setup home under ~/
    homeDir := filepath.Join(os.Getenv("HOME"), "agentpaas-e2e-" + fmt.Sprint(time.Now().UnixNano()))
    defer os.RemoveAll(homeDir)
    
    hp := home.NewHomePaths(homeDir)
    if err := home.Ensure(hp); err != nil {
        t.Fatalf("home.Ensure: %v", err)
    }

    // Real Docker runtime (no mock)
    rt, err := runtime.NewDockerRuntime()
    if err != nil {
        t.Fatalf("NewDockerRuntime: %v", err)
    }

    // Setup audit
    auditPath := filepath.Join(hp.State, "audit.jsonl")
    writer, _ := audit.NewAuditWriter(auditPath)
    defer writer.Close()
    indexer, _ := audit.NewSQLiteIndexer(filepath.Join(hp.State, "audit.db"))
    defer indexer.Close()

    server := &controlServer{
        homePaths:   hp,
        auditWriter: writer,
        auditIndex:  indexer,
    }
    server.runtimeOnce.Do(func() {})
    server.dockerRT = rt

    // Pack the test agent
    // Option A: Use the Pack handler directly
    // Option B: Use CLI subprocess
    // ...
    
    // Run
    runResp, err := server.Run(context.Background(), &controlv1.RunRequest{
        AgentName: "weather-agent",
    })
    // ...
    
    // Wait for invoke
    time.Sleep(30 * time.Second)
    
    // Stop
    _, err = server.Stop(context.Background(), &controlv1.StopRequest{
        RunId: runResp.GetRunId(),
    })
    
    // Query audit
    queryResp, err := server.AuditQuery(context.Background(), &controlv1.AuditQueryRequest{
        RunId: runResp.GetRunId(),
    })
    
    // Assert egress_denied events exist
    var hasEgressDenied bool
    for _, entry := range queryResp.GetEntries() {
        if strings.Contains(entry.GetEventType(), "egress_denied") {
            hasEgressDenied = true
            break
        }
    }
    if !hasEgressDenied {
        t.Error("expected at least one egress_denied audit event")
    }
}
```

**Use the in-process approach (Option above) — it's simpler and more reliable.**
But for Pack, you may need to either:
- Call `server.Pack()` with the project path
- Or use CLI: `exec.Command("bin/agentpaas", "pack", projectDir)`

For the Pack step, the test agent project must have the SDK bundled:
```go
// Copy SDK into the project
sdkSrc := filepath.Join(repoRoot, "python", "agentpaas_sdk")
sdkDst := filepath.Join(projectDir, "python", "agentpaas_sdk")
// Use os.MkdirAll + copy or exec.Command("cp", "-r", sdkSrc, sdkDst)
```

### Key Pitfalls (from build-rhythm skill)

1. **AGENTPAAS_HOME must be under `~/`** — colima only mounts /Users
2. **SDK must be bundled into the agent project** — `cp -r python/agentpaas_sdk <project>/python/`
3. **Harness binary**: the pack Dockerfile needs `agentpaas-harness-linux` (GOOS=linux GOARCH=arm64)
4. **AGENTPAAS_AGENT_PATH=/app/main.py** — already set by the Run handler (line 215)

### Test Helper

Look at `internal/daemon/control_handlers_test.go` line 465 (`TestStop_IngestsHarnessAudit`) 
for how to set up a controlServer with audit writer/indexer. The difference is: 
use a REAL `runtime.NewDockerRuntime()` instead of a mock driver.

### Build Requirements

The test will need the harness binary built for Linux ARM64:
```go
// In the test, build if not present:
harnessPath := filepath.Join(repoRoot, "bin", "agentpaas-harness-linux")
if _, err := os.Stat(harnessPath); os.IsNotExist(err) {
    cmd := exec.Command("go", "build", "-o", harnessPath, "./cmd/harness")
    cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64")
    if err := cmd.Run(); err != nil {
        t.Fatalf("build harness: %v", err)
    }
}
```

## Verification

1. `cd /Users/pms88/projects/agentpaas && go build ./...` — must compile
2. `go test ./internal/daemon/... -count=1 -race -timeout 120s` — non-Docker tests must pass
3. If Docker (colima) is running, try: `AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 -run TestE2E_PackRunInvokeStopAudit ./internal/daemon/... -timeout 300s`
4. If Docker is NOT running, just verify the test skips gracefully

## Rules
- The test MUST skip gracefully when `AGENTPAAS_DOCKER_TESTS != "1"`
- Do NOT modify any production code — test file only
- Do NOT modify the Makefile — the gate is already wired
- Commit with message: `test(14a0-t04): e2e pack→run→invoke→stop→audit flow`
