package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SecretLabel represents a cloud secret without its value.
type SecretLabel struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListSecretsResponse is the envelope for GET /v1/secrets.
type ListSecretsResponse struct {
	Secrets []SecretLabel `json:"secrets"`
}

// PutSecret calls PUT /v1/secrets/:name with a Bearer token, expecting 204.
func (c *CloudClient) PutSecret(ctx context.Context, token, name, value string) error {
	// Sanitize: name must not contain path traversal or newlines.
	if strings.ContainsAny(name, "/\\\n\r") {
		return fmt.Errorf("put secret: invalid name %q", name)
	}

	body := map[string]string{"value": value}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("put secret: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.BaseURL+"/v1/secrets/"+name, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("put secret: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return wrapTransportError("put secret", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("put secret: not authenticated (token may be expired or invalid)")
	}
	if resp.StatusCode != http.StatusNoContent {
		return statusError("put secret", resp)
	}
	return nil
}

// ListSecrets calls GET /v1/secrets with a Bearer token.
func (c *CloudClient) ListSecrets(ctx context.Context, token string) ([]SecretLabel, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/secrets", nil)
	if err != nil {
		return nil, fmt.Errorf("list secrets: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("list secrets", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list secrets: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list secrets", resp)
	}

	var result ListSecretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list secrets: decode response: %w", err)
	}
	return result.Secrets, nil
}

// DeleteSecret calls DELETE /v1/secrets/:name with a Bearer token, expecting 204.
func (c *CloudClient) DeleteSecret(ctx context.Context, token, name string) error {
	// Sanitize: name must not contain path traversal or newlines.
	if strings.ContainsAny(name, "/\\\n\r") {
		return fmt.Errorf("delete secret: invalid name %q", name)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.BaseURL+"/v1/secrets/"+name, nil)
	if err != nil {
		return fmt.Errorf("delete secret: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return wrapTransportError("delete secret", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("delete secret: not authenticated (token may be expired or invalid)")
	}
	if resp.StatusCode != http.StatusNoContent {
		return statusError("delete secret", resp)
	}
	return nil
}

// DeploymentSecretBinding is metadata for a deployment secret binding (never values).
type DeploymentSecretBinding struct {
	SecretName  string  `json:"secret_name"`
	InjectAs    string  `json:"inject_as"`
	HeaderName  *string `json:"header_name,omitempty"`
	HostPattern *string `json:"host_pattern,omitempty"`
}

// SetDeploymentSecretsRequest is the body for PUT /v1/deployments/:id/secrets.
type SetDeploymentSecretsRequest struct {
	Bindings []DeploymentSecretBinding `json:"bindings"`
}

// SetDeploymentSecrets replaces all secret bindings on a deployment.
func (c *CloudClient) SetDeploymentSecrets(ctx context.Context, token, deploymentID string, bindings []DeploymentSecretBinding) error {
	if strings.ContainsAny(deploymentID, "/\\\n\r") {
		return fmt.Errorf("set deployment secrets: invalid deployment id")
	}
	body, err := json.Marshal(SetDeploymentSecretsRequest{Bindings: bindings})
	if err != nil {
		return fmt.Errorf("set deployment secrets: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.BaseURL+"/v1/deployments/"+deploymentID+"/secrets", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("set deployment secrets: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return wrapTransportError("set deployment secrets", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("set deployment secrets: not authenticated (token may be expired or invalid)")
	}
	if resp.StatusCode != http.StatusNoContent && !jsonOK(resp.StatusCode) {
		return statusError("set deployment secrets", resp)
	}
	return nil
}

// ListDeploymentSecrets lists binding metadata for a deployment (never values).
func (c *CloudClient) ListDeploymentSecrets(ctx context.Context, token, deploymentID string) ([]DeploymentSecretBinding, error) {
	if strings.ContainsAny(deploymentID, "/\\\n\r") {
		return nil, fmt.Errorf("list deployment secrets: invalid deployment id")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/v1/deployments/"+deploymentID+"/secrets", nil)
	if err != nil {
		return nil, fmt.Errorf("list deployment secrets: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("list deployment secrets", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list deployment secrets: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("list deployment secrets", resp)
	}

	// API may return {bindings:[...]} or a bare array.
	var envelope struct {
		Bindings []DeploymentSecretBinding `json:"bindings"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("list deployment secrets: decode: %w", err)
	}
	if envelope.Bindings != nil {
		return envelope.Bindings, nil
	}
	return []DeploymentSecretBinding{}, nil
}
