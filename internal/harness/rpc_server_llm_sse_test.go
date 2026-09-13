package harness

import (
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
		budget:       NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		timeEnvelope: env,
	}
	return state
}

func envelopeMS(t *testing.T, modelCallTimeoutMs int64) *routedrun.TimeEnvelope {
	t.Helper()
	env, ok := routedrun.TimeEnvelopeFromCeilings(60_000, 60_000, 10_000, modelCallTimeoutMs)
	if !ok {
		t.Fatal("expected envelope")
	}
	return &env
}

// TestHandleLLM_SSEBodyCompletesWithoutWaitingForClose reproduces the
// founder-demo hang: upstream already finished (finish_reason=stop) and
// streamed SSE, but the connection stays open. handleLLM must parse until
// [DONE] and return, not ReadAll a never-ending body.
func TestHandleLLM_SSEBodyCompletesWithoutWaitingForClose(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected flusher")
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}],\"usage\":{\"total_tokens\":4,\"prompt_tokens\":1,\"completion_tokens\":3},\"model\":\"gpt-4o\"}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
		// Hold open after [DONE], matching providers that finish generation
		// but leave the HTTP stream open.
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	defer ts.Close()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{
		nowMonotonicMs: func() int64 { return nowMs },
	}
	state := llmStateForOpenAI(t, envelopeMS(t, 2000))
	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "Say hello"}}
	start := time.Now()
	resp := s.handleLLM(req, state)
	elapsed := time.Since(start)
	if !resp.OK {
		t.Fatalf("expected OK after SSE [DONE], got error=%s code=%s", resp.Error, resp.Code)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed = %v, want < 1.5s (must not wait for the held-open body)", elapsed)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "Hello world" {
		t.Fatalf("text = %q, want %q", result["text"], "Hello world")
	}
}

// TestHandleLLM_SSEBodyDetectedFromDataPrefix covers providers that omit
// text/event-stream but still write SSE (body starts with data:).
func TestHandleLLM_SSEBodyDetectedFromDataPrefix(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	defer ts.Close()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{nowMonotonicMs: func() int64 { return nowMs }}
	state := llmStateForOpenAI(t, envelopeMS(t, 2000))
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hi"}}, state)
	if !resp.OK {
		t.Fatalf("expected OK, got error=%s code=%s", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "ok" {
		t.Fatalf("text = %q, want %q", result["text"], "ok")
	}
}

// TestHandleLLM_HTTPTimeoutFailsClosedLLMFailed pins fail-closed: a stuck
// upstream must surface llm_failed so the run terminals, not stay running.
func TestHandleLLM_HTTPTimeoutFailsClosedLLMFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer ts.Close()
	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{nowMonotonicMs: func() int64 { return nowMs }}
	state := llmStateForOpenAI(t, envelopeMS(t, 200))
	start := time.Now()
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hi"}}, state)
	elapsed := time.Since(start)
	if resp.OK {
		t.Fatal("expected error (client timeout), got OK")
	}
	if resp.Code != "llm_failed" {
		t.Fatalf("code = %q, want llm_failed", resp.Code)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed = %v, want < 1.5s", elapsed)
	}
	if !strings.Contains(strings.ToLower(resp.Error), "timeout") &&
		!strings.Contains(strings.ToLower(resp.Error), "deadline") &&
		!strings.Contains(strings.ToLower(resp.Error), "context") {
		t.Fatalf("error = %q, want timeout/deadline", resp.Error)
	}
}
