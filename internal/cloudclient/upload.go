package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// UploadImageStartRequest is the body for POST /v1/images/upload-start.
type UploadImageStartRequest struct {
	ImageDigest string      `json:"image_digest"`
	Platform    string      `json:"platform"`
	AgentLock   interface{} `json:"agent_lock"`
}

// UploadImageStartResponse is the response from POST /v1/images/upload-start.
type UploadImageStartResponse struct {
	UploadID       string `json:"upload_id"`
	ImageID        string `json:"image_id"`
	ChunkSizeBytes int    `json:"chunk_size_bytes"`
}

// UploadImageStart creates a tenant-authenticated image upload session.
func (c *CloudClient) UploadImageStart(ctx context.Context, token string, request UploadImageStartRequest) (*UploadImageStartResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("start image upload: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/images/upload-start", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("start image upload: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("start image upload", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("start image upload: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("start image upload", resp)
	}

	var result UploadImageStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("start image upload: decode response: %w", err)
	}
	return &result, nil
}

// UploadImageChunk uploads one raw tar chunk to an upload session.
func (c *CloudClient) UploadImageChunk(ctx context.Context, token, uploadID string, index int, chunk []byte) error {
	if err := validateUploadID(uploadID); err != nil {
		return fmt.Errorf("upload image chunk: %w", err)
	}
	if index < 1 {
		return fmt.Errorf("upload image chunk: invalid chunk index %d", index)
	}

	path := fmt.Sprintf("%s/v1/images/upload/%s/chunk/%d", c.BaseURL, uploadID, index)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, path, bytes.NewReader(chunk))
	if err != nil {
		return fmt.Errorf("upload image chunk: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return wrapTransportError("upload image chunk", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("upload image chunk: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return statusError("upload image chunk", resp)
	}
	return nil
}

// UploadImageComplete completes an upload and returns the admitted image.
func (c *CloudClient) UploadImageComplete(ctx context.Context, token, uploadID string) (*AdmitImageResponse, error) {
	if err := validateUploadID(uploadID); err != nil {
		return nil, fmt.Errorf("complete image upload: %w", err)
	}

	path := c.BaseURL + "/v1/images/upload/" + uploadID + "/complete"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("complete image upload: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("complete image upload", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("complete image upload: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("complete image upload", resp)
	}

	var result AdmitImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("complete image upload: decode response: %w", err)
	}
	return &result, nil
}

func validateUploadID(uploadID string) error {
	if uploadID == "" || uploadID == "." || uploadID == ".." || strings.Contains(uploadID, "..") || strings.ContainsAny(uploadID, "/\\\n\r\x00?#") {
		return fmt.Errorf("invalid upload ID")
	}
	return nil
}
