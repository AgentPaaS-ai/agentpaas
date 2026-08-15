package harness

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/mcpmanager"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func TestInstallHTTPMCPBindingsJSONRegistersHTTPServer(t *testing.T) {
	const token = "inv_secret_token_xyz"
	raw := `[{"name":"ext","url":"http://127.0.0.1:9/mcp","headers":{"X-Agentpaas-Invoke-Token":"` + token + `"},"allowed_tools":["lookup"]}]`

	cfg := Config{
		Addr:          "127.0.0.1:0",
		AgentPath:     t.TempDir() + "/agent.py",
		Python:        "echo",
		ImportTimeout: 100 * time.Millisecond,
	}
	server := NewServer(cfg)
	defer func() { _ = server.Close() }()

	manager := mcpmanager.NewManager()
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)
	server.SetRouter(router, manager)

	if err := server.InstallHTTPMCPBindingsJSON(raw); err != nil {
		t.Fatalf("InstallHTTPMCPBindingsJSON error = %v", err)
	}
	if !manager.IsToolAllowed("ext", "lookup") {
		t.Fatal("expected ext/lookup to be registered as allowed")
	}
	found := false
	for _, res := range manager.Status() {
		if res.ServerID == "ext" {
			found = true
			if res.Transport != "http" {
				t.Fatalf("transport = %q, want http", res.Transport)
			}
		}
	}
	if !found {
		t.Fatal("expected ext server in manager status")
	}
}

func TestWorkerEnvStripsMCPBindingsJSONToken(t *testing.T) {
	const token = "inv_secret_token_xyz"
	raw := `[{"name":"ext","url":"http://127.0.0.1:9/mcp","headers":{"X-Agentpaas-Invoke-Token":"` + token + `"}}]`
	env := workerEnv([]string{
		"PATH=/usr/bin",
		"AGENTPAAS_MCP_BINDINGS_JSON=" + raw,
	}, "127.0.0.1:1")
	for _, item := range env {
		if strings.Contains(item, token) {
			t.Fatalf("worker env leaked invoke token: %q", item)
		}
		if strings.HasPrefix(item, "AGENTPAAS_MCP_BINDINGS_JSON=") {
			t.Fatalf("worker env must not include AGENTPAAS_MCP_BINDINGS_JSON: %q", item)
		}
	}
}

func TestInstallHTTPMCPBindingsJSONDoesNotWipeExisting(t *testing.T) {
	cfg := Config{
		Addr:          "127.0.0.1:0",
		AgentPath:     t.TempDir() + "/agent.py",
		Python:        "echo",
		ImportTimeout: 100 * time.Millisecond,
	}
	server := NewServer(cfg)
	defer func() { _ = server.Close() }()

	manager := mcpmanager.NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "local",
		Transport:    "stdio",
		Command:      "echo",
		AllowedTools: []string{"lookup"},
	}}, "agent-1", "run-1")
	router := mcpmanager.NewRouter(manager, nil, http.DefaultClient, nil)
	server.SetRouter(router, manager)

	raw := `[{"name":"ext","url":"http://127.0.0.1:9/mcp","allowed_tools":["ping"]}]`
	if err := server.InstallHTTPMCPBindingsJSON(raw); err != nil {
		t.Fatalf("InstallHTTPMCPBindingsJSON error = %v", err)
	}
	if !manager.IsToolAllowed("local", "lookup") {
		t.Fatal("existing local server was wiped")
	}
	if !manager.IsToolAllowed("ext", "ping") {
		t.Fatal("http binding was not added")
	}
}
