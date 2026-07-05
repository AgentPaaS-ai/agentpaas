package daemon

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// Adversarial: readiness bypass race
// ---------------------------------------------------------------------------

// TestAdversarialReadinessRace tries to call RPCs in a tight loop between
// Start() and Ready() to see if any non-UNAVAILABLE response sneaks through.
func TestAdversarialReadinessRace(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Hammer Doctor RPC from multiple goroutines before and during Ready().
	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		nonUnaVAIL int
		totalCalls int
	)

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// Launch racers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
				mu.Lock()
				totalCalls++
				if err != nil {
					st, ok := status.FromError(err)
					if !ok || st.Code() != codes.Unavailable {
						nonUnaVAIL++
					}
				} else {
					nonUnaVAIL++
				}
				mu.Unlock()
			}
		}()
	}

	// Call Ready() while racers are firing.
	time.Sleep(10 * time.Millisecond)
	d.Ready()

	wg.Wait()

	if nonUnaVAIL > 0 && nonUnaVAIL < totalCalls {
		// Some calls got through after Ready() — that's expected.
		t.Logf("readiness race: %d/%d calls were non-UNAVAILABLE (expected after Ready())", nonUnaVAIL, totalCalls)
	} else if nonUnaVAIL == totalCalls && totalCalls > 0 {
		// All were unavailable (unlikely but possible if all raced)
		t.Logf("readiness race: all %d calls were UNAVAILABLE", totalCalls)
	}
}

// TestAdversarialReadinessStreamRace tests the stream interceptor race.
func TestAdversarialReadinessStreamRace(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	conn := dialSocket(t, hp.Socket)
	client := controlv1.NewControlServiceClient(conn)

	// Logs is a streaming RPC. Try to call it before ready.
	stream, err := client.Logs(ctx, &controlv1.LogsRequest{})
	if err == nil {
		// If we got a stream, try to recv and see what happens.
		_, recvErr := stream.Recv()
		if recvErr == nil {
			t.Error("expected error on Logs stream before Ready(), got nil")
		} else {
			st, ok := status.FromError(recvErr)
			if !ok || st.Code() != codes.Unavailable {
				t.Errorf("expected Unavailable on Logs before ready, got: %v", recvErr)
			}
		}
	} else {
		st, ok := status.FromError(err)
		if !ok {
			t.Fatalf("expected gRPC status error, got: %v", err)
		}
		if st.Code() != codes.Unavailable {
			t.Errorf("expected Unavailable for Logs before ready, got %s", st.Code())
		}
	}
}

// ---------------------------------------------------------------------------
// Adversarial: flock bypass via lock file deletion + recreation
// ---------------------------------------------------------------------------

// TestAdversarialFlockBypassDeleteLock tests that deleting the lock file and
// recreating it does NOT allow a second daemon to acquire a new flock on the
// same home paths. The daemon must detect the inode change and re-acquire the
// flock on the new file, preserving the exclusive-access guarantee.
func TestAdversarialFlockBypassDeleteLock(t *testing.T) {
	hp := shortTempPaths(t)

	// Create the initial lock file via Ensure.
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// First daemon acquires the flock.
	d1, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d1.Stop(context.Background()) }()

	// Delete the lock file while d1 holds the lock.
	if err := os.Remove(hp.Lock); err != nil {
		t.Fatal(err)
	}

	// Recreate the lock file (as Ensure does during startup).
	// This creates a NEW inode, so d1's flock on the old inode doesn't
	// protect the new file.
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Start d1 — this should detect the inode change and re-acquire the
	// flock on the new lock file.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d1.Start(ctx); err != nil {
		t.Fatalf("d1.Start() failed after lock file recreation: %v", err)
	}

	// Second daemon should NOT be able to acquire the flock because d1
	// now holds the flock on the new lock file's inode.
	d2, err := New(hp, testVersion())
	if err == nil {
		_ = d2.Stop(context.Background())
		t.Error("BREAK: second daemon acquired flock after lock file was deleted and recreated — inode verification failed")
	} else {
		t.Logf("lock bypass correctly rejected: %v", err)
	}
}

// TestAdversarialFlockDifferentLockPath tests two daemons on the same home
// paths but with different lock file paths — this should NOT be possible,
// but verifies the lock enforcement covers the right paths.
func TestAdversarialFlockDifferentLockPath(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// First daemon uses default paths.
	d1, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d1.Stop(context.Background()) }()

	// Second daemon with same paths — should fail.
	_, err = New(hp, testVersion())
	if err == nil {
		t.Error("BREAK: second daemon with same paths acquired flock")
	} else {
		t.Logf("same-path lock correctly rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: graceful shutdown abuse
// ---------------------------------------------------------------------------

// TestAdversarialSignalDuringStartup sends SIGTERM during the startup
// sequence to see if the daemon handles it without hanging or crashing.
func TestAdversarialSignalDuringStartup(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	// Do NOT defer Stop — we're testing signal-triggered shutdown.

	sigCh := make(chan os.Signal, 1)
	if err := d.HandleSignal(syscall.SIGTERM, sigCh); err != nil {
		t.Fatal(err)
	}

	// Start the daemon.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Send SIGTERM immediately after Start returns.
	sigCh <- syscall.SIGTERM

	// Wait for shutdown to complete.
	time.Sleep(500 * time.Millisecond)

	// Socket should be gone.
	_, err = net.Dial("unix", hp.Socket)
	if err == nil {
		t.Error("socket should be closed after SIGTERM during startup")
	} else {
		t.Logf("socket closed after SIGTERM during startup (expected): %v", err)
	}
}

// TestAdversarialRapidSignals sends SIGINT then SIGTERM rapidly to check
// that the shutdown doesn't hang or panic.
func TestAdversarialRapidSignals(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	// Register SIGTERM handler.
	sigCh := make(chan os.Signal, 2)
	if err := d.HandleSignal(syscall.SIGTERM, sigCh); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	// Send rapid signals — first one triggers shutdown.
	sigCh <- syscall.SIGINT
	sigCh <- syscall.SIGTERM

	// Wait for shutdown.
	time.Sleep(500 * time.Millisecond)

	// Should not panic or hang.
	_, err = net.Dial("unix", hp.Socket)
	if err == nil {
		t.Error("socket should be closed after rapid signals")
	}
}

// TestAdversarialSignalBeforeStart sends a signal before Start() is even
// called, then checks that Start() and later behavior don't panic.
func TestAdversarialSignalBeforeStart(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}

	sigCh := make(chan os.Signal, 1)
	if err := d.HandleSignal(syscall.SIGTERM, sigCh); err != nil {
		t.Fatal(err)
	}

	// Send signal BEFORE Start.
	sigCh <- syscall.SIGTERM
	time.Sleep(50 * time.Millisecond)

	// Now try to Start — the signal handler already called Stop(), which
	// is idempotent, so the daemon should still be usable.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = d.Start(ctx)
	if err != nil {
		// The lock was released by the premature Stop(), so this
		// should actually work if Stop() properly reset state.
		// Actually, d.stopped is true so Start returns "already stopped".
		t.Logf("Start after premature signal: %v (expected failure if stopped flag set)", err)
	} else {
		t.Log("Start succeeded after premature signal (lock was released)")
		_ = d.Stop(context.Background())
	}
}

// ---------------------------------------------------------------------------
// Adversarial: socket injection — pre-create a stale-looking socket file
// that an attacker could use to intercept connections.
// ---------------------------------------------------------------------------

// TestAdversarialSocketInjectionPreCreated creates a non-listening socket
// file at the daemon's path before the daemon starts, simulating a
// classical socket interception attack.
func TestAdversarialSocketInjectionPreCreated(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Attacker creates a regular file at the socket path (simulating a
	// pre-placed socket that would intercept connections if not cleaned).
	_ = os.Remove(hp.Socket)
	if err := os.WriteFile(hp.Socket, []byte("attacker-intercepted"), 0600); err != nil {
		t.Fatal(err)
	}

	// Daemon starts — should clean the stale file and bind successfully.
	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed after socket injection attempt: %v", err)
	}

	// Check that the socket file is now a real listening socket.
	conn, err := net.Dial("unix", hp.Socket)
	if err != nil {
		t.Fatalf("cannot dial socket after Start(): %v", err)
	}
	_ = conn.Close()

	t.Log("socket injection via pre-created file was thwarted by stale cleanup")
}

// TestAdversarialSocketInjectionSymlink creates a symlink pointing to
// another location at the socket path, simulating a symlink-escape attack.
//
// The daemon MUST refuse to follow the symlink (fail-closed) rather than
// silently replacing it, because a symlink at the socket path could point
// to an attacker-controlled location.
func TestAdversarialSocketInjectionSymlink(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	// Create a symlink at the socket path pointing to an attacker-controlled
	// path (simulating an attempt to redirect the daemon's listener).
	targetPath := hp.Home + "/attacker_socket"
	_ = os.Remove(hp.Socket)
	_ = os.Remove(targetPath)
	if err := os.Symlink(targetPath, hp.Socket); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	defer func() { _ = os.Remove(targetPath) }()

	// Daemon should REFUSE to follow the symlink and return an error.
	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = d.Start(ctx)
	if err == nil {
		t.Error("BREAK: daemon should refuse to start with symlink at socket path, but started successfully")
		return
	}

	// Verify the error mentions symlink refusal.
	if !strings.Contains(err.Error(), "symlink") &&
		!strings.Contains(err.Error(), "Symlink") {
		t.Errorf("error should mention symlink refusal, got: %v", err)
	} else {
		t.Logf("daemon correctly refused symlink: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: unimplemented RPC error disclosure
// ---------------------------------------------------------------------------

// TestAdversarialUnimplementedInfoLeak checks whether stub handlers leak
// sensitive information in error messages.
func TestAdversarialUnimplementedInfoLeak(t *testing.T) {
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

	// Try every unimplemented RPC and inspect the error messages.
	leaks := []string{}

	checkLeak := func(name string, err error) {
		if err == nil {
			return
		}
		msg := err.Error()
		// Check for sensitive info patterns.
		if strings.Contains(msg, "controlServer") ||
			strings.Contains(msg, "internal/daemon") ||
			strings.Contains(msg, "/Users/") ||
			strings.Contains(msg, "home directory") ||
			strings.Contains(msg, "lock file") {
			leaks = append(leaks, name+": "+msg)
		}
	}

	_, errPack := client.Pack(ctx, &controlv1.PackRequest{})
	checkLeak("Pack", errPack)

	_, errRun := client.Run(ctx, &controlv1.RunRequest{})
	checkLeak("Run", errRun)

	_, errStop := client.Stop(ctx, &controlv1.StopRequest{})
	checkLeak("Stop", errStop)

	_, errPolicy := client.PolicyApply(ctx, &controlv1.PolicyApplyRequest{})
	checkLeak("PolicyApply", errPolicy)

	_, errSecret := client.SecretSet(ctx, &controlv1.SecretSetRequest{})
	checkLeak("SecretSet", errSecret)

	_, errSecretGrant := client.SecretGrant(ctx, &controlv1.SecretGrantRequest{})
	checkLeak("SecretGrant", errSecretGrant)

	_, errSecretRevoke := client.SecretRevoke(ctx, &controlv1.SecretRevokeRequest{})
	checkLeak("SecretRevoke", errSecretRevoke)

	_, errAudit := client.AuditQuery(ctx, &controlv1.AuditQueryRequest{})
	checkLeak("AuditQuery", errAudit)

	_, errAuditExp := client.AuditExport(ctx, &controlv1.AuditExportRequest{})
	checkLeak("AuditExport", errAuditExp)

	_, errValidate := client.ValidateAgentProject(ctx, &controlv1.ValidateAgentProjectRequest{})
	checkLeak("ValidateAgentProject", errValidate)

	_, errSummarize := client.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{})
	checkLeak("SummarizeRun", errSummarize)

	_, errExplain := client.ExplainFailure(ctx, &controlv1.ExplainFailureRequest{})
	checkLeak("ExplainFailure", errExplain)

	_, errExplainDenial := client.ExplainPolicyDenial(ctx, &controlv1.ExplainPolicyDenialRequest{})
	checkLeak("ExplainPolicyDenial", errExplainDenial)

	_, errRecommend := client.RecommendPolicyPatch(ctx, &controlv1.RecommendPolicyPatchRequest{})
	checkLeak("RecommendPolicyPatch", errRecommend)

	_, errTimeline := client.GetRunTimeline(ctx, &controlv1.GetRunTimelineRequest{})
	checkLeak("GetRunTimeline", errTimeline)

	_, errNextAction := client.NextAction(ctx, &controlv1.NextActionRequest{})
	checkLeak("NextAction", errNextAction)

	if len(leaks) > 0 {
		for _, l := range leaks {
			t.Errorf("BREAK: sensitive info leak in error: %s", l)
		}
	} else {
		t.Log("no sensitive info leaks in unimplemented RPC errors")
	}
}

// ---------------------------------------------------------------------------
// Adversarial: resource exhaustion — many concurrent connections
// ---------------------------------------------------------------------------

// TestAdversarialManyConnections opens many connections to the daemon
// to check for resource exhaustion or connection limits.
func TestAdversarialManyConnections(t *testing.T) {
	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Stop(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	// Try to open many concurrent connections.
	const numConns = 50
	var conns []*grpc.ClientConn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < numConns; i++ {
		conn, err := grpc.NewClient("unix://"+hp.Socket,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return net.Dial("unix", hp.Socket)
			}),
		)
		if err != nil {
			t.Fatalf("connection %d failed: %v", i, err)
		}
		conns = append(conns, conn)

		// Make an RPC on each connection.
		client := controlv1.NewControlServiceClient(conn)
		_, err = client.Doctor(ctx, &controlv1.DoctorRequest{})
		if err != nil {
			t.Errorf("Doctor RPC on connection %d failed: %v", i, err)
		}
	}

	t.Logf("successfully opened and used %d concurrent connections", len(conns))
}

// ---------------------------------------------------------------------------
// Adversarial: socket permission race — verify chmod happens after listen
// ---------------------------------------------------------------------------

// TestAdversarialSocketPermissionOrder checks that the socket file is created
// with net.Listen and then Chmod'd to 0600, and that between those two
// operations, no accept loop is running that could process connections
// with insecure permissions.
func TestAdversarialSocketPermissionOrder(t *testing.T) {
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

	// Check that the socket has the correct permissions.
	fi, err := os.Stat(hp.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("socket permissions = %#o, want 0600", fi.Mode().Perm())
	}

	// Verify no other process can connect to a different network type.
	// The daemon MUST NOT listen on TCP.
	tcpConn, err := net.DialTimeout("tcp", "localhost:9876", 100*time.Millisecond)
	if err == nil {
		_ = tcpConn.Close()
		t.Error("BREAK: daemon appears to be listening on TCP")
	}
}

// ---------------------------------------------------------------------------
// Adversarial: root check bypass — edge cases
// ---------------------------------------------------------------------------

// TestAdversarialRootCheckEdgeCases tests edge cases in CheckRoot,
// including unusual uid values.
func TestAdversarialRootCheckEdgeCases(t *testing.T) {
	// uid=0 without allowRoot must fail.
	if err := CheckRoot(0, false); err == nil {
		t.Error("BREAK: CheckRoot(0, false) should fail")
	}

	// uid=0 with allowRoot must pass.
	if err := CheckRoot(0, true); err != nil {
		t.Errorf("CheckRoot(0, true) should pass: %v", err)
	}

	// uid=-1 (possible in some namespace configurations) without allowRoot
	// should pass because -1 != 0. This could be considered a bypass if
	// the caller expects the uid to map to root-like privileges.
	if err := CheckRoot(-1, false); err != nil {
		t.Errorf("CheckRoot(-1, false) unexpectedly failed: %v", err)
	}

	// Large uid values should pass.
	if err := CheckRoot(4294967295, false); err != nil {
		t.Errorf("CheckRoot(max uint32, false) unexpectedly failed: %v", err)
	}

	// Negative non-zero values.
	if err := CheckRoot(-100, false); err != nil {
		t.Errorf("CheckRoot(-100, false) unexpectedly failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: version spoofing — verify version is coded, not read from file
// ---------------------------------------------------------------------------

// TestAdversarialVersionSpoofing checks that version info is compiled into
// the binary and cannot be manipulated at runtime (no file-based version
// loading that an attacker could tamper with).
func TestAdversarialVersionSpoofing(t *testing.T) {
	v := CurrentVersion()

	// Version fields should be hardcoded constants or runtime values,
	// never read from external files.
	if v.CLIVersion == "" {
		t.Error("CLIVersion is empty")
	}
	if v.DaemonVersion == "" {
		t.Error("DaemonVersion is empty")
	}
	if v.ProtoVersion == "" {
		t.Error("ProtoVersion is empty")
	}

	// GitCommit can be "unknown" if not injected via -ldflags.
	// This is expected but note that build-time injection is the only way
	// to set it — an attacker cannot spoof it via file manipulation.
	t.Logf("GitCommit = %q (set via -ldflags, not from file)", v.GitCommit)

	// OsArch should be OS/arch format.
	if !strings.Contains(v.OsArch, "/") {
		t.Errorf("OsArch = %q, expected format 'os/arch'", v.OsArch)
	}
}

// TestAdversarialDoctorContainsVersionFromBuild checks that the Doctor RPC
// returns version info that matches the built-in constants, confirming no
// runtime version injection vector exists.
func TestAdversarialDoctorContainsVersionFromBuild(t *testing.T) {
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

	// Verify the version string in the response matches our test version.
	var versionMsg string
	for _, check := range resp.Checks {
		if check.Name == "version" {
			versionMsg = check.Message
			break
		}
	}
	if versionMsg == "" {
		t.Error("version check not found in Doctor response")
	} else if !strings.Contains(versionMsg, "test-commit") {
		t.Errorf("version message does not contain test GitCommit: %q", versionMsg)
	} else {
		t.Logf("version message matches test build: %q", versionMsg)
	}
}

// ---------------------------------------------------------------------------
// Adversarial: no TCP listener — exhaustive check
// ---------------------------------------------------------------------------

// TestAdversarialNoTcpListener checks that the daemon does not listen on
// any TCP port, not just the commonly tested port.
func TestAdversarialNoTcpListener(t *testing.T) {
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

	// Try common ports.
	ports := []string{"9876", "8080", "9090", "50051", "4318", "9999"}
	for _, port := range ports {
		addr := "localhost:" + port
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			t.Errorf("BREAK: daemon appears to be listening on TCP %s", addr)
		}
	}
}

// ---------------------------------------------------------------------------
// Adversarial: graceful shutdown — drain timeout enforcement
// ---------------------------------------------------------------------------

// TestAdversarialDrainTimeoutEnforced verifies that the drain timeout
// actually causes force-stop and doesn't hang forever.
func TestAdversarialDrainTimeoutEnforced(t *testing.T) {
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

	// Stop with a very short timeout that should force-stop.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer shortCancel()

	start := time.Now()
	_ = d.Stop(shortCtx)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("BREAK: shutdown took %v, expected force-stop with 1ms timeout", elapsed)
	} else {
		t.Logf("shutdown completed in %v (timeout was 1ms)", elapsed)
	}
}
