//go:build adversary

package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAdversaryB9T01_EmptyAuthorizationHeader(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	_, err := client.Invoke(context.Background(), &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: Invoke without auth succeeded or wrong code: %v", err)
	}
	t.Logf("Tested: empty Authorization header on gRPC Invoke — auth required (Unauthenticated returned)")
}

func TestAdversaryB9T01_BearerWithEmptyToken(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer ")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: empty token after Bearer accepted: %v", err)
	}
	t.Logf("Tested: 'Bearer ' (empty token) — rejected as unauthenticated")
}

func TestAdversaryB9T01_LowercaseBearerScheme(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "bearer test-key-123")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: lowercase 'bearer' scheme accepted: %v", err)
	}
	t.Logf("Tested: lowercase 'bearer' scheme — bearerToken requires exact 'Bearer ' prefix, rejected")
}

func TestAdversaryB9T01_WrongSchemeBasic(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Basic test-key-123")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: Basic scheme accepted: %v", err)
	}
	t.Logf("Tested: wrong scheme 'Basic' — rejected")
}

func TestAdversaryB9T01_WhitespaceOnlyToken(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer   ")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: whitespace token accepted: %v", err)
	}
	t.Logf("Tested: whitespace-only token after Bearer — rejected")
}

func TestAdversaryB9T01_VeryLongToken(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	longToken := "Bearer " + strings.Repeat("x", 1<<20) // 1MiB token
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", longToken)
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: very long token accepted or caused panic: %v", err)
	}
	t.Logf("Tested: 1MiB+ token DoS attempt — rejected without crash")
}

func TestAdversaryB9T01_CORSPreFlightFromEvilOrigin(t *testing.T) {
	handler := NewCORSMiddleware([]string{"https://app.example"}).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "/v1/trigger/invoke", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("SECURITY BREAK: CORS preflight from evil origin allowed (status=%d)", rr.Code)
	}
	t.Logf("Tested: CORS preflight from unlisted evil origin — 403 Forbidden, deny-by-default holds")
}

func TestAdversaryB9T01_CORSPreFlightNoOriginHeader(t *testing.T) {
	handler := NewCORSMiddleware([]string{"https://app.example"}).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodOptions, "/v1/trigger/invoke", nil)
	// no Origin header
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("SECURITY BREAK: OPTIONS without Origin not handled as no-content: %d", rr.Code)
	}
	t.Logf("Tested: CORS preflight with no Origin header — proceeds to NoContent (no allow header set)")
}

func TestAdversaryB9T01_RESTPOSTWithoutAuth(t *testing.T) {
	httpSrv, cleanup := startRESTGatewayTestServer(t, testAuthenticator())
	defer cleanup()

	body, _ := json.Marshal(&triggerv1.InvokeRequest{AgentName: "agent-a"})
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/trigger/invoke", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("SECURITY BREAK: REST POST without auth got status %d, want 401", resp.StatusCode)
	}
	t.Logf("Tested: REST gateway POST without Authorization — 401 enforced")
}

func TestAdversaryB9T01_RESTPOSTWithRevokedKey(t *testing.T) {
	revokedAuth := NewAPIKeyAuthenticator(map[string]*APIKeyMeta{
		"revoked-key": {ID: "key-r", Revoked: true},
	})
	httpSrv, cleanup := startRESTGatewayTestServer(t, revokedAuth)
	defer cleanup()

	body, _ := json.Marshal(&triggerv1.InvokeRequest{AgentName: "agent-a"})
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/trigger/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer revoked-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("SECURITY BREAK: revoked key on REST accepted with status %d", resp.StatusCode)
	}
	t.Logf("Tested: REST POST with revoked key — 401, rejected by lookup")
}

func TestAdversaryB9T01_InvokePayloadExceeds1MiB(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer test-key-123")
	payload := bytes.Repeat([]byte("x"), DefaultMaxPayload+1)
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a", Payload: payload})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("SECURITY BREAK: oversized payload accepted: %v", err)
	}
	t.Logf("Tested: Invoke payload 1MiB+1 byte — InvalidArgument enforced")
}

func TestAdversaryB9T01_InvokeStreamWithoutAuth(t *testing.T) {
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	stream, err := client.InvokeStream(context.Background(), &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("SECURITY BREAK: InvokeStream without auth returned wrong code: %v", err)
		}
		t.Logf("Tested: InvokeStream without auth — Unauthenticated enforced (stream interceptor)")
		return
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: InvokeStream without auth Recv succeeded or wrong code: %v", err)
	}
	t.Logf("Tested: InvokeStream without auth — Unauthenticated enforced (stream interceptor)")
}

func TestAdversaryB9T01_RevokedKeyOnGRPC(t *testing.T) {
	revokedAuth := NewAPIKeyAuthenticator(map[string]*APIKeyMeta{
		"revoked-key": {ID: "key-r", Revoked: true},
	})
	client, cleanup := startGRPCTestServer(t, revokedAuth, DefaultMaxPayload)
	defer cleanup()

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer revoked-key")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("SECURITY BREAK: revoked key on gRPC accepted: %v", err)
	}
	t.Logf("Tested: gRPC Invoke with revoked key — Unauthenticated, lookup rejects")
}

func TestAdversaryB9T01_ExposeWithoutAuthenticatorStillBlocked(t *testing.T) {
	_, err := New(ServerConfig{Exposed: true, Authenticator: nil})
	if err == nil {
		t.Errorf("SECURITY BREAK: --expose accepted with nil Authenticator")
	}
	t.Logf("Tested: New with Exposed=true and no Authenticator — error returned")
}

func TestAdversaryB9T01_MTLSClaimNoActualSupport(t *testing.T) {
	// mTLS AuthMethod defined but no mTLS creds configured in New/Start
	// Attempting to use would require transport creds; test confirms API key only path
	client, cleanup := startGRPCTestServer(t, testAuthenticator(), DefaultMaxPayload)
	defer cleanup()

	// No mTLS setup possible without changing transport; just verify no panic and auth is API key path
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer test-key-123")
	_, err := client.Invoke(ctx, &triggerv1.InvokeRequest{AgentName: "agent-a"})
	if err != nil {
		t.Errorf("unexpected error on valid key: %v", err)
	}
	t.Logf("Tested: mTLS claim — only APIKeyAuthenticator path active, no mTLS enforcement in T01")
}
