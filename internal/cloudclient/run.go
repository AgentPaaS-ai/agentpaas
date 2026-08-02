package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// RunRecord represents a single run record returned by the API.
type RunRecord struct {
	ID               string  `json:"id"`
	TenantID         string  `json:"tenant_id"`
	DeploymentID     string  `json:"deployment_id"`
	Status           string  `json:"status"`
	Admission        *string `json:"admission,omitempty"`
	QueuePosition    *int `json:"queue_position,omitempty"`
	CreatedAt        string  `json:"created_at"`
	ConcurrencyLimit int     `json:"concurrency_limit"`
	ActiveBefore     int  `json:"active_before"`
	SlotID           *string `json:"slot_id,omitempty"`
	ContainerID      *string `json:"container_id,omitempty"`
	UpdatedAt        *string `json:"updated_at,omitempty"`
	Error            *string `json:"error,omitempty"`
	StartedAt        *string `json:"started_at,omitempty"`
	FinishedAt       *string `json:"finished_at,omitempty"`
}

// CreateRunRequest is the body for POST /v1/runs.
type CreateRunRequest struct {
	DeploymentID string   `json:"deployment_id"`
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
}

// CreateRun calls POST /v1/runs with a Bearer token.
func (c *CloudClient) CreateRun(ctx context.Context, token string, req CreateRunRequest) (*RunRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("create run: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/runs", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create run: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("create run", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("create run: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("create run", resp)
	}

	var result RunRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("create run: decode response: %w", err)
	}
	return &result, nil
}

// ListRuns calls GET /v1/runs with a Bearer token.
func (c *CloudClient) ListRuns(ctx context.Context, token string) ([]RunRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/runs", nil)
	if err != nil {
		return nil, fmt.Errorf("list runs: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("list runs", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list runs: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list runs", resp)
	}

	var results []RunRecord
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("list runs: decode response: %w", err)
	}
	return results, nil
}

// GetRun calls GET /v1/runs/{id} with a Bearer token.
func (c *CloudClient) GetRun(ctx context.Context, token string, id string) (*RunRecord, error) {
	// Sanitize: id must not contain path traversal or newlines.
	if strings.ContainsAny(id, "/\\\n\r") {
		return nil, fmt.Errorf("get run: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/runs/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("get run: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("get run", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get run: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get run", resp)
	}

	var result RunRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get run: decode response: %w", err)
	}
	return &result, nil
}

// CancelRun calls POST /v1/runs/{id}/cancel with a Bearer token.
func (c *CloudClient) CancelRun(ctx context.Context, token string, id string) (*RunRecord, error) {
	// Sanitize: id must not contain path traversal or newlines.
	if strings.ContainsAny(id, "/\\\n\r") {
		return nil, fmt.Errorf("cancel run: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/runs/"+id+"/cancel", nil)
	if err != nil {
		return nil, fmt.Errorf("cancel run: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("cancel run", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("cancel run: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("cancel run", resp)
	}

	var result RunRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("cancel run: decode response: %w", err)
	}
	return &result, nil
}
