package llm

import (
	"context"
	"net/http"
	"strings"
)

// ProviderAdapter maps an LLM provider to its API endpoint, auth header,
// request body format, and response parser. Used by the harness to route
// agent.llm() calls as credentialed HTTP egress (Option B unified egress).
type ProviderAdapter interface {
	// BuildRequest creates an HTTP request for the LLM provider.
	// credentialValue is the API key (NEVER logged or included in errors).
	BuildRequest(ctx context.Context, model, prompt string, credentialValue string, maxTokens ...int) (*http.Request, error)
	// ParseResponse extracts text and token count from the provider response body.
	ParseResponse(statusCode int, body []byte) (*LLMResult, error)
	// Endpoint returns the provider's API endpoint URL (for logging/audit).
	Endpoint() string
	// AuthHeader returns the header name used for the API key (e.g. "Authorization", "x-api-key").
	AuthHeader() string
	// Name returns the provider name (e.g. "openai", "anthropic", "xiai").
	Name() string
}

// LLMResult holds the parsed response from an LLM provider call.
type LLMResult struct {
	Text         string `json:"text"`
	Tokens       int64  `json:"tokens"`
	Model        string `json:"model,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
}

// EstimateCost returns a conservative per-call USD estimate. Unknown models
// use the documented fallback price so observability remains useful without
// exposing provider credentials or requiring a remote price lookup.
func EstimateCost(provider, model string, inputTokens, outputTokens int64) float64 {
	inputRate, outputRate := 0.000003, 0.000003
	if strings.EqualFold(provider, "openrouter") && strings.EqualFold(model, "deepseek/deepseek-v4-flash") {
		inputRate, outputRate = 0.00000011, 0.00000028
	}
	return float64(inputTokens)*inputRate + float64(outputTokens)*outputRate
}

// GetAdapter returns the adapter for the given provider name.
// Supported: "openrouter", "openai", "anthropic", "xai"/"xiai", "nous". Returns nil for unknown providers.
func GetAdapter(provider string) ProviderAdapter {
	switch provider {
	case "openrouter":
		return &openRouterAdapter{}
	case "openai":
		return &openAIAdapter{}
	case "anthropic":
		return &anthropicAdapter{}
	case "xai", "xiai":
		return &xAIAdapter{}
	case "nous":
		return &nousAdapter{}
	default:
		return nil
	}
}

// SupportedProviders returns the list of supported provider names.
func SupportedProviders() []string {
	return []string{"openrouter", "openai", "anthropic", "xai", "nous"}
}

// ProviderDomain returns the egress domain (hostname) for a given provider.
// Returns empty string for unknown providers.
func ProviderDomain(provider string) string {
	switch strings.ToLower(provider) {
	case "openrouter":
		return "openrouter.ai"
	case "openai":
		return "api.openai.com"
	case "anthropic":
		return "api.anthropic.com"
	case "xai", "xiai":
		return "api.x.ai"
	case "nous":
		return "inference-api.nousresearch.com"
	default:
		return ""
	}
}

// ProviderDomains returns a map of all provider → domain mappings.
func ProviderDomains() map[string]string {
	return map[string]string{
		"openrouter": "openrouter.ai",
		"openai":     "api.openai.com",
		"anthropic":  "api.anthropic.com",
		"xai":        "api.x.ai",
		"nous":       "inference-api.nousresearch.com",
	}
}
