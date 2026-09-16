package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

const (
	t16CanarySSN   = "078-05-1120"
	t16CanaryEmail = "canary-pii-m155-t16@example.com"
	t16CanaryCC    = "4111111111111111"
)

type t16ThrowAudit struct{}

func (t16ThrowAudit) Append(audit.AuditRecord) error {
	return errors.New("audit_down")
}

func t16PIIState(pii map[string]any) *rpcInvokeState {
	payload := map[string]any{
		"llm": map[string]any{
			"provider":   "openai",
			"model":      "gpt-4o",
			"credential": testCredID,
		},
	}
	if pii != nil {
		payload["pii"] = pii
	}
	return &rpcInvokeState{
		payload: payload,
		credentials: map[string]rpcCredential{
			testCredID: {Header: "Authorization", Value: testSecret},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
}

func t16RejectPII() map[string]any {
	return map[string]any{
		"action":        "reject",
		"builtins":      []any{"Ssn"},
		"patterns":      []any{},
		"reject_status": 422,
		"reject_body":   "pii_rejected",
	}
}

func t16MaskPII() map[string]any {
	return map[string]any{
		"action":   "mask",
		"builtins": []any{"Email", "Ssn", "CreditCard"},
		"patterns": []any{},
	}
}

func t16AuditBlob(rec *recordingAuditAppender) string {
	b, err := json.Marshal(rec.events())
	if err != nil {
		return ""
	}
	return string(b)
}

func TestM15_5_T16_SC6_RejectOnRequestNoUpstream(t *testing.T) {
	rec := &recordingAuditAppender{}
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "SHOULD_NOT"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, t16PIIState(t16RejectPII()))
	if upstream != 0 {
		t.Fatalf("upstream=%d want 0", upstream)
	}
	if resp.OK {
		t.Fatalf("expected reject, got OK result=%v", resp.Result)
	}
	if resp.Error != "pii_rejected" {
		t.Fatalf("error=%q want pii_rejected", resp.Error)
	}
	if strings.Contains(resp.Error, t16CanarySSN) || strings.Contains(t16AuditBlob(rec), t16CanarySSN) {
		t.Fatalf("SC6 canary leaked: err=%q audit=%s", resp.Error, t16AuditBlob(rec))
	}
}

func TestM15_5_T16_SC6_MaskBeforeAudit(t *testing.T) {
	rec := &recordingAuditAppender{}
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "contact " + t16CanaryEmail + " card " + t16CanaryCC + " ssn " + t16CanarySSN}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(t16MaskPII()))
	if upstream != 1 {
		t.Fatalf("upstream=%d want 1", upstream)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got %s code=%s", resp.Error, resp.Code)
	}
	result, _ := resp.Result.(map[string]any)
	text, _ := result["text"].(string)
	blob := text + t16AuditBlob(rec)
	for _, c := range []string{t16CanaryEmail, t16CanaryCC, t16CanarySSN} {
		if strings.Contains(blob, c) {
			t.Fatalf("SC6 canary %q in text/audit: text=%q audit=%s", c, text, t16AuditBlob(rec))
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("expected [REDACTED], got %q", text)
	}
}

func TestM15_5_T16_SC8_PIIOffRaw(t *testing.T) {
	rec := &recordingAuditAppender{}
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "keep " + t16CanaryEmail}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, t16PIIState(nil))
	if upstream != 1 {
		t.Fatalf("upstream=%d want 1", upstream)
	}
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	if result["text"] != "keep "+t16CanaryEmail {
		t.Fatalf("text=%v", result["text"])
	}
}

func TestM15_5_T16_D202_SplitEmailMasked(t *testing.T) {
	rec := &recordingAuditAppender{}
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			return &llm.LLMResult{Text: "prefix canary-pii-m155-" + "t16@example.com suffix"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "ok"}}, t16PIIState(t16MaskPII()))
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	text, _ := result["text"].(string)
	if strings.Contains(text, t16CanaryEmail) || strings.Contains(t16AuditBlob(rec), t16CanaryEmail) {
		t.Fatalf("D202 canary survived: %q audit=%s", text, t16AuditBlob(rec))
	}
}

func TestM15_5_T16_D208_AuditDownFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		audit: t16ThrowAudit{},
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "SHOULD_NOT"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, t16PIIState(t16RejectPII()))
	if upstream != 0 {
		t.Fatalf("upstream=%d want 0", upstream)
	}
	if resp.OK {
		t.Fatal("expected fail-closed")
	}
	if strings.Contains(resp.Error, t16CanarySSN) {
		t.Fatalf("canary in error: %q", resp.Error)
	}
}

func TestM15_5_T16_D203_NoSecretPackDefaults(t *testing.T) {
	rec := &recordingAuditAppender{}
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			return &llm.LLMResult{Text: "AKIAIOSFODNN7EXAMPLE ghp_deadbeef PEM-----BEGIN PRIVATE KEY----- " + t16CanaryEmail}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "ok"}}, t16PIIState(t16MaskPII()))
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	text, _ := result["text"].(string)
	if strings.Contains(text, t16CanaryEmail) {
		t.Fatalf("email not masked: %q", text)
	}
	if !strings.Contains(text, "AKIAIOSFODNN7EXAMPLE") || !strings.Contains(text, "ghp_deadbeef") {
		t.Fatalf("D203 secret-pack must not be builtins: %q", text)
	}
}

func TestM15_5_T16_EmptyObjectFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "nope"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(map[string]any{}))
	if upstream != 0 {
		t.Fatalf("upstream=%d want 0", upstream)
	}
	if resp.OK {
		t.Fatal("pii:{} must fail-closed")
	}
}

func TestM15_5_T16_UnknownActionMASKFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "nope"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "1", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(map[string]any{"action": "MASK", "builtins": []any{"Email"}}))
	if upstream != 0 {
		t.Fatalf("upstream=%d want 0", upstream)
	}
	if resp.OK {
		t.Fatal("action MASK must fail-closed")
	}
}
