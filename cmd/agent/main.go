// Command agent is the AgentPaaS CLI entry point.
//
// It provides subcommands for daemon lifecycle management, agent control,
// policy, secrets, audit, and diagnostics.
//
// Usage:
//
//	agent [--json] [--socket <path>] [--home <dir>] <command> [args]
//
// Start the daemon first: agent daemon start
// Check the daemon status: agent daemon status
// Print CLI version:      agent version
package main

import (
	"fmt"
	"os"

	"github.com/parvezsyed/agentpaas/internal/cli"
)

func main() {
	root := cli.AgentCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}