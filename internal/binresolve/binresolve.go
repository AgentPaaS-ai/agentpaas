// Package binresolve resolves the agentpaas-harness binary and Python SDK
// directory shared by the daemon pack path and the CLI install path.
//
// The logic here is ported from the daemon's internal helpers
// (resolveHarnessBinary / resolveSDKDir) so both code paths stay in sync.
package binresolve

import (
	"os"
	"path/filepath"
)

// Executable returns the path to the current executable. Tests may override it.
var Executable = os.Executable

// HarnessBinary finds the agentpaas-harness binary for container images.
// It prefers the linux/arm64 cross-compile (agentpaas-harness-linux) over the
// darwin/arm64 Mac binary (agentpaas-harness).
//
// Resolution order:
//  1. AGENTPAAS_HARNESS_PATH env var (CI/operator pin).
//  2. Sibling in the same directory as the running executable (preferred —
//     keeps the harness bundled with the daemon, avoiding stale brew
//     installations when running from a repo build).
//  3. ../bin/ relative to the executable (repo build layout).
//  4. The darwin binary as a fallback sibling.
//
// PATH is intentionally not consulted — a stale brew binary on PATH would
// otherwise be embedded silently. Callers must treat "" as an error.
func HarnessBinary() string {
	// AGENTPAAS_HARNESS_PATH env var overrides all resolution logic.
	// Used by CI and operators who want to pin a specific binary.
	if p := os.Getenv("AGENTPAAS_HARNESS_PATH"); p != "" {
		if p := harnessCandidate(p); p != "" {
			return p
		}
	}

	exePath, err := Executable()
	if err == nil {
		// If the executable is itself a symlink (common with brew
		// installations: /opt/homebrew/bin/agentpaasd -> Cellar path),
		// resolve it so we look for the harness next to the real binary,
		// not next to the symlink. We only resolve the leaf file, not
		// every component of the path (avoids /var -> /private/var on
		// macOS breaking path comparisons).
		if fi, lerr := os.Lstat(exePath); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			if realExe, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
				exePath = realExe
			}
		}
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
	return ""
}

// HarnessBinaryForPlatform resolves a harness for an explicit target platform.
// linux/amd64 uses the architecture-qualified binary first so a host ARM
// installation cannot accidentally be embedded in an x86-64 image.
//
// Resolution order:
//  1. AGENTPAAS_HARNESS_PATH env var (CI/operator pin).
//  2. For linux/amd64: sibling / ../bin agentpaas-harness-linux-amd64.
//  3. HarnessBinary() (sibling linux then darwin; no PATH).
//
// Returns "" if not found; callers must treat that as an error.
func HarnessBinaryForPlatform(platform string) string {
	// AGENTPAAS_HARNESS_PATH env var overrides all resolution logic.
	// Used by CI and operators who want to pin a specific binary.
	if p := os.Getenv("AGENTPAAS_HARNESS_PATH"); p != "" {
		if p := harnessCandidate(p); p != "" {
			return p
		}
	}

	if platform != "linux/amd64" {
		return HarnessBinary()
	}

	exePath, err := Executable()
	if err == nil {
		if fi, lerr := os.Lstat(exePath); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			if realExe, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
				exePath = realExe
			}
		}
		exeDir := filepath.Dir(exePath)
		for _, candidate := range []string{
			filepath.Join(exeDir, "agentpaas-harness-linux-amd64"),
			filepath.Join(exeDir, "..", "bin", "agentpaas-harness-linux-amd64"),
		} {
			if p := harnessCandidate(candidate); p != "" {
				return p
			}
		}
	}
	return HarnessBinary()
}

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
