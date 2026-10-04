package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
)

func main() {
	dir := flag.String("dir", "bin", "directory for the pinned OpenShell binaries")
	flag.Parse()
	if err := openshellrt.EnsureInstalled(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "ensure-openshell: %v\n", err)
		os.Exit(1)
	}
}
