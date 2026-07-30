package harness

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImportCrashYieldsFailedStatusWithStructuredReason(t *testing.T) {
	agentPath := writeAgent(t, `raise RuntimeError("boom during import")`)

	srv := NewServer(Config{AgentPath: agentPath})
	t.Cleanup(func() { _ = srv.Close() })

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d; body %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal readyz error response: %v", err)
	}
	if got.Status != "FAILED" || got.Reason != "import_failed" || !strings.Contains(got.Detail, "boom during import") {
		t.Fatalf("readyz response = %#v, want structured import failure", got)
	}
}

func TestErrorResponseDetailIsSanitizedAtJSONBoundary(t *testing.T) {
	rec := httptest.NewRecorder()

	writeJSON(rec, http.StatusServiceUnavailable, ErrorResponse{
		Status: "FAILED",
		Reason: "import_failed",
		Detail: "boom\nwith\x00controls\u2028and\u2029separators",
	})

	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	for _, forbidden := range []string{"\n", "\x00", "\u2028", "\u2029"} {
		if strings.Contains(got.Detail, forbidden) {
			t.Fatalf("detail = %q, want no raw %q", got.Detail, forbidden)
		}
	}
	if !strings.Contains(got.Detail, "boom") || !strings.Contains(got.Detail, "controls") {
		t.Fatalf("detail = %q, want sanitized content preserved", got.Detail)
	}
}

func TestInvokeRejectsRequestBodiesOverTenMegabytes(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return {"ok": True}
`)
	defer func() { _ = srv.Close() }()

	body := bytes.NewReader(bytes.Repeat([]byte("x"), MaxPayloadBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/invoke", body)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("invoke status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHealthzAndReadyzReturnOKWhenAgentLoaded(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return payload
`)
	defer func() { _ = srv.Close() }()

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body %s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestInvokeRunsPythonAndCapturesStdoutStderrPointers(t *testing.T) {
	srv := newReadyServer(t, `import sys

def invoke(payload):
    print("hello stdout")
    print("hello stderr", file=sys.stderr)
    return {"echo": payload["value"]}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"value":"abc"}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got InvokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal invoke response: %v", err)
	}
	if got.Status != "OK" || got.Result["echo"] != "abc" {
		t.Fatalf("invoke response = %#v, want OK echo", got)
	}
	if got.Stdout == "" || got.Stderr == "" {
		t.Fatalf("stdout/stderr pointers = %q/%q, want file paths", got.Stdout, got.Stderr)
	}

	stdout, err := os.ReadFile(got.Stdout)
	if err != nil {
		t.Fatalf("read stdout capture: %v", err)
	}
	stderr, err := os.ReadFile(got.Stderr)
	if err != nil {
		t.Fatalf("read stderr capture: %v", err)
	}
	if !strings.Contains(string(stdout), "hello stdout") {
		t.Fatalf("stdout capture = %q, want user stdout", stdout)
	}
	if !strings.Contains(string(stderr), "hello stderr") {
		t.Fatalf("stderr capture = %q, want user stderr", stderr)
	}
}

func TestInvokeRejectsReservedResultKeys(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return {"ok": True, "__proto__": "pollution"}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if got.Reason != "invalid_result" {
		t.Fatalf("reason = %q, want invalid_result", got.Reason)
	}
}

func TestInvokeResourceLimitsBlockSubprocessEscape(t *testing.T) {
	escapePath := filepath.Join(os.TempDir(), "agentpaas-harness-escape-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	t.Cleanup(func() { _ = os.Remove(escapePath) })

	srv := newReadyServer(t, `import os

escape_path = `+strconv.Quote(escapePath)+`

def invoke(payload):
    os.system("echo escape > " + escape_path + " 2>/dev/null || true")
    return {"escaped": os.path.exists(escape_path)}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got InvokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal invoke response: %v", err)
	}
	if got.Result["escaped"] == true {
		t.Fatalf("subprocess wrote %s, want resource limits to block fork", escapePath)
	}
}

func TestInvokeRequestsAreSerialized(t *testing.T) {
	srv := newReadyServer(t, `import time

def invoke(payload):
    time.sleep(0.15)
    return {"value": payload["value"]}
`)
	defer func() { _ = srv.Close() }()

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	elapsed := make(chan time.Duration, 2)

	for _, payload := range []string{`{"value":"one"}`, `{"value":"two"}`} {
		payload := payload
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(payload))
			rec := httptest.NewRecorder()
			begin := time.Now()
			srv.ServeHTTP(rec, req)
			elapsed <- time.Since(begin)
			if rec.Code != http.StatusOK {
				t.Errorf("invoke status = %d, want %d; body %s", rec.Code, http.StatusOK, rec.Body.String())
			}
		}()
	}

	close(start)
	wg.Wait()
	close(elapsed)

	var slowSeen bool
	for d := range elapsed {
		if d >= 250*time.Millisecond {
			slowSeen = true
		}
	}
	if !slowSeen {
		t.Fatal("concurrent invokes completed without one waiting for serialization")
	}
}

func newReadyServer(t *testing.T, source string) *Server {
	t.Helper()

	srv := NewServer(Config{AgentPath: writeAgent(t, source)})
	t.Cleanup(func() { _ = srv.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			return srv
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
	return nil
}

func writeAgent(t *testing.T, source string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent.py")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	return path
}

// TestServer_MCPBridge_StartsWithServiceKind verifies that when the Server
// starts with AgentKind == "mcp_service", an MCPBridge is created and stored
// on the Server, and the bridge can respond to HTTP JSON-RPC requests via
// the real Python service worker.
func TestServer_MCPBridge_StartsWithServiceKind(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Write a service agent with mcp_tool decorators.
	agentCode := `
from agentpaas_sdk import agent

@agent.mcp_tool("echo")
def echo(args):
    return {"received": args.get("message", ""), "marker": "server-wire-test"}

@agent.mcp_tool("ping")
def ping(args):
    return {"pong": True}
`
	agentPath := writeAgent(t, agentCode)

	// Override Python path to include the SDK.
	pythonPath := filepath.Join(repoRoot, "python")
	t.Setenv("PYTHONPATH", pythonPath)

	// Set the MCP capability env var so the bridge can authenticate.
	cap := randomCapability(t)
	t.Setenv("AGENTPAAS_MCP_CAPABILITY", cap)

	cfg := Config{
		AgentPath:        agentPath,
		AgentKind:        "mcp_service",
		MCPDeclaredTools: "echo,ping",
		MCPMaxConcurrency: 1,
	}
	srv := NewServer(cfg)
	t.Cleanup(func() { _ = srv.Close() })

	// Wait for ready.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			break
		}
		if rec.Code == http.StatusServiceUnavailable {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		t.Fatalf("readyz status = %d, body: %s", rec.Code, rec.Body.String())
	}

	// Verify the MCPBridge exists.
	bridge := srv.MCPBridge()
	if bridge == nil {
		t.Fatal("MCPBridge is nil for mcp_service kind")
	}

	// Verify the bridge has a listen address.
	addr := bridge.Addr()
	if addr == "" {
		t.Fatal("MCPBridge addr is empty")
	}

	t.Logf("bridge addr: %s", addr)

	// Make a real HTTP request to the bridge.
	callResp, err := postMCPRequest(addr, cap, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"server-wire-test-msg"}}}`)
	if err != nil {
		t.Fatalf("tools/call request: %v", err)
	}
	if callResp["error"] != nil {
		t.Fatalf("tools/call error: %v", callResp["error"])
	}
	callResult := callResp["result"].(map[string]any)
	content, _ := callResult["content"].([]any)
	textItem := content[0].(map[string]any)
	text := textItem["text"].(string)
	if !strings.Contains(text, "server-wire-test-msg") {
		t.Fatalf("text missing server-wire-test-msg: %s", text)
	}
}

// TestServer_MCPBridge_NotStartedWithoutServiceKind verifies that the MCPBridge
// is nil when AgentKind is not "mcp_service".
func TestServer_MCPBridge_NotStartedWithoutServiceKind(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return payload
`)
	defer func() { _ = srv.Close() }()

	bridge := srv.MCPBridge()
	if bridge != nil {
		t.Fatal("MCPBridge should be nil when AgentKind is not mcp_service")
	}
}

func TestLoopbackAddrCheckRespectsAllowEnv(t *testing.T) {
	agentPath := writeAgent(t, `def invoke(payload):
    return {"ok": True}
`)

	// Without the env var, a non-loopback address should be rejected.
	t.Run("rejected_without_env", func(t *testing.T) {
		srv := NewServer(Config{AgentPath: agentPath, Addr: "0.0.0.0:8080"})
		t.Cleanup(func() { _ = srv.Close() })

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("readyz status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
		var got ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if got.Reason != "invalid_listen_addr" {
			t.Fatalf("reason = %s, want invalid_listen_addr", got.Reason)
		}
	})

	// With the env var set, a non-loopback address should be allowed.
	t.Run("allowed_with_env", func(t *testing.T) {
		t.Setenv("AGENTPAAS_ALLOW_NON_LOOPBACK_LISTEN", "1")

		srv := NewServer(Config{AgentPath: agentPath, Addr: "0.0.0.0:8080"})
		t.Cleanup(func() { _ = srv.Close() })

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK {
				return // success
			}
			if rec.Code == http.StatusServiceUnavailable {
				var got ErrorResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err == nil && got.Reason == "invalid_listen_addr" {
					t.Fatalf("readyz returned invalid_listen_addr even with AGENTPAAS_ALLOW_NON_LOOPBACK_LISTEN=1")
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatalf("unexpected readyz status = %d; body: %s", rec.Code, rec.Body.String())
		}
		t.Fatal("server did not become ready within deadline")
	})
}
