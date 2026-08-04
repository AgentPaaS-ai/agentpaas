package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

func TestCloudRegistry_SuccessText(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_registry_cli"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/registry" {
			t.Errorf("path = %q, want /v1/registry", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_registry_cli" {
			t.Errorf("Authorization = %q, want registry token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloudclient.RegistryResponse{
			TenantAssets: []cloudclient.RegistryAsset{{
				ID: "asset-1", Kind: "agent", Name: "weather-agent", Version: "1.0.0", Status: "ready",
			}},
			Platform: cloudclient.RegistryPlatform{
				MCPCatalog: []cloudclient.MCPRegistryEntry{{ID: "mcp-1", Name: "GitHub", Description: "GitHub tools"}},
			},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry")
	if err != nil {
		t.Fatalf("cloud registry: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "weather-agent") || !strings.Contains(stdout, "GitHub") {
		t.Fatalf("registry output = %q, want tenant and MCP entries", stdout)
	}
}

func TestCloudRegistry_JSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_registry_json"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tenant_assets": []map[string]string{{"id": "asset-json", "kind": "mcp", "name": "Slack"}},
			"platform": map[string]any{
				"mcp_catalog": []map[string]string{{"id": "catalog-json", "name": "Slack"}},
			},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "--json")
	if err != nil {
		t.Fatalf("cloud registry --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("cloud registry --json stderr = %q", stderr)
	}
	var got cloudclient.RegistryResponse
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode registry JSON: %v; output=%q", err, stdout)
	}
	if len(got.TenantAssets) != 1 || got.TenantAssets[0].Name != "Slack" {
		t.Fatalf("tenant assets = %#v", got.TenantAssets)
	}
	if len(got.Platform.MCPCatalog) != 1 || got.Platform.MCPCatalog[0].Name != "Slack" {
		t.Fatalf("MCP catalog = %#v", got.Platform.MCPCatalog)
	}
}

func TestCloudRegistry_ListAlias(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	found, _, err := cmd.Find([]string{"cloud", "list"})
	if err != nil {
		t.Fatalf("Find cloud list: %v", err)
	}
	if found.Name() != "registry" {
		t.Fatalf("cloud list resolves to %q, want registry", found.Name())
	}
}

func TestCloudDeploy_MCPType(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_mcp_deploy"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deployments" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /v1/deployments", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode deployment request: %v", err)
		}
		if got, ok := body["kind"].(string); !ok || got != "mcp" {
			t.Errorf("kind = %#v, want mcp", body["kind"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
			ID: "dep-mcp", ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: "pending",
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "--type", "mcp", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("cloud deploy --type mcp: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-mcp") {
		t.Fatalf("deploy output = %q, want dep-mcp", stdout)
	}
}

func TestCloudDeploy_TypeHelp(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "deploy", "--help")
	if err != nil {
		t.Fatalf("cloud deploy --help: %v", err)
	}
	if !strings.Contains(stdout, "agent|mcp") {
		t.Fatalf("deploy help = %q, want agent|mcp type help", stdout)
	}
}
