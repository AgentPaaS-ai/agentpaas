package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegistryResponse is GET /v1/registry (matches cloud src/registry.ts).
type RegistryResponse struct {
	TenantID string           `json:"tenant_id"`
	Assets   RegistryAssets   `json:"assets"`
	Platform RegistryPlatform `json:"platform"`
}

type RegistryAssets struct {
	Deployments []RegistryDeployment `json:"deployments"`
	Images      []RegistryImage      `json:"images"`
	Secrets     []RegistrySecret     `json:"secrets"`
}

type RegistryDeployment struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	ImageDigest string `json:"image_digest"`
	AgentName   string `json:"agent_name,omitempty"`
}

type RegistryImage struct {
	ID          string  `json:"id"`
	ImageDigest string  `json:"image_digest"`
	AgentName   *string `json:"agent_name"`
	Status      string  `json:"status"`
}

type RegistrySecret struct {
	Label string `json:"label"`
}

type RegistryPlatform struct {
	MCPCatalog []MCPRegistryEntry `json:"mcp_catalog"`
}

type MCPRegistryEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
}

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
