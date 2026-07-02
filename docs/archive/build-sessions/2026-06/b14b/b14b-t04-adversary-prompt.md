# Adversary Review: Block 14B-T04 — DockerRuntime.Stats() Implementation

## What Changed

The stub DockerRuntime.Stats() was replaced with a real implementation:
- New function: parseContainerStatsJSON(data []byte) (ContainerStats, error)
- New function: computeCPUPercent(cpuDelta, systemDelta int64, onlineCPUs uint32) float64
- Stats() now calls d.cli.ContainerStats(ctx, id, false), reads the JSON body,
  and delegates to parseContainerStatsJSON.
- Stats() has the standard delegation guard for the mock driver.
- New test file: internal/runtime/docker_stats_test.go (5 tests).

## Files to Review

1. internal/runtime/docker.go — the Stats() implementation, parseContainerStatsJSON,
   computeCPUPercent, dockerStatsJSON struct.
2. internal/runtime/docker_stats_test.go — the tests.

## Review Focus

1. **Resource leak**: Does statsResp.Body get properly closed? Is the defer
   in the right place relative to error returns?

2. **Integer overflow**: cpuDelta and systemDelta are computed as
   int64(raw.CPUStats.CPUUsage.TotalUsage) - int64(raw.PreCPUStats.CPUUsage.TotalUsage).
   Could these overflow if the raw values are very large (uint64 max)?

3. **Division by zero / NaN**: Is computeCPUPercent safe against:
   - systemDelta = 0
   - onlineCPUs = 0
   - Both being zero
   - systemDelta being negative (time moving backwards)

4. **Error handling**: Does ContainerStats error from Docker get properly
   propagated? Is IsNotFound checked correctly?

5. **Delegation guard**: Is the guard present and correct (matching the
   pattern used by other methods)?

6. **JSON parsing robustness**: What happens with malformed JSON? Empty body?
   Missing fields (e.g., no precpu_stats)? Does it fail gracefully?

7. **Test coverage gaps**: Are there cases the tests should cover but don't?

Report findings as:
FINDING N: [CRITICAL|HIGH|MEDIUM|LOW] [title]
Description of the issue and recommended fix.
