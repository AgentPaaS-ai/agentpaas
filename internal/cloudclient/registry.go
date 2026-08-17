package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// ComponentIndexSchemaV1 is the only registry card schema the CLI will render.
const ComponentIndexSchemaV1 = "component-index/1"

// RegistryComponentSummary is one list/search row.
type RegistryComponentSummary struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	EgressCount int    `json:"egress_count"`
}

// RegistryComponentList is GET /v1/registry/components.
type RegistryComponentList struct {
	Components []RegistryComponentSummary `json:"components"`
	NextCursor string                     `json:"next_cursor,omitempty"`
}

// RegistryComponentCard is the full component index JSON.
type RegistryComponentCard struct {
	SchemaVersion string         `json:"schema_version"`
	ID            string         `json:"id"`
	Kind          string         `json:"kind"`
	Name          string         `json:"name"`
	Title         string         `json:"title,omitempty"`
	Version       string         `json:"version,omitempty"`
	Description   string         `json:"description,omitempty"`
	Egress        []string       `json:"egress"`
	Raw           map[string]any `json:"-"`
}

// ListRegistryComponentsQuery is the list/search query string.
type ListRegistryComponentsQuery struct {
	Kind string
	Q    string
}

// ListRegistryComponents calls GET /v1/registry/components.
func (c *CloudClient) ListRegistryComponents(ctx context.Context, token string, query ListRegistryComponentsQuery) (*RegistryComponentList, error) {
	values := url.Values{}
	if strings.ContainsAny(query.Kind, "\r\n\x00") || strings.ContainsAny(query.Q, "\r\n\x00") {
		return nil, fmt.Errorf("list registry components: invalid filter")
	}
	if query.Kind != "" {
		values.Set("kind", query.Kind)
	}
	if query.Q != "" {
		values.Set("q", query.Q)
	}
	path := "/v1/registry/components"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("list registry components: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("list registry components", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list registry components: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list registry components", resp)
	}
	var result RegistryComponentList
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list registry components: decode response: %w", err)
	}
	return &result, nil
}

// GetRegistryComponent calls GET /v1/registry/components/:idOrName.
func (c *CloudClient) GetRegistryComponent(ctx context.Context, token, idOrName string) (*RegistryComponentCard, error) {
	if strings.TrimSpace(idOrName) == "" || strings.ContainsAny(idOrName, "\r\n\x00") {
		return nil, fmt.Errorf("get registry component: invalid name")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/registry/components/"+url.PathEscape(idOrName), nil)
	if err != nil {
		return nil, fmt.Errorf("get registry component: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("get registry component", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get registry component: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get registry component", resp)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("get registry component: decode response: %w", err)
	}
	card := &RegistryComponentCard{Raw: raw}
	if v, ok := raw["schema_version"].(string); ok {
		card.SchemaVersion = v
	}
	if v, ok := raw["id"].(string); ok {
		card.ID = v
	}
	if v, ok := raw["kind"].(string); ok {
		card.Kind = v
	}
	if v, ok := raw["name"].(string); ok {
		card.Name = v
	}
	if v, ok := raw["title"].(string); ok {
		card.Title = v
	}
	if v, ok := raw["version"].(string); ok {
		card.Version = v
	}
	if v, ok := raw["description"].(string); ok {
		card.Description = v
	}
	if arr, ok := raw["egress"].([]any); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				card.Egress = append(card.Egress, s)
			}
		}
	}
	return card, nil
}
