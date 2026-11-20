package cloudclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_McpCall_PostsToolsCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep_x/mcp" {
			t.Errorf("expected mcp path, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_tok" {
			t.Errorf("Authorization = %q, want Bearer inv_tok", auth)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if !strings.Contains(string(requestBody), "tools/call") {
			t.Errorf("request body = %q, want tools/call", requestBody)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer func() { server.Close() }()

	body := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`)
	result, err := NewCloudClient(server.URL).McpCall(context.Background(), "inv_tok", "dep_x", body)
	if err != nil {
		t.Fatalf("McpCall: %v", err)
	}
	if !strings.Contains(string(result), `"ok":true`) {
		t.Errorf("result = %s, want raw RPC response", result)
	}
}
