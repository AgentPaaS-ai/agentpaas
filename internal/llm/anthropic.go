package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultAnthropicModel = "claude-3-5-sonnet-20241022"

type anthropicAdapter struct{}

// anthropicAdapter.Name returns the provider or component name.
func (a *anthropicAdapter) Name() string       { return "anthropic" }
// anthropicAdapter.Endpoint returns the API endpoint URL.
func (a *anthropicAdapter) Endpoint() string   { return anthropicEndpoint }
// anthropicAdapter.AuthHeader returns the HTTP authorization header name and value prefix.
func (a *anthropicAdapter) AuthHeader() string { return "x-api-key" }

// anthropicAdapter.BuildRequest builds the provider-specific request payload and headers.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *anthropicAdapter) BuildRequest(ctx context.Context, model, prompt, credentialValue string, maxTokens ...int) (*http.Request, error) {
	if model == "" {
		model = defaultAnthropicModel
	}
	body := map[string]interface{}{
		"model":      model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if len(maxTokens) > 0 && maxTokens[0] > 0 {
		body["max_tokens"] = maxTokens[0]
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	req.Header.Set("x-api-key", credentialValue)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// anthropicAdapter.ParseResponse parses the provider-specific response into a normalized result.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *anthropicAdapter) ParseResponse(statusCode int, body []byte) (*LLMResult, error) {
	if statusCode < 200 || statusCode >= 300 {
		return nil, formatHTTPError("anthropic", statusCode, body)
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	text := ""
	if len(resp.Content) > 0 {
		text = resp.Content[0].Text
	}
	return &LLMResult{
		Text:         text,
		Tokens:       resp.Usage.OutputTokens,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		Model:        resp.Model,
	}, nil
}
