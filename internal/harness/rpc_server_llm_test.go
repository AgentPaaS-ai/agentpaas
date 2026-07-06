package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

const (
	testSecret = "sk-test-secret-12345"
	testCredID = "openai-key"
)

func TestHandleLLM_FakeFallback_NoLLMConfig(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_LLM", "1")
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload:   map[string]any{},
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate: nil,
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello world"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "agentpaas fake llm response" {
		t.Fatalf("text = %q, want fake response", result["text"])
	}
	if result["tokens"].(int64) != 2 {
		t.Fatalf("tokens = %v, want 2", result["tokens"])
	}
}

func TestHandleLLM_RealCall_OpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request has the right auth header and content-type.
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
				{"message": map[string]any{"content": "Hello from OpenAI"}},
			},
			"usage": map[string]any{"total_tokens": 15},
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

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "Say hello"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s (code: %s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["text"] != "Hello from OpenAI" {
		t.Fatalf("text = %q, want 'Hello from OpenAI'", result["text"])
	}
	if result["tokens"].(int64) != 15 {
		t.Fatalf("tokens = %v, want 15", result["tokens"])
	}
	if result["model"] != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", result["model"])
	}
}

func TestHandleLLM_UnknownProvider(t *testing.T) {
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "unknown",
				"model":      "some-model",
				"credential": testCredID,
			},
		},
		credentials: map[string]rpcCredential{},
		budget:      NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}
	resp := s.handleLLM(req, state)

	if resp.OK {
		t.Fatalf("expected error, got OK: %#v", resp.Result)
	}
	if resp.Code != "llm_failed" {
		t.Fatalf("code = %q, want llm_failed", resp.Code)
	}
	if !strings.Contains(resp.Error, "unknown llm provider") {
		t.Fatalf("error = %q, want 'unknown llm provider'", resp.Error)
	}
}

func TestHandleLLM_CredentialNotFound(t *testing.T) {
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload: map[string]any{
			"llm": map[string]any{
				"provider":   "openai",
				"model":      "gpt-4o",
				"credential": "missing-key",
			},
		},
		credentials: map[string]rpcCredential{
			testCredID: {Header: "Authorization", Value: testSecret},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}
	resp := s.handleLLM(req, state)

	if resp.OK {
		t.Fatalf("expected error, got OK: %#v", resp.Result)
	}
	if resp.Code != "credential_denied" {
		t.Fatalf("code = %q, want credential_denied", resp.Code)
	}
	if !strings.Contains(resp.Error, "credential not declared") {
		t.Fatalf("error = %q, want 'credential not declared'", resp.Error)
	}
}

func TestHandleLLM_ModelOverride(t *testing.T) {
	var actualModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the actual model sent in the request body.
		var reqBody struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		actualModel = reqBody.Model

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Model override works"}},
			},
			"usage": map[string]any{"total_tokens": 5},
			"model": actualModel,
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{
		"prompt": "hello",
		"model":  "gpt-4o-mini",
	}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	// Verify the adapter received the override model.
	if actualModel != "gpt-4o-mini" {
		t.Fatalf("adapter received model = %q, want gpt-4o-mini", actualModel)
	}
	result, _ := resp.Result.(map[string]any)
	if result["model"] != "gpt-4o-mini" {
		t.Fatalf("result model = %q, want gpt-4o-mini", result["model"])
	}
}

func TestHandleLLM_HTTPFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}
	resp := s.handleLLM(req, state)

	if resp.OK {
		t.Fatalf("expected error, got OK: %#v", resp.Result)
	}
	if resp.Code != "llm_failed" {
		t.Fatalf("code = %q, want llm_failed", resp.Code)
	}
	// The error should mention HTTP 500.
	if !strings.Contains(resp.Error, "500") {
		t.Fatalf("error = %q, want mention of 500", resp.Error)
	}
}

func TestHandleLLM_AuditRecorded(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "audit test"}},
			},
			"usage": map[string]any{"total_tokens": 7},
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "audit"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}

	event := recorder.lastEvent(t)
	if event.EventType != "egress_allowed" {
		t.Fatalf("event type = %q, want egress_allowed", event.EventType)
	}
	if event.Actor != "harness" {
		t.Fatalf("actor = %q, want harness", event.Actor)
	}
	if event.Payload["destination"] != ts.URL {
		t.Fatalf("destination = %q, want %s", event.Payload["destination"], ts.URL)
	}
	if event.Payload["method"] != "POST" {
		t.Fatalf("method = %q, want POST", event.Payload["method"])
	}
	if event.Payload["decision"] != "allowed" {
		t.Fatalf("decision = %q, want allowed", event.Payload["decision"])
	}
	if event.Payload["credential_id"] != testCredID {
		t.Fatalf("credential_id = %q, want %s", event.Payload["credential_id"], testCredID)
	}
	statusStr, ok := event.Payload["status_code"].(string)
	if !ok || statusStr != "200" {
		t.Fatalf("status_code = %v (type=%T), want '200'", event.Payload["status_code"], event.Payload["status_code"])
	}
}

func TestHandleLLM_SecretNotInResult(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "clean response"}},
			},
			"usage": map[string]any{"total_tokens": 3},
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}
	resp := s.handleLLM(req, state)

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(encoded), testSecret) {
		t.Fatalf("response leaked secret: %s", encoded)
	}

	// Also check audit records don't contain the secret.
	for _, record := range recorder.events() {
		recordJSON, _ := json.Marshal(record)
		if strings.Contains(string(recordJSON), testSecret) {
			t.Fatalf("audit record leaked secret: %s", recordJSON)
		}
	}
}

func TestHandleLLM_BudgetExceededTerminates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "budget busting response"}},
			},
			"usage": map[string]any{"total_tokens": 100},
			"model": "gpt-4o",
		})
	}))
	defer ts.Close()

	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	terminateCalled := make(chan struct{})
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
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 5}),
		terminate: func() { close(terminateCalled) },
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}
	resp := s.handleLLM(req, state)

	if resp.Code != StatusBudgetExceeded {
		t.Fatalf("expected BUDGET_EXCEEDED, got code=%s error=%s", resp.Code, resp.Error)
	}
	// Wait for the async terminate goroutine to fire.
	<-terminateCalled
}

func TestHandleLLM_TokensZeroFallbackToWordCount(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Return 0 tokens to trigger word-count fallback.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "four word response here"}},
			},
			"usage": map[string]any{"total_tokens": 0},
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "test"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	// "four word response here" has 4 words → 4 tokens.
	if result["tokens"].(int64) != 4 {
		t.Fatalf("tokens = %v, want 4 (word count fallback)", result["tokens"])
	}
}

// Ensure errors.Is works with budget errors.
func TestHandleLLM_BudgetExceededWithoutTerminateReturnsLLMFailed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "response"}},
			},
			"usage": map[string]any{"total_tokens": 10},
			"model": "gpt-4o",
		})
	}))
	defer ts.Close()

	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	s := &harnessRPCServer{audit: nil}
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
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 3}),
		terminate: nil,
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "test"}}
	resp := s.handleLLM(req, state)

	// When terminate is nil, budget exceeded still returns llm_failed
	// (the terminate signal path is only for the full harness which always sets it).
	if resp.Code != "llm_failed" {
		t.Fatalf("expected llm_failed, got %s error=%s", resp.Code, resp.Error)
	}
}

func TestHandleLLM_EmptyPromptStillWorks(t *testing.T) {
	// Empty prompt with no LLM config should still work (word count = 0).
	t.Setenv("AGENTPAAS_TEST_FAKE_LLM", "1")
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload:   map[string]any{},
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate: nil,
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": ""}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["tokens"].(int64) != 0 {
		t.Fatalf("tokens = %v, want 0", result["tokens"])
	}
}

func TestHandleLLM_AnthropicAdapter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("x-api-key")
		if auth != testSecret {
			t.Errorf("x-api-key = %q, want %s", auth, testSecret)
		}
		anthropicVersion := r.Header.Get("anthropic-version")
		if anthropicVersion != "2023-06-01" {
			t.Errorf("anthropic-version = %q, want 2023-06-01", anthropicVersion)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Claude response"},
			},
			"usage": map[string]any{"output_tokens": 12},
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "Hello Claude"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK, got error: %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["text"] != "Claude response" {
		t.Fatalf("text = %q, want 'Claude response'", result["text"])
	}
	if result["tokens"].(int64) != 12 {
		t.Fatalf("tokens = %v, want 12", result["tokens"])
	}
}

func TestHandleLLM_NilAuditDoesNotPanic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "ok"}},
			},
			"usage": map[string]any{"total_tokens": 1},
			"model": "gpt-4o",
		})
	}))
	defer ts.Close()

	restore := llm.SetTestEndpoints(ts.URL, "", "")
	defer restore()

	s := &harnessRPCServer{audit: nil}
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
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "test"}}
	resp := s.handleLLM(req, state)

	if !resp.OK {
		t.Fatalf("expected OK with nil audit, got error: %s", resp.Error)
	}
}

// TestHandleLLM_NoConfigProductionFailsClosed verifies that in production mode
// (without AGENTPAAS_TEST_FAKE_LLM=1), calling agent.llm() without a configured
// llm provider returns a structured error — not fake text.
func TestHandleLLM_NoConfigProductionFailsClosed(t *testing.T) {
	// No AGENTPAAS_TEST_FAKE_LLM set → production mode.
	recorder := &recordingAuditAppender{}
	s := &harnessRPCServer{audit: recorder}
	state := &rpcInvokeState{
		payload:   map[string]any{},
		budget:    NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
		terminate: nil,
	}

	req := rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello world"}}
	resp := s.handleLLM(req, state)

	if resp.OK {
		t.Fatalf("expected error in production without LLM config, got OK: %#v", resp.Result)
	}
	if resp.Code != "llm_failed" {
		t.Fatalf("code = %q, want llm_failed", resp.Code)
	}
	if !strings.Contains(resp.Error, "llm not configured") {
		t.Fatalf("error = %q, want 'llm not configured'", resp.Error)
	}
	if !strings.Contains(resp.Error, "AGENTPAAS_TEST_FAKE_LLM=1") {
		t.Fatalf("error = %q, want mention of AGENTPAAS_TEST_FAKE_LLM=1", resp.Error)
	}
	// Must NOT contain fake text.
	if strings.Contains(resp.Error, "agentpaas fake") {
		t.Fatalf("error = %q, must not contain fake llm response text", resp.Error)
	}
}
