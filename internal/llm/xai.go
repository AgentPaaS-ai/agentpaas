package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultXAIModel = "grok-beta"

type xAIAdapter struct{}

func (a *xAIAdapter) Name() string       { return "xiai" }
func (a *xAIAdapter) Endpoint() string   { return xaiEndpoint }
func (a *xAIAdapter) AuthHeader() string { return "Authorization" }

func (a *xAIAdapter) BuildRequest(ctx context.Context, model, prompt, credentialValue string) (*http.Request, error) {
	if model == "" {
		model = defaultXAIModel
	}
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal xai request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, xaiEndpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build xai request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+credentialValue)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (a *xAIAdapter) ParseResponse(statusCode int, body []byte) (*LLMResult, error) {
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("xai returned HTTP %d", statusCode)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int64 `json:"total_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse xai response: %w", err)
	}
	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
	}
	return &LLMResult{
		Text:   text,
		Tokens: resp.Usage.TotalTokens,
		Model:  resp.Model,
	}, nil
}
