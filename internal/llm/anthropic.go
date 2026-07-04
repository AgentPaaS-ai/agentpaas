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

func (a *anthropicAdapter) Name() string       { return "anthropic" }
func (a *anthropicAdapter) Endpoint() string   { return anthropicEndpoint }
func (a *anthropicAdapter) AuthHeader() string { return "x-api-key" }

func (a *anthropicAdapter) BuildRequest(ctx context.Context, model, prompt, credentialValue string) (*http.Request, error) {
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
		Text:   text,
		Tokens: resp.Usage.OutputTokens,
		Model:  resp.Model,
	}, nil
}
