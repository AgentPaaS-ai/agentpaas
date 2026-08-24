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
	ID                string  `json:"id"`
	ImageDigest       string  `json:"image_digest"`
	SlotID            *string `json:"slot_id,omitempty"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
	MaxConcurrentRuns *int    `json:"max_concurrent_runs,omitempty"`
}

// DeploymentDeleteResult is the response from DELETE /v1/deployments/{id}.
type DeploymentDeleteResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// CreateDeploymentRequest is the body for POST /v1/deployments.
type CreateDeploymentRequest struct {
	ImageDigest       string  `json:"image_digest"`
	Kind              string  `json:"kind,omitempty"`
	SlotID            *string `json:"slot_id,omitempty"`
	InstanceType      *string `json:"instance_type,omitempty"`
	MaxConcurrentRuns *int    `json:"max_concurrent_runs,omitempty"`
	Callees           []struct {
		DeploymentID string `json:"deployment_id"`
	} `json:"callees,omitempty"`
}

// PatchDeploymentRequest is the body for PATCH /v1/deployments/{id}.
// MaxConcurrentRuns has no omitempty so a nil pointer marshals as JSON null (unlock).
type PatchDeploymentRequest struct {
	MaxConcurrentRuns *int `json:"max_concurrent_runs"`
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
		return nil, wrapTransportError("create deployment", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("create deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("create deployment", resp)
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
		return nil, wrapTransportError("list deployments", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list deployments: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list deployments", resp)
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
		return nil, wrapTransportError("get deployment", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get deployment", resp)
	}

	var result DeploymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get deployment: decode response: %w", err)
	}
	return &result, nil
}

// PatchDeployment calls PATCH /v1/deployments/{id} with a Bearer token.
func (c *CloudClient) PatchDeployment(ctx context.Context, token string, id string, req PatchDeploymentRequest) (*DeploymentRecord, error) {
	if strings.ContainsAny(id, "/\\\n\r") {
		return nil, fmt.Errorf("patch deployment: invalid id %q", id)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("patch deployment: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.BaseURL+"/v1/deployments/"+id, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("patch deployment: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("patch deployment", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("patch deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("patch deployment", resp)
	}

	var result DeploymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("patch deployment: decode response: %w", err)
	}
	return &result, nil
}

// DeleteDeployment calls DELETE /v1/deployments/{id} with a Bearer token.
func (c *CloudClient) DeleteDeployment(ctx context.Context, token string, id string) (*DeploymentDeleteResult, error) {
	// Sanitize: id must not contain path traversal or newlines.
	if strings.ContainsAny(id, "/\\\n\r") {
		return nil, fmt.Errorf("undeploy deployment: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/v1/deployments/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("undeploy deployment: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("undeploy deployment", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("undeploy deployment: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("undeploy deployment", resp)
	}

	var result DeploymentDeleteResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("undeploy deployment: decode response: %w", err)
	}
	return &result, nil
}
