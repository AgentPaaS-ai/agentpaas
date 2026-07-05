package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

func TestDaemonMain_EnsureCreatesLockFile(t *testing.T) {
	tmpHome := t.TempDir()
	homeDir := filepath.Join(tmpHome, "fresh-agentpaas-home")
	t.Setenv("AGENTPAAS_HOME", homeDir)

	paths := home.NewHomePaths(homeDir)

	// Verify lock file does not exist yet (fresh install)
	if _, err := os.Stat(paths.Lock); !os.IsNotExist(err) {
		t.Fatalf("lock file should not exist on fresh install, got err=%v", err)
	}

	// Call Ensure — this is what main.go must call before daemon.New()
	if err := home.Ensure(paths); err != nil {
		t.Fatalf("home.Ensure failed: %v", err)
	}

	// Verify lock file now exists
	if _, err := os.Stat(paths.Lock); err != nil {
		t.Fatalf("lock file should exist after Ensure, got err=%v", err)
	}
}