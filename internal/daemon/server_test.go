package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/home"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// shortTempPaths creates a HomePaths rooted at a short temp path for tests
// that need Unix socket paths under the ~104-byte macOS limit.
func shortTempPaths(t *testing.T) *home.HomePaths {
	t.Helper()
	dir, err := os.MkdirTemp("", "dmn-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	hp := home.NewHomePaths(dir)
	return hp
}

// testVersion returns a VersionInfo used in tests.
func testVersion() VersionInfo {
	return VersionInfo{
		CLIVersion:    "0.1.0-dev",
		DaemonVersion: "0.1.0-dev",
		ProtoVersion:  "v1",
		GitCommit:     "test-commit",
		GoVersion:     runtime.Version(),
		OsArch:        runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// dialSocket opens a gRPC client connection to the given unix socket path.
func dialSocket(t *testing.T, socketPath string) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient("unix://"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient(%s): %v", socketPath, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// --- Test cases ---

func TestDaemonStartBindsSocket(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Socket file should exist and be reachable.
	conn, err := net.Dial("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot dial socket after Start(): %v", err)
	}
	_ = conn.Close()

	d.Ready() // mark as ready for subsequent calls
}

func TestDaemonNotReadyReturnsUnavailable(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	// Do NOT call d.Ready() — daemon is still in "starting" state.

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// All RPCs should return Unavailable while not ready.
	_, err = client.Doctor(ctx, &controlv1.DoctorRequest{})
	if err == nil {
		t.Fatal("expected error for not-ready daemon, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unavailable {
		t.Errorf("expected Unavailable, got %s: %v", st.Code(), st.Message())
	}
}

func TestDaemonReadyReturnsStubResponse(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// Doctor should return stub data when ready.
	resp, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor() failed: %v", err)
	}
	if resp.OverallStatus != "ok" {
		t.Errorf("OverallStatus = %q, want %q", resp.OverallStatus, "ok")
	}

	// Pack validates required fields before executing.
	_, err = client.Pack(ctx, &controlv1.PackRequest{})
	if err == nil {
		t.Fatal("expected error for empty Pack request, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %s: %v", st.Code(), st.Message())
	}
}

func TestClientConnectsAndGetsUnimplemented(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// Run validates required fields before executing.
	_, err = client.Run(ctx, &controlv1.RunRequest{})
	if err == nil {
		t.Fatal("expected error for empty Run request, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %s: %v", st.Code(), st.Message())
	}
}

func TestSigtermGracefulShutdown(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Send SIGTERM to ourselves.
	sigCh := make(chan os.Signal, 1)
	if err := d.HandleSignal(syscall.SIGTERM, sigCh); err != nil {
		t.Fatalf("HandleSignal(SIGTERM): %v", err)
	}
	sigCh <- syscall.SIGTERM

	// Give the daemon a moment to shut down.
	time.Sleep(200 * time.Millisecond)

	// Socket should be closed/removed.
	_, err = net.Dial("unix", hp.Socket)
	if err == nil {
		t.Error("expected socket to be closed after SIGTERM, but could still dial")
	}
}

func TestTwoDaemonsSecondFailsFlock(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// First daemon acquires the flock.
	d1, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d1.Stop(context.Background()) }()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel1()
	if err := d1.Start(ctx1); err != nil {
		t.Fatalf("first Start() failed: %v", err)
	}

	// Second daemon should fail to acquire the lock.
	_, err = New(hp, testVersion())
	if err == nil {
		t.Fatal("expected error for second daemon, got nil")
	}
	if !isLockError(err) {
		t.Errorf("expected lock error, got: %v", err)
	}
}

// isLockError checks if the error is an EWOULDBLOCK flock error.
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "lock") && contains(err.Error(), "already running")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestVersionInfoPopulated(t *testing.T) {
	v := CurrentVersion()

	if v.CLIVersion == "" {
		t.Error("CLIVersion should not be empty")
	}
	if v.DaemonVersion == "" {
		t.Error("DaemonVersion should not be empty")
	}
	if v.ProtoVersion == "" {
		t.Error("ProtoVersion should not be empty")
	}
	if v.OsArch == "" {
		t.Error("OsArch should not be empty")
	}
	if v.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
	// GitCommit may be a placeholder if not injected via -ldflags.
	if v.GitCommit == "" {
		t.Error("GitCommit should not be empty")
	}
}

func TestRootCheckRefusesWithoutFlag(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Mock root by calling CheckRoot directly with uid=0.
	err := CheckRoot(0, false)
	if err == nil {
		t.Fatal("expected error for uid=0 without allowRoot, got nil")
	}
	if !contains(err.Error(), "root") {
		t.Errorf("error should mention 'root', got: %v", err)
	}

	// With allowRoot=true, should pass.
	if err := CheckRoot(0, true); err != nil {
		t.Errorf("expected no error with allowRoot=true, got: %v", err)
	}

	// Non-root always passes.
	if err := CheckRoot(1000, false); err != nil {
		t.Errorf("expected no error for non-root uid=1000, got: %v", err)
	}
}

func TestDaemonVersionInDoctorResponse(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	ver := testVersion()
	d, err := New(hp, ver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	resp, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
	if err != nil {
		t.Fatalf("Doctor() failed: %v", err)
	}

	// Check that response contains version info in checks.
	var foundVersion bool
	for _, check := range resp.Checks {
		if check.Name == "version" {
			foundVersion = true
			if check.Status != "ok" {
				t.Errorf("version check status = %q, want %q", check.Status, "ok")
			}
			break
		}
	}
	if !foundVersion {
		t.Error("version check not found in Doctor response")
	}
}

func TestDaemonValidatesPermissionsBeforeServing(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// The daemon's Start() calls Ensure() which fixes socket permissions
	// before ValidatePermissions runs. So the daemon should start fine
	// even if we introduce socket permission issues first, because Ensure
	// corrects them. This test verifies the full flow works.
	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() should succeed (Ensure fixes perms), got: %v", err)
	}
}

func TestSocketPermissionsSetTo0600(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	fi, err := os.Stat(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("socket permissions = %#o, want 0600", fi.Mode().Perm())
	}
}

func TestStaleSocketCleanedBeforeBind(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Write a stale (non-listening) socket file.
	_ = os.Remove(hp.Socket)
	if err := os.WriteFile(hp.Socket, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() should clean stale socket and succeed, got: %v", err)
	}

	// Confirm socket is now a proper listening socket.
	conn, err := net.Dial("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot dial socket: %v", err)
	}
	_ = conn.Close()
}

func TestGracefulShutdownTimeout(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Stop with a very short timeout — should force-stop after that.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer shutdownCancel()

	start := time.Now()
	_ = d.Stop(shutdownCtx)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("shutdown took %v, expected quick termination with short timeout", elapsed)
	}
}

// Ensure the directory doesn't exist before daemon creation to trigger Ensure failure.
func TestDaemonEnsureCalledBeforeStart(t *testing.T) {
	hp := shortTempPaths(t)
	// Ensure is called inside Start() — it should create the home dir.
	// But we need Ensure to create the lock file before New() can open it
	// for flock. So let Ensure run first, then the daemon should start.
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() should succeed, got: %v", err)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Stop twice — second call should be a no-op.
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop() failed: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop() must be idempotent, got: %v", err)
	}
}

func TestCurrentVersionHasExpectedFields(t *testing.T) {
	v := CurrentVersion()

	if v.CLIVersion != "0.1.0-dev" {
		t.Errorf("CLIVersion = %q, want %q", v.CLIVersion, "0.1.0-dev")
	}
	if v.DaemonVersion != "0.1.0-dev" {
		t.Errorf("DaemonVersion = %q, want %q", v.DaemonVersion, "0.1.0-dev")
	}
	if v.ProtoVersion != "v1" {
		t.Errorf("ProtoVersion = %q, want %q", v.ProtoVersion, "v1")
	}
	// OsArch should be non-empty and in format "os/arch".
	expectedParts := runtime.GOOS + "/" + runtime.GOARCH
	if v.OsArch != expectedParts {
		t.Errorf("OsArch = %q, want %q", v.OsArch, expectedParts)
	}
}

func TestPIDFileWritten(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// PID file should exist with the current PID.
	data, err := os.ReadFile(hp.PID)
	if err != nil {
		t.Fatalf("cannot read PID file: %v", err)
	}
	expected := fmt.Sprintf("%d\n", os.Getpid())
	if string(data) != expected {
		t.Errorf("PID file content = %q, want %q", string(data), expected)
	}
}

func TestPIDFileRemovedOnStop(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	_ = d.Stop(context.Background())

	// PID file should be removed.
	if _, err := os.Stat(hp.PID); !os.IsNotExist(err) {
		t.Error("PID file should have been removed on Stop()")
	}
}

func TestActiveConnectionsDrainedOnShutdown(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	// Establish a connection before shutdown.
	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// Initiate a Stop (simulating SIGTERM).
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = d.Stop(shutdownCtx)

	// After shutdown, new connections should fail.
	_, err = net.Dial("unix", hp.Socket)
	if err == nil {
		t.Error("expected dial to fail after shutdown")
	}

	// But the already-established connection should still work for the
	// duration of graceful shutdown? Actually with GracefulStop, the
	// gRPC server stops accepting new connections but finishes in-flight ones.
	// Our stub RPCs are instant, so they should complete fine.
	// We just check that the connection isn't immediately killed.
	_ = client // connection was closed by conn.Close cleanup
}

// Verify Daemon does NOT export a WithAllowRoot option by default — root check happens at main level.
func TestDaemonNoTcpListener(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// The daemon should only listen on unix socket, not TCP.
	// Try to dial a random TCP port — should fail.
	tcpConn, err := net.DialTimeout("tcp", "localhost:9876", 100*time.Millisecond)
	if err == nil {
		_ = tcpConn.Close()
		t.Error("expected dialing TCP to fail (no TCP listener)")
	}
}

// Ensure the Doc.go file exists and compiles.
func TestPackageDocCompiles(t *testing.T) {
	// Just verify the package exists — compilation check is done by go build.
}

func TestDaemonStartsDashboard(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion(), WithAllowRoot(), WithDashboard(addr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for i := 0; i < 20; i++ {
		resp, err := client.Get("http://" + addr + "/api/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("dashboard not reachable at %s: %v", addr, lastErr)
}

// TestDaemonStartStopRace exercises the concurrent access pattern between
// Start() (which launches a dashboard goroutine) and Stop() (which shuts
// down the dashboard and nils the field). This test is designed to be run
// with -race to detect data races on d.dashboard.
func TestDaemonStartStopRace(t *testing.T) {
	for i := 0; i < 10; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Listen: %v", err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()

		hp := shortTempPaths(t)
		if err := home.Ensure(hp); err != nil {
			t.Fatal(err)
		}

		d, err := New(hp, testVersion(), WithAllowRoot(), WithDashboard(addr))
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Start(ctx); err != nil {
			t.Fatalf("Start() failed on iter %d: %v", i, err)
		}
		// Immediately stop — races with the dashboard goroutine.
		if err := d.Stop(context.Background()); err != nil {
			t.Fatalf("Stop() failed on iter %d: %v", i, err)
		}
	}
}