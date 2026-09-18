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
	client := newLLMChatClient(baseURL, "", "dummy-not-a-real-key", 2*time.Second)
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

func TestCallLLMChatCompletion_OpenRouterExcludesReasoning(t *testing.T) {
	cases := []struct {
		name         string
		originalHost string
		provider     string
	}{
		{name: "originalHost openrouter.ai", originalHost: "openrouter.ai"},
		{name: "provider openrouter", provider: "openrouter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":      "chatcmpl-or",
					"object":  "chat.completion",
					"created": 1,
					"model":   "deepseek/deepseek-v4-flash",
					"choices": []map[string]any{
						{
							"index":         0,
							"finish_reason": "stop",
							"message":       map[string]any{"role": "assistant", "content": "Folsom: 72F and sunny"},
						},
					},
					"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
				})
			}))
			defer func() { ts.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			res, err := callLLMChatCompletion(ctx, ts.URL+"/v1/", tc.originalHost, "dummy", "deepseek/deepseek-v4-flash", "What's the weather in Folsom?", 0, tc.provider)
			if err != nil {
				t.Fatalf("callLLMChatCompletion: %v", err)
			}
			if res == nil || res.Text != "Folsom: 72F and sunny" {
				t.Fatalf("text = %#v, want weather reply (deepseek path still works)", res)
			}
			if v, ok := gotBody["stream"]; !ok || v != false {
				t.Fatalf("stream = %v, want false", gotBody["stream"])
			}
			reasoning, _ := gotBody["reasoning"].(map[string]any)
			if reasoning["exclude"] != true {
				t.Fatalf("reasoning.exclude = %v, want true; body=%v", gotBody["reasoning"], gotBody)
			}
		})
	}
}

func TestRewriteOpenAIErrorJSON_StringFieldBecomesObject(t *testing.T) {
	got := rewriteOpenAIErrorJSON([]byte(`{"error":"Unauthorized"}`))
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatal(err)
	}
	errObj, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatalf("error not object: %s", got)
	}
	if errObj["message"] != "Unauthorized" {
		t.Fatalf("message = %v", errObj["message"])
	}
}

func TestRewriteOpenAIErrorJSON_ObjectUnchanged(t *testing.T) {
	in := []byte(`{"error":{"message":"nope","type":"invalid_request_error"}}`)
	got := rewriteOpenAIErrorJSON(in)
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatal(err)
	}
	errObj, ok := obj["error"].(map[string]any)
	if !ok {
		t.Fatalf("error not object: %s", got)
	}
	if errObj["message"] != "nope" {
		t.Fatalf("message = %v", errObj["message"])
	}
}

func TestCallLLMChatCompletion_StringErrorBodyIsNotUnmarshalCrash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"gateway secret missing"}`))
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := callLLMChatCompletion(ctx, ts.URL+"/v1/", "openrouter.ai", "dummy", "m", "hi", 0, "openrouter")
	if err == nil {
		t.Fatal("expected llm error")
	}
	if strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("0.5 openai-go parse crash leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "gateway secret missing") {
		t.Fatalf("lost provider message: %v", err)
	}
}

func TestCallLLMChatCompletion_ObjectErrorBodyStillSurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "quota", "type": "insufficient_quota"},
		})
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := callLLMChatCompletion(ctx, ts.URL+"/v1/", "", "dummy", "m", "hi", 0, "openai")
	if err == nil {
		t.Fatal("expected llm error")
	}
	if !strings.Contains(err.Error(), "quota") {
		t.Fatalf("lost object message: %v", err)
	}
}
