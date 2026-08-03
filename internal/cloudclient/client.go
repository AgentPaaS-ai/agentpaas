package cloudclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultCloudAPIURL is the live production API (workers.dev until cloud.agentpaas.ai DNS is configured). Users can still override with AGENTPAAS_CLOUD_API_URL.
	DefaultCloudAPIURL = "https://agentpaas-cloud-api.parvezsyed.workers.dev"
)

// HTTPStatusError reports a non-successful HTTP response from the cloud API.
// The message is kept user-facing while StatusCode lets callers distinguish
// retryable server failures from validation errors.
type HTTPStatusError struct {
	StatusCode    int
	Message       string
	ErrorCode     string
	Reason        string
	RetryAfterSec int
}

// Error implements error.
func (e *HTTPStatusError) Error() string { return e.Message }

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

// IsRetryableError reports whether err is a server or transport failure that
// may succeed when the request is retried.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= http.StatusInternalServerError && statusErr.StatusCode < 600
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// statusError reads a non-2xx response body and returns an error that surfaces
// the API `error` field when present (UX-HTTP-BODY). Callers must not use
// resp.Body after this returns.
func statusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(body))
	if msg != "" {
		var payload struct {
			Error         string `json:"error"`
			Reason        string `json:"reason"`
			Message       string `json:"message"`
			Hint          string `json:"hint"`
			RetryAfterSec int    `json:"retry_after_sec"`
		}
		if json.Unmarshal(body, &payload) == nil && (payload.Error != "" || payload.Reason != "") {
			errMsg := fmt.Sprintf("%s: %s (status %d)", op, payload.Error, resp.StatusCode)
			if payload.Error == "" {
				errMsg = fmt.Sprintf("%s: %s (status %d)", op, payload.Reason, resp.StatusCode)
			}
			if payload.Message != "" {
				errMsg += ": " + payload.Message
			}
			if payload.Hint != "" {
				errMsg += " — " + payload.Hint
			}
			return &HTTPStatusError{
				StatusCode:    resp.StatusCode,
				Message:       errMsg,
				ErrorCode:     payload.Error,
				Reason:        payload.Reason,
				RetryAfterSec: payload.RetryAfterSec,
			}
		}
		// Non-JSON body: include a short excerpt.
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s: unexpected status %d: %s", op, resp.StatusCode, msg),
		}
	}
	return &HTTPStatusError{
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("%s: unexpected status %d", op, resp.StatusCode),
	}
}

// wrapTransportError rewrites DNS/connection failures into an actionable
// message when the default Cloud hostname is used without AGENTPAAS_CLOUD_API_URL
// (UX-APIHOST empty-stdout / blank whoami).
func wrapTransportError(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var dnsErr *net.DNSError
	dnsFail := errors.As(err, &dnsErr) ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "Name or service not known") ||
		strings.Contains(msg, "nodename nor servname")
	if dnsFail {
		if os.Getenv("AGENTPAAS_CLOUD_API_URL") == "" {
			return fmt.Errorf("%s: cannot reach %s (%v). Set AGENTPAAS_CLOUD_API_URL to your live API host (e.g. https://agentpaas-cloud-api.<account>.workers.dev)",
				op, DefaultCloudAPIURL, err)
		}
		return fmt.Errorf("%s: cannot reach API host (%v). Check AGENTPAAS_CLOUD_API_URL=%s",
			op, err, os.Getenv("AGENTPAAS_CLOUD_API_URL"))
	}
	return fmt.Errorf("%s: %w", op, err)
}

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
		return nil, wrapTransportError("start cli auth", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !jsonOK(resp.StatusCode) {
		return nil, statusError("start cli auth", resp)
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
		return nil, wrapTransportError("whoami", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("whoami: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("whoami", resp)
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
