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
			TenantID: "tenant-cli-1",
			Assets: cloudclient.RegistryAssets{
				Deployments: []cloudclient.RegistryDeployment{{
					ID: "deployment-1", Kind: "agent", Status: "running", ImageDigest: "sha256:image", AgentName: "weather-agent",
				}},
				Images: []cloudclient.RegistryImage{{
					ID: "image-1", ImageDigest: "sha256:image", Status: "ready",
				}},
				Secrets: []cloudclient.RegistrySecret{{Label: "OPENAI_API_KEY"}},
			},
			Platform: cloudclient.RegistryPlatform{
				MCPCatalog: []cloudclient.MCPRegistryEntry{{Name: "GitHub", Description: "GitHub tools"}},
			},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry")
	if err != nil {
		t.Fatalf("cloud registry: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{
		"Tenant: tenant-cli-1",
		"Deployments (1):",
		"weather-agent",
		"Images (1):",
		"sha256:image",
		"Secrets (1):",
		"OPENAI_API_KEY",
		"GitHub",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("registry output = %q, want %q", stdout, want)
		}
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
			"tenant_id": "tenant-json-1",
			"assets": map[string]any{
				"deployments": []map[string]string{{"id": "deployment-json", "kind": "mcp", "agent_name": "Slack", "status": "running"}},
				"images":      []map[string]string{{"id": "image-json", "image_digest": "sha256:json", "status": "ready"}},
				"secrets":     []map[string]string{{"label": "SLACK_TOKEN"}},
			},
			"platform": map[string]any{
				"mcp_catalog": []map[string]string{{"name": "Slack"}},
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
	if got.TenantID != "tenant-json-1" {
		t.Fatalf("tenant ID = %q, want tenant-json-1", got.TenantID)
	}
	if len(got.Assets.Deployments) != 1 || got.Assets.Deployments[0].ID != "deployment-json" {
		t.Fatalf("deployments = %#v", got.Assets.Deployments)
	}
	if len(got.Assets.Images) != 1 || got.Assets.Images[0].ImageDigest != "sha256:json" {
		t.Fatalf("images = %#v", got.Assets.Images)
	}
	if len(got.Assets.Secrets) != 1 || got.Assets.Secrets[0].Label != "SLACK_TOKEN" {
		t.Fatalf("secrets = %#v", got.Assets.Secrets)
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

func TestCloudDeploy_ToolType(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_tool_deploy"); err != nil {
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
		if got, ok := body["kind"].(string); !ok || got != "tool" {
			t.Errorf("kind = %#v, want tool", body["kind"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
			ID: "dep-tool", ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: "pending",
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "--type", "tool", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("cloud deploy --type tool: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-tool") {
		t.Fatalf("deploy output = %q, want dep-tool", stdout)
	}
}

func TestCloudDeploy_UnknownType(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_widget_deploy"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "--type", "widget", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected error for --type widget")
	}
	combined := err.Error() + stderr
	want := "--type must be agent, mcp, or tool"
	if !strings.Contains(combined, want) {
		t.Fatalf("error should contain %q, got: %s", want, combined)
	}
}

func TestCloudDeploy_TypeHelp(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "deploy", "--help")
	if err != nil {
		t.Fatalf("cloud deploy --help: %v", err)
	}
	if !strings.Contains(stdout, "agent|mcp|tool") {
		t.Fatalf("deploy help = %q, want agent|mcp|tool type help", stdout)
	}
}

func sampleComponentCard() map[string]any {
	return map[string]any{
		"schema_version": "component-index/1",
		"id":             "img_github",
		"kind":           "mcp",
		"name":           "github-mcp",
		"title":          "GitHub helpers",
		"version":        "1.0.0",
		"description":    "GitHub issues",
		"egress":         []string{"api.github.com"},
		"mcp": map[string]any{
			"protocolVersion": "2025-06-18",
			"tools": []map[string]any{{
				"name":         "list_issues",
				"title":        "List issues",
				"inputSchema":  map[string]any{"type": "object"},
				"outputSchema": map[string]any{"type": "object"},
			}},
		},
	}
}

func TestCloudRegistry_ListComponentsJSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_reg_list"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/registry/components" {
			t.Errorf("path = %q, want /v1/registry/components", r.URL.Path)
		}
		if r.URL.Query().Get("kind") != "mcp" {
			t.Errorf("kind = %q, want mcp", r.URL.Query().Get("kind"))
		}
		if r.URL.Query().Get("q") != "github" {
			t.Errorf("q = %q, want github", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"components": []map[string]any{{
				"id":           "img_github",
				"kind":         "mcp",
				"name":         "github-mcp",
				"title":        "GitHub helpers",
				"version":      "1.0.0",
				"description":  "GitHub issues",
				"egress_count": 1,
			}},
			"next_cursor": nil,
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "list", "--kind", "mcp", "--q", "github", "--json")
	if err != nil {
		t.Fatalf("registry list --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "github-mcp") || !strings.Contains(stdout, "egress_count") {
		t.Fatalf("list json = %q", stdout)
	}
}

func TestCloudRegistry_ListComponentsText(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_reg_list_text"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"components": []map[string]any{{
				"id":           "img_github",
				"kind":         "mcp",
				"name":         "github-mcp",
				"version":      "1.0.0",
				"description":  "GitHub issues",
				"egress_count": 2,
			}},
		})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "list")
	if err != nil {
		t.Fatalf("registry list: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{"mcp", "github-mcp", "1.0.0", "2", "GitHub issues"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list text = %q, want %q", stdout, want)
		}
	}
}

func TestCloudRegistry_GetInspectSearchJSON(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_reg_get"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/registry/components/github-mcp":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sampleComponentCard())
		case r.URL.Path == "/v1/registry/components" && r.URL.Query().Get("q") == "github issues":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"components": []map[string]any{{
					"id":           "img_github",
					"kind":         "mcp",
					"name":         "github-mcp",
					"version":      "1.0.0",
					"description":  "GitHub issues",
					"egress_count": 1,
				}},
			})
		default:
			t.Errorf("unexpected path %s?%s", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "get", "github-mcp", "--json")
	if err != nil {
		t.Fatalf("registry get: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"schema_version": "component-index/1"`) || !strings.Contains(stdout, "list_issues") {
		t.Fatalf("get json = %q", stdout)
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "registry", "inspect", "github-mcp", "--json")
	if err != nil {
		t.Fatalf("registry inspect: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "list_issues") || !strings.Contains(stdout, "inputSchema") {
		t.Fatalf("inspect json = %q", stdout)
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "registry", "search", "github issues", "--json")
	if err != nil {
		t.Fatalf("registry search: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "github-mcp") {
		t.Fatalf("search json = %q", stdout)
	}
}

func TestCloudRegistry_SchemaVersionFailClosed(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_reg_schema"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		card := sampleComponentCard()
		card["schema_version"] = "component-index/99"
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "inspect", "github-mcp", "--json")
	if err == nil {
		t.Fatal("expected error for unknown schema_version")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "schema_version") {
		t.Fatalf("error = %s, want schema_version rejection", combined)
	}
}

func TestCloudRegistry_CrossTenantNotFound(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_reg_404"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "registry", "get", "secret-mcp")
	if err == nil {
		t.Fatal("expected not found")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not found") && !strings.Contains(combined, "404") && !strings.Contains(combined, "not_found") {
		t.Fatalf("error = %s, want not found", combined)
	}
}
