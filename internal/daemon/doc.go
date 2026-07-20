// Package daemon provides the AgentPaaS control daemon — a Unix-socket-bound
// gRPC server that implements the ControlService API.
//
// The daemon is the long-lived process behind agentpaasd. It owns home-directory
// layout validation, single-instance locking, run tracking, policy/secret/audit
// RPCs, operator diagnostics, routed-run deployment APIs, and optional dashboard
// resource enumeration.
//
// # Lifecycle
//
// The daemon lifecycle has three phases:
//
//  1. Starting – home directory is validated, the lock file and Unix socket are
//     acquired, and interceptors reject RPCs with Unavailable until Ready.
//  2. Ready – the caller signals readiness via Ready(). RPCs are served
//     normally. Methods not yet wired may still return Unimplemented.
//  3. Shutdown – SIGTERM/SIGINT triggers graceful shutdown: new RPCs are
//     rejected and in-flight requests are drained with a configurable timeout.
//
// # Single-instance enforcement
//
// The daemon uses POSIX flock(2) on a lock file inside the home directory.
// A second daemon trying to start fails with EWOULDBLOCK. Lock inode identity
// is tracked so a replaced lock file cannot be mistaken for the original hold.
//
// # Security
//
//   - The daemon refuses to run as root unless --allow-root-for-test / WithAllowRoot
//     is set (also checked at the cmd/agentpaasd level).
//   - The Unix socket file is created with mode 0600.
//   - The home directory and socket permissions are validated before serving.
//   - Pack/run paths enforce agent resolution, credential existence, and
//     fail-closed routed-run detection where required.
//
// # Control surface
//
// Handlers cover pack/run/stop/logs, policy apply, secrets, audit query/export,
// cron, operator tools (validate, summarize, explain-failure/denial, recommend
// patch, timeline, next-action), confirmations for trust-boundary changes,
// export preview/export, and routed deployment/invocation/workflow RPCs.
//
// # Versioning
//
// VersionInfo is embedded in gRPC response trailers and returned as a
// diagnostic check in the Doctor RPC so that the CLI can verify daemon
// compatibility before issuing commands.
package daemon
