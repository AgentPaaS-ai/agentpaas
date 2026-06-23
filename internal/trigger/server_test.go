package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestServerStartsOnLoopbackGRPCAndREST(t *testing.T) {
	grpcAddr := freeTCPAddr(t)
	restAddr := freeTCPAddr(t)

	srv, err := New(ServerConfig{
		GRPCAddr:      grpcAddr,
		RESTAddr:      restAddr,
		Authenticator: testAuthenticator(),
		CORS:          NewCORSMiddleware(nil),
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	t.Cleanup(srv.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient(): %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := triggerv1.NewTriggerServiceClient(conn)
	authedCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-key-123")
	resp, err := client.Invoke(authedCtx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Fatalf("Invoke(): %v", err)
	}
	if resp.GetRun().GetRunId() == "" {
		t.Fatal("Invoke() returned empty run ID")
	}

	httpResp, err := http.Post("http://"+restAddr+"/v1/trigger/invoke", "application/json", bytes.NewReader([]byte(`{"agentName":"agent-a"}`)))
	if err != nil {
		t.Fatalf("POST /v1/trigger/invoke: %v", err)
	}
	defer func() { _ = httpResp.Body.Close() }()
	if httpResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated REST status = %d, want %d", httpResp.StatusCode, http.StatusUnauthorized)
	}
}

func TestUnauthenticatedInvokeReturnsUnauthenticated(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	_, err := client.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Invoke() code = %v, want %v (err=%v)", status.Code(err), codes.Unauthenticated, err)
	}
}

func TestUnauthenticatedInvokeStreamReturnsUnauthenticated(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	_, err := client.InvokeStream(context.Background(), &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("InvokeStream() code = %v, want %v (err=%v)", status.Code(err), codes.Unauthenticated, err)
	}
}

func TestValidAPIKeyInvokeReturnsStubResponse(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer test-key-123")
	resp, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Fatalf("Invoke(): %v", err)
	}
	if got := resp.GetRun().GetRunId(); !strings.HasPrefix(got, "run-") {
		t.Fatalf("RunId = %q, want run-*", got)
	}
	if got := resp.GetRun().GetAgentName(); got != "agent-a" {
		t.Fatalf("AgentName = %q, want %q", got, "agent-a")
	}
	if got := resp.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_PENDING)
	}
}

func TestValidAPIKeyInvokeStreamReturnsLifecycleResponses(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer test-key-123")
	stream, err := client.InvokeStream(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Fatalf("InvokeStream(): %v", err)
	}
	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("InvokeStream().Recv(): %v", err)
	}
	if got := resp.GetRun().GetRunId(); !strings.HasPrefix(got, "run-") {
		t.Fatalf("RunId = %q, want run-*", got)
	}
	if got := resp.GetRun().GetAgentName(); got != "agent-a" {
		t.Fatalf("AgentName = %q, want %q", got, "agent-a")
	}
	if got := resp.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_PENDING)
	}

	terminal, err := stream.Recv()
	if err != nil {
		t.Fatalf("InvokeStream().Recv() terminal: %v", err)
	}
	if got := terminal.GetRun().GetRunId(); got != resp.GetRun().GetRunId() {
		t.Fatalf("terminal RunId = %q, want %q", got, resp.GetRun().GetRunId())
	}
	if got := terminal.GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("terminal Status = %v, want %v", got, triggerv1.RunStatus_RUN_STATUS_SUCCEEDED)
	}
}

func TestCORSDeniesUnlistedOrigin(t *testing.T) {
	handler := NewCORSMiddleware([]string{"https://app.example"}).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "/v1/trigger/invoke", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestCORSAllowsListedOrigin(t *testing.T) {
	handler := NewCORSMiddleware([]string{"https://app.example"}).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "/v1/trigger/invoke", nil)
	req.Header.Set("Origin", "https://app.example")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "https://app.example")
	}
}

func TestRESTAuthViaGateway(t *testing.T) {
	httpSrv, cleanup := startRESTGatewayTestServer(t, testAuthenticator())
	defer cleanup()

	body, err := json.Marshal(&triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/trigger/invoke", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest(): %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("unauthenticated Invoke request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	req, err = http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/trigger/invoke", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequest(): %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key-123")
	req.Header.Set("Content-Type", "application/json")
	resp, err = httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("authenticated Invoke request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestOversizedPayloadReturnsError(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer test-key-123")
	payload := bytes.Repeat([]byte("x"), DefaultMaxPayload+1)
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a", Payload: payload})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Invoke() code = %v, want %v (err=%v)", status.Code(err), codes.InvalidArgument, err)
	}
}

func TestExposeWithoutAuthenticatorReturnsError(t *testing.T) {
	_, err := New(ServerConfig{Exposed: true})
	if err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func testAuthenticator() *APIKeyAuthenticator {
	return NewAPIKeyAuthenticator(map[string]*APIKeyMeta{
		"test-key-123": {ID: "key-1", KeyHash: "", Scopes: []string{"*"}},
	})
}

func startGRPCTestServer(t *testing.T, auth Authenticator, maxPayload int) (triggerv1.TriggerServiceClient, func()) {
	t.Helper()

	addr := freeTCPAddr(t)
	srv, err := New(ServerConfig{
		GRPCAddr:        addr,
		RESTAddr:        freeTCPAddr(t),
		Authenticator:   auth,
		CORS:            NewCORSMiddleware(nil),
		MaxPayloadBytes: maxPayload,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start(): %v", err)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		srv.Stop()
		t.Fatalf("grpc.NewClient(): %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
		cancel()
	}
	return triggerv1.NewTriggerServiceClient(conn), cleanup
}

func startRESTGatewayTestServer(t *testing.T, auth Authenticator) (*httptest.Server, func()) {
	t.Helper()

	grpcAddr := freeTCPAddr(t)
	srv, err := New(ServerConfig{
		GRPCAddr:      grpcAddr,
		RESTAddr:      freeTCPAddr(t),
		Authenticator: auth,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start(): %v", err)
	}

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := triggerv1.RegisterTriggerServiceHandlerFromEndpoint(ctx, mux, grpcAddr, opts); err != nil {
		srv.Stop()
		cancel()
		t.Fatalf("RegisterTriggerServiceHandlerFromEndpoint(): %v", err)
	}
	httpSrv := httptest.NewServer(NewCORSMiddleware(nil).Wrap(mux))

	cleanup := func() {
		httpSrv.Close()
		srv.Stop()
		cancel()
	}
	return httpSrv, cleanup
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(): %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}
