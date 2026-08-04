package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegistryResponse is the response from GET /v1/registry.
type RegistryResponse struct {
	TenantAssets []RegistryAsset  `json:"tenant_assets"`
	Platform     RegistryPlatform `json:"platform"`
}

// RegistryAsset is an asset owned by the authenticated tenant.
type RegistryAsset struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Status      string `json:"status"`
	Image       string `json:"image,omitempty"`
	ImageDigest string `json:"image_digest,omitempty"`
	RegistryRef string `json:"registry_ref,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// RegistryPlatform contains platform-managed registry data.
type RegistryPlatform struct {
	MCPCatalog []MCPRegistryEntry `json:"mcp_catalog"`
}

// MCPRegistryEntry is an MCP server available from the platform catalog.
type MCPRegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Status      string `json:"status,omitempty"`
	Image       string `json:"image,omitempty"`
	ImageDigest string `json:"image_digest,omitempty"`
	RegistryRef string `json:"registry_ref,omitempty"`
}

// GetRegistry calls GET /v1/registry with a Bearer token.
func (c *CloudClient) GetRegistry(ctx context.Context, token string) (*RegistryResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/registry", nil)
	if err != nil {
		return nil, fmt.Errorf("get registry: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("get registry", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get registry: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get registry", resp)
	}

	var result RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get registry: decode response: %w", err)
	}
	return &result, nil
}
