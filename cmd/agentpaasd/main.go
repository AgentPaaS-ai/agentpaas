// Command agentpaasd is the AgentPaaS control daemon. It binds a gRPC server
// to a Unix domain socket and serves the ControlService API.
//
// Usage:
//
//	agentpaasd [--allow-root-for-test] [--version]
//
// The daemon creates its home directory (~/.agentpaas by default) on first
// run and uses flock(2) to ensure only one instance runs at a time.
// Signals SIGTERM and SIGINT trigger graceful shutdown.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/parvezsyed/agentpaas/internal/daemon"
	"github.com/parvezsyed/agentpaas/internal/home"
)

func main() {
	allowRoot := flag.Bool("allow-root-for-test", false, "allow running as root (for testing only)")
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *showVersion {
		v := daemon.CurrentVersion()
		fmt.Println(v.String())
		os.Exit(0)
	}

	// Resolve home directory.
	homeDir, err := home.DiscoverHome()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot discover home directory: %v\n", err)
		os.Exit(1)
	}

	paths := home.NewHomePaths(homeDir)

	// Check root. The --allow-root-for-test flag bypasses the root check
	// for test environments only.
	if err := daemon.CheckRoot(os.Getuid(), *allowRoot); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Create the daemon and acquire the lock file.
	d, err := daemon.New(paths, daemon.CurrentVersion())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Set up signal handling for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "received signal %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.Stop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		}
		os.Exit(0)
	}()

	// Start the daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Signal readiness.
	d.Ready()
	fmt.Fprintf(os.Stderr, "agentpaasd ready on %s\n", paths.Socket)

	// Block until signal.
	select {}
}