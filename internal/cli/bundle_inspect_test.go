package cli

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
)

func TestBundleInspect_OfflineNoDaemon(t *testing.T) {
	// Ensure we are not talking to a live daemon.
	t.Setenv("AGENTPAAS_SOCKET", filepath.Join(t.TempDir(), "nonexistent.sock"))

	path := writeCLITestBundle(t)
	resetAgentCmd()
	cmd := AgentCmd()
	out := captureStdout(t, func() {
		cmd.SetArgs([]string{"bundle", "inspect", path})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})
	if !strings.Contains(out, "Integrity") || !strings.Contains(out, "PASS") {
		t.Fatalf("expected integrity PASS output, got: %s", out)
	}
	if !strings.Contains(out, "Publisher") {
		t.Fatalf("expected publisher section, got: %s", out)
	}
}

func TestBundleInspect_TamperedExitError(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", filepath.Join(t.TempDir(), "nonexistent.sock"))
	path := writeCLITestBundle(t)
	tampered := filepath.Join(t.TempDir(), "bad.agentpaas")
	if err := bundle.TamperManifestCreatedAt(path, tampered); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	resetAgentCmd()
	cmd := AgentCmd()
	var execErr error
	out := captureStdout(t, func() {
		cmd.SetArgs([]string{"bundle", "inspect", tampered})
		execErr = cmd.Execute()
	})
	if execErr == nil {
		t.Fatal("expected error exit for tampered bundle")
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL lines, got: %s", out)
	}
	if strings.Contains(out, "Policy summary") {
		t.Fatal("policy must be withheld")
	}
}

func TestBundleInspect_JSON(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", filepath.Join(t.TempDir(), "nonexistent.sock"))
	path := writeCLITestBundle(t)
	resetAgentCmd()
	cmd := AgentCmd()
	out := captureStdout(t, func() {
		cmd.SetArgs([]string{"--json", "bundle", "inspect", path})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
	})
	var report struct {
		Verified bool `json:"verified"`
		Header   struct {
			File string `json:"file"`
		} `json:"header"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &report); err != nil {
		t.Fatalf("json: %v body=%q", err, out)
	}
	if !report.Verified {
		t.Fatal("expected verified true")
	}
	if report.Header.File != path {
		t.Fatalf("header file = %q want %q", report.Header.File, path)
	}
}

func TestBundleInspect_DaemonStopped(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", filepath.Join(t.TempDir(), "offline.sock"))
	// Best-effort: stop daemon if running (inspect must still work).
	_ = exec.Command("agent", "daemon", "stop").Run()

	path := writeCLITestBundle(t)
	resetAgentCmd()
	cmd := AgentCmd()
	out := captureStdout(t, func() {
		cmd.SetArgs([]string{"bundle", "inspect", path})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("inspect with daemon stopped: %v", err)
		}
	})
	if !strings.Contains(out, "BUNDLE INSPECT") {
		t.Fatalf("unexpected output: %s", out)
	}
}
