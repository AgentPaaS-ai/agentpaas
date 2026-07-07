package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

func TestResolveRunTarget_InstalledRef(t *testing.T) {
	state := t.TempDir()
	home := filepath.Join(state, "home")
	if err := os.MkdirAll(filepath.Join(home, "state", "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	ref := "weather@a1b2c3d4"
	dir := filepath.Join(home, "state", "agents", ref)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "install-manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"agent_name":"weather","publisher_fingerprint":"a1b2c3d4","alias":"maria"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("home", home, "")
	got, err := resolveRunTarget(cmd, nil, ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != ref {
		t.Fatalf("got %q want %q", got, ref)
	}
	display := install.DisplayForDaemonKey(filepath.Join(home, "state"), got)
	if display != "weather@a1b2c3d4 (maria)" {
		t.Fatalf("display=%q", display)
	}
}

func TestResolveRunTarget_Phase1BareName(t *testing.T) {
	state := t.TempDir()
	home := filepath.Join(state, "home")
	agentDir := filepath.Join(home, "state", "agents", "localagent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.lock"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String("home", home, "")
	got, err := resolveRunTarget(cmd, nil, "localagent")
	if err != nil || got != "localagent" {
		t.Fatalf("got %q err=%v", got, err)
	}
}