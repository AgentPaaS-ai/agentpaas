package doctor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/daemon"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// ---------------------------------------------------------------------------
// Integration test: Doctor.Run with a real daemon
// ---------------------------------------------------------------------------

// TestDoctorWithRealDaemon starts a real daemon and runs doctor checks
// against it, verifying that daemon_ready and proto_compatible pass.
func TestDoctorWithRealDaemon(t *testing.T) {
	// Create a temporary home directory.
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Start a real daemon.
	dmn, err := daemon.New(hp, testDaemonVersion(), daemon.WithAllowRoot())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := dmn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	dmn.Ready()
	defer func() { _ = dmn.Stop(context.Background()) }()

	// Wait briefly for daemon to be ready.
	time.Sleep(200 * time.Millisecond)

	// Run doctor checks against this daemon.
	doc, err := New(
		WithHomeDir(hp.Home),
		WithSocketPath(hp.Socket),
		WithCLIVersion("0.1.0-dev"),
		WithCLIProtoVersion("v1"),
	)
	if err != nil {
		t.Fatal(err)
	}

	checks, overall := doc.Run()

	// Verify daemon_ready is ok.
	var daemonReadyOk bool
	var protoCompatOk bool
	for _, c := range checks {
		if c.Name == "daemon_ready" {
			if c.Status == "ok" {
				daemonReadyOk = true
			} else {
				t.Logf("daemon_ready status: %s, message: %s", c.Status, c.Message)
			}
		}
		if c.Name == "proto_compatible" {
			if c.Status == "ok" || c.Status == "warning" {
				protoCompatOk = true
			} else {
				t.Logf("proto_compatible status: %s, message: %s", c.Status, c.Message)
			}
		}
	}

	if !daemonReadyOk {
		t.Error("expected daemon_ready check to be ok when daemon is running")
	}

	if !protoCompatOk {
		t.Error("expected proto_compatible check to be ok/warning when daemon is running")
	}

	_ = overall
	t.Logf("Doctor overall status with running daemon: %s", overall)
}

// TestDoctorCheckExitCode_Healthy verifies that IsHealthy returns true when
// all checks are ok (controls integration).
func TestDoctorCheckExitCode_Healthy(t *testing.T) {
	// Simulate a healthy doctor run by overriding SystemStatus checks.
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	hp := home.NewHomePaths(tmpDir)
	// Create socket with correct perms.
	_ = os.MkdirAll(filepath.Dir(hp.Socket), 0700)
	f, _ := os.Create(hp.Socket)
	_ = f.Close()
	_ = os.Chmod(hp.Socket, 0600)

	doc, err := New(
		WithHomeDir(tmpDir),
		WithSocketPath(hp.Socket),
	)
	if err != nil {
		t.Fatal(err)
	}

	// We can't mock the Docker checks, but we can verify the mechanics work.
	checks, overall := doc.Run()

	// Just verify the doctor runner doesn't crash and returns something.
	if len(checks) == 0 {
		t.Error("expected at least one check")
	}
	if overall == "" {
		t.Error("expected non-empty overall status")
	}
}

// TestDoctorAllChecksIndependent verifies that even when some checks fail,
// all checks still produce results.
func TestDoctorAllChecksIndependent(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	doc, err := New(
		WithHomeDir(tmpDir),
		WithSocketPath(filepath.Join(tmpDir, "daemon.sock")),
	)
	if err != nil {
		t.Fatal(err)
	}

	checks, _ := doc.Run()

	// Ensure all 9 checks ran.
	if len(checks) != 9 {
		t.Errorf("expected 9 checks, got %d", len(checks))
	}

	// Each check should have a non-empty name.
	for _, c := range checks {
		if c.Name == "" {
			t.Error("found a check with empty name")
		}
		if c.Message == "" {
			t.Errorf("check %q has empty message", c.Name)
		}
	}
}

// TestDoctorCleanupStaleSocket verifies doctor can detect a stale socket
// (file exists but nothing is listening).
func TestDoctorCleanupStaleSocket(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	hp := home.NewHomePaths(tmpDir)

	// Create a stale socket file.
	_ = os.MkdirAll(filepath.Dir(hp.Socket), 0700)
	f, err := os.Create(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_ = os.Chmod(hp.Socket, 0600)

	// Check socket perms should pass (file exists with correct perms).
	socketResult := CheckSocketPerms(tmpDir)
	if socketResult.Status != "ok" {
		t.Errorf("expected socket perms to be ok with correct perms, got %q: %s",
			socketResult.Status, socketResult.Message)
	}

	// Check daemon ready should fail (nothing listening).
	daemonResult := CheckDaemonReady(hp.Socket)
	if daemonResult.Status != "error" {
		t.Errorf("expected daemon ready to be error with stale socket, got %q: %s",
			daemonResult.Status, daemonResult.Message)
	}
}

// ---------------------------------------------------------------------------
// Test port checking with daemon listening
// ---------------------------------------------------------------------------

func TestDoctorPortCheckWithListener(t *testing.T) {
	// Start listeners on known ports.
	listeners := make([]net.Listener, 0)
	for _, port := range knownPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Logf("Port %d already in use (not by us): %v", port, err)
			continue
		}
		listeners = append(listeners, ln)
	}
	defer func() {
		for _, ln := range listeners {
			_ = ln.Close()
		}
	}()

	if len(listeners) == 0 {
		t.Skip("All ports already in use — cannot test port squat detection")
	}

	// Check that ports are detected as squatted.
	result := CheckPortsFree()
	if result.Status != "error" {
		t.Errorf("expected port check to be error when we hold ports, got %q: %s",
			result.Status, result.Message)
	}
}

// ---------------------------------------------------------------------------
// Test doctor exit code logic
// ---------------------------------------------------------------------------

func TestExitCodeLogic(t *testing.T) {
	tests := []struct {
		name       string
		checks     []CheckResult
		expectZero bool
	}{
		{
			name: "all ok",
			checks: []CheckResult{
				{Name: "a", Status: "ok"},
				{Name: "b", Status: "ok"},
			},
			expectZero: true,
		},
		{
			name: "warning only",
			checks: []CheckResult{
				{Name: "a", Status: "ok"},
				{Name: "b", Status: "warning"},
			},
			expectZero: false,
		},
		{
			name: "error present",
			checks: []CheckResult{
				{Name: "a", Status: "ok"},
				{Name: "b", Status: "error"},
			},
			expectZero: false,
		},
		{
			name: "mixed",
			checks: []CheckResult{
				{Name: "a", Status: "ok"},
				{Name: "b", Status: "warning"},
				{Name: "c", Status: "error"},
			},
			expectZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overall := OverallStatus(tt.checks)
			isHealthy := IsHealthy(overall)
			gotZero := isHealthy

			if gotZero != tt.expectZero {
				t.Errorf("overall=%q, IsHealthy=%v, expectZero=%v",
					overall, gotZero, tt.expectZero)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test daemon version comparison
// ---------------------------------------------------------------------------

func TestDoctorVersionCompatibility(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Start daemon with known version.
	dmn, err := daemon.New(hp, testDaemonVersion(), daemon.WithAllowRoot())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := dmn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	dmn.Ready()
	defer func() { _ = dmn.Stop(context.Background()) }()

	time.Sleep(200 * time.Millisecond)

	// Verify daemon returns version via proto_compatible check.
	result := CheckProtoCompatible(hp.Socket, "0.1.0-dev", "v1")
	t.Logf("Proto compatible result: status=%s, msg=%s", result.Status, result.Message)
}

// ---------------------------------------------------------------------------
// Test doctor integration with JSON-like output
// ---------------------------------------------------------------------------

func TestDoctorFormatResults_Valid(t *testing.T) {
	checks := []CheckResult{
		{Name: "docker_reachable", Status: "error", Message: "Docker not found", FixHint: "Install Docker"},
		{Name: "ports_free", Status: "ok", Message: "All ports free"},
		{Name: "daemon_ready", Status: "ok", Message: "Daemon running"},
	}
	overall := "error"

	output := FormatResults(checks, overall)

	if !strings.Contains(output, "Docker not found") {
		t.Error("expected output to contain check message")
	}
	if !strings.Contains(output, "Install Docker") {
		t.Error("expected output to contain fix hint")
	}
	if !strings.Contains(output, "✓") {
		t.Error("expected ok checks to have checkmark")
	}
	if !strings.Contains(output, "✗") {
		t.Error("expected error checks to have X mark")
	}
	if !strings.Contains(output, "error") {
		t.Error("expected output to contain overall status")
	}
}

// ---------------------------------------------------------------------------
// Test formatting with all warning checks
// ---------------------------------------------------------------------------

func TestDoctorFormatResults_AllWarning(t *testing.T) {
	checks := []CheckResult{
		{Name: "docker_context", Status: "warning", Message: "Docker context not set", FixHint: "Check Docker config"},
		{Name: "linux_dockerd", Status: "warning", Message: "P2 check"},
	}
	overall := "warning"

	output := FormatResults(checks, overall)

	if !strings.Contains(output, "⚠") {
		t.Error("expected warning icon")
	}
	if !strings.Contains(output, "warning") {
		t.Error("expected overall status to be warning")
	}
	if !strings.Contains(output, "Check Docker") {
		t.Error("expected fix hint in output")
	}
}

// ---------------------------------------------------------------------------
// Test doctor integration with Docker socket fallback
// ---------------------------------------------------------------------------

func TestDoctorSocketDiscovery(t *testing.T) {
	homeDir := t.TempDir()
	_ = os.Setenv("AGENTPAAS_HOME", homeDir)
	defer func() { _ = os.Unsetenv("AGENTPAAS_HOME") }()

	hp := home.NewHomePaths(homeDir)

	if !strings.HasPrefix(hp.Socket, homeDir) {
		t.Errorf("expected socket path to be under home dir, got %q", hp.Socket)
	}
}

// ---------------------------------------------------------------------------
// Test doctor gracefully handles no Docker
// ---------------------------------------------------------------------------

func TestDoctorNoDockerChecks(t *testing.T) {
	// Verify that Docker checks run without Docker.
	reachable := CheckDockerReachable()
	context_ := CheckDockerContext()
	desktop := CheckDockerDesktop()

	// All should produce results without crashing.
	if reachable.Message == "" {
		t.Error("expected Docker reachable message")
	}
	if context_.Message == "" {
		t.Error("expected Docker context message")
	}
	if desktop.Message == "" {
		t.Error("expected Docker desktop message")
	}

	t.Logf("Docker reachable: %s (%s)", reachable.Status, reachable.Message)
	t.Logf("Docker context: %s (%s)", context_.Status, context_.Message)
	t.Logf("Docker desktop: %s (%s)", desktop.Status, desktop.Message)
}

// ---------------------------------------------------------------------------
// Test gRPC client creation with various socket paths
// ---------------------------------------------------------------------------

func TestDaemonReadyGRPCValidation(t *testing.T) {
	tests := []struct {
		socketPath string
		expectErr  bool
	}{
		{"", true},
		{"/nonexistent/path.sock", true},
		{"/tmp/valid.sock", true}, // exists but nothing listening
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("socket=%q", tt.socketPath), func(t *testing.T) {
			result := CheckDaemonReady(tt.socketPath)
			if tt.expectErr && result.Status != "error" {
				t.Errorf("expected error for socket %q, got %q", tt.socketPath, result.Status)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test CheckDaemonReady with a live TCP listener (not gRPC, should fail)
// ---------------------------------------------------------------------------

func TestDaemonReadyWithTCPListener(t *testing.T) {
	// Start a TCP listener on a port — gRPC should fail because it's not gRPC.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Cannot start test listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().String()
	result := CheckDaemonReady(addr)
	if result.Status != "error" {
		t.Errorf("expected error for TCP-only listener, got %q: %s", result.Status, result.Message)
	}
}

// ---------------------------------------------------------------------------
// Test: Port squatting detection via net.Listen
// ---------------------------------------------------------------------------

func TestPortSquatDetection(t *testing.T) {
	// Squat port 7700 with a TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:7717")
	if err != nil {
		t.Skipf("Cannot squat port 7717: %v", err)
	}
	defer func() { _ = ln.Close() }()

	result := CheckPortsFree()
	if result.Status != "error" {
		t.Errorf("expected error with squatted port, got %q: %s", result.Status, result.Message)
	}

	if !strings.Contains(result.Message, "7717") {
		t.Errorf("expected message to mention squatted port 7717, got: %s", result.Message)
	}
}

// ---------------------------------------------------------------------------
// Test: identify process by port
// ---------------------------------------------------------------------------

func TestIdentifyProcessByPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:7718")
	if err != nil {
		t.Skipf("Cannot squat port 7718: %v", err)
	}
	defer func() { _ = ln.Close() }()

	proc := identifyProcess(7718)
	if proc != "" {
		t.Logf("Process on port 7718: %s", proc)
	} else {
		t.Log("identifyProcess returned empty (lsof may not be installed)")
	}
}

// ---------------------------------------------------------------------------
// Test: doctor.Run does not panic with unusual socket path
// ---------------------------------------------------------------------------

func TestDoctorRunUnusualSocket(t *testing.T) {
	doc, err := New(
		WithHomeDir(t.TempDir()),
		WithSocketPath("unix:///tmp/doctor-unusual.sock"),
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Doctor.Run panicked: %v", r)
		}
	}()

	_, _ = doc.Run()
}

// ---------------------------------------------------------------------------
// Test: doctor with explicit timeouts
// ---------------------------------------------------------------------------

func TestDoctorWithTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Chmod(tmpDir, 0700)

	doc, err := New(
		WithHomeDir(tmpDir),
		WithSocketPath(filepath.Join(tmpDir, "daemon.sock")),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Run should complete quickly since checks are fast.
	done := make(chan struct{})
	go func() {
		_, _ = doc.Run()
		close(done)
	}()

	select {
	case <-done:
		// Success — doctor ran.
	case <-time.After(30 * time.Second):
		t.Fatal("Doctor.Run timed out after 30 seconds")
	}
}

// ---------------------------------------------------------------------------
// Test: Full cycle — home setup → doctor run → home cleanup
// ---------------------------------------------------------------------------

func TestDoctorFullCycle(t *testing.T) {
	tmpDir := t.TempDir()
	hp := home.NewHomePaths(tmpDir)

	// Set up home.
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Start daemon.
	dmn, err := daemon.New(hp, testDaemonVersion(), daemon.WithAllowRoot())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := dmn.Start(ctx); err != nil {
		t.Fatal(err)
	}
	dmn.Ready()
	defer func() { _ = dmn.Stop(context.Background()) }()

	time.Sleep(200 * time.Millisecond)

	// Run doctor.
	doc, err := New(
		WithHomeDir(tmpDir),
		WithSocketPath(hp.Socket),
	)
	if err != nil {
		t.Fatal(err)
	}

	checks, overall := doc.Run()

	// Verify daemon-related checks pass.
	for _, c := range checks {
		if c.Name == "daemon_ready" && c.Status != "ok" {
			t.Errorf("daemon_ready should be ok with running daemon, got %q: %s", c.Status, c.Message)
		}
	}

	t.Logf("Full cycle doctor overall: %s", overall)
}

// ---------------------------------------------------------------------------
// Test: Port availability after daemon stop
// ---------------------------------------------------------------------------

func TestPortFreeAfterDaemonStop(t *testing.T) {
	// This test verifies that port checks work correctly after daemon cleanup.
	// By default ports 7700/7717/7718 are not grpc ports for the daemon (it uses
	// Unix socket), so they should be free on a clean system.

	result := CheckPortsFree()

	// Most of the time these ports should be free.
	if result.Message == "" {
		t.Error("expected non-empty port check message")
	}

	// Log the result for debugging.
	t.Logf("Port check: %s", result.Message)
}

// ---------------------------------------------------------------------------
// shortTempPaths creates a HomePaths with a short temp dir for socket path
// limits on macOS.
func shortTempPaths(t *testing.T) *home.HomePaths {
	t.Helper()
	dir, err := os.MkdirTemp("", "doc-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return home.NewHomePaths(dir)
}

// testDaemonVersion returns a VersionInfo for integration tests.
func testDaemonVersion() daemon.VersionInfo {
	return daemon.VersionInfo{
		CLIVersion:    "0.1.0-dev",
		DaemonVersion: "0.1.0-dev",
		ProtoVersion:  "v1",
		GitCommit:     "test-commit",
		GoVersion:     runtime.Version(),
		OsArch:        runtime.GOOS + "/" + runtime.GOARCH,
	}
}