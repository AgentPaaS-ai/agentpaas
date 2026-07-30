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
		return fmt.Errorf("put secret: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("put secret: not authenticated (token may be expired or invalid)")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("put secret: unexpected status %d", resp.StatusCode)
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
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list secrets: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("list secrets: unexpected status %d", resp.StatusCode)
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
		return fmt.Errorf("delete secret: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("delete secret: not authenticated (token may be expired or invalid)")
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete secret: unexpected status %d", resp.StatusCode)
	}
	return nil
}
