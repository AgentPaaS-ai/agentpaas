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

func TestHarnessBinaryForPlatform_PrefersLinuxAMD64(t *testing.T) {
	dir := t.TempDir()
	amd64Harness := filepath.Join(dir, "agentpaas-harness-linux-amd64")
	armHarness := filepath.Join(dir, "agentpaas-harness-linux")
	daemonBinary := filepath.Join(dir, "agentpaasd")
	for _, p := range []string{amd64Harness, armHarness, daemonBinary} {
		writeStub(t, p)
	}

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	if got := HarnessBinaryForPlatform("linux/amd64"); got != amd64Harness {
		t.Fatalf("HarnessBinaryForPlatform(linux/amd64) = %q, want %q", got, amd64Harness)
	}
}

func TestHarnessBinaryForPlatform_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	armHarness := filepath.Join(dir, "agentpaas-harness-linux")
	daemonBinary := filepath.Join(dir, "agentpaasd")
	for _, p := range []string{armHarness, daemonBinary} {
		writeStub(t, p)
	}

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	if got := HarnessBinaryForPlatform("linux/amd64"); got != armHarness {
		t.Fatalf("HarnessBinaryForPlatform fallback = %q, want %q", got, armHarness)
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

func TestHarnessBinary_NoPathFallback(t *testing.T) {
	// PATH must not silently resolve a harness; only adjacent/env paths count.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "agentpaas-harness-linux")
	writeStub(t, fakeBin)

	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	t.Setenv("PATH", dir)
	t.Setenv("AGENTPAAS_HARNESS_PATH", "")

	got := HarnessBinary()
	if got != "" {
		t.Fatalf("HarnessBinary() PATH fallback = %q, want \"\" (PATH lookup removed)", got)
	}
}

func TestHarnessBinaryForPlatform_NoPathFallback(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "agentpaas-harness-linux-amd64")
	writeStub(t, fakeBin)

	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	t.Setenv("PATH", dir)
	t.Setenv("AGENTPAAS_HARNESS_PATH", "")

	got := HarnessBinaryForPlatform("linux/amd64")
	if got != "" {
		t.Fatalf("HarnessBinaryForPlatform PATH fallback = %q, want \"\"", got)
	}
}

func TestHarnessBinary_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	envHarness := filepath.Join(dir, "pinned-harness")
	writeStub(t, envHarness)

	// Sibling harness would otherwise win without env override.
	exeDir := t.TempDir()
	sibling := filepath.Join(exeDir, "agentpaas-harness-linux")
	daemonBinary := filepath.Join(exeDir, "agentpaasd")
	writeStub(t, sibling)
	writeStub(t, daemonBinary)

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	t.Setenv("AGENTPAAS_HARNESS_PATH", envHarness)

	got := HarnessBinary()
	if got != envHarness {
		t.Fatalf("HarnessBinary() env override = %q, want %q", got, envHarness)
	}
}

func TestHarnessBinaryForPlatform_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	envHarness := filepath.Join(dir, "pinned-harness-amd64")
	writeStub(t, envHarness)

	exeDir := t.TempDir()
	amd64Sibling := filepath.Join(exeDir, "agentpaas-harness-linux-amd64")
	daemonBinary := filepath.Join(exeDir, "agentpaasd")
	writeStub(t, amd64Sibling)
	writeStub(t, daemonBinary)

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	t.Setenv("AGENTPAAS_HARNESS_PATH", envHarness)

	got := HarnessBinaryForPlatform("linux/amd64")
	if got != envHarness {
		t.Fatalf("HarnessBinaryForPlatform env override = %q, want %q", got, envHarness)
	}
}

func TestHarnessBinary_EnvOverrideMissingFallsThrough(t *testing.T) {
	exeDir := t.TempDir()
	sibling := filepath.Join(exeDir, "agentpaas-harness-linux")
	daemonBinary := filepath.Join(exeDir, "agentpaasd")
	writeStub(t, sibling)
	writeStub(t, daemonBinary)

	oldExe := Executable
	Executable = func() (string, error) { return daemonBinary, nil }
	t.Cleanup(func() { Executable = oldExe })

	t.Setenv("AGENTPAAS_HARNESS_PATH", filepath.Join(t.TempDir(), "nonexistent"))

	got := HarnessBinary()
	if got != sibling {
		t.Fatalf("HarnessBinary() with missing env path = %q, want sibling %q", got, sibling)
	}
}

func TestHarnessBinary_NotFound(t *testing.T) {
	emptyDir := t.TempDir()
	oldExe := Executable
	Executable = func() (string, error) { return filepath.Join(emptyDir, "agentpaasd"), nil }
	t.Cleanup(func() { Executable = oldExe })

	otherDir := t.TempDir()
	t.Setenv("PATH", otherDir)
	t.Setenv("AGENTPAAS_HARNESS_PATH", "")

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
	t.Setenv("AGENTPAAS_HARNESS_PATH", "")

	got := HarnessBinary()
	// Resolve both paths through EvalSymlinks for comparison — on macOS
	// /var is a symlink to /private/var, so TempDir paths may differ.
	gotResolved, _ := filepath.EvalSymlinks(got)
	wantResolved, _ := filepath.EvalSymlinks(harnessLinux)
	if gotResolved != wantResolved {
		t.Fatalf("HarnessBinary() = %q, want %q (should resolve symlink and find sibling in Cellar)", got, harnessLinux)
	}
}
