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
//	  pack                 — Build an agent image (not yet implemented)
//	  run                  — Start a new agent run (not yet implemented)
//	  stop                 — Terminate a running agent (not yet implemented)
//	  logs                 — Follow agent logs (not yet implemented)
//	  policy               — Policy management commands (not yet implemented)
//	  secrets              — Secret management commands (not yet implemented)
//	  audit                — Audit log commands (not yet implemented)
//	  validate             — Validate an agent project (not yet implemented)
//	  summarize            — Summarize a completed run (not yet implemented)
//	  explain-failure      — Analyze a failed run (not yet implemented)
//	  explain-denial       — Explain a policy denial (not yet implemented)
//	  recommend-patch      — Suggest a policy patch (not yet implemented)
//	  timeline             — Show run timeline (not yet implemented)
//	  next-action          — Recommend next action (not yet implemented)
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
