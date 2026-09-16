package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

// Adversary SC6 tests for M15.5-T16 harness PII enforce (SHA 7535bad).
// Break-test only. Do not clone cloud T2/T3 ADV or dashed-SSN reject harness.

func advCaptureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return buf
}

func advJSONRoundTripPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestAdvSC6_InjectSystemPromptBypassesRequestPII(t *testing.T) {
	// Grok: inject_system_prompt is applied after enforceRequestPII (rpc_server.go:774-783).
	rec := &recordingAuditAppender{}
	var sawPrompt string
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			sawPrompt = prompt
			return &llm.LLMResult{Text: "ok"}, nil
		},
	}
	state := t16PIIState(t16RejectPII())
	state.payload["inject_system_prompt"] = "context ssn " + t16CanarySSN
	resp := s.handleLLM(rpcRequest{ID: "adv-sys", Method: "llm", Params: map[string]any{"prompt": "hello"}}, state)
	if upstream != 0 {
		t.Fatalf("// ADVERSARY BREAK: system prompt SSN reached llmChatCompletion upstream=%d prompt=%q", upstream, sawPrompt)
	}
	if resp.OK {
		t.Fatalf("// ADVERSARY BREAK: expected request reject, got OK result=%v", resp.Result)
	}
	if strings.Contains(resp.Error, t16CanarySSN) || strings.Contains(t16AuditBlob(rec), t16CanarySSN) {
		t.Fatalf("canary leaked err=%q audit=%s", resp.Error, t16AuditBlob(rec))
	}
}

func TestAdvSC6_LLMErrorEchoesCanaryIntoAuditErrorLogs(t *testing.T) {
	// Luna: llmChatCompletion error path audits/returns/logs err.Error() with no PII pass (rpc_server.go:841-845).
	logs := advCaptureLogs(t)
	rec := &recordingAuditAppender{}
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return nil, fmt.Errorf("provider 400 echoed %s", t16CanaryEmail)
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-err", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(t16MaskPII()))
	if resp.OK {
		t.Fatal("llm error path must not return OK")
	}
	blob := resp.Error + t16AuditBlob(rec) + logs.String()
	if strings.Contains(blob, t16CanaryEmail) {
		t.Fatalf("// ADVERSARY BREAK: canary in resp.Error/AuditRecord/logs upstream=%d err=%q audit=%s logs=%q", upstream, resp.Error, t16AuditBlob(rec), logs.String())
	}
}

func TestAdvSC6_MaskPathAuditAppendDownMustFailClosed(t *testing.T) {
	// Kimi: mask success never calls auditPIIDecision; auditEgressDecision swallows Append errors (rpc_server.go:921-925, 1458-1484).
	upstream := 0
	s := &harnessRPCServer{
		audit: t16ThrowAudit{},
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "contact " + t16CanaryEmail}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-d208-mask", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(t16MaskPII()))
	if resp.OK {
		t.Fatalf("// ADVERSARY BREAK: D208 mask path audit-down returned OK text=%v upstream=%d", resp.Result, upstream)
	}
	if strings.Contains(resp.Error, t16CanaryEmail) {
		t.Fatalf("canary in error: %q", resp.Error)
	}
}

func TestAdvSC6_HTTPBodyNeverInspected(t *testing.T) {
	// DeepSeek: handleHTTP never calls applyPIIToText (rpc_server.go:999). Invalid URL avoids network.
	s := &harnessRPCServer{audit: &recordingAuditAppender{}}
	resp := s.handleHTTP(rpcRequest{
		ID:     "adv-http",
		Method: "http",
		Params: map[string]any{
			"method": "POST",
			"url":    ":",
			"body":   "ssn " + t16CanarySSN,
		},
	}, t16PIIState(t16RejectPII()), false)
	if resp.Code == StatusPIIBlocked || resp.Error == "pii_rejected" {
		return
	}
	t.Fatalf("// ADVERSARY BREAK: HTTP body SSN skipped PII inspect code=%s err=%q ok=%t", resp.Code, resp.Error, resp.OK)
}

func TestAdvSC6_MCPInputNeverInspected(t *testing.T) {
	s := &harnessRPCServer{audit: &recordingAuditAppender{}}
	resp := s.handleMCP(rpcRequest{
		ID:     "adv-mcp",
		Method: "mcp",
		Params: map[string]any{
			"server_id": "mailer",
			"tool":      "send",
			"input":     map[string]any{"ssn": t16CanarySSN, "to": t16CanaryEmail},
		},
	}, t16PIIState(t16RejectPII()))
	if resp.Code == StatusPIIBlocked || resp.Error == "pii_rejected" {
		return
	}
	t.Fatalf("// ADVERSARY BREAK: MCP input skipped PII inspect code=%s err=%q ok=%t", resp.Code, resp.Error, resp.OK)
}

func TestAdvSC6_LowercaseBuiltinFailOpens(t *testing.T) {
	// Unknown/case-mismatched builtin is dropped; action reject still valid=true (guardrails.go:180-198).
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "ok"}, nil
		},
	}
	pii := map[string]any{"action": "reject", "builtins": []any{"email", "ssn", "creditcard"}, "reject_status": 422, "reject_body": "pii_rejected"}
	resp := s.handleLLM(rpcRequest{ID: "adv-case", Method: "llm", Params: map[string]any{"prompt": "mail " + t16CanaryEmail + " ssn " + t16CanarySSN}}, t16PIIState(pii))
	if upstream != 0 {
		t.Fatalf("// ADVERSARY BREAK: lowercase builtins fail-open upstream=%d ok=%t err=%q", upstream, resp.OK, resp.Error)
	}
	if resp.OK {
		t.Fatal("expected reject or fail-closed")
	}
}

func TestAdvSC6_EmptyDetectorsRejectFailOpens(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "ok"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-empty", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, t16PIIState(map[string]any{
		"action": "reject", "builtins": []any{}, "patterns": []any{}, "reject_status": 422, "reject_body": "pii_rejected",
	}))
	if upstream != 0 {
		t.Fatalf("// ADVERSARY BREAK: reject with zero detectors fail-open upstream=%d ok=%t", upstream, resp.OK)
	}
	if resp.OK {
		t.Fatal("expected fail-closed")
	}
}

func TestAdvSC6_JSONRoundTripPayloadStillRejects(t *testing.T) {
	rec := &recordingAuditAppender{}
	upstream := 0
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "SHOULD_NOT"}, nil
		},
	}
	state := t16PIIState(t16RejectPII())
	state.payload = advJSONRoundTripPayload(t, state.payload)
	resp := s.handleLLM(rpcRequest{ID: "adv-json", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, state)
	if upstream != 0 {
		t.Fatalf("D205 json round-trip dropped enforce upstream=%d", upstream)
	}
	if resp.OK || resp.Error != "pii_rejected" {
		t.Fatalf("expected pii_rejected, got ok=%t err=%q", resp.OK, resp.Error)
	}
}

func TestAdvSC6_RequestMaskRedactsBeforeUpstream(t *testing.T) {
	var sawPrompt string
	s := &harnessRPCServer{
		audit: &recordingAuditAppender{},
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			sawPrompt = prompt
			return &llm.LLMResult{Text: "ok"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-reqmask", Method: "llm", Params: map[string]any{"prompt": "mail " + t16CanaryEmail}}, t16PIIState(t16MaskPII()))
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	if strings.Contains(sawPrompt, t16CanaryEmail) {
		t.Fatalf("request mask must redact before llmChatCompletion, got %q", sawPrompt)
	}
	if !strings.Contains(sawPrompt, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in upstream prompt, got %q", sawPrompt)
	}
}

func TestAdvSC6_ResponseRejectAuditDownFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		audit: t16ThrowAudit{},
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "ssn " + t16CanarySSN}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-d208-resp", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(t16RejectPII()))
	if resp.OK {
		t.Fatal("D208 response reject audit-down must fail-closed")
	}
	if strings.Contains(resp.Error, t16CanarySSN) {
		t.Fatalf("canary in error: %q", resp.Error)
	}
	if upstream != 1 {
		t.Fatalf("response-side reject is after upstream, upstream=%d", upstream)
	}
}

func TestAdvSC6_BoolPIIFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "nope"}, nil
		},
	}
	state := t16PIIState(nil)
	state.payload["pii"] = false
	resp := s.handleLLM(rpcRequest{ID: "adv-bool", Method: "llm", Params: map[string]any{"prompt": "hello"}}, state)
	if upstream != 0 {
		t.Fatalf("pii:false must fail-closed upstream=%d", upstream)
	}
	if resp.OK {
		t.Fatal("pii:false must fail-closed")
	}
}

func TestAdvSC6_MaskThenAllowedAuditOmitsCanary(t *testing.T) {
	rec := &recordingAuditAppender{}
	s := &harnessRPCServer{
		audit: rec,
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			return &llm.LLMResult{Text: "card " + t16CanaryCC}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-mask-audit", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(t16MaskPII()))
	if !resp.OK {
		t.Fatalf("expected OK, got %s", resp.Error)
	}
	if strings.Contains(t16AuditBlob(rec), t16CanaryCC) {
		t.Fatalf("canary in audit: %s", t16AuditBlob(rec))
	}
}

func TestAdvSC6_NilAuditStillRejectsRequest(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "SHOULD_NOT"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-nilaudit", Method: "llm", Params: map[string]any{"prompt": "ssn " + t16CanarySSN}}, t16PIIState(t16RejectPII()))
	if upstream != 0 {
		t.Fatalf("nil audit must not skip reject upstream=%d", upstream)
	}
	if resp.OK || resp.Error != "pii_rejected" {
		t.Fatalf("expected pii_rejected, got ok=%t err=%q", resp.OK, resp.Error)
	}
}

func TestAdvSC6_InvalidPatternFailClosed(t *testing.T) {
	upstream := 0
	s := &harnessRPCServer{
		llmChatCompletion: func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
			upstream++
			return &llm.LLMResult{Text: "nope"}, nil
		},
	}
	resp := s.handleLLM(rpcRequest{ID: "adv-badre", Method: "llm", Params: map[string]any{"prompt": "hello"}}, t16PIIState(map[string]any{
		"action": "mask", "builtins": []any{"Email"}, "patterns": []any{"("},
	}))
	if upstream != 0 {
		t.Fatalf("invalid extra pattern must fail-closed upstream=%d", upstream)
	}
	if resp.OK {
		t.Fatal("invalid pattern must fail-closed")
	}
}
