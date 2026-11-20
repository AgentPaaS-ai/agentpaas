package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// MintInvokeTokenResponse is the response from POST
// /v1/deployments/{id}/invoke-token.
type MintInvokeTokenResponse struct {
	DeploymentID      string `json:"deployment_id"`
	InvokeToken       string `json:"invoke_token"`
	InvokeTokenPrefix string `json:"invoke_token_prefix"`
	Message           string `json:"message"`
}

// MintInvokeToken requests a short-lived deployment invoke token with a
// tenant API token.
func (c *CloudClient) MintInvokeToken(ctx context.Context, tenantToken, deploymentID string) (*MintInvokeTokenResponse, error) {
	if err := validateInvokeDeploymentID("mint invoke token", deploymentID); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/deployments/"+deploymentID+"/invoke-token", nil)
	if err != nil {
		return nil, fmt.Errorf("mint invoke token: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("mint invoke token", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("mint invoke token: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("mint invoke token", resp)
	}

	var result MintInvokeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mint invoke token: decode response: %w", err)
	}
	return &result, nil
}

// InvokeDeploymentResult is the response from POST
// /v1/deployments/{id}/invoke.
type InvokeDeploymentResult struct {
	RunID       string          `json:"run_id"`
	ID          string          `json:"id"` // some failure paths return RunRecord.id only
	Status      string          `json:"status"`
	Error       json.RawMessage `json:"error"` // string or object
	FinalOutput json.RawMessage `json:"final_output"`
}

// EffectiveRunID returns run_id, falling back to id for older/error envelopes.
func (r *InvokeDeploymentResult) EffectiveRunID() string {
	if r == nil {
		return ""
	}
	if r.RunID != "" {
		return r.RunID
	}
	return r.ID
}

// ErrorString returns a displayable error, or "" if none.
func (r *InvokeDeploymentResult) ErrorString() string {
	if r == nil || len(r.Error) == 0 || string(r.Error) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(r.Error, &s); err == nil {
		if s == "" || s == "undefined" {
			return ""
		}
		return s
	}
	return string(r.Error)
}

// InvokeDeployment invokes a deployment with an invoke token and returns the
// deployment invocation result.
func (c *CloudClient) InvokeDeployment(ctx context.Context, invokeToken, deploymentID string, body json.RawMessage) (*InvokeDeploymentResult, error) {
	if err := validateInvokeDeploymentID("invoke deployment", deploymentID); err != nil {
		return nil, err
	}

	requestBody := body
	if len(bytes.TrimSpace(requestBody)) == 0 {
		requestBody = json.RawMessage(`{}`)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/deployments/"+deploymentID+"/invoke", bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("invoke deployment: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+invokeToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("invoke deployment", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invoke deployment: not authenticated (invoke token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("invoke deployment", resp)
	}

	var result InvokeDeploymentResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("invoke deployment: decode response: %w", err)
	}
	return &result, nil
}

func validateInvokeDeploymentID(operation, deploymentID string) error {
	if deploymentID == "" || strings.ContainsAny(deploymentID, "/\\\n\r\x00?#") {
		return fmt.Errorf("%s: invalid deployment ID %q", operation, deploymentID)
	}
	return nil
}
