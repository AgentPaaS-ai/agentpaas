package daemon

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func freeTCPAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(): %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

func startDaemonWithTriggerAddrs(t *testing.T, grpcAddr, restAddr string) (*Daemon, *home.HomePaths) {
	t.Helper()

	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", grpcAddr)
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", restAddr)

	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}

	d, err := New(hp, testVersion(), WithAllowRoot(), WithDashboard("off"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	d.Ready()

	return d, hp
}

func waitForTriggerGRPC(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			_ = conn.Close()
			return
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("trigger gRPC server not reachable at %s: %v", addr, lastErr)
}

func dialTriggerGRPC(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	if isDefaultTriggerListen(addr, trigger.DefaultGRPCPort) {
		t.Fatalf("refusing to dial default trigger address %s", addr)
	}
	waitForTriggerGRPC(t, addr)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s): %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// reportedTriggerAddrs returns the addresses the daemon's own trigger
// server bound. Callers dial these, never a default or the pre-bind env
// value when that value used port 0.
func reportedTriggerAddrs(t *testing.T, d *Daemon) (grpcAddr, restAddr string) {
	t.Helper()
	if d == nil || d.triggerServer == nil {
		t.Fatal("daemon trigger server did not start")
	}
	grpcAddr = d.triggerServer.BoundGRPCAddr()
	restAddr = d.triggerServer.BoundRESTAddr()
	if isDefaultTriggerListen(grpcAddr, trigger.DefaultGRPCPort) || isDefaultTriggerListen(restAddr, trigger.DefaultRESTPort) {
		t.Fatalf("trigger server reported a default address grpc=%s rest=%s", grpcAddr, restAddr)
	}
	if strings.HasSuffix(grpcAddr, ":0") || strings.HasSuffix(restAddr, ":0") {
		t.Fatalf("trigger server reported an unbound port grpc=%s rest=%s", grpcAddr, restAddr)
	}
	return grpcAddr, restAddr
}

func mustReportedTriggerGRPC(t *testing.T, d *Daemon) string {
	t.Helper()
	grpcAddr, _ := reportedTriggerAddrs(t, d)
	return grpcAddr
}

func injectMockRuntime(t *testing.T, d *Daemon) {
	t.Helper()
	if d.control == nil {
		t.Fatal("daemon control server not initialized")
	}
	d.control.dockerRT = runtime.NewDockerRuntimeWithDriver(defaultMockRuntimeDriver())
	d.control.runtimeErr = nil
}

func TestTriggerServer_StartsOnLoopback(t *testing.T) {
	t.Setenv("AGENTPAAS_SKIP_GATEWAY_WAIT", "1") // skip gateway readiness dial
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	hp := shortTempPaths(t)
	if err := home.Ensure(hp); err != nil {
		t.Fatal(err)
	}
	deployTestAgent(t, hp, "test-agent")

	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", grpcAddr)
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", restAddr)

	d, err := New(hp, testVersion(), WithAllowRoot(), WithDashboard("off"))
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
	injectMockRuntime(t, d)

	grpcBound, _ := reportedTriggerAddrs(t, d)
	conn := dialTriggerGRPC(t, grpcBound)
	client := triggerv1.NewTriggerServiceClient(conn)

	resp, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Invoke(): %v", err)
	}
	if resp.GetRun().GetRunId() == "" {
		t.Fatal("Invoke() returned empty run ID")
	}
	if got := resp.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_RUNNING {
		t.Fatalf("Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_RUNNING)
	}

	host := strings.Split(grpcBound, ":")[0]
	if host != "127.0.0.1" {
		t.Fatalf("trigger gRPC bound to %q, want loopback 127.0.0.1", host)
	}
}

func TestTriggerServer_AddressesFromEnv(t *testing.T) {
	customGRPC := freeTCPAddr(t)
	customREST := freeTCPAddr(t)

	d, _ := startDaemonWithTriggerAddrs(t, customGRPC, customREST)
	defer func() { _ = d.Stop(context.Background()) }()

	grpcBound, restBound := reportedTriggerAddrs(t, d)
	if grpcBound != customGRPC {
		t.Fatalf("reported gRPC %q, want env address %q", grpcBound, customGRPC)
	}
	if restBound != customREST {
		t.Fatalf("reported REST %q, want env address %q", restBound, customREST)
	}
	_ = dialTriggerGRPC(t, grpcBound)

	if d.triggerServer == nil {
		t.Fatal("daemon trigger server not initialized")
	}
}

func TestTriggerServer_GracefulShutdown(t *testing.T) {
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	d, _ := startDaemonWithTriggerAddrs(t, grpcAddr, restAddr)

	bound, _ := reportedTriggerAddrs(t, d)
	waitForTriggerGRPC(t, bound)

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", bound, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected trigger TCP listener at %s to close after daemon Stop()", bound)
}

func TestTriggerService_InvokeFuncWired(t *testing.T) {
	service := trigger.NewTriggerService(nil, trigger.DefaultMaxPayload)
	service.SetInvokeFunc(func(_ context.Context, agentName string, payload []byte) (string, error) {
		if agentName != "wired-agent" {
			t.Fatalf("invokeFunc agent = %q, want %q", agentName, "wired-agent")
		}
		// Verify payload is threaded through (regression for payload-dropped bug).
		if len(payload) == 0 {
			t.Fatalf("invokeFunc payload is empty; expected the bytes from InvokeRequest.payload")
		}
		return "run-from-daemon", nil
	})

	resp, err := service.Invoke(context.Background(), &triggerv1.InvokeRequest{
		AgentName: "wired-agent",
		Payload:   []byte(`{"hello":"world"}`),
	})
	if err != nil {
		t.Fatalf("Invoke(): %v", err)
	}
	if got := resp.GetRun().GetRunId(); got != "run-from-daemon" {
		t.Fatalf("RunId = %q, want %q", got, "run-from-daemon")
	}
	if got := resp.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_RUNNING {
		t.Fatalf("Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_RUNNING)
	}
}

func TestTriggerServer_APIKeyAuthRequired(t *testing.T) {
	const testAPIKey = "test-trigger-api-key"
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	t.Setenv("AGENTPAAS_TRIGGER_API_KEY", testAPIKey)
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", grpcAddr)
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", restAddr)

	d, _ := startDaemonWithTriggerAddrs(t, grpcAddr, restAddr)
	defer func() { _ = d.Stop(context.Background()) }()

	conn := dialTriggerGRPC(t, mustReportedTriggerGRPC(t, d))
	client := triggerv1.NewTriggerServiceClient(conn)
	ctx := context.Background()

	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "any-agent"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Invoke() without auth code = %v, want %v (err=%v)", status.Code(err), codes.Unauthenticated, err)
	}

	authedCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+testAPIKey)
	_, err = client.Invoke(authedCtx, &triggerv1.InvokeRequest{AgentName: "any-agent"})
	if status.Code(err) == codes.Unauthenticated {
		t.Fatalf("Invoke() with valid API key code = %v, want not Unauthenticated (err=%v)", codes.Unauthenticated, err)
	}
}

func TestTriggerServer_NoAuthWhenKeyUnset(t *testing.T) {
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	t.Setenv("AGENTPAAS_TRIGGER_API_KEY", "")
	d, _ := startDaemonWithTriggerAddrs(t, grpcAddr, restAddr)
	defer func() { _ = d.Stop(context.Background()) }()

	conn := dialTriggerGRPC(t, mustReportedTriggerGRPC(t, d))
	client := triggerv1.NewTriggerServiceClient(conn)

	_, err := client.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "any-agent"})
	if status.Code(err) == codes.Unauthenticated {
		t.Fatalf("Invoke() without auth code = %v, want not Unauthenticated for backward compat (err=%v)", codes.Unauthenticated, err)
	}
}

func TestDaemonStartFailsFastOnDefaultTriggerAddr(t *testing.T) {
	// Explicit defaults, and the empty-env path that Start resolves to them.
	// Either one must fail before a listen on the founder's ports.
	cases := []struct {
		name string
		grpc string
		rest string
	}{
		{name: "explicit defaults", grpc: "127.0.0.1:7718", rest: "127.0.0.1:7717"},
		{name: "unset", grpc: "", rest: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hp := shortTempPaths(t)
			if err := home.Ensure(hp); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", tc.grpc)
			t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", tc.rest)

			d, err := New(hp, testVersion())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Stop(context.Background()) })

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err = d.Start(ctx)
			if err == nil {
				t.Fatal("Start() succeeded on a default trigger address; want fail-fast")
			}
			if !strings.Contains(err.Error(), "default trigger address") {
				t.Fatalf("Start() error = %v, want default trigger address refusal", err)
			}
			if strings.Contains(err.Error(), "address already in use") {
				t.Fatalf("Start() bound a default trigger address: %v", err)
			}
			if d.triggerServer != nil {
				t.Fatal("trigger server started on a default trigger address")
			}
		})
	}
}

func TestTriggerService_InvokeFuncNil_StubBehavior(t *testing.T) {
	service := trigger.NewTriggerService(nil, trigger.DefaultMaxPayload)

	resp, err := service.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Fatalf("Invoke(): %v", err)
	}
	if got := resp.GetRun().GetRunId(); !strings.HasPrefix(got, "run-") {
		t.Fatalf("RunId = %q, want run-* prefix", got)
	}
	if got := resp.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_PENDING)
	}
}
