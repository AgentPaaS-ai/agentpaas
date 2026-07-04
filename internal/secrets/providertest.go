package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProviderTestResult holds the result of a provider credential test.
type ProviderTestResult struct {
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	Status     string `json:"status"` // "ok" or "error"
	HTTPStatus int    `json:"http_status"`
	Detail     string `json:"detail"`
}

// Package-level endpoint URLs so tests can override them.
var (
	openAIEndpoint    = "https://api.openai.com/v1/chat/completions"
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	xaiEndpoint       = "https://api.x.ai/v1/chat/completions"
)

// SetTestEndpoints overrides provider endpoints for testing. Returns a restore function.
func SetTestEndpoints(openAI, anthropic, xai string) func() {
	origOpenAI := openAIEndpoint
	origAnthropic := anthropicEndpoint
	origXAI := xaiEndpoint
	openAIEndpoint = openAI
	anthropicEndpoint = anthropic
	xaiEndpoint = xai
	return func() {
		openAIEndpoint = origOpenAI
		anthropicEndpoint = origAnthropic
		xaiEndpoint = origXAI
	}
}

// TestProvider makes a trivial authenticated call to validate a credential.
// It NEVER includes the secretValue in the result.
func TestProvider(ctx context.Context, provider string, secretValue []byte) ProviderTestResult {
	switch strings.ToLower(provider) {
	case "openai":
		return testOpenAI(ctx, secretValue)
	case "anthropic":
		return testAnthropic(ctx, secretValue)
	case "xai", "xiai":
		return testXAI(ctx, secretValue)
	default:
		return ProviderTestResult{
			Provider: provider,
			Status:   "error",
			Detail:   fmt.Sprintf("unknown provider %q", provider),
		}
	}
}

func testOpenAI(parentCtx context.Context, secretValue []byte) ProviderTestResult {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	body := map[string]interface{}{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "user", "content": "say OK"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ProviderTestResult{
			Provider: "openai",
			Endpoint: openAIEndpoint,
			Status:   "error",
			Detail:   fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("Authorization", "Bearer "+string(secretValue))
	req.Header.Set("Content-Type", "application/json")

	return doProviderRequest(req, "openai", openAIEndpoint)
}

func testAnthropic(parentCtx context.Context, secretValue []byte) ProviderTestResult {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	body := map[string]interface{}{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 5,
		"messages": []map[string]string{
			{"role": "user", "content": "say OK"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ProviderTestResult{
			Provider: "anthropic",
			Endpoint: anthropicEndpoint,
			Status:   "error",
			Detail:   fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("x-api-key", string(secretValue))
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	return doProviderRequest(req, "anthropic", anthropicEndpoint)
}

func testXAI(parentCtx context.Context, secretValue []byte) ProviderTestResult {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	body := map[string]interface{}{
		"model": "grok-beta",
		"messages": []map[string]string{
			{"role": "user", "content": "say OK"},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xaiEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ProviderTestResult{
			Provider: "xiai",
			Endpoint: xaiEndpoint,
			Status:   "error",
			Detail:   fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("Authorization", "Bearer "+string(secretValue))
	req.Header.Set("Content-Type", "application/json")

	return doProviderRequest(req, "xiai", xaiEndpoint)
}

func doProviderRequest(req *http.Request, provider, endpoint string) ProviderTestResult {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ProviderTestResult{
			Provider: provider,
			Endpoint: endpoint,
			Status:   "error",
			Detail:   fmt.Sprintf("failed to reach %s: %v", endpoint, err),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain body to allow connection reuse, but discard contents.
	_, _ = io.Copy(io.Discard, resp.Body)

	status := "ok"
	detail := ""
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "error"
		detail = fmt.Sprintf("%s returned HTTP %d", provider, resp.StatusCode)
	}

	return ProviderTestResult{
		Provider:   provider,
		Endpoint:   endpoint,
		Status:     status,
		HTTPStatus: resp.StatusCode,
		Detail:     detail,
	}
}
