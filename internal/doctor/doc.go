// Package doctor provides system diagnostic checks for agentpaas.
//
// The doctor package implements the "agent doctor" command by running a
// collection of independent checks against the local system and aggregating
// the results into an overall health status.
//
// # Checks
//
// The following checks are implemented:
//   - Docker reachable — verifies the Docker daemon is responsive
//   - Docker context   — reports the active Docker context name
//   - Docker Desktop   — detects whether Docker Desktop or Colima is running on macOS
//   - Linux dockerd    — informational check; dockerd is P2, not a P1 gate
//   - Ports free       — verifies ports 7700, 7717, and 7718 are not in use
//   - Socket perms     — validates daemon socket file permissions (0600)
//   - Home dir perms   — validates ~/.agentpaas directory permissions (0700)
//   - Daemon ready     — checks if the agentpaas daemon gRPC endpoint responds
//   - Proto compatible — compares CLI and daemon protocol versions
//   - Harness copies   — detects divergent agentpaas-harness binaries on the host
//
// Each check is independent; one failure does not block subsequent checks.
package doctor