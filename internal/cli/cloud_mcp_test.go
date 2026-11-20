package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudMcpCall_PostsToolsCallWithEnvToken(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("AGENTPAAS_CLOUD_INVOKE_TOKEN", "inv_tok")
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep_x/mcp" {
			t.Errorf("path = %q, want /v1/deployments/dep_x/mcp", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_tok" {
			t.Errorf("Authorization = %q, want Bearer inv_tok", auth)
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if !strings.Contains(r.URL.Path, "/mcp") {
			t.Errorf("path = %q, want /mcp", r.URL.Path)
		}
		var rpc struct {
			Method string `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(requestBody, &rpc); err != nil {
			t.Errorf("unmarshal RPC: %v body=%s", err, requestBody)
		}
		if rpc.Method != "tools/call" {
			t.Errorf("method = %q, want tools/call", rpc.Method)
		}
		if rpc.Params.Name != "list_projects" {
			t.Errorf("tool name = %q, want list_projects", rpc.Params.Name)
		}
		if string(rpc.Params.Arguments) != `{"q":1}` {
			t.Errorf("arguments = %s, want {\"q\":1}", rpc.Params.Arguments)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"projects":[]}}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "mcp", "call", "dep_x", "list_projects", "--args", `{"q":1}`)
	if err != nil {
		t.Fatalf("cloud mcp call: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"projects":[]`) {
		t.Errorf("expected raw JSON body in output, got %q", stdout)
	}
}
