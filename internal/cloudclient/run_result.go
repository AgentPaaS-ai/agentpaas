package cloudclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RunArtifact is a signed result-package artifact reference returned by the
// cloud result endpoint.
type RunArtifact struct {
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	URL          string `json:"url"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

// RunResult is the customer-facing result package summary for a run.
type RunResult struct {
	RunID       string          `json:"run_id"`
	Status      string          `json:"status"`
	Error       *string         `json:"error"`
	FinishedAt  *string         `json:"finished_at"`
	FinalOutput json.RawMessage `json:"final_output"`
	Artifacts   []RunArtifact   `json:"artifacts"`
}

// ErrRunLogsNotFound indicates that the result package did not include a
// logs.txt artifact.
var ErrRunLogsNotFound = errors.New("run logs artifact not found")

// GetRunResult calls GET /v1/runs/{id}/result with a Bearer token.
func (c *CloudClient) GetRunResult(ctx context.Context, token, id string) (*RunResult, error) {
	if err := validateRunID("get run result", id); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/runs/"+id+"/result", nil)
	if err != nil {
		return nil, fmt.Errorf("get run result: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("get run result", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get run result: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get run result", resp)
	}

	var result RunResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get run result: decode response: %w", err)
	}
	return &result, nil
}

// GetRunLogs fetches the logs.txt artifact from the signed URL in the run
// result package. Cloud currently has no separate logs endpoint.
func (c *CloudClient) GetRunLogs(ctx context.Context, token, id string) ([]byte, error) {
	result, err := c.GetRunResult(ctx, token, id)
	if err != nil {
		return nil, err
	}
	for _, artifact := range result.Artifacts {
		if artifact.Name != "logs.txt" {
			continue
		}
		if artifact.URL == "" {
			return nil, fmt.Errorf("%w for run %q: signed URL is empty", ErrRunLogsNotFound, id)
		}
		return c.FetchSignedURL(ctx, artifact.URL)
	}
	return nil, fmt.Errorf("%w for run %q", ErrRunLogsNotFound, id)
}

// FetchSignedURL fetches an artifact capability URL without adding a Bearer
// token. The URL is supplied by the authenticated result endpoint.
func (c *CloudClient) FetchSignedURL(ctx context.Context, signedURL string) ([]byte, error) {
	if strings.ContainsAny(signedURL, "\r\n\x00") {
		return nil, fmt.Errorf("fetch signed URL: URL contains a control character")
	}
	parsed, err := url.Parse(signedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch signed URL: parse URL: %w", err)
	}
	if !parsed.IsAbs() {
		base, baseErr := url.Parse(c.BaseURL)
		if baseErr != nil {
			return nil, fmt.Errorf("fetch signed URL: parse client base URL: %w", baseErr)
		}
		parsed = base.ResolveReference(parsed)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("fetch signed URL: URL must be an absolute HTTP(S) URL without user information")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch signed URL: create request: %w", err)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("fetch signed URL", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !jsonOK(resp.StatusCode) {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("fetch signed URL: signed URL expired or invalid (status %d)", resp.StatusCode)
		}
		return nil, statusError("fetch signed URL", resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch signed URL: read response: %w", err)
	}
	return data, nil
}

func validateRunID(operation, id string) error {
	if id == "" || strings.ContainsAny(id, "/\\\n\r\x00?#") {
		return fmt.Errorf("%s: invalid id %q", operation, id)
	}
	return nil
}
