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
	"github.com/parvezsyed/agentpaas/internal/dockerclient"
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

	// Export the resolved Docker endpoint to DOCKER_HOST so that child
	// processes spawned by the daemon (syft, cosign, etc.) can reach a
	// Docker daemon that is configured via Docker context (colima, Docker
	// Desktop) rather than an explicit DOCKER_HOST env var. This is a no-op
	// when DOCKER_HOST is already set or the default socket is in use.
	_ = dockerclient.ExportHostToEnv()

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

	// Ensure the home directory structure exists (creates ~/.agentpaas/,
	// subdirs, and the empty lock/pid/socket files). Without this, daemon.New()
	// fails because os.OpenFile(paths.Lock, O_RDWR, 0600) does not create the file.
	if err := home.Ensure(paths); err != nil {
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