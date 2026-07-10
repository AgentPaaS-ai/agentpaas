package harness

import (
	"strings"
	"testing"
)

func TestGuardrailsFromPayload_Empty(t *testing.T) {
	if guardrailsFromPayload(nil) != nil {
		t.Fatal("expected nil for nil payload")
	}
	if guardrailsFromPayload(map[string]any{}) != nil {
		t.Fatal("expected nil for empty payload")
	}
}

func TestApplyGuardrails_RegexBlock(t *testing.T) {
	cg := guardrailsFromPayload(map[string]any{
		"guardrails": []any{
			map[string]any{"type": "regex", "pattern": `(?i)password\s*=\s*\S+`, "action": "block"},
		},
	})
	if cg == nil {
		t.Fatal("expected compiled guardrails")
	}
	_, err := applyGuardrailsToText(cg, "leak password=supersecret now", "request", nil)
	if err == nil {
		t.Fatal("expected block error")
	}
	if !strings.Contains(err.Error(), "guardrail blocked") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestApplyGuardrails_RegexMask(t *testing.T) {
	cg := guardrailsFromPayload(map[string]any{
		"guardrails": []any{
			map[string]any{"type": "regex", "pattern": `\d{3}-\d{2}-\d{4}`, "action": "mask"},
		},
	})
	got, err := applyGuardrailsToText(cg, "ssn 123-45-6789 ok", "response", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(got, "123-45-6789") {
		t.Fatalf("expected mask, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED], got %q", got)
	}
}

func TestCombineSystemPrompt(t *testing.T) {
	got := combineSystemPrompt("Be concise.", "hello")
	if !strings.Contains(got, "System: Be concise.") {
		t.Fatalf("missing system section: %q", got)
	}
	if !strings.Contains(got, "User: hello") {
		t.Fatalf("missing user section: %q", got)
	}
	if combineSystemPrompt("", "hello") != "hello" {
		t.Fatal("empty system should passthrough")
	}
}

func TestHandleLLM_Fake_GuardrailBlock(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_LLM", "1")
	s := &harnessRPCServer{audit: &recordingAuditAppender{}}
	state := &rpcInvokeState{
		payload: map[string]any{
			"guardrails": []any{
				map[string]any{"type": "regex", "pattern": `(?i)secret`, "action": "block"},
			},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "my secret token"}}, state)
	if resp.OK {
		t.Fatalf("expected blocked, got OK result=%v", resp.Result)
	}
	if resp.Code != StatusGuardrailBlocked {
		t.Fatalf("code = %q, want %s", resp.Code, StatusGuardrailBlocked)
	}
}

func TestHandleLLM_Fake_InjectSystemPrompt(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_FAKE_LLM", "1")
	s := &harnessRPCServer{audit: &recordingAuditAppender{}}
	state := &rpcInvokeState{
		payload: map[string]any{
			"inject_system_prompt": "Always answer briefly.",
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello there"}}, state)
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	// tokens are counted on combined prompt words ("System:" "Always"...); ensure call succeeded.
	result := resp.Result.(map[string]any)
	if result["text"] != "agentpaas fake llm response" {
		t.Fatalf("text = %v", result["text"])
	}
	if result["tokens"].(int64) < 3 {
		t.Fatalf("expected tokens reflecting injected system prompt, got %v", result["tokens"])
	}
}
