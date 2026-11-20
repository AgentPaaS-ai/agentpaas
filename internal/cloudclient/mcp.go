package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// McpCall posts a JSON-RPC body to POST /v1/deployments/{id}/mcp with an
// invoke token and returns the raw RPC response.
func (c *CloudClient) McpCall(ctx context.Context, invokeToken, deploymentID string, body json.RawMessage) (json.RawMessage, error) {
	if err := validateInvokeDeploymentID("mcp call", deploymentID); err != nil {
		return nil, err
	}

	requestBody := body
	if len(bytes.TrimSpace(requestBody)) == 0 {
		requestBody = json.RawMessage(`{}`)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/deployments/"+deploymentID+"/mcp", bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("mcp call: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+invokeToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("mcp call", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("mcp call: not authenticated (invoke token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("mcp call", resp)
	}

	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mcp call: decode response: %w", err)
	}
	return result, nil
}
