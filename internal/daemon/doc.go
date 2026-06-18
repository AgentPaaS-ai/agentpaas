// Package daemon provides the AgentPaaS control daemon — a Unix-socket-bound
// gRPC server that implements the ControlService API.
//
// The daemon lifecycle has three phases:
//
//  1. Starting  – home directory is validated, socket is bound, but the daemon
//     is not yet ready to serve requests. All RPCs return Unavailable.
//  2. Ready     – the caller signals readiness via Ready(). RPCs are served
//     normally. Stub handlers return Unimplemented for methods that have not
//     been wired up yet.
//  3. Shutdown  – SIGTERM/SIGINT triggers graceful shutdown: new RPCs are
//     rejected and in-flight requests are drained with a configurable timeout.
//
// Single-instance enforcement
//
// The daemon uses POSIX flock(2) on a lock file inside the home directory.
// A second daemon trying to start will fail with EWOULDBLOCK.
//
// Security
//
//   - The daemon refuses to run as root unless --allow-root-for-test is set
//     (checked at the cmd/agentpaasd level).
//   - The Unix socket file is created with mode 0600.
//   - The home directory and socket permissions are validated before serving.
//
// Versioning
//
// VersionInfo is embedded in gRPC response trailers and returned as a
// diagnostic check in the Doctor RPC so that the CLI can verify daemon
// compatibility before issuing commands.
package daemon