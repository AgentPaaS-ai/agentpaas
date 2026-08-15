package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CreateWorkflowRequest is the body for POST /v1/workflows.
type CreateWorkflowRequest struct {
	Name     string          `json:"name"`
	Envelope json.RawMessage `json:"envelope"`
}

// WorkflowRecord is one workflow returned by the cloud API.
type WorkflowRecord struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Name      string          `json:"name"`
	Version   int             `json:"version"`
	Status    string          `json:"status"`
	CreatedAt string          `json:"created_at"`
	Envelope  json.RawMessage `json:"envelope,omitempty"`
}

// WorkflowListResponse is GET /v1/workflows.
type WorkflowListResponse struct {
	Workflows []WorkflowRecord `json:"workflows"`
}

// StartWorkflowRequest is the body for POST /v1/workflows/{id}/instances.
type StartWorkflowRequest struct {
	InitialHandoff json.RawMessage `json:"initial_handoff,omitempty"`
}

// WorkflowInstanceRecord is one workflow instance returned by the cloud API.
type WorkflowInstanceRecord struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	WorkflowID        string          `json:"workflow_id"`
	CFInstanceID      *string         `json:"cf_instance_id,omitempty"`
	Status            string          `json:"status"`
	CurrentStageIndex int             `json:"current_stage_index"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	ParentInstanceID  *string         `json:"parent_instance_id,omitempty"`
	StageCommits      json.RawMessage `json:"stage_commits,omitempty"`
}

func invalidWorkflowID(id string) bool {
	return strings.ContainsAny(id, "/\\\n\r")
}

// CreateWorkflow calls POST /v1/workflows with a Bearer token.
func (c *CloudClient) CreateWorkflow(ctx context.Context, token string, req CreateWorkflowRequest) (*WorkflowRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("create workflow: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/workflows", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create workflow: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("create workflow", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("create workflow: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("create workflow", resp)
	}

	var result WorkflowRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("create workflow: decode response: %w", err)
	}
	return &result, nil
}

// ListWorkflows calls GET /v1/workflows with a Bearer token.
func (c *CloudClient) ListWorkflows(ctx context.Context, token string) (*WorkflowListResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/workflows", nil)
	if err != nil {
		return nil, fmt.Errorf("list workflows: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("list workflows", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list workflows: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list workflows", resp)
	}

	var result WorkflowListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list workflows: decode response: %w", err)
	}
	if result.Workflows == nil {
		result.Workflows = []WorkflowRecord{}
	}
	return &result, nil
}

// GetWorkflow calls GET /v1/workflows/{id} with a Bearer token.
func (c *CloudClient) GetWorkflow(ctx context.Context, token string, id string) (*WorkflowRecord, error) {
	if invalidWorkflowID(id) {
		return nil, fmt.Errorf("get workflow: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/workflows/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("get workflow", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get workflow: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get workflow", resp)
	}

	var result WorkflowRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get workflow: decode response: %w", err)
	}
	return &result, nil
}

// StartWorkflowInstance calls POST /v1/workflows/{id}/instances with a Bearer token.
func (c *CloudClient) StartWorkflowInstance(ctx context.Context, token string, id string, req StartWorkflowRequest) (*WorkflowInstanceRecord, error) {
	if invalidWorkflowID(id) {
		return nil, fmt.Errorf("start workflow: invalid id %q", id)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("start workflow: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/workflows/"+id+"/instances", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("start workflow: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("start workflow", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("start workflow: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("start workflow", resp)
	}

	var result WorkflowInstanceRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("start workflow: decode response: %w", err)
	}
	return &result, nil
}

// GetWorkflowInstance calls GET /v1/workflow-instances/{id} with a Bearer token.
func (c *CloudClient) GetWorkflowInstance(ctx context.Context, token string, id string) (*WorkflowInstanceRecord, error) {
	if invalidWorkflowID(id) {
		return nil, fmt.Errorf("get workflow instance: invalid id %q", id)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/workflow-instances/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow instance: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("get workflow instance", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get workflow instance: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get workflow instance", resp)
	}

	var result WorkflowInstanceRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get workflow instance: decode response: %w", err)
	}
	return &result, nil
}
