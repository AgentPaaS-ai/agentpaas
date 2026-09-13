package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
)

// TestNewLLMChatClient_LoopbackBaseURL pins M16-compat: the openai-go client
// must accept an injectable loopback BaseURL (127.0.0.1) and a dummy API key
// with no format validation. Completions.New must hit /v1/chat/completions.
func TestNewLLMChatClient_LoopbackBaseURL(t *testing.T) {
	var sawPath, sawAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-loopback",
			"object":  "chat.completion",
			"created": 1,
			"model":   "loopback-model",
			"choices": []map[string]any{
				{
					"index":         0,
					"finish_reason": "stop",
					"message":       map[string]any{"role": "assistant", "content": "ok"},
				},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer func() { ts.Close() }()

	baseURL := ts.URL + "/v1/"
	client := newLLMChatClient(baseURL, "", "dummy-not-a-real-key")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: "loopback-model",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("hi"),
		},
	})
	if err != nil {
		t.Fatalf("loopback Completions.New: %v", err)
	}
	if sawPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions (M16 OpenAI-compatible shape)", sawPath)
	}
	if !strings.HasPrefix(sawAuth, "Bearer ") {
		t.Fatalf("Authorization = %q, want Bearer prefix", sawAuth)
	}
	if res == nil || len(res.Choices) == 0 || res.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected completion: %#v", res)
	}
}

func TestOpenAIChatCompletionsBaseURL_StripsChatCompletions(t *testing.T) {
	got, err := openaiChatCompletionsBaseURL("https://openrouter.ai/api/v1/chat/completions")
	if err != nil {
		t.Fatalf("openaiChatCompletionsBaseURL: %v", err)
	}
	if got != "https://openrouter.ai/api/v1/" {
		t.Fatalf("base URL = %q, want https://openrouter.ai/api/v1/", got)
	}
}

func TestOpenAIChatCompletionsBaseURL_Loopback(t *testing.T) {
	got, err := openaiChatCompletionsBaseURL("http://127.0.0.1:18765/v1/chat/completions")
	if err != nil {
		t.Fatalf("openaiChatCompletionsBaseURL: %v", err)
	}
	if got != "http://127.0.0.1:18765/v1/" {
		t.Fatalf("base URL = %q, want http://127.0.0.1:18765/v1/", got)
	}
}

func TestChatCompletionText_PrefersContentOverReasoning(t *testing.T) {
	msg := openai.ChatCompletionMessage{Content: "visible answer"}
	raw, err := json.Marshal(map[string]any{
		"role":      "assistant",
		"content":   "visible answer",
		"reasoning": "hidden chain",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := msg.UnmarshalJSON(raw); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if got := chatCompletionText(msg); got != "visible answer" {
		t.Fatalf("text = %q, want visible answer", got)
	}
}

func TestChatCompletionText_EmptyContentFallsBackToReasoning(t *testing.T) {
	var msg openai.ChatCompletionMessage
	raw, err := json.Marshal(map[string]any{
		"role":      "assistant",
		"content":   "",
		"reasoning": "the answer is 42",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := msg.UnmarshalJSON(raw); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if got := chatCompletionText(msg); got != "the answer is 42" {
		t.Fatalf("text = %q, want reasoning fallback", got)
	}
}
