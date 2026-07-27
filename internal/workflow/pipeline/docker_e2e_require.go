package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// requireDockerE2E ensures Docker is available for this test.
// It NEVER calls t.Skip — Docker is mandatory. If Docker is not running,
// it shells out to scripts/ensure-docker.sh to auto-start Colima (macOS)
// or install via brew. On failure, it calls t.Fatalf.
func requireDockerE2E(t *testing.T) {
	t.Helper()

	// Fast path: docker info succeeds.
	if dockerInfoOK() {
		return
	}

	// Shell out to ensure-docker.sh from the repo root.
	repoRoot := findRepoRoot()
	script := filepath.Join(repoRoot, "scripts", "ensure-docker.sh")

	t.Logf("Docker not running — executing: %s", script)
	cmd := exec.Command("bash", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Docker required for this test (no skip): %v\nRun: make ensure-docker", err)
	}

	// Re-verify after script.
	if !dockerInfoOK() {
		t.Fatalf("Docker still not available after ensure-docker.sh — cannot run this test (no skip)")
	}
}

// dockerInfoOK returns true if the Docker daemon is reachable.
func dockerInfoOK() bool {
	return exec.Command("docker", "info").Run() == nil
}

// findRepoRoot walks up from the current directory looking for go.mod.
func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}
