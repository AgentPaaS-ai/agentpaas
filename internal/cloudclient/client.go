package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultCloudAPIURL is the production AgentPaaS Cloud API base URL.
	DefaultCloudAPIURL = "https://cloud.agentpaas.ai"
)

// StartCLIAuthRequest is the body for POST /v1/auth/cli/start.
type StartCLIAuthRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

// StartCLIAuthResponse is the response from POST /v1/auth/cli/start.
type StartCLIAuthResponse struct {
	State      string `json:"state"`
	ApproveURL string `json:"approve_url"`
}

// WhoamiResponse is the response from GET /v1/whoami.
type WhoamiResponse struct {
	TenantID         string `json:"tenant_id"`
	Tier             string `json:"tier"`
	ConcurrencyLimit int    `json:"concurrency_limit"`
	SecretsBackend   string `json:"secrets_backend"`
}

// CloudClient is an HTTP client for the AgentPaaS Cloud API.
type CloudClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewCloudClient creates a new CloudClient with the given base URL.
func NewCloudClient(baseURL string) *CloudClient {
	if baseURL == "" {
		baseURL = DefaultCloudAPIURL
	}
	return &CloudClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 180 * time.Second},
	}
}

// jsonOK reports whether status is a successful HTTP response that may carry a JSON body (any 2xx).
func jsonOK(code int) bool { return code >= 200 && code < 300 }

// StartCLIAuth initiates a CLI-based login flow and returns the approve URL
// and state parameter.
//
// Cloud API returns 201 Created (pending cli_login). Older mocks may return 200.
// Accept any 2xx so we do not break on either.
func (c *CloudClient) StartCLIAuth(ctx context.Context, redirectURI string) (*StartCLIAuthResponse, error) {
	body := StartCLIAuthRequest{
		RedirectURI: redirectURI,
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("start cli auth: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/auth/cli/start", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, fmt.Errorf("start cli auth: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("start cli auth: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("start cli auth: unexpected status %d", resp.StatusCode)
	}

	var result StartCLIAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("start cli auth: decode response: %w", err)
	}
	return &result, nil
}

// Whoami calls GET /v1/whoami with a Bearer token and returns the user info.
func (c *CloudClient) Whoami(ctx context.Context, token string) (*WhoamiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/whoami", nil)
	if err != nil {
		return nil, fmt.Errorf("whoami: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("whoami: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, fmt.Errorf("whoami: unexpected status %d", resp.StatusCode)
	}

	var result WhoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("whoami: decode response: %w", err)
	}
	return &result, nil
}

// ResolveApproveURL resolves the approve URL. If approveURL is absolute (has a
// scheme), it is returned as-is. Otherwise, it is joined with baseURL.
func ResolveApproveURL(baseURL, approveURL string) string {
	parsed, err := url.Parse(approveURL)
	if err == nil && parsed.IsAbs() {
		return approveURL
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(approveURL, "/")
}
