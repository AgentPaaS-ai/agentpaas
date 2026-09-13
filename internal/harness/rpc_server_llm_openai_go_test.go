package harness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

func llmStateForOpenAI(t *testing.T, env *routedrun.TimeEnvelope) *rpcInvokeState {
	t.Helper()
	return &rpcInvokeState{
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
		budget:       NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		timeEnvelope: env,
	}
}

func envelopeMS(t *testing.T, modelCallTimeoutMs int64) *routedrun.TimeEnvelope {
	t.Helper()
	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 10_000, modelCallTimeoutMs)
	if !ok {
		t.Fatal("expected envelope")
	}
	return &env
}

func writeChatCompletion(w http.ResponseWriter, content, reasoning, model string, totalTokens int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoning != "" {
		msg["reasoning"] = reasoning
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": 1,
		"model":   model,
		"choices": []map[string]any{
			{"index": 0, "finish_reason": "stop", "message": msg},
		},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": totalTokens},
	})
}

// TestHandleLLM_NonStreamContentPreferredOverReasoning pins Decision A:
// stream:false JSON with both message.content and message.reasoning returns
// content to the agent. Do not block waiting for reasoning.
func TestHandleLLM_NonStreamContentPreferredOverReasoning(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatCompletion(w, "visible answer", "hidden chain of thought", "gpt-4o", 4)
	}))
	defer func() { ts.Close() }()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	s := &harnessRPCServer{}
	state := llmStateForOpenAI(t, nil)
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hi"}}, state)
	if !resp.OK {
		t.Fatalf("expected OK, got error=%s code=%s", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "visible answer" {
		t.Fatalf("text = %q, want visible answer (content, not reasoning)", result["text"])
	}
}

// TestHandleLLM_EmptyContentFallsBackToReasoning pins OpenRouter
// message.reasoning as a fallback only when content is empty.
func TestHandleLLM_EmptyContentFallsBackToReasoning(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatCompletion(w, "", "the answer is 42", "gpt-4o", 4)
	}))
	defer func() { ts.Close() }()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	s := &harnessRPCServer{}
	state := llmStateForOpenAI(t, nil)
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hi"}}, state)
	if !resp.OK {
		t.Fatalf("expected OK, got error=%s code=%s", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "the answer is 42" {
		t.Fatalf("text = %q, want reasoning fallback", result["text"])
	}
}

// TestHandleLLM_KeepaliveNeverCloses_TerminalsAtCtxDeadline pins the
// Decision A timeout contract: a stub that never completes the JSON body
// (keepalive-only / held-open) must terminal at the ctx deadline with
// llm_failed. openai-go + context.WithTimeout owns the deadline — not a
// custom idle timer.
func TestHandleLLM_KeepaliveNeverCloses_TerminalsAtCtxDeadline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = io.WriteString(w, ": keepalive\n")
			flusher.Flush()
			_, _ = io.WriteString(w, "{")
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer func() { ts.Close() }()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{nowMonotonicMs: func() int64 { return nowMs }}
	state := llmStateForOpenAI(t, envelopeMS(t, 200))
	start := time.Now()
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hi"}}, state)
	elapsed := time.Since(start)
	if resp.OK {
		t.Fatal("expected error (ctx deadline), got OK")
	}
	if resp.Code != "llm_failed" {
		t.Fatalf("code = %q, want llm_failed", resp.Code)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed = %v, want < 1.5s (ctx deadline 200ms)", elapsed)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("elapsed = %v, want roughly the 200ms ctx deadline (not an immediate parse error)", elapsed)
	}
	errLower := strings.ToLower(resp.Error)
	if !strings.Contains(errLower, "timeout") &&
		!strings.Contains(errLower, "deadline") &&
		!strings.Contains(errLower, "context") {
		t.Fatalf("error = %q, want timeout/deadline/context", resp.Error)
	}
}

// TestHandleLLM_OpenRouterRequestExcludesReasoning pins OpenRouter docs:
// reasoning.exclude=true means the model still reasons internally but omits
// reasoning tokens from the response. stream:false stays set. DeepSeek
// (weather-agent default) treats exclude as a no-op and still returns content.
func TestHandleLLM_OpenRouterRequestExcludesReasoning(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeChatCompletion(w, "Folsom: 72F and sunny", "", "deepseek/deepseek-v4-flash", 8)
	}))
	defer func() { ts.Close() }()

	t.Setenv("AGENTPAAS_GATEWAY_URL", ts.URL)

	s := &harnessRPCServer{}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "openrouter",
				"model":      "deepseek/deepseek-v4-flash",
				"credential": testCredID,
			},
		},
		credentials: map[string]rpcCredential{
			testCredID: {Header: "Authorization", Value: testSecret},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{
		"prompt": "What's the weather in Folsom?",
	}}, state)
	if !resp.OK {
		t.Fatalf("expected OK, got error=%s code=%s", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "Folsom: 72F and sunny" {
		t.Fatalf("text = %q, want weather reply (deepseek path still works)", result["text"])
	}
	if v, ok := gotBody["stream"]; !ok || v != false {
		t.Fatalf("stream = %v, want false", gotBody["stream"])
	}
	reasoning, _ := gotBody["reasoning"].(map[string]any)
	if reasoning["exclude"] != true {
		t.Fatalf("reasoning.exclude = %v, want true; body=%v", gotBody["reasoning"], gotBody)
	}
}
