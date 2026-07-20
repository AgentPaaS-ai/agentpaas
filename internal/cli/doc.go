// Package cli implements the AgentPaaS CLI command surface using cobra.
//
// The CLI is structured as a tree of commands rooted at `agent`:
//
//	agent [--json] [--socket <path>] [--home <dir>]
//	  version              — Print CLI version information
//	  daemon               — Daemon lifecycle (status/start/stop/restart/install)
//	  doctor               — System diagnostics
//	  init                 — Scaffold a new agent project
//	  pack                 — Build an agent image / lock
//	  export               — Export bundle artifacts
//	  bundle               — Offline bundle inspect
//	  install              — Install a bundle
//	  run                  — Start or control runs
//	  stop                 — Terminate a running agent
//	  logs                 — Follow agent logs
//	  policy               — Policy management
//	  secrets              — Secret management
//	  audit                — Audit log commands
//	  validate             — Validate an agent project
//	  summarize            — Summarize a completed run
//	  explain-failure      — Analyze a failed run
//	  explain-denial       — Explain a policy denial
//	  recommend-patch      — Suggest a policy patch
//	  timeline             — Show run timeline
//	  next-action          — Recommend next action
//	  status               — Run/agent status
//	  trigger              — Trigger API helpers
//	  cron                 — Cron schedule management
//	  trust                — Trust store commands
//	  identity             — Publisher/local identity
//	  installed            — List installed agents
//	  fork                 — Fork an agent project
//	  provenance           — Show provenance
//	  deploy               — Deployment and alias CAS surface
//	  confirm(ations)      — Trust-boundary confirmations
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
// a clear error when the daemon is not running. Paths passed to the daemon are
// resolved to absolute form; --home/--socket values are validated via
// home.ValidatePath when explicitly set.
package cli
