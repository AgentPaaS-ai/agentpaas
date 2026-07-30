package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// DeploymentRecord represents a single deployment record returned by the API.
type DeploymentRecord struct {
	ID          string  `json:"id"`
	ImageDigest string  `json:"image_digest"`
	SlotID      *string `json:"slot_id,omitempty"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
}

// CreateDeploymentRequest is the body for POST /v1/deployments.
type CreateDeploymentRequest struct {
	ImageDigest string  `json:"image_digest"`
	SlotID      *string `json:"slot_id,omitempty"`
}

// CreateDeployment calls POST /v1/deployments with a Bearer token.
func (c *CloudClient) CreateDeployment(ctx context.Context, token string, req CreateDeploymentRequest) (*DeploymentRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("create deployment: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/deployments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create deployment: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("create deployment: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("create deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("create deployment: unexpected status %d", resp.StatusCode)
	}

	var result DeploymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("create deployment: decode response: %w", err)
	}
	return &result, nil
}

// ListDeployments calls GET /v1/deployments with a Bearer token.
func (c *CloudClient) ListDeployments(ctx context.Context, token string) ([]DeploymentRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/deployments", nil)
	if err != nil {
		return nil, fmt.Errorf("list deployments: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list deployments: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("list deployments: unexpected status %d", resp.StatusCode)
	}

	var results []DeploymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("list deployments: decode response: %w", err)
	}
	return results, nil
}

// GetDeployment calls GET /v1/deployments/{id} with a Bearer token.
func (c *CloudClient) GetDeployment(ctx context.Context, token string, id string) (*DeploymentRecord, error) {
	// Sanitize: id must not contain path traversal or newlines.
	if strings.ContainsAny(id, "/\\\n\r") {
		return nil, fmt.Errorf("get deployment: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/deployments/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("get deployment: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("get deployment: unexpected status %d", resp.StatusCode)
	}

	var result DeploymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get deployment: decode response: %w", err)
	}
	return &result, nil
}
