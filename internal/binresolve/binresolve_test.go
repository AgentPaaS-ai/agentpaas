package binresolve

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStub(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}

func TestHarnessBinary_PrefersLinuxNextToExe(t *testing.T) {
	dir := t.TempDir()
	linuxHarness := filepath.Join(dir, "agentpaas-harness-linux")
	macHarness := filepath.Join(dir, "agentpaas-harness")
	daemonBinary := filepath.Join(dir, "agentpaasd")
	for _, p := range []string{linuxHarness, macHarness, daemonBinary} {
		writeStub(t, p)
	}

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	got := HarnessBinary()
	if got != linuxHarness {
		t.Fatalf("HarnessBinary() = %q, want %q", got, linuxHarness)
	}
}

func TestHarnessBinary_FallsBackToMacWhenNoLinux(t *testing.T) {
	dir := t.TempDir()
	macHarness := filepath.Join(dir, "agentpaas-harness")
	daemonBinary := filepath.Join(dir, "agentpaasd")
	for _, p := range []string{macHarness, daemonBinary} {
		writeStub(t, p)
	}

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	got := HarnessBinary()
	if got != macHarness {
		t.Fatalf("HarnessBinary() = %q, want %q", got, macHarness)
	}
}

func TestHarnessBinary_PathFallback(t *testing.T) {
	// When no harness is next to the exe, fall back to PATH.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "agentpaas-harness-linux")
	writeStub(t, fakeBin)

	// Point the exe to a directory with NO harness files so we exercise PATH.
	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	// Save and restore PATH.
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir)
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	got := HarnessBinary()
	if got != fakeBin {
		t.Fatalf("HarnessBinary() PATH fallback = %q, want %q", got, fakeBin)
	}
}

func TestHarnessBinary_NotFound(t *testing.T) {
	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	// PATH with no harness binary.
	otherDir := t.TempDir()
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", otherDir)
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })

	if got := HarnessBinary(); got != "" {
		t.Fatalf("HarnessBinary() = %q, want \"\"", got)
	}
}

func TestSDKDir_FindsAgentpaasSDK(t *testing.T) {
	dir := t.TempDir()
	// Simulate a harness at <dir>/bin/agentpaas-harness-linux with SDK at
	// <dir>/python/agentpaas_sdk.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%s) error = %v", binDir, err)
	}
	harnessPath := filepath.Join(binDir, "agentpaas-harness-linux")
	writeStub(t, harnessPath)

	sdkDir := filepath.Join(dir, "python", "agentpaas_sdk")
	if err := os.MkdirAll(sdkDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%s) error = %v", sdkDir, err)
	}

	got := SDKDir(harnessPath)
	want := filepath.Join(dir, "python")
	if got != want {
		t.Fatalf("SDKDir() = %q, want %q", got, want)
	}
}

func TestSDKDir_EmptyHarnessReturnsEmpty(t *testing.T) {
	if got := SDKDir(""); got != "" {
		t.Fatalf("SDKDir(\"\") = %q, want \"\"", got)
	}
}

func TestSDKDir_NotFound(t *testing.T) {
	dir := t.TempDir()
	harnessPath := filepath.Join(dir, "agentpaas-harness-linux")
	writeStub(t, harnessPath)

	// Override Executable so it doesn't find a repo python dir either.
	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	if got := SDKDir(harnessPath); got != "" {
		t.Fatalf("SDKDir() = %q, want \"\"", got)
	}
}

func TestHarnessBinary_ResolvesExeSymlink(t *testing.T) {
	// Simulate a brew-installed setup where the binary is symlinked from
	// /opt/homebrew/bin/agentpaasd -> /opt/homebrew/Cellar/agentpaas/0.3.0/bin/agentpaasd,
	// and the harness lives next to the real binary in the Cellar.
	cellarDir := t.TempDir()
	brewDir := t.TempDir()

	realDaemon := filepath.Join(cellarDir, "agentpaasd")
	harnessLinux := filepath.Join(cellarDir, "agentpaas-harness-linux")
	symlinkDaemon := filepath.Join(brewDir, "agentpaasd")

	writeStub(t, realDaemon)
	writeStub(t, harnessLinux)

	if err := os.Symlink(realDaemon, symlinkDaemon); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	oldExe := Executable
	Executable = func() (string, error) { return symlinkDaemon, nil }
	t.Cleanup(func() { Executable = oldExe })

	got := HarnessBinary()
	// Resolve both paths through EvalSymlinks for comparison — on macOS
	// /var is a symlink to /private/var, so TempDir paths may differ.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(harnessLinux)
	if gotResolved != wantResolved {
		t.Fatalf("HarnessBinary() = %q, want %q (should resolve symlink and find sibling in Cellar)", got, harnessLinux)
	}
}
