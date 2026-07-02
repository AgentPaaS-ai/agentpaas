// Package cli implements the AgentPaaS CLI command surface using cobra.
//
// The CLI is structured as a tree of commands rooted at `agent`:
//
//	agent [--json] [--socket <path>] [--home <dir>]
//	  version              — Print CLI version information
//	  daemon               — Daemon lifecycle commands
//	    status             — Query daemon version and readiness
//	    start              — Start the control daemon
//	    stop               — Stop the control daemon
//	    restart            — Restart the control daemon
//	    install            — Install as a system service (not yet implemented)
//	    uninstall          — Remove from system services (not yet implemented)
//	  doctor               — Run system diagnostics (v0 stub)
//	  pack                 — Build an agent image
//	  run                  — Start a new agent run
//	  stop                 — Terminate a running agent
//	  logs                 — Follow agent logs
//	  policy               — Policy management commands
//	  secrets              — Secret management commands
//	  audit                — Audit log commands
//	  validate             — Validate an agent project
//	  summarize            — Summarize a completed run
//	  explain-failure      — Analyze a failed run
//	  explain-denial       — Explain a policy denial
//	  recommend-patch      — Suggest a policy patch
//	  timeline             — Show run timeline
//	  next-action          — Recommend next action
//
// Global flags:
//
//	--json        Output in JSON format instead of human-readable text.
//	--socket      Override the daemon Unix socket path. Defaults to the
//	              AGENTPAAS_SOCKET environment variable or <home>/daemon.sock.
//	--home        Override the agentpaas home directory. Defaults to the
//	              AGENTPAAS_HOME environment variable or ~/.agentpaas.
//
// Commands that interact with a running daemon use gRPC over a Unix domain
// socket. The connection helper in connection.go handles dialing and presents
// a clear error when the daemon is not running.
package cli
