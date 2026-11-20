package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
)

// TestLoadMCPBindingSidecar_WiresManagedPath verifies that the harness
// loads a sidecar file after SetRouter and the managed path is wired.
func TestLoadMCPBindingSidecar_WiresManagedPath(t *testing.T) {
	// Stand up a fake MCP service endpoint.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcpResponse{
			JSONRPC: "2.0",
			ID:      0,
			Result:  json.RawMessage(`{"harness":"wired","source":"managed"}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { ts.Close() }()

	// Create a sidecar file pointing at the test server.
	dir := t.TempDir()
	sidecarPath := filepath.Join(dir, "sidecar.json")
	sc := mcpmanager.MCPBindingSidecar{
		WorkflowID: "wf-harness",
		Bindings: []mcpmanager.MCPBindingSidecarEntry{
			{
				BindingID:    "harness-binding",
				Endpoint:     ts.URL,
				Capability:   "cap-harness-0123456789abcdef0123456789ab",
				AllowedTools: []string{"lookup"},
				State:        "READY",
			},
		},
	}
	if err := mcpmanager.WriteMCPBindingSidecar(sidecarPath, sc); err != nil {
		t.Fatalf("WriteMCPBindingSidecar error = %v", err)
	}

	// Create a minimal harness Server with the sidecar path.
	cfg := Config{
		Addr:                    "127.0.0.1:0",
		AgentPath:               t.TempDir() + "/agent.py",
		Python:                  "echo",     // non-existent, worker won't start
		ImportTimeout:           100 * time.Millisecond, // fail fast
		MCPBindingSidecarPath:   sidecarPath,
	}
	server := NewServer(cfg)

	// SetRouter installs the router+manager before sidecar load.
	manager := mcpmanager.NewManager()
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)
	server.SetRouter(router, manager)

	// Install the sidecar.
	if err := server.InstallMCPBindingSidecar(sidecarPath); err != nil {
		t.Fatalf("InstallMCPBindingSidecar error = %v", err)
	}

	// After install, the Router should be able to call the managed tool.
	// Access the router through the rpc server.
	router = server.GetRouter()
	if router == nil {
		t.Fatal("GetRouter returned nil after SetRouter")
	}

	result, err := router.CallTool(context.Background(), "harness-binding", "lookup",
		map[string]any{"q": "test"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if resultMap["harness"] != "wired" {
		t.Fatalf("result.harness = %q, want wired", resultMap["harness"])
	}

	_ = server.Close()
}

// TestLoadMCPBindingSidecar_DeleteAfterLoad verifies the file is deleted
// after successful load when writable.
func TestLoadMCPBindingSidecar_DeleteAfterLoad(t *testing.T) {
	dir := t.TempDir()
	sidecarPath := filepath.Join(dir, "sidecar.json")
	sc := mcpmanager.MCPBindingSidecar{
		WorkflowID: "wf-del",
		Bindings: []mcpmanager.MCPBindingSidecarEntry{
			{
				BindingID:    "del-binding",
				Endpoint:     "http://localhost:0",
				Capability:   "cap-del-0123456789abcdef0123456789ab0",
				AllowedTools: []string{"lookup"},
				State:        "READY",
			},
		},
	}
	if err := mcpmanager.WriteMCPBindingSidecar(sidecarPath, sc); err != nil {
		t.Fatalf("WriteMCPBindingSidecar error = %v", err)
	}

	// Verify file exists before install.
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Fatalf("sidecar file not found before install: %v", err)
	}

	cfg := Config{
		Addr:                    "127.0.0.1:0",
		AgentPath:               t.TempDir() + "/agent.py",
		Python:                  "echo",
		ImportTimeout:           100 * time.Millisecond,
		MCPBindingSidecarPath:   sidecarPath,
	}
	server := NewServer(cfg)

	// SetRouter installs the router+manager before sidecar load.
	manager := mcpmanager.NewManager()
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)
	server.SetRouter(router, manager)

	if err := server.InstallMCPBindingSidecar(sidecarPath); err != nil {
		t.Fatalf("InstallMCPBindingSidecar error = %v", err)
	}

	// File should be deleted after successful load.
	if _, err := os.Stat(sidecarPath); err == nil {
		t.Fatal("sidecar file should be deleted after successful load")
	}

	_ = server.Close()
}

// TestLoadMCPBindingSidecar_CapabilityNotInEnviron verifies capability
// tokens do not leak into environment variables.
func TestLoadMCPBindingSidecar_CapabilityNotInEnviron(t *testing.T) {
	dir := t.TempDir()
	sidecarPath := filepath.Join(dir, "sidecar.json")
	capToken := "cap-env-0abcdef0123456789abcdef0123456789ab"
	sc := mcpmanager.MCPBindingSidecar{
		WorkflowID: "wf-env",
		Bindings: []mcpmanager.MCPBindingSidecarEntry{
			{
				BindingID:    "env-binding",
				Endpoint:     "http://localhost:0",
				Capability:   capToken,
				AllowedTools: []string{"lookup"},
				State:        "READY",
			},
		},
	}
	if err := mcpmanager.WriteMCPBindingSidecar(sidecarPath, sc); err != nil {
		t.Fatalf("WriteMCPBindingSidecar error = %v", err)
	}

	cfg := Config{
		Addr:                    "127.0.0.1:0",
		AgentPath:               t.TempDir() + "/agent.py",
		Python:                  "echo",
		ImportTimeout:           100 * time.Millisecond,
		MCPBindingSidecarPath:   sidecarPath,
	}
	server := NewServer(cfg)

	// SetRouter installs the router+manager before sidecar load.
	manager := mcpmanager.NewManager()
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)
	server.SetRouter(router, manager)

	if err := server.InstallMCPBindingSidecar(sidecarPath); err != nil {
		t.Fatalf("InstallMCPBindingSidecar error = %v", err)
	}

	// Verify no CAP env var was set.
	for _, env := range os.Environ() {
		if len(env) > len(capToken) {
			// Quick check — full env scan
			for i := 0; i <= len(env)-len(capToken); i++ {
				if env[i:i+len(capToken)] == capToken {
					t.Fatalf("capability token found in environment: %q", env)
				}
			}
		}
	}

	_ = server.Close()
}

// mcpResponse mirrors the internal mcpmanager.mcpResponse type for test.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}