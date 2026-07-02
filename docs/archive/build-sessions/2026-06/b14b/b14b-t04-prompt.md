# Block 14B-T04: Implement DockerRuntime.Stats()

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. The DockerRuntime
in internal/runtime/docker.go currently has a stub Stats() method that returns
errDockerNotImplemented. The dashboard resource monitor needs real CPU/memory/PID
data for running agent containers.

## Current State

In internal/runtime/docker.go, line 291-294:
```go
// Stats returns resource usage for a Docker container. Not yet implemented.
func (d *DockerRuntime) Stats(_ context.Context, _ ContainerID) (ContainerStats, error) {
	return ContainerStats{}, errDockerNotImplemented
}
```

The ContainerStats struct (internal/runtime/driver.go line 156-166):
```go
type ContainerStats struct {
	CPUPercent float64  // 0.0-100.0
	MemoryMB   float64
	PIDs       int
}
```

## What to Implement

1. Replace the stub Stats() method with a real implementation using the Docker API.

2. Use a one-shot stats snapshot (NOT streaming). Call:
   ```go
   stats, err := d.cli.ContainerStats(ctx, string(id), false) // false = no stream
   ```
   This returns types.ContainerStats with a Body (io.ReadCloser) containing JSON.

3. Parse the Docker stats JSON. The relevant fields are:
   - `cpu_stats.cpu_usage.total_usage` and `precpu_stats.cpu_usage.total_usage`
   - `cpu_stats.system_cpu_usage` and `precpu_stats.system_cpu_usage`
   - `cpu_stats.online_cpus`
   - `memory_stats.usage` (bytes)
   - `memory_stats.limit` (bytes)
   - `pids_stats.current`

4. Compute CPU percentage:
   - CPU delta = cpu_stats.total_usage - precpu_stats.total_usage
   - System delta = cpu_stats.system_cpu_usage - precpu_stats.system_cpu_usage
   - If system_delta > 0 and cpu_delta >= 0:
     CPUPercent = (cpu_delta / system_delta) * online_cpus * 100.0
   - Handle edge cases: online_cpus=0, negative deltas, system_delta=0

5. Convert memory bytes to MB (bytes / 1024 / 1024).

6. PIDs from pids_stats.current.

7. Add the delegation guard at the top of Stats() (CRITICAL — existing pattern):
   ```go
   func (d *DockerRuntime) Stats(ctx context.Context, id ContainerID) (ContainerStats, error) {
       if d.driver != nil {
           return d.driver.Stats(ctx, id)
       }
       // ... rest
   }
   ```

8. Validate empty container ID:
   ```go
   if string(id) == "" {
       return ContainerStats{}, fmt.Errorf("%w: empty container ID", ErrContainerNotFound)
   }
   ```

## Tests

Write unit tests in internal/runtime/docker_stats_test.go:

1. `TestStats_ParsesCPUAndMemory` — mock the Docker API response by using
   a test server or by constructing the JSON manually and parsing it.
   Since DockerRuntime uses d.cli (the real Docker client), test the JSON
   parsing logic by extracting it into a testable function:
   `parseContainerStatsJSON(data []byte) (ContainerStats, error)`.

2. `TestStats_CPUPercentCalculation` — verify the CPU percentage formula with:
   - Normal case: cpu_delta=50000000, system_delta=1000000000, online_cpus=2 → 10.0%
   - Zero system delta → CPUPercent=0 (no panic)
   - Zero online_cpus → CPUPercent=0
   - Negative delta → CPUPercent=0

3. `TestStats_MemoryCalculation` — 104857600 bytes → 100.0 MB

4. `TestStats_EmptyID` — returns ErrContainerNotFound

5. `TestStats_DelegationGuard` — using NewDockerRuntimeWithDriver(mock),
   verify Stats() delegates to the mock driver.

## Constraints

- Do NOT change the ContainerStats struct (it's in driver.go — public API).
- Do NOT add streaming support (P1 uses snapshots only).
- Close the stats Body io.ReadCloser after reading.
- Use encoding/json for parsing (define a local struct, not importing Docker types).
- Follow the existing code style (look at other methods in docker.go for patterns).
- Run `make lint` and `go test ./internal/runtime/... -race -count=1` — both must pass.

## What NOT to Do

- Do NOT modify any other file (no daemon changes, no dashboard changes, no driver.go).
- Do NOT implement Logs() (that's a separate task).
- Do NOT add new dependencies.
- Do NOT change the ContainerStats struct fields.
