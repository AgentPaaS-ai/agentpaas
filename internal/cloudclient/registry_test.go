package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudClient_GetRegistry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/registry" {
			t.Errorf("path = %s, want /v1/registry", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_registry_token" {
			t.Errorf("Authorization = %q, want Bearer apc_registry_token", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tenant_assets": []map[string]any{
				{
					"id":            "asset-agent-1",
					"kind":          "agent",
					"name":          "weather-agent",
					"version":       "1.0.0",
					"status":        "ready",
					"image_digest":  "sha256:agent",
					"registry_ref":  "registry.example/tenant/weather:1.0.0",
				},
				{
					"id":       "asset-mcp-1",
					"kind":     "mcp",
					"name":     "github-mcp",
					"version":  "2.0.0",
					"status":   "ready",
					"image":    "ghcr.io/example/github-mcp:2.0.0",
				},
			},
			"platform": map[string]any{
				"mcp_catalog": []map[string]any{
					{
						"id":          "catalog-github",
						"name":        "GitHub",
						"description": "GitHub tools",
						"image":       "ghcr.io/example/github-mcp:2.0.0",
						"version":     "2.0.0",
					},
				},
			},
		})
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).GetRegistry(context.Background(), "apc_registry_token")
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	if len(result.TenantAssets) != 2 {
		t.Fatalf("TenantAssets length = %d, want 2", len(result.TenantAssets))
	}
	if result.TenantAssets[0].Kind != "agent" {
		t.Errorf("TenantAssets[0].Kind = %q, want agent", result.TenantAssets[0].Kind)
	}
	if result.TenantAssets[1].Image != "ghcr.io/example/github-mcp:2.0.0" {
		t.Errorf("TenantAssets[1].Image = %q, want MCP image", result.TenantAssets[1].Image)
	}
	if len(result.Platform.MCPCatalog) != 1 {
		t.Fatalf("MCPCatalog length = %d, want 1", len(result.Platform.MCPCatalog))
	}
	if result.Platform.MCPCatalog[0].Name != "GitHub" {
		t.Errorf("MCPCatalog[0].Name = %q, want GitHub", result.Platform.MCPCatalog[0].Name)
	}
}

func TestCloudClient_GetRegistry_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).GetRegistry(context.Background(), "expired")
	if err == nil {
		t.Fatal("GetRegistry should reject an unauthorized response")
	}
}
