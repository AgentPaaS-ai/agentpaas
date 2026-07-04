package llm

import (
	"context"
	"net/http"
)

// ProviderAdapter maps an LLM provider to its API endpoint, auth header,
// request body format, and response parser. Used by the harness to route
// agent.llm() calls as credentialed HTTP egress (Option B unified egress).
type ProviderAdapter interface {
	// BuildRequest creates an HTTP request for the LLM provider.
	// credentialValue is the API key (NEVER logged or included in errors).
	BuildRequest(ctx context.Context, model, prompt string, credentialValue string) (*http.Request, error)
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
	Text   string `json:"text"`
	Tokens int64  `json:"tokens"`
	Model  string `json:"model,omitempty"`
}

// GetAdapter returns the adapter for the given provider name.
// Supported: "openai", "anthropic", "xai"/"xiai". Returns nil for unknown providers.
func GetAdapter(provider string) ProviderAdapter {
	switch provider {
	case "openai":
		return &openAIAdapter{}
	case "anthropic":
		return &anthropicAdapter{}
	case "xai", "xiai":
		return &xAIAdapter{}
	default:
		return nil
	}
}

// SupportedProviders returns the list of supported provider names.
func SupportedProviders() []string {
	return []string{"openai", "anthropic", "xai"}
}
