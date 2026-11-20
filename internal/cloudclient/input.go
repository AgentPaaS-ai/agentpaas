package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// UploadInputResponse is returned by POST /v1/inputs (M13.8).
type UploadInputResponse struct {
	InputID   string          `json:"input_id"`
	R2Key     string          `json:"r2_key"`
	SHA256    string          `json:"sha256"`
	SizeBytes int64           `json:"size_bytes"`
	InputRef  json.RawMessage `json:"input_ref"`
}

// UploadInput uploads raw bytes as a tenant-scoped input object and returns
// the input_ref fields to attach to a deployment invoke body.
func (c *CloudClient) UploadInput(ctx context.Context, token string, data []byte, contentType string) (*UploadInputResponse, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("upload input: empty body")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/inputs", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("upload input: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(data))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("upload input", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("upload input: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("upload input", resp)
	}

	var result UploadInputResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("upload input: decode response: %w", err)
	}
	if result.R2Key == "" || result.SHA256 == "" {
		return nil, fmt.Errorf("upload input: incomplete response")
	}
	return &result, nil
}

// MergeInputRefIntoBody injects input_ref into a JSON invoke body object.
// body may be empty or "{}".
func MergeInputRefIntoBody(body json.RawMessage, inputRef map[string]any) (json.RawMessage, error) {
	base := map[string]any{}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && string(trimmed) != "null" {
		if err := json.Unmarshal(trimmed, &base); err != nil {
			return nil, fmt.Errorf("merge input_ref: body is not a JSON object: %w", err)
		}
		if base == nil {
			base = map[string]any{}
		}
	}
	base["input_ref"] = inputRef
	out, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("merge input_ref: marshal: %w", err)
	}
	return out, nil
}

// ReadAllLimited reads r up to max+1 bytes; errors if over max.
func ReadAllLimited(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("input exceeds %d bytes", max)
	}
	return data, nil
}
