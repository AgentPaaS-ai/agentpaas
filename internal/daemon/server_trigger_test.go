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
	waitForTriggerGRPC(t, addr)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(%s): %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
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

	conn := dialTriggerGRPC(t, grpcAddr)
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

	host := strings.Split(grpcAddr, ":")[0]
	if host != "127.0.0.1" {
		t.Fatalf("trigger gRPC bound to %q, want loopback 127.0.0.1", host)
	}
}

func TestTriggerServer_AddressesFromEnv(t *testing.T) {
	customGRPC := freeTCPAddr(t)
	customREST := freeTCPAddr(t)

	d, _ := startDaemonWithTriggerAddrs(t, customGRPC, customREST)
	defer func() { _ = d.Stop(context.Background()) }()

	_ = dialTriggerGRPC(t, customGRPC)

	if d.triggerServer == nil {
		t.Fatal("daemon trigger server not initialized")
	}
}

func TestTriggerServer_GracefulShutdown(t *testing.T) {
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	d, _ := startDaemonWithTriggerAddrs(t, grpcAddr, restAddr)

	waitForTriggerGRPC(t, grpcAddr)

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected trigger TCP listener at %s to close after daemon Stop()", grpcAddr)
}

func TestTriggerService_InvokeFuncWired(t *testing.T) {
	service := trigger.NewTriggerService(nil, trigger.DefaultMaxPayload)
	service.SetInvokeFunc(func(_ context.Context, agentName string) (string, error) {
		if agentName != "wired-agent" {
			t.Fatalf("invokeFunc agent = %q, want %q", agentName, "wired-agent")
		}
		return "run-from-daemon", nil
	})

	resp, err := service.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "wired-agent"})
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

	conn := dialTriggerGRPC(t, grpcAddr)
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

	conn := dialTriggerGRPC(t, grpcAddr)
	client := triggerv1.NewTriggerServiceClient(conn)

	_, err := client.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "any-agent"})
	if status.Code(err) == codes.Unauthenticated {
		t.Fatalf("Invoke() without auth code = %v, want not Unauthenticated for backward compat (err=%v)", codes.Unauthenticated, err)
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