package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// openaiChatCompletionsBaseURL converts a chat-completions endpoint into the
// openai-go BaseURL. Completions.New joins "chat/completions" onto this
// directory, so a full .../v1/chat/completions URL must be stripped to .../v1/.
func openaiChatCompletionsBaseURL(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse chat completions endpoint: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("chat completions endpoint must be absolute")
	}
	path := strings.TrimSuffix(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if path == "" {
		path = "/"
	} else if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// preserveHostRoundTripper sets req.Host after openai-go's origin check so the
// gateway can match routes by the original provider host (BUG-033/034). Host
// must not be set on the Request before Client.Do — the SDK rejects a Host
// that differs from the BaseURL origin.
type preserveHostRoundTripper struct {
	host string
	next http.RoundTripper
}

func (t preserveHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	if t.host == "" {
		return next.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	cloned.Host = t.host
	return next.RoundTrip(cloned)
}

// newLLMChatClient constructs an openai-go client aimed at an injectable
// BaseURL (gateway rewrite or M16 127.0.0.1 loopback). Dummy API keys are
// accepted — the gateway injects the real credential. Redirects are not
// followed (BUG-033/034). Retries are disabled so context.WithTimeout is the
// sole deadline owner.
func newLLMChatClient(baseURL, originalHost, apiKey string) openai.Client {
	if apiKey == "" {
		apiKey = "dummy"
	}
	httpClient := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: preserveHostRoundTripper{host: originalHost},
	}
	return openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	)
}

func chatCompletionText(msg openai.ChatCompletionMessage) string {
	if msg.Content != "" {
		return msg.Content
	}
	return messageReasoning(msg)
}

func messageReasoning(msg openai.ChatCompletionMessage) string {
	raw := msg.RawJSON()
	if raw == "" {
		return ""
	}
	var parsed struct {
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return parsed.Reasoning
}

func callLLMChatCompletion(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int) (*llm.LLMResult, error) {
	client := newLLMChatClient(baseURL, originalHost, apiKey)
	params := openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	}
	if maxTokens > 0 {
		params.MaxTokens = openai.Int(int64(maxTokens))
	}
	completion, err := client.Chat.Completions.New(ctx, params, option.WithJSONSet("stream", false))
	if err != nil {
		return nil, err
	}
	if completion == nil {
		return nil, errors.New("llm chat completion: empty response")
	}
	text := ""
	if len(completion.Choices) > 0 {
		text = chatCompletionText(completion.Choices[0].Message)
	}
	return &llm.LLMResult{
		Text:         text,
		Tokens:       completion.Usage.TotalTokens,
		Model:        completion.Model,
		InputTokens:  completion.Usage.PromptTokens,
		OutputTokens: completion.Usage.CompletionTokens,
	}, nil
}

func llmHTTPStatusFromError(err error) string {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode != 0 {
		return strconv.Itoa(apiErr.StatusCode)
	}
	return ""
}
