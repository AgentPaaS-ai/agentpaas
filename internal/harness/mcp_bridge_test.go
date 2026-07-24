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
	"os"
	"os/exec"
	"path/filepath"
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

// TestMCPBridge_WithRealServiceWorker starts a Python service worker, wires an
// MCPBridge to it, and verifies the HTTP JSON-RPC → stdin/stdout → JSON-RPC
// round-trip works correctly. This is the integration test for the bridge
// wire-in to the production path when AgentKind == "mcp_service".
func TestMCPBridge_WithRealServiceWorker(t *testing.T) {
	repoRoot := findRepoRoot(t)
	python := "python3"

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "svc_agent.py")
	agentCode := `
from agentpaas_sdk import agent

@agent.mcp_tool("echo")
def echo(args):
    return {"received": args.get("message", ""), "marker": "bridge-integration-real"}

@agent.mcp_tool("ping")
def ping(args):
    return {"pong": True}
`
	if err := os.WriteFile(agentPath, []byte(agentCode), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	stdoutPath := filepath.Join(dir, "stdout.txt")

	pythonPath := filepath.Join(repoRoot, "python")

	env := os.Environ()
	env = append(env, "AGENTPAAS_AGENT_KIND=mcp_service")
	env = append(env, "AGENTPAAS_AGENT_PATH="+agentPath)
	env = append(env, "AGENTPAAS_STDOUT_PATH="+stdoutPath)
	env = append(env, "AGENTPAAS_MCP_DECLARED_TOOLS=echo,ping")
	env = append(env, "PYTHONPATH="+pythonPath)
	env = append(env, "AGENTPAAS_MCP_MAX_CONCURRENCY=1")

	runnerScript := `
import sys, os
sys.path.insert(0, os.environ.get("PYTHONPATH", "."))
from agentpaas_sdk.runner import run
run()
`
	cmd := exec.Command(python, "-u", "-c", runnerScript)
	cmd.Env = env
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	// Wait for the "ready" message.
	decoder := json.NewDecoder(stdout)
	var readyMsg map[string]any
	if err := decoder.Decode(&readyMsg); err != nil {
		t.Fatalf("decode ready: %v (stderr: %s)", err, readStderr(stderrPipe))
	}
	if readyMsg["type"] != "ready" {
		t.Fatalf("expected ready, got %v", readyMsg)
	}

	// Wire the MCPBridge to the real worker.
	cap := randomCapability(t)
	bridge := NewMCPBridge(MCPBridgeConfig{
		Addr:       "127.0.0.1:0",
		Capability: cap,
		Stdin:      stdin,
		Stdout:     stdout,
	})
	if err := bridge.Start(); err != nil {
		t.Fatalf("bridge start: %v", err)
	}
	defer func() { _ = bridge.Close() }()

	// Get the bridge's actual listen address.
	addr := bridge.Addr()
	if addr == "" {
		t.Fatal("bridge addr is empty")
	}

	// 1. tools/list via HTTP.
	listResp, err := postMCPRequest(addr, cap, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if err != nil {
		t.Fatalf("tools/list request: %v", err)
	}
	if listResp["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc: %v", listResp["jsonrpc"])
	}
	result := listResp["result"].(map[string]any)
	toolsAny := result["tools"].([]any)
	if len(toolsAny) == 0 {
		t.Fatalf("tools list empty, result: %v", result)
	}
	// The tools list from the bridge returns strings directly, not objects with .name.
	t.Logf("tools: %v", toolsAny)

	// 2. tools/call echo with a distinctive marker.
	callResp, err := postMCPRequest(addr, cap, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello-real-bridge"}}}`)
	if err != nil {
		t.Fatalf("tools/call request: %v", err)
	}
	if callResp["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc: %v", callResp["jsonrpc"])
	}
	if callResp["error"] != nil {
		t.Fatalf("tools/call error: %v", callResp["error"])
	}
	callResult := callResp["result"].(map[string]any)
	// MCP wraps the result in content array with text items.
	content, ok := callResult["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content missing or empty: %v", callResult)
	}
	textItem := content[0].(map[string]any)
	text := textItem["text"].(string)
	if !strings.Contains(text, "hello-real-bridge") {
		t.Fatalf("text does not contain hello-real-bridge: %s", text)
	}
	if !strings.Contains(text, "bridge-integration-real") {
		t.Fatalf("text does not contain bridge-integration-real marker: %s", text)
	}

	// 3. Close bridge and verify graceful shutdown.
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
		t.Fatal("bridge Close() timed out")
	}
}

// postMCPRequest sends a JSON-RPC POST to the bridge with the capability header.
func postMCPRequest(addr, capability, body string) (map[string]any, error) {
	url := "http://" + addr + "/"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentPaaS-MCP-Capability", capability)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// readStderr reads all available bytes from a stderr pipe.
func readStderr(stderr io.ReadCloser) string {
	buf := make([]byte, 4096)
	n, _ := stderr.Read(buf)
	return string(buf[:n])
}