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
			"tenant_id": "tenant-registry-1",
			"assets": map[string]any{
				"deployments": []map[string]any{
					{
						"id":           "deployment-1",
						"image_digest": "sha256:deployment",
						"status":       "running",
					},
				},
				"images": []map[string]any{
					{
						"id":           "image-1",
						"image_digest": "sha256:image",
						"status":       "ready",
					},
				},
				"secrets": []map[string]any{
					{"label": "OPENAI_API_KEY"},
				},
			},
			"platform": map[string]any{
				"mcp_catalog": []map[string]any{
					{
						"name":        "GitHub",
						"description": "GitHub tools",
						"visibility":  "public",
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
	if result.TenantID != "tenant-registry-1" {
		t.Errorf("TenantID = %q, want tenant-registry-1", result.TenantID)
	}
	if len(result.Assets.Deployments) != 1 {
		t.Fatalf("Deployments length = %d, want 1", len(result.Assets.Deployments))
	}
	if result.Assets.Deployments[0].ID != "deployment-1" {
		t.Errorf("Deployments[0].ID = %q, want deployment-1", result.Assets.Deployments[0].ID)
	}
	if len(result.Assets.Images) != 1 {
		t.Fatalf("Images length = %d, want 1", len(result.Assets.Images))
	}
	if result.Assets.Images[0].ImageDigest != "sha256:image" {
		t.Errorf("Images[0].ImageDigest = %q, want sha256:image", result.Assets.Images[0].ImageDigest)
	}
	if len(result.Assets.Secrets) != 1 {
		t.Fatalf("Secrets length = %d, want 1", len(result.Assets.Secrets))
	}
	if result.Assets.Secrets[0].Label != "OPENAI_API_KEY" {
		t.Errorf("Secrets[0].Label = %q, want OPENAI_API_KEY", result.Assets.Secrets[0].Label)
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
