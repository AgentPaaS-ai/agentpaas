package harness

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// MCP bridge unit tests — TDD: write failing tests first.
// ---------------------------------------------------------------------------

// fakeMCPWorker simulates the Python service worker's stdin/stdout protocol.
type fakeMCPWorker struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr bytes.Buffer

	tools       []string
	callHandler func(tool string, args map[string]any) (map[string]any, error)

	done chan struct{}
}

func startFakeMCPWorker(t *testing.T, tools []string, callHandler func(string, map[string]any) (map[string]any, error)) *fakeMCPWorker {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	w := &fakeMCPWorker{
		stdin:       stdinW,
		stdout:      stdoutR,
		tools:       tools,
		callHandler: callHandler,
		done:        make(chan struct{}),
	}

	go w.run(stdinR, stdoutW)
	return w
}

func (w *fakeMCPWorker) run(r io.Reader, wOut io.WriteCloser) {
	defer close(w.done)
	defer func() { _ = wOut.Close() }()

	dec := json.NewDecoder(r)
	enc := json.NewEncoder(wOut)

	for {
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			return
		}
		reqType, _ := req["type"].(string)
		reqID, _ := req["id"].(string)

		switch reqType {
		case "mcp_tools_list":
			_ = enc.Encode(map[string]any{
				"type":  "mcp_tools_list_result",
				"id":    reqID,
				"ok":    true,
				"tools": w.tools,
			})
		case "mcp_tools_call":
			tool, _ := req["tool"].(string)
			args, _ := req["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			if w.callHandler != nil {
				result, err := w.callHandler(tool, args)
				if err != nil {
					_ = enc.Encode(map[string]any{
						"type": "mcp_tools_result",
						"id":   reqID,
						"ok":   false,
						"error": map[string]any{
							"code":    "tool_error",
							"message": err.Error(),
						},
					})
				} else {
					_ = enc.Encode(map[string]any{
						"type":   "mcp_tools_result",
						"id":     reqID,
						"ok":     true,
						"result": result,
					})
				}
			} else {
				_ = enc.Encode(map[string]any{
					"type": "mcp_tools_result",
					"id":   reqID,
					"ok":   false,
					"error": map[string]any{
						"code":    "tool_not_found",
						"message": "unknown tool: " + tool,
					},
				})
			}
		case "shutdown":
			_ = enc.Encode(map[string]any{
				"type": "shutdown_ack",
				"id":   reqID,
			})
			return
		}
	}
}

func (w *fakeMCPWorker) Close() error {
	_ = w.stdin.Close()
	_ = w.stdout.Close()
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMCPBridge_ToolsList(t *testing.T) {
	// Create a fake worker with sample tools.
	worker := startFakeMCPWorker(t, []string{"echo", "ping", "lookup"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	// Build JSON-RPC tools/list request.
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", cap)

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc: %v", resp["jsonrpc"])
	}
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools = %v", tools)
	}
}

func TestMCPBridge_ToolsCall(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, func(tool string, args map[string]any) (map[string]any, error) {
		return map[string]any{"received": args["message"], "distinctive": "bridge-test"}, nil
	})

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	reqBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello-bridge"}}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", cap)

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result := resp["result"].(map[string]any)
	// The MCP response returns content array with text items.
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("content empty")
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hello-bridge") && !strings.Contains(text, "distinctive") {
		t.Fatalf("unexpected text: %s", text)
	}
}

func TestMCPBridge_MissingCapabilityHeader(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No capability header.

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	// Body must NOT contain the capability.
	if strings.Contains(rec.Body.String(), cap) {
		t.Fatal("response body contains capability")
	}
}

func TestMCPBridge_WrongCapabilityHeader(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", randomCapability(t)) // wrong cap

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestMCPBridge_UnknownTool(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	reqBody := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", cap)

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestMCPBridge_InvalidJSON(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", cap)

	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestMCPBridge_ConcurrentRequests(t *testing.T) {
	var callCount atomic.Int64
	worker := startFakeMCPWorker(t, []string{"counter"}, func(tool string, args map[string]any) (map[string]any, error) {
		callCount.Add(1)
		// Small delay to ensure concurrency.
		time.Sleep(5 * time.Millisecond)
		return map[string]any{"count": callCount.Load()}, nil
	})

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	// Send a few concurrent requests.
	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			reqBody := `{"jsonrpc":"2.0","id":` + string(rune('0'+i)) + `,"method":"tools/call","params":{"name":"counter","arguments":{}}}`
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-AgentPaaS-MCP-Capability", cap)
			rec := httptest.NewRecorder()
			bridge.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				results <- fmt.Errorf("status %d", rec.Code)
				return
			}
			results <- nil
		}()
	}
	for i := 0; i < 3; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestMCPBridge_CapabilityNtInLogs(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	var logBuf bytes.Buffer
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
		ErrorLog:   log.New(&logBuf, "", 0),
	})
	_ = bridge.Start()
	defer func() { _ = bridge.Close() }()

	// Send a request that fails (wrong capability) and check logs.
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	// No header — triggers unauthorized.
	rec := httptest.NewRecorder()
	bridge.ServeHTTP(rec, req)

	// Check that the capability value does not appear in any log output.
	if strings.Contains(logBuf.String(), cap) {
		t.Fatal("log buffer contains capability")
	}
	if strings.Contains(rec.Body.String(), cap) {
		t.Fatal("response body contains capability")
	}
}

func TestMCPBridge_Shutdown(t *testing.T) {
	worker := startFakeMCPWorker(t, []string{"echo"}, nil)

	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      worker.stdin,
		Stdout:     worker.stdout,
	})
	_ = bridge.Start()

	// Close should shut down without hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = bridge.Close()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-ctx.Done():
		t.Fatal("Close() timed out")
	}
}

func randomCapability(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(buf)
}