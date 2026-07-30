package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// AdmitImageRequest is the body for POST /v1/images/admit.
type AdmitImageRequest struct {
	ImageDigest string      `json:"image_digest"`
	Platform    string      `json:"platform"`
	RegistryRef string      `json:"registry_ref,omitempty"`
	AgentLock   interface{} `json:"agent_lock"`
}

// AdmitImageResponse is the response from POST /v1/images/admit.
type AdmitImageResponse struct {
	ID          string `json:"id"`
	ImageDigest string `json:"image_digest"`
	Status      string `json:"status"`
}

// ImageRecord represents a single image record returned by the API.
type ImageRecord struct {
	ID          string `json:"id"`
	ImageDigest string `json:"image_digest"`
	Status      string `json:"status"`
}

// AdmitImage calls POST /v1/images/admit with a Bearer token.
func (c *CloudClient) AdmitImage(ctx context.Context, token string, req AdmitImageRequest) (*AdmitImageResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("admit image: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/images/admit", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("admit image: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("admit image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("admit image: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("admit image: unexpected status %d", resp.StatusCode)
	}

	var result AdmitImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("admit image: decode response: %w", err)
	}
	return &result, nil
}

// ListImages calls GET /v1/images with a Bearer token.
func (c *CloudClient) ListImages(ctx context.Context, token string) ([]ImageRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/images", nil)
	if err != nil {
		return nil, fmt.Errorf("list images: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("list images: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("list images: unexpected status %d", resp.StatusCode)
	}

	var results []ImageRecord
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("list images: decode response: %w", err)
	}
	return results, nil
}

// GetImage calls GET /v1/images/{idOrDigest} with a Bearer token.
func (c *CloudClient) GetImage(ctx context.Context, token string, idOrDigest string) (*ImageRecord, error) {
	// Sanitize: idOrDigest must not contain path traversal or newlines.
	if strings.ContainsAny(idOrDigest, "/\\\n\r") {
		return nil, fmt.Errorf("get image: invalid idOrDigest %q", idOrDigest)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/images/"+idOrDigest, nil)
	if err != nil {
		return nil, fmt.Errorf("get image: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("get image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get image: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("get image: unexpected status %d", resp.StatusCode)
	}

	var result ImageRecord
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get image: decode response: %w", err)
	}
	return &result, nil
}