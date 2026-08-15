package mcpmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func TestRouteHTTPIncludesInvokeTokenHeader(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Agentpaas-Invoke-Token")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "sidecar",
		Transport:    "http",
		Endpoint:     server.URL,
		AllowedTools: []string{"lookup"},
		Headers: map[string]string{
			"X-Agentpaas-Invoke-Token": "inv_test_token",
		},
	}}, "agent-1", "run-1")

	_, err := NewRouter(manager, nil, server.Client(), nil).CallTool(
		context.Background(), "sidecar", "lookup", map[string]any{"q": "status"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if gotToken != "inv_test_token" {
		t.Fatalf("X-Agentpaas-Invoke-Token = %q, want inv_test_token", gotToken)
	}
}

func TestListToolsPostsToolsList(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		defer func() { _ = r.Body.Close() }()
		gotMethod = req.Method
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"lookup"}]}}`))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "sidecar",
		Transport: "http",
		Endpoint:  server.URL,
		Headers: map[string]string{
			"X-Agentpaas-Invoke-Token": "inv_list_token",
		},
	}}, "agent-1", "run-1")

	result, err := NewRouter(manager, nil, server.Client(), nil).ListTools(context.Background(), "sidecar")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if gotMethod != "tools/list" {
		t.Fatalf("JSON-RPC method = %q, want tools/list", gotMethod)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if _, ok := resultMap["tools"]; !ok {
		t.Fatalf("result missing tools: %#v", result)
	}
}

func TestListToolsUnknownServerErrors(t *testing.T) {
	manager := NewManager()
	_, err := NewRouter(manager, nil, http.DefaultClient, nil).ListTools(context.Background(), "missing")
	if err == nil {
		t.Fatal("ListTools() error = nil, want unknown server error")
	}
}

func TestListToolsNilGatewayErrors(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "sidecar",
		Transport: "http",
		URL:       "http://127.0.0.1:9/mcp",
	}}, "agent-1", "run-1")

	_, err := NewRouter(manager, nil, nil, nil).ListTools(context.Background(), "sidecar")
	if err == nil {
		t.Fatal("ListTools() error = nil, want nil gateway error")
	}
}

func TestListPromptsPostsPromptsList(t *testing.T) {
	var gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		defer func() { _ = r.Body.Close() }()
		gotMethod = req.Method
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"prompts":[{"name":"summarize"}]}}`))
	}))
	defer func() { server.Close() }()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:      "sidecar",
		Transport: "http",
		URL:       server.URL,
	}}, "agent-1", "run-1")

	result, err := NewRouter(manager, nil, server.Client(), nil).ListPrompts(context.Background(), "sidecar")
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if gotMethod != "prompts/list" {
		t.Fatalf("JSON-RPC method = %q, want prompts/list", gotMethod)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if _, ok := resultMap["prompts"]; !ok {
		t.Fatalf("result missing prompts: %#v", result)
	}
}
