package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultNousModel = "deepseek/deepseek-v4-flash"

type nousAdapter struct{}

// nousAdapter.Name returns the provider or component name.
func (a *nousAdapter) Name() string       { return "nous" }
// nousAdapter.Endpoint returns the API endpoint URL.
func (a *nousAdapter) Endpoint() string   { return nousEndpoint }
// nousAdapter.AuthHeader returns the HTTP authorization header name and value prefix.
func (a *nousAdapter) AuthHeader() string { return "Authorization" }

// nousAdapter.BuildRequest builds the provider-specific request payload and headers.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *nousAdapter) BuildRequest(ctx context.Context, model, prompt, credentialValue string, maxTokens ...int) (*http.Request, error) {
	if model == "" {
		model = defaultNousModel
	}
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if len(maxTokens) > 0 && maxTokens[0] > 0 {
		body["max_tokens"] = maxTokens[0]
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal nous request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nousEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build nous request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+credentialValue)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// nousAdapter.ParseResponse parses the provider-specific response into a normalized result.
//
// It returns an error if the operation fails or inputs are invalid.
func (a *nousAdapter) ParseResponse(statusCode int, body []byte) (*LLMResult, error) {
	if statusCode < 200 || statusCode >= 300 {
		return nil, formatHTTPError("nous", statusCode, body)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens      int64 `json:"total_tokens"`
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse nous response: %w", err)
	}
	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
	}
	return &LLMResult{
		Text:         text,
		Tokens:       resp.Usage.TotalTokens,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		Model:        resp.Model,
	}, nil
}
