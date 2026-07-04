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
	nousEndpoint      = "https://inference-api.nousresearch.com/v1/chat/completions"
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
	case "nous":
		return testNous(ctx, secretValue)
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
		"model": "grok-3-mini",
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

func testNous(parentCtx context.Context, secretValue []byte) ProviderTestResult {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()

	body := map[string]interface{}{
		"model": "deepseek/deepseek-v4-flash",
		"messages": []map[string]string{
			{"role": "user", "content": "say OK"},
		},
		"max_tokens": 5,
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nousEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ProviderTestResult{
			Provider: "nous",
			Endpoint: nousEndpoint,
			Status:   "error",
			Detail:   fmt.Sprintf("failed to build request: %v", err),
		}
	}
	req.Header.Set("Authorization", "Bearer "+string(secretValue))
	req.Header.Set("Content-Type", "application/json")

	return doProviderRequest(req, "nous", nousEndpoint)
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

	// Read the response body so we can surface provider error details.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	status := "ok"
	detail := ""
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "error"
		// Try to extract a human-readable message from common JSON error
		// formats. Providers use different shapes:
		//   OpenAI/Anthropic: {"error": {"message": "..."}}
		//   xAI:              {"code": "...", "error": "..."}
		//   Google/Z.AI:      {"error": {"code": ..., "message": "..."}}
		providerMsg := ""
		var parsed map[string]any
		if json.Unmarshal(respBody, &parsed) == nil {
			if errObj, ok := parsed["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok {
					providerMsg = msg
				}
			} else if errStr, ok := parsed["error"].(string); ok {
				// xAI format: error is a plain string
				providerMsg = errStr
			}
			if providerMsg == "" {
				if code, ok := parsed["code"].(string); ok {
					providerMsg = code
				}
			}
		}
		// Add actionable guidance based on status code.
		guidance := ""
		switch resp.StatusCode {
		case 401, 403:
			guidance = " — credential may be expired or invalid; refresh the API key/OAuth token and re-store via 'agentpaas secret add'"
		case 404:
			guidance = " — model not found; check the model name in agent.yaml"
		case 429:
			guidance = " — rate limit exceeded; reduce request frequency"
		}
		if providerMsg != "" {
			detail = fmt.Sprintf("%s returned HTTP %d: %s%s", provider, resp.StatusCode, providerMsg, guidance)
		} else {
			detail = fmt.Sprintf("%s returned HTTP %d%s", provider, resp.StatusCode, guidance)
		}
	}

	return ProviderTestResult{
		Provider:   provider,
		Endpoint:   endpoint,
		Status:     status,
		HTTPStatus: resp.StatusCode,
		Detail:     detail,
	}
}
