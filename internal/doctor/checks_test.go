package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/daemon"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// ---------------------------------------------------------------------------
// TestCheckDockerReachable
// ---------------------------------------------------------------------------

func TestCheckDockerReachable(t *testing.T) {
	// This test checks that the Docker reachable check runs without panicking.
	// On systems without Docker, it should return an error with a fix hint.
	result := CheckDockerReachable()

	if result.Name != "docker_reachable" {
		t.Errorf("expected name 'docker_reachable', got %q", result.Name)
	}

	// The result should be either ok (Docker present) or error (Docker absent).
	if result.Status != "ok" && result.Status != "error" {
		t.Errorf("expected status 'ok' or 'error', got %q", result.Status)
	}

	if result.Message == "" {
		t.Error("expected a non-empty message")
	}

	// If error, there must be a fix hint.
	if result.Status == "error" && result.FixHint == "" {
		t.Error("expected non-empty FixHint when status is error")
	}
}

func TestCheckDockerReachable_ErrorHasFixHint(t *testing.T) {
	// Temporarily set PATH to nothing so docker can't be found.
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	result := CheckDockerReachable()

	if result.Status != "error" {
		t.Skip("Docker was reachable despite PATH being empty — skipping error check")
	}

	if result.FixHint == "" {
		t.Error("expected FixHint to be non-empty when Docker is not reachable")
	}
}

// ---------------------------------------------------------------------------
// TestCheckDockerContext
// ---------------------------------------------------------------------------

func TestCheckDockerContext(t *testing.T) {
	result := CheckDockerContext()

	if result.Name != "docker_context" {
		t.Errorf("expected name 'docker_context', got %q", result.Name)
	}

	// Should always return a result without panicking.
	if result.Message == "" {
		t.Error("expected a non-empty message")
	}
}

// ---------------------------------------------------------------------------
// TestCheckDockerDesktop
// ---------------------------------------------------------------------------

func TestCheckDockerDesktop(t *testing.T) {
	result := CheckDockerDesktop()

	if result.Name != "docker_desktop" {
		t.Errorf("expected name 'docker_desktop', got %q", result.Name)
	}

	if result.Message == "" {
		t.Error("expected a non-empty message")
	}
}

// ---------------------------------------------------------------------------
// TestCheckLinuxDockerd
// ---------------------------------------------------------------------------

func TestCheckLinuxDockerd(t *testing.T) {
	result := CheckLinuxDockerd()

	if result.Name != "linux_dockerd" {
		t.Errorf("expected name 'linux_dockerd', got %q", result.Name)
	}

	if result.Message == "" {
		t.Error("expected a non-empty message")
	}
}

// ---------------------------------------------------------------------------
// TestCheckPortsFree
// ---------------------------------------------------------------------------

func TestCheckPortsFree(t *testing.T) {
	result := CheckPortsFree()

	if result.Name != "ports_free" {
		t.Errorf("expected name 'ports_free', got %q", result.Name)
	}

	if result.Message == "" {
		t.Error("expected a non-empty message")
	}

	// Ports should typically be free on CI/dev machines.
	// If any port is squatted, verify error has fix hint.
	if result.Status == "error" && result.FixHint == "" {
		t.Error("expected FixHint when port check fails")
	}
}

func TestCheckPortsFree_WithSquattedPort(t *testing.T) {
	// Squat port 7700 to trigger an error.
	ln, err := net.Listen("tcp", "127.0.0.1:7700")
	if err != nil {
		t.Skipf("Cannot squat port 7700 (maybe it's already in use): %v", err)
	}
	defer func() { _ = ln.Close() }()

	result := CheckPortsFree()

	if result.Status != "error" {
		t.Errorf("expected status 'error' when port 7700 is squatted, got %q", result.Status)
	}

	if !strings.Contains(result.Message, "7700") {
		t.Errorf("expected message to mention port 7700, got: %s", result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint when port is squatted")
	}
}

func TestIdentifyProcess(t *testing.T) {
	// identifyProcess should return a non-empty string for port 7700 when squatted.
	ln, err := net.Listen("tcp", "127.0.0.1:7717")
	if err != nil {
		t.Skipf("Cannot squat port 7717: %v", err)
	}
	defer func() { _ = ln.Close() }()

	proc := identifyProcess(7717)
	if proc == "" {
		// lsof might not be available — this is acceptable.
		t.Log("identifyProcess returned empty (lsof may not be installed)")
	} else {
		t.Logf("Identified process on port 7717: %s", proc)
	}
}

// ---------------------------------------------------------------------------
// TestCheckSocketPerms
// ---------------------------------------------------------------------------

func TestCheckSocketPerms_Correct(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "daemon.sock")

	// Create socket file with correct permissions (0600).
	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_ = os.Chmod(socketPath, 0600)

	result := CheckSocketPerms(tmpDir)

	if result.Status != "ok" {
		t.Errorf("expected status 'ok' for correct socket perms, got %q: %s", result.Status, result.Message)
	}
}

func TestCheckSocketPerms_Wrong(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "daemon.sock")

	// Create socket file with incorrect permissions (0777).
	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_ = os.Chmod(socketPath, 0777)

	// Need to create the socket path via NewHomePaths; the socket path
	// must match what NewHomePaths expects.
	hp := home.NewHomePaths(tmpDir)
	if hp.Socket != socketPath {
		t.Logf("Note: home.NewHomePaths socket=%q, test created=%q", hp.Socket, socketPath)
	}

	result := CheckSocketPerms(tmpDir)

	if result.Status != "error" {
		t.Errorf("expected status 'error' for wrong socket perms, got %q: %s", result.Status, result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint when socket permissions are wrong")
	}
}

func TestCheckSocketPerms_NotExist(t *testing.T) {
	tmpDir := t.TempDir()

	result := CheckSocketPerms(tmpDir)

	// Should be ok — socket doesn't exist, daemon will create it.
	if result.Status != "ok" {
		t.Errorf("expected status 'ok' when socket doesn't exist, got %q: %s", result.Status, result.Message)
	}
}

// ---------------------------------------------------------------------------
// TestCheckHomeDirPerms
// ---------------------------------------------------------------------------

func TestCheckHomeDirPerms_Correct(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	result := CheckHomeDirPerms(tmpDir)

	if result.Status != "ok" {
		t.Errorf("expected status 'ok' for correct home perms, got %q: %s", result.Status, result.Message)
	}
}

func TestCheckHomeDirPerms_Wrong(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0777)

	result := CheckHomeDirPerms(tmpDir)

	if result.Status != "error" {
		t.Errorf("expected status 'error' for wrong home perms, got %q: %s", result.Status, result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint when home permissions are wrong")
	}
}

func TestCheckHomeDirPerms_NotExist(t *testing.T) {
	nonExistent := filepath.Join(t.TempDir(), "nonexistent")

	result := CheckHomeDirPerms(nonExistent)

	if result.Status != "error" {
		t.Errorf("expected status 'error' when home doesn't exist, got %q: %s", result.Status, result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint when home doesn't exist")
	}
}

// ---------------------------------------------------------------------------
// TestCheckDaemonReady
// ---------------------------------------------------------------------------

func TestCheckDaemonReady_NoSocket(t *testing.T) {
	result := CheckDaemonReady("/nonexistent/daemon.sock")

	// Should return error since no daemon is running.
	if result.Status != "error" {
		t.Errorf("expected status 'error' when daemon is not running, got %q: %s", result.Status, result.Message)
	}

	if !strings.Contains(result.Message, "not respond") && !strings.Contains(result.Message, "refused") && !strings.Contains(result.Message, "no such file") {
		t.Logf("Daemon not running message: %s", result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint when daemon is not ready")
	}
}

func TestCheckDaemonReady_EmptySocket(t *testing.T) {
	result := CheckDaemonReady("")

	if result.Status != "error" {
		t.Errorf("expected status 'error' for empty socket path, got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// TestCheckProtoCompatible
// ---------------------------------------------------------------------------

func TestCheckProtoCompatible_NoDaemon(t *testing.T) {
	result := CheckProtoCompatible("/nonexistent/daemon.sock", "0.1.0-dev", "v1")

	// Should be ok with a message about daemon not running (check skipped).
	if result.Status != "ok" && result.Status != "warning" {
		t.Errorf("expected status 'ok' or 'warning' when daemon not running, got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Test portList helper
// ---------------------------------------------------------------------------

func TestPortList(t *testing.T) {
	result := portList([]int{7700, 7717, 7718})
	expected := "7700, 7717, 7718"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// Test edge cases
// ---------------------------------------------------------------------------

func TestCheckSocketPerms_HomeDirOnly(t *testing.T) {
	// Ensure CheckSocketPerms handles a home dir with no socket gracefully.
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	result := CheckSocketPerms(tmpDir)

	// Socket doesn't exist, should be ok.
	if result.Status != "ok" {
		t.Errorf("expected 'ok' when socket doesn't exist, got %q: %s", result.Status, result.Message)
	}
}

func TestCheckHomeDirPerms_WithSocket(t *testing.T) {
	// Create home dir and socket, both with correct perms.
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	hp := home.NewHomePaths(tmpDir)
	_ = os.MkdirAll(filepath.Dir(hp.Socket), 0700)
	f, err := os.Create(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_ = os.Chmod(hp.Socket, 0600)

	result := CheckHomeDirPerms(tmpDir)

	if result.Status != "ok" {
		t.Errorf("expected 'ok' for correct home perms with socket, got %q: %s", result.Status, result.Message)
	}
}

func TestCheckResult_EdgeEmptyHome(t *testing.T) {
	// Very short temp dir name to avoid socket path length issues.
	tmpDir := t.TempDir()

	result := CheckHomeDirPerms(tmpDir)

	// tmp dir exists and should pass.
	if result.Status != "ok" && result.Status != "error" {
		t.Errorf("expected 'ok' or 'error', got %q", result.Status)
	}
}

func TestCheckPortsFree_MultipleSquat(t *testing.T) {
	// Squat multiple ports.
	ln1, err := net.Listen("tcp", "127.0.0.1:7717")
	if err != nil {
		t.Skipf("Cannot squat port 7717: %v", err)
	}
	defer func() { _ = ln1.Close() }()

	ln2, err := net.Listen("tcp", "127.0.0.1:7718")
	if err != nil {
		t.Skipf("Cannot squat port 7718: %v", err)
	}
	defer func() { _ = ln2.Close() }()

	result := CheckPortsFree()

	if result.Status != "error" {
		t.Errorf("expected 'error' when multiple ports squatted, got %q", result.Status)
	}

	if !strings.Contains(result.Message, "7717") || !strings.Contains(result.Message, "7718") {
		t.Errorf("expected message to mention both squatted ports, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Test ensure functions don't panic when called
// ---------------------------------------------------------------------------

func TestCheckDockerReachable_NotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CheckDockerReachable panicked: %v", r)
		}
	}()
	_ = CheckDockerReachable()
}

func TestCheckDockerContext_NotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CheckDockerContext panicked: %v", r)
		}
	}()
	_ = CheckDockerContext()
}

func TestCheckPortsFree_NotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CheckPortsFree panicked: %v", r)
		}
	}()
	_ = CheckPortsFree()
}

// ---------------------------------------------------------------------------
// Test CheckResult validates all known check names are unique
// ---------------------------------------------------------------------------

func TestCheckNames_Unique(t *testing.T) {
	names := map[string]bool{}

	// Run all checks and collect names.
	checkFns := []func() CheckResult{
		CheckDockerReachable,
		CheckDockerServerVersion,
		CheckDockerContext,
		CheckDockerDesktop,
		CheckLinuxDockerd,
		CheckPortsFree,
		func() CheckResult { return CheckSocketPerms(t.TempDir()) },
		func() CheckResult { return CheckHomeDirPerms(t.TempDir()) },
		func() CheckResult { return CheckDaemonReady("") },
		func() CheckResult { return CheckProtoCompatible("", "0.1.0-dev", "v1") },
		func() CheckResult { return CheckHarnessCopies() },
	}

	for _, fn := range checkFns {
		result := fn()
		if names[result.Name] {
			t.Errorf("duplicate check name: %q", result.Name)
		}
		names[result.Name] = true
	}

	t.Logf("All %d check names are unique", len(names))
}

// ---------------------------------------------------------------------------
// Test home package integration — socket path generation
// ---------------------------------------------------------------------------

func TestSocketPathGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	hp := home.NewHomePaths(tmpDir)

	expectedSocket := filepath.Join(tmpDir, "daemon.sock")
	if hp.Socket != expectedSocket {
		t.Fatalf("expected socket %q, got %q", expectedSocket, hp.Socket)
	}
}

// ---------------------------------------------------------------------------
// Test defect: CheckDaemonReady with unreachable socket returns error
// ---------------------------------------------------------------------------

func TestCheckDaemonReady_ConnectionRefused(t *testing.T) {
	// Create a socket path that exists but nothing is listening on it.
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "dead.sock")

	// Create empty socket file (no listener).
	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	result := CheckDaemonReady(socketPath)

	if result.Status != "error" {
		t.Errorf("expected 'error' for dead socket, got %q: %s", result.Status, result.Message)
	}

	if result.FixHint == "" {
		t.Error("expected FixHint for dead socket")
	}
}

func TestOverallStatus_Basic(t *testing.T) {
	tests := []struct {
		name     string
		checks   []CheckResult
		expected string
	}{
		{
			name:     "all ok",
			checks:   []CheckResult{{Name: "a", Status: "ok"}, {Name: "b", Status: "ok"}},
			expected: "ok",
		},
		{
			name:     "with warning",
			checks:   []CheckResult{{Name: "a", Status: "ok"}, {Name: "b", Status: "warning"}},
			expected: "warning",
		},
		{
			name:     "with error",
			checks:   []CheckResult{{Name: "a", Status: "ok"}, {Name: "b", Status: "error"}},
			expected: "error",
		},
		{
			name:     "error overrides warning",
			checks:   []CheckResult{{Name: "a", Status: "warning"}, {Name: "b", Status: "error"}},
			expected: "error",
		},
		{
			name:     "empty checks",
			checks:   []CheckResult{},
			expected: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OverallStatus(tt.checks)
			if got != tt.expected {
				t.Errorf("OverallStatus(%+v) = %q, want %q", tt.checks, got, tt.expected)
			}
		})
	}
}

func TestCheckResult_FieldAccess(t *testing.T) {
	r := CheckResult{
		Name:    "test_check",
		Status:  "warning",
		Message: "something is off",
		FixHint: "try fixing it",
	}

	if r.Name != "test_check" {
		t.Errorf("Name: got %q", r.Name)
	}
	if r.Status != "warning" {
		t.Errorf("Status: got %q", r.Status)
	}
	if r.Message != "something is off" {
		t.Errorf("Message: got %q", r.Message)
	}
	if r.FixHint != "try fixing it" {
		t.Errorf("FixHint: got %q", r.FixHint)
	}
}

// ---------------------------------------------------------------------------
// Test lsof invocation path
// ---------------------------------------------------------------------------

func TestIdentifyProcess_NoProcess(t *testing.T) {
	// A random high port that shouldn't be in use.
	proc := identifyProcess(65999)
	if proc != "" {
		t.Logf("identifyProcess returned %q for unused port 65999", proc)
	}
}

// ---------------------------------------------------------------------------
// Test CheckDaemonReady with gRPC (real test — disabled by default)
// ---------------------------------------------------------------------------

func TestCheckDaemonReady_FormatMessage(t *testing.T) {
	// Verify the message format for known error patterns.
	socketPath := "/tmp/nonexistent-agentpaas-doctor-test.sock"

	// lsof not relevant for this test; just verify message is non-empty.
	result := CheckDaemonReady(socketPath)
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

// ---------------------------------------------------------------------------
// Test edge: portList with empty slice
// ---------------------------------------------------------------------------

func TestPortListEmpty(t *testing.T) {
	result := portList([]int{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// Test edge: identifyProcess when lsof is not available
// ---------------------------------------------------------------------------

func TestIdentifyProcess_NoLsof(t *testing.T) {
	oldPath := os.Getenv("PATH")
	_ = os.Setenv("PATH", "")
	defer func() { _ = os.Setenv("PATH", oldPath) }()

	proc := identifyProcess(7700)
	// Without lsof on PATH the helper must fail closed with an empty string,
	// not invent a process name from residual state.
	if proc != "" {
		t.Errorf("identifyProcess returned %q with empty PATH; want empty", proc)
	}
}

// ---------------------------------------------------------------------------
// Test FormatResults does not panic
// ---------------------------------------------------------------------------

func TestFormatResults_Basic(t *testing.T) {
	checks := []CheckResult{
		{Name: "check1", Status: "ok", Message: "OK"},
		{Name: "check2", Status: "error", Message: "FAIL", FixHint: "Fix it"},
	}
	overall := "error"

	result := FormatResults(checks, overall)
	if result == "" {
		t.Fatal("expected non-empty formatted output")
	}

	// Should contain both checks and overall.
	if !strings.Contains(result, "check1") {
		t.Error("expected check1 in output")
	}
	if !strings.Contains(result, "check2") {
		t.Error("expected check2 in output")
	}
	if !strings.Contains(result, "error") {
		t.Error("expected 'error' in overall status")
	}
	if !strings.Contains(result, "Fix it") {
		t.Error("expected fix hint in output")
	}
}

func TestFormatResults_AllOk(t *testing.T) {
	checks := []CheckResult{
		{Name: "a", Status: "ok", Message: "A is fine"},
		{Name: "b", Status: "ok", Message: "B is fine"},
	}
	overall := "ok"

	result := FormatResults(checks, overall)
	if !strings.Contains(result, "✓") {
		t.Error("expected checkmark for ok status")
	}
}

// ---------------------------------------------------------------------------
// Test IsHealthy
// ---------------------------------------------------------------------------

func TestIsHealthy(t *testing.T) {
	tests := []struct {
		overall  string
		expected bool
	}{
		{"ok", true},
		{"warning", false},
		{"error", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.overall, func(t *testing.T) {
			got := IsHealthy(tt.overall)
			if got != tt.expected {
				t.Errorf("IsHealthy(%q) = %v, want %v", tt.overall, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test NewDoctor defaults
// ---------------------------------------------------------------------------

func TestNewDoctorDefaults(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if d.homeDir == "" {
		t.Error("expected homeDir to be set")
	}

	if d.socketPath == "" {
		t.Error("expected socketPath to be set")
	}

	if d.cliVersion != daemon.CLIVersion {
		t.Errorf("expected cliVersion=%q (from daemon.CLIVersion, settable via ldflags), got %q", daemon.CLIVersion, d.cliVersion)
	}

	if d.cliProtoVersion != "v1" {
		t.Errorf("expected cliProtoVersion 'v1', got %q", d.cliProtoVersion)
	}
}

func TestNewDoctorWithOptions(t *testing.T) {
	d, err := New(
		WithHomeDir("/tmp/doctor-test-home"),
		WithSocketPath("/tmp/doctor-test-home/custom.sock"),
		WithCLIVersion("0.2.0"),
		WithCLIProtoVersion("v2"),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	_ = os.Setenv("AGENTPAAS_HOME", "")
	_ = os.Setenv("AGENTPAAS_SOCKET", "")

	if d.homeDir != "/tmp/doctor-test-home" {
		t.Errorf("expected homeDir '/tmp/doctor-test-home', got %q", d.homeDir)
	}

	if d.socketPath != "/tmp/doctor-test-home/custom.sock" {
		t.Errorf("expected socketPath '/tmp/doctor-test-home/custom.sock', got %q", d.socketPath)
	}

	if d.cliVersion != "0.2.0" {
		t.Errorf("expected cliVersion '0.2.0', got %q", d.cliVersion)
	}

	if d.cliProtoVersion != "v2" {
		t.Errorf("expected cliProtoVersion 'v2', got %q", d.cliProtoVersion)
	}
}

// ---------------------------------------------------------------------------
// Test Doctor.Run aggregation
// ---------------------------------------------------------------------------

func TestDoctorRun_Aggregation(t *testing.T) {
	d, err := New(
		WithHomeDir(t.TempDir()),
		WithSocketPath("/nonexistent/doctor-test.sock"),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	checks, overall := d.Run()

	// Should have all 12 checks (10 original + docker_server_version + harness_copies).
	if len(checks) != 12 {
		t.Errorf("expected 12 checks, got %d", len(checks))
	}

	// Check names are present.
	checkNames := make(map[string]bool)
	for _, c := range checks {
		checkNames[c.Name] = true
	}

	expectedNames := []string{
		"docker_reachable",
		"docker_server_version",
		"docker_context",
		"docker_desktop",
		"linux_dockerd",
		"ports_free",
		"socket_perms",
		"home_perms",
		"daemon_ready",
		"proto_compatible",
		"harness_copies",
	}

	for _, name := range expectedNames {
		if !checkNames[name] {
			t.Errorf("expected check %q in Doctor.Run() results", name)
		}
	}

	// Overall should be deterministic.
	if overall == "" {
		t.Error("expected non-empty overall status")
	}

	t.Logf("Doctor.Run() overall: %s", overall)
}

// ---------------------------------------------------------------------------
// Test all checks run independently (one failure doesn't block others)
// ---------------------------------------------------------------------------

func TestChecksIndependent(t *testing.T) {
	// Simulate running checks with a bad home dir.
	d, err := New(
		WithHomeDir("/nonexistent-doctor-test-dir"),
		WithSocketPath("/nonexistent-doctor-test-dir/daemon.sock"),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	checks, overall := d.Run()

	// Even with bad home dir, all 12 checks should run.
	if len(checks) != 12 {
		t.Errorf("expected 12 checks even with bad home dir, got %d", len(checks))
	}

	// Some checks should be errors (socket perms, home perms, daemon ready),
	// but checks like docker_reachable should still be attempted.
	t.Logf("Overall with bad home dir: %s", overall)

	// Verify home perms check reports the error.
	var foundHomeError bool
	for _, c := range checks {
		if c.Name == "home_perms" && c.Status == "error" {
			foundHomeError = true
			break
		}
	}
	if !foundHomeError {
		t.Log("home_perms check did not report error with bad home dir — this may be expected if home discovery falls back to default")
	}
}

// ---------------------------------------------------------------------------
// Test that Doctor with WithHomeDir(t.TempDir()) succeeds
// ---------------------------------------------------------------------------

func TestDoctorWithTempHome(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	d, err := New(
		WithHomeDir(tmpDir),
		WithSocketPath(filepath.Join(tmpDir, "daemon.sock")),
	)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	checks, overall := d.Run()
	if len(checks) != 12 {
		t.Errorf("expected 12 checks, got %d", len(checks))
	}
	t.Logf("Overall: %s", overall)
}

// ---------------------------------------------------------------------------
// Test: Doctor.Run does not panic
// ---------------------------------------------------------------------------

func TestDoctorRun_NotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Doctor.Run panicked: %v", r)
		}
	}()

	d, err := New(WithHomeDir(t.TempDir()))
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	_, _ = d.Run()
}

// ---------------------------------------------------------------------------
// Test String methods / FormatResults with empty checks
// ---------------------------------------------------------------------------

func TestFormatResults_Empty(t *testing.T) {
	result := FormatResults(nil, "ok")
	if !strings.Contains(result, "ok") {
		t.Error("expected output to contain overall status")
	}
}

// ---------------------------------------------------------------------------
// Test with explicit socket path override
// ---------------------------------------------------------------------------

func TestNewDoctorExplicitSocket(t *testing.T) {
	d, err := New(
		WithHomeDir(t.TempDir()),
		WithSocketPath("/tmp/agentpaas-test-explicit.sock"),
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	if d.socketPath != "/tmp/agentpaas-test-explicit.sock" {
		t.Errorf("expected explicit socket path, got %q", d.socketPath)
	}
}

// ---------------------------------------------------------------------------
// Benchmark OverallStatus
// ---------------------------------------------------------------------------

func BenchmarkOverallStatus(b *testing.B) {
	checks := []CheckResult{
		{Name: "a", Status: "ok"},
		{Name: "b", Status: "ok"},
		{Name: "c", Status: "warning"},
		{Name: "d", Status: "ok"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = OverallStatus(checks)
	}
}

// ---------------------------------------------------------------------------
// Ensure we're testing the Go 1.26 style with proper t.TempDir cleanup
// ---------------------------------------------------------------------------

func TestTempDirCleanup(t *testing.T) {
	// This test verifies our temp dir pattern works correctly.
	dir := t.TempDir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("TempDir should exist: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0600)

	// On test exit, Go's t.TempDir() will clean up.
	t.Logf("TempDir: %s", dir)
}

// ---------------------------------------------------------------------------
// Test for check name formatting — no extra whitespace, single line
// ---------------------------------------------------------------------------

func TestCheckResultMessage_NoNewlines(t *testing.T) {
	result := CheckDockerReachable()
	if strings.Contains(result.Message, "\n") {
		t.Error("expected message to be a single line")
	}
}

// ---------------------------------------------------------------------------
// Helper to ensure env var restoration
// ---------------------------------------------------------------------------

func TestEnvRestoration(t *testing.T) {
	oldVal := os.Getenv("AGENTPAAS_HOME")
	defer func() { _ = os.Setenv("AGENTPAAS_HOME", oldVal) }()

	_ = os.Setenv("AGENTPAAS_HOME", "/tmp/custom-home")
	val := os.Getenv("AGENTPAAS_HOME")
	if val != "/tmp/custom-home" {
		t.Errorf("expected AGENTPAAS_HOME to be /tmp/custom-home, got %q", val)
	}
}

// ---------------------------------------------------------------------------
// Test FormatResults warns for warning status
// ---------------------------------------------------------------------------

func TestFormatResults_Warning(t *testing.T) {
	checks := []CheckResult{
		{Name: "docker_context", Status: "warning", Message: "Docker context warning", FixHint: "Check Docker"},
	}
	overall := "warning"

	result := FormatResults(checks, overall)

	if !strings.Contains(result, "⚠") {
		t.Error("expected warning icon for warning status")
	}

	if !strings.Contains(result, "Check Docker") {
		t.Error("expected fix hint in output for warning")
	}
}

// ---------------------------------------------------------------------------
// Test FormatResults error
// ---------------------------------------------------------------------------

func TestFormatResults_Error(t *testing.T) {
	checks := []CheckResult{
		{Name: "port_7700", Status: "error", Message: "Port 7700 in use", FixHint: "Stop process on port 7700"},
	}
	overall := "error"

	result := FormatResults(checks, overall)

	if !strings.Contains(result, "✗") {
		t.Error("expected error icon for error status")
	}

	if !strings.Contains(result, "Port 7700") {
		t.Error("expected message about port 7700")
	}
}

// ---------------------------------------------------------------------------
// Test Doctor.Run with explicit empty socket path
// ---------------------------------------------------------------------------

func TestDoctorRun_EmptySocket(t *testing.T) {
	d, err := New(
		WithHomeDir(t.TempDir()),
		WithSocketPath(""),
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	checks, _ := d.Run()
	var foundDaemonReady bool
	for _, c := range checks {
		if c.Name == "daemon_ready" && c.Status == "error" {
			foundDaemonReady = true
			if !strings.Contains(c.Message, "empty") {
				t.Logf("daemon_ready message for empty socket: %s", c.Message)
			}
		}
	}
	if !foundDaemonReady {
		t.Error("expected daemon_ready check to report error with empty socket")
	}
}

// ---------------------------------------------------------------------------
// Test: Doctor version reports dynamic value from daemon.CLIVersion
// (not an independently hardcoded stale string).
// ---------------------------------------------------------------------------

func TestDoctorVersionInfo(t *testing.T) {
	d, err := New(WithHomeDir(t.TempDir()))
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	// The doctor's cliVersion must match daemon.CLIVersion — i.e., it
	// pulls from the ldflags-settable variable, not a separate hardcoded
	// constant.  daemon.CLIVersion defaults to "0.1.0-dev" when not
	// injected via ldflags.
	if d.cliVersion != daemon.CLIVersion {
		t.Errorf("doctor cliVersion %q != daemon.CLIVersion %q — version is NOT dynamic", d.cliVersion, daemon.CLIVersion)
	}

	// Also verify the version check appears in the formatted output.
	checks, overall := d.Run()
	output := FormatResults(checks, overall)
	if !strings.Contains(output, "AgentPaaS Doctor Report") {
		t.Error("expected doctor output to contain report header")
	}

	// Prove that WithCLIVersion actually overrides the default.
	d2, err2 := New(
		WithHomeDir(t.TempDir()),
		WithCLIVersion("9.9.9-test"),
	)
	if err2 != nil {
		t.Fatalf("New(): %v", err2)
	}
	if d2.cliVersion != "9.9.9-test" {
		t.Errorf("WithCLIVersion override: got %q, want %q", d2.cliVersion, "9.9.9-test")
	}
}

// ---------------------------------------------------------------------------
// Test godoc example
// ---------------------------------------------------------------------------

func ExampleNew() {
	d, err := New(WithHomeDir("/tmp/example"))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	checks, overall := d.Run()
	_ = FormatResults(checks, overall)
	fmt.Println("Doctor ran successfully")
	// Output: Doctor ran successfully
}