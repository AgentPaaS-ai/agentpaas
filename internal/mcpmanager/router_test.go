package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type recordingAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (a *recordingAudit) Append(record audit.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return nil
}

func (a *recordingAudit) snapshot() []audit.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]audit.AuditRecord, len(a.records))
	copy(out, a.records)
	return out
}

func TestRouterCallToolStdioAllowedReturnsResult(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{stdioResponderScript(t), filepath.Join(t.TempDir(), "request.json")},
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "local", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	result, err := NewRouter(manager, lifecycle, nil, nil).CallTool(ctx, "local", "lookup", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("CallTool() result type = %T, want map[string]any", result)
	}
	if resultMap["source"] != "stdio" {
		t.Fatalf("CallTool() source = %v, want stdio", resultMap["source"])
	}
}

func TestRouterCallToolDeniedEmitsAudit(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{"-c", "cat"},
		AllowedTools: []string{"allowed"},
	}}, "agent-1", "run-1")
	appender := &recordingAudit{}

	_, err := NewRouter(manager, NewLifecycle(manager, nil, ""), nil, appender).CallTool(context.Background(), "local", "denied", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool() error = nil, want denial")
	}
	if err.Error() != "mcp server/tool not allowed" {
		t.Fatalf("CallTool() error = %q, want policy denial", err.Error())
	}
	records := appender.snapshot()
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	if records[0].Payload["server_id"] != "local" || records[0].Payload["tool"] != "denied" {
		t.Fatalf("audit payload = %#v", records[0].Payload)
	}
}

func TestRouterCallToolUndeclaredServerReturnsError(t *testing.T) {
	manager := NewManager()
	manager.Register(nil, "agent-1", "run-1")

	_, err := NewRouter(manager, NewLifecycle(manager, nil, ""), nil, nil).CallTool(context.Background(), "missing", "lookup", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool() error = nil, want undeclared server error")
	}
}

func TestRouterCallToolHTTPPostsToEndpoint(t *testing.T) {
	var got mcpRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type = %s, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		defer func() { _ = r.Body.Close() }()
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"source":"http","ok":true}}`))
	}))
	defer func() { server.Close() }()
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		Endpoint:     server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	result, err := NewRouter(manager, nil, server.Client(), nil).CallTool(context.Background(), "sidecar", "lookup", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if got.Method != "tools/call" || got.Params.Name != "lookup" || got.Params.Arguments["q"] != "status" {
		t.Fatalf("request = %#v", got)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["source"] != "http" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRouterCallToolHTTPErrorPropagated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"tool failed"}}`))
	}))
	defer func() { server.Close() }()
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	_, err := NewRouter(manager, nil, server.Client(), nil).CallTool(context.Background(), "sidecar", "lookup", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool() error = nil, want MCP error")
	}
	if err.Error() != "mcp error -32000: tool failed" {
		t.Fatalf("CallTool() error = %q", err.Error())
	}
}

func TestRouterCallToolAuditsRequestAndResponseMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer func() { server.Close() }()
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	appender := &recordingAudit{}

	_, err := NewRouter(manager, nil, server.Client(), appender).CallTool(context.Background(), "sidecar", "lookup", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	records := appender.snapshot()
	if len(records) != 1 {
		t.Fatalf("audit records = %d, want 1", len(records))
	}
	payload := records[0].Payload
	for _, key := range []string{"server_id", "tool", "input_hash", "output_hash", "timing_ms", "agent_id", "run_id"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("audit payload missing %s: %#v", key, payload)
		}
	}
}

func TestRouterStdioRoutingWritesJSONRPCRequest(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{stdioResponderScript(t), requestPath},
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "local", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := NewRouter(manager, lifecycle, nil, nil).CallTool(ctx, "local", "lookup", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	var got mcpRequest
	requestBytes, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(requestBytes, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.JSONRPC != "2.0" || got.Method != "tools/call" || got.Params.Name != "lookup" || got.Params.Arguments["q"] != "status" {
		t.Fatalf("stdio request = %#v", got)
	}
}

func TestRouterDeniedCallDoesNotReachStdioProcess(t *testing.T) {
	requestPath := filepath.Join(t.TempDir(), "request.json")
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{stdioResponderScript(t), requestPath},
		AllowedTools: []string{"allowed"},
	}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "local", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := NewRouter(manager, lifecycle, nil, nil).CallTool(ctx, "local", "denied", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool() error = nil, want denial")
	}
	if _, statErr := os.Stat(requestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("request file stat error = %v, want not exist", statErr)
	}
}

func TestRouterCallToolNilAuditNoPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer func() { server.Close() }()
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		URL:          server.URL,
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")

	if _, err := NewRouter(manager, nil, server.Client(), nil).CallTool(context.Background(), "sidecar", "lookup", map[string]any{}, "agent-1", "run-1"); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
}

func TestRouterCallToolStoppedServerReturnsCrashContext(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "sh",
		Args:         []string{"-c", "exit 9"},
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	lifecycle := NewLifecycle(manager, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "local", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	_, err := NewRouter(manager, lifecycle, nil, nil).CallTool(ctx, "local", "lookup", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool() error = nil, want stopped server error")
	}
	if !errors.Is(err, ErrServerCrashed) {
		t.Fatalf("CallTool() error = %v, want ErrServerCrashed", err)
	}
}

func stdioResponderScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdio-responder.sh")
	script := `IFS= read -r request
printf '%s\n' "$request" > "$1"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"source":"stdio","ok":true}}'
`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
