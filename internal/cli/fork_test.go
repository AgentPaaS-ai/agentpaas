package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
)

func executeFork(t *testing.T, homeDir, ref, target string) (string, string, error) {
	t.Helper()
	args := []string{"fork", ref, target, "--home", homeDir}
	return executeCmd(args...)
}

func TestForkCmd_Success(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	target := filepath.Join(t.TempDir(), "weather-fork")

	out, _, err := executeFork(t, fix.homeDir, fix.ref, target)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if !strings.Contains(out, "Forked weather-agent") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "Next steps") || !strings.Contains(out, "agentpaas pack") {
		t.Fatalf("missing next steps: %q", out)
	}
	if !strings.Contains(out, "agentpaas export") {
		t.Fatalf("missing export hint: %q", out)
	}
	if !strings.Contains(out, "bumping agent.version") {
		t.Fatalf("missing rename hint: %q", out)
	}
	if _, err := os.Stat(filepath.Join(target, "agent.yaml")); err != nil {
		t.Fatalf("target not populated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "lineage.json")); err != nil {
		t.Fatalf("lineage missing: %v", err)
	}
}

func TestForkCmd_BadRef(t *testing.T) {
	homeDir := t.TempDir()
	stateRoot := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "fork")
	_, _, err := executeFork(t, homeDir, "nobody@deadbeef", target)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestForkCmd_NonEmptyDir(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	target := filepath.Join(t.TempDir(), "full")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "x"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := executeFork(t, fix.homeDir, fix.ref, target)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestForkCmd_TamperedInstalled(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: filepath.Join(fix.homeDir, "state"),
		Input:     fix.ref,
	})
	if err != nil {
		t.Fatal(err)
	}
	nameParts := strings.Split(resolved.Ref, "@")
	if len(nameParts) != 2 {
		t.Fatal(resolved.Ref)
	}
	installedDir, err := install.InstalledAgentPath(filepath.Join(fix.homeDir, "state"), nameParts[0], fix.pubFP)
	if err != nil {
		t.Fatal(err)
	}
	pol := filepath.Join(installedDir, "policy.yaml")
	if err := os.WriteFile(pol, []byte("tampered: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "clean")
	_, _, err = executeFork(t, fix.homeDir, fix.ref, target)
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(target); err == nil {
		ents, _ := os.ReadDir(target)
		if len(ents) > 0 {
			t.Fatal("files written to target after tampered fork")
		}
	}
}