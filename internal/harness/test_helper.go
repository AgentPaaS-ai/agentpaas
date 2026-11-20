package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

func ensureHarnessLinuxARM64(t *testing.T) string {
	t.Helper()
	repoRoot := findRepoRoot(t)
	harnessPath := filepath.Join(repoRoot, "bin", "agentpaas-harness-linux")
	if _, err := os.Stat(harnessPath); err == nil {
		return harnessPath
	}
	if err := os.MkdirAll(filepath.Dir(harnessPath), 0o755); err != nil {
		t.Fatalf("MkdirAll bin: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", harnessPath, "./cmd/harness")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build harness: %v\n%s", err, out)
	}
	return harnessPath
}
