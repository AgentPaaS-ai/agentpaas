package llm

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

// TestChatCompletionsAdapters_ExplicitStreamFalse pins the founder-demo hang:
// OpenRouter (and other chat-completions adapters) must send stream:false.
// Omitting the field is not equivalent to false — some providers default to
// streaming, and the harness then ReadAlls a never-ending SSE body.
func TestChatCompletionsAdapters_ExplicitStreamFalse(t *testing.T) {
	adapters := []ProviderAdapter{
		&openRouterAdapter{},
		&openAIAdapter{},
		&nousAdapter{},
		&xAIAdapter{},
	}
	for _, adapter := range adapters {
		t.Run(adapter.Name(), func(t *testing.T) {
			req, err := adapter.BuildRequest(context.Background(), "test-model", "hello", "test-key")
			if err != nil {
				t.Fatalf("BuildRequest: %v", err)
			}
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("ReadAll body: %v", err)
			}
			defer func() { _ = req.Body.Close() }()

			var body map[string]any
			if err := json.Unmarshal(bodyBytes, &body); err != nil {
				t.Fatalf("Unmarshal body: %v", err)
			}
			v, ok := body["stream"]
			if !ok {
				t.Fatalf("stream field missing; omitting it is not default-false")
			}
			b, isBool := v.(bool)
			if !isBool || b {
				t.Fatalf("stream = %v (%T), want false", v, v)
			}
		})
	}
}
