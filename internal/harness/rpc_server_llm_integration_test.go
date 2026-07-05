package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

func TestHandleLLM_Integration_FullFlow_OpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+testSecret {
			t.Errorf("Authorization = %q, want Bearer %s", auth, testSecret)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Integration: Hello from OpenAI"}},
			},
			"usage": map[string]any{"total_tokens": 42},
			"model": "gpt-4o",
		})
	}))
	defer ts.Close()

	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "openai",
				"model":      "gpt-4o",
				"credential": testCredID,
			},
		},
		credentials: map[string]rpcCredential{
			testCredID: {Header: "Authorization", Value: testSecret},
		},
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate: nil,
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "Integration test prompt"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s (code: %s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "Integration: Hello from OpenAI" {
		t.Fatalf("text = %q, want 'Integration: Hello from OpenAI'", result["text"])
	}
	if result["tokens"].(int64) != 42 {
		t.Fatalf("tokens = %v, want 42", result["tokens"])
	}
	if result["model"] != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", result["model"])
	}
}

func TestHandleLLM_Integration_FullFlow_Anthropic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("x-api-key")
		if auth != testSecret {
			t.Errorf("x-api-key = %q, want %s", auth, testSecret)
		}
		anthropicVersion := r.Header.Get("anthropic-version")
		if anthropicVersion != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want 2023-06-01", anthropicVersion)
		}
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Integration: Bonjour from Claude"},
			},
			"usage": map[string]any{"output_tokens": 77},
			"model": "claude-3-5-sonnet-20241022",
		})
	}))
	defer ts.Close()

	restore := llm.SetTestEndpoints("", ts.URL, "")
	defer restore()

	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "anthropic",
				"model":      "claude-3-5-sonnet-20241022",
				"credential": testCredID,
			},
		},
		credentials: map[string]rpcCredential{
			testCredID: {Header: "x-api-key", Value: testSecret},
		},
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate: nil,
	}

	req := rpcRequest{ID: "2", Method: "llm", Params: map[string]any{"prompt": "Integration test prompt for Claude"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s (code: %s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "Integration: Bonjour from Claude" {
		t.Fatalf("text = %q, want 'Integration: Bonjour from Claude'", result["text"])
	}
	if result["tokens"].(int64) != 77 {
		t.Fatalf("tokens = %v, want 77", result["tokens"])
	}
	if result["model"] != "claude-3-5-sonnet-20241022" {
		t.Fatalf("model = %q, want claude-3-5-sonnet-20241022", result["model"])
	}
}
