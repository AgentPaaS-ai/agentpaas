package mcpmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// Test_StdioMCP_EOL2E is the M13.12 T07 golden E2E test. It exercises the
// full stdio MCP server lifecycle through the mcpmanager: start a fixture
// stdio MCP server, send initialize / tools/list / tools/call over the
// JSON-RPC stdio transport, assert each response, then stop the server and
// verify clean shutdown. The test is hermetic: no network, no external
// services — the fixture server is a Python script in t.TempDir().
func Test_StdioMCP_EOL2E(t *testing.T) {
	scriptPath := stdioMCPEchoServerScript(t)

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "echo",
		Transport:    "stdio",
		Command:      "python3",
		Args:         []string{scriptPath},
		AllowedTools: []string{"echo"},
	}}, "agent-1", "run-1")

	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// (c) Start the stdio MCP server.
	if err := lifecycle.Start(ctx, "echo", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "echo") }()

	// (d) Send initialize, assert protocol version 2024-11-05.
	initResp, err := sendRawStdioRequest(ctx, lifecycle, "echo", 100, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":       map[string]any{"name": "e2e-test", "version": "1.0"},
	})
	if err != nil {
		t.Fatalf("initialize error = %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error response: code=%d message=%s", initResp.Error.Code, initResp.Error.Message)
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(initResp.Result, &initResult); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if initResult.ProtocolVersion != "2024-11-05" {
		t.Fatalf("protocolVersion = %q, want 2024-11-05", initResult.ProtocolVersion)
	}

	// (e) Send tools/list, assert exactly one tool named "echo".
	listResp, err := sendRawStdioRequest(ctx, lifecycle, "echo", 101, "tools/list", map[string]any{})
	if err != nil {
		t.Fatalf("tools/list error = %v", err)
	}
	if listResp.Error != nil {
		t.Fatalf("tools/list error response: code=%d message=%s", listResp.Error.Code, listResp.Error.Message)
	}
	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(listResp.Result, &listResult); err != nil {
		t.Fatalf("unmarshal tools/list result: %v", err)
	}
	if len(listResult.Tools) != 1 {
		t.Fatalf("tools/list returned %d tools, want 1", len(listResult.Tools))
	}
	if listResult.Tools[0].Name != "echo" {
		t.Fatalf("tool name = %q, want echo", listResult.Tools[0].Name)
	}

	// (f) Send tools/call via the router, assert response contains "hello-mcp".
	result, err := router.CallTool(ctx, "echo", "echo", map[string]any{"message": "hello-mcp"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal CallTool result: %v", err)
	}
	if !strings.Contains(string(resultBytes), "hello-mcp") {
		t.Fatalf("CallTool result = %s, want to contain hello-mcp", string(resultBytes))
	}

	// (g) Stop the server, assert clean shutdown.
	if err := lifecycle.Stop(ctx, "echo"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if lifecycle.IsRunning("echo") {
		t.Fatal("IsRunning() = true, want false after Stop")
	}
}

// sendRawStdioRequest writes a JSON-RPC 2.0 request to the stdio MCP
// server's stdin and reads the matching response from its stdout. It uses
// the same pipe infrastructure as the Router (lifecycle.StdioPipes +
// decodeMCPResponse) so initialize and tools/list — which the Router does
// not expose — can be sent directly.
func sendRawStdioRequest(ctx context.Context, lifecycle *Lifecycle, serverID string, id int64, method string, params any) (mcpResponse, error) {
	stdin, stdoutLines, err := lifecycle.StdioPipes(serverID)
	if err != nil {
		return mcpResponse{}, fmt.Errorf("get stdio pipes for %q: %w", serverID, err)
	}
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any   `json:"params"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		return mcpResponse{}, fmt.Errorf("write stdio %s request: %w", method, err)
	}
	return decodeMCPResponse(ctx, stdoutLines, id)
}

// stdioMCPEchoServerScript writes a Python stdio MCP server fixture to
// t.TempDir() and returns its path. The server reads JSON-RPC 2.0 requests
// from stdin line by line and writes responses to stdout (one JSON object
// per line, flushed). It handles:
//   - initialize: responds with protocolVersion 2024-11-05
//   - tools/list: responds with one tool named "echo" taking {message: string}
//   - tools/call: echoes the message argument back as text content
func stdioMCPEchoServerScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo_mcp_server.py")
	script := `#!/usr/bin/env python3
import sys
import json

def respond(req_id, result):
    resp = {"jsonrpc": "2.0", "id": req_id, "result": result}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()

def respond_error(req_id, code, message):
    resp = {"jsonrpc": "2.0", "id": req_id, "error": {"code": code, "message": message}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()

while True:
    line = sys.stdin.readline()
    if not line:
        break
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        continue
    method = req.get("method", "")
    req_id = req.get("id", 0)
    if method == "initialize":
        respond(req_id, {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "serverInfo": {"name": "echo-server", "version": "1.0.0"}
        })
    elif method == "tools/list":
        respond(req_id, {
            "tools": [{
                "name": "echo",
                "description": "Echoes back the message",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "message": {"type": "string"}
                    },
                    "required": ["message"]
                }
            }]
        })
    elif method == "tools/call":
        params = req.get("params", {})
        args = params.get("arguments", {})
        message = args.get("message", "")
        respond(req_id, {
            "content": [{"type": "text", "text": message}]
        })
    elif method == "notifications/initialized":
        # Notification — no response expected.
        pass
    else:
        respond_error(req_id, -32601, "method not found: " + method)
`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
