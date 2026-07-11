// Package binresolve resolves the agentpaas-harness binary and Python SDK
// directory shared by the daemon pack path and the CLI install path.
//
// The logic here is ported from the daemon's internal helpers
// (resolveHarnessBinary / resolveSDKDir) so both code paths stay in sync.
package binresolve

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Executable returns the path to the current executable. Tests may override it.
var Executable = os.Executable

// HarnessBinary finds the agentpaas-harness binary for container images.
// It prefers the linux/arm64 cross-compile (agentpaas-harness-linux) over the
// darwin/arm64 Mac binary (agentpaas-harness). Returns an empty string if not
// found; callers fall back to pack.BuildImage's own exec.LookPath and clear
// error.
func HarnessBinary() string {
	exePath, err := Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		if p := harnessCandidate(filepath.Join(exeDir, "agentpaas-harness-linux")); p != "" {
			return p
		}
		if p := harnessCandidate(filepath.Join(exeDir, "..", "bin", "agentpaas-harness-linux")); p != "" {
			return p
		}
		if p := harnessCandidate(filepath.Join(exeDir, "agentpaas-harness")); p != "" {
			return p
		}
	}
	if p, err := exec.LookPath("agentpaas-harness-linux"); err == nil {
		return p
	}
	if p, err := exec.LookPath("agentpaas-harness"); err == nil {
		return p
	}
	return ""
}

// harnessCandidate returns path if it points to a regular file, else "".
func harnessCandidate(path string) string {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// SDKDir finds the Python SDK directory (containing agentpaas_sdk)
// relative to the harness binary. The SDK lives in a "python/" subdirectory
// alongside the harness binary (e.g. /usr/local/bin → /usr/local/python).
// If not found there, it checks common repo locations. Returns "" if not found.
func SDKDir(harnessPath string) string {
	if harnessPath == "" {
		return ""
	}

	// Check sibling "python" directory: <harnessDir>/../python and
	// <harnessDir>/python.
	harnessDir := filepath.Dir(harnessPath)
	candidates := []string{
		filepath.Join(filepath.Dir(harnessDir), "python"),
		filepath.Join(harnessDir, "python"),
	}

	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(c, "agentpaas_sdk")); err == nil && info.IsDir() {
			return c
		}
	}

	// Check if the binary is running from a repo build (bin/ directory).
	if exePath, err := Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		// If exeDir is bin/, check ../python
		repoPython := filepath.Join(exeDir, "..", "python")
		if info, err := os.Stat(filepath.Join(repoPython, "agentpaas_sdk")); err == nil && info.IsDir() {
			return repoPython
		}
	}

	return ""
}
