package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

func TestOpenAILoopbackBinds127001(t *testing.T) {
	rpc := &harnessRPCServer{}
	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	host, _, err := net.SplitHostPort(lb.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", lb.Addr(), err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("bind host = %q, want 127.0.0.1", host)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("bind IP = %v, want 127.0.0.1", ip)
	}
}

func TestOpenAILoopbackChatCompletionsReachesHandleLLM(t *testing.T) {
	const realKey = "sk-real-provider-key-do-not-leak"
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	var reached bool
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		reached = true
		if prompt != "hello from workload" {
			t.Errorf("prompt = %q, want hello from workload", prompt)
		}
		if apiKey != realKey {
			t.Errorf("handleLLM apiKey = %q, want gateway credential (not dummy loopback key)", apiKey)
		}
		return &llm.LLMResult{Text: "from-handleLLM", Tokens: 3, Model: "gpt-4o"}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: realKey},
	})
	rpc.SetInvoke(map[string]any{
		"llm": map[string]any{
			"provider":   "openai",
			"model":      "gpt-4o",
			"credential": testCredID,
		},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	body, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "hello from workload"},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-agentpaas-loopback")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, raw)
	}
	if !reached {
		t.Fatal("POST /v1/chat/completions did not reach handleLLM")
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, raw)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content != "from-handleLLM" {
		t.Fatalf("choices = %#v, want handleLLM text", parsed.Choices)
	}

	for _, event := range recorder.events() {
		if auditRecordContainsSecret(event, realKey) {
			t.Fatalf("audit leaked real provider key: %+v", event)
		}
	}
}

func TestWorkerEnvOmitsRealProviderKey(t *testing.T) {
	const realKey = "sk-real-provider-key-do-not-leak"
	env := workerEnvOpenAI([]string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + realKey,
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENAI_API_BASE=https://api.openai.com/v1",
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")

	joined := strings.Join(env, "\n")
	if strings.Contains(joined, realKey) {
		t.Fatalf("worker env leaked real provider key: %q", env)
	}
	if strings.Contains(joined, "api.openai.com") {
		t.Fatalf("worker env leaked provider host: %q", env)
	}

	var sawKey, sawBase, sawAPIBase bool
	for _, item := range env {
		switch item {
		case "OPENAI_API_KEY=sk-agentpaas-loopback":
			sawKey = true
		case "OPENAI_BASE_URL=http://127.0.0.1:54321/v1":
			sawBase = true
		case "OPENAI_API_BASE=http://127.0.0.1:54321/v1":
			sawAPIBase = true
		}
	}
	if !sawKey || !sawBase || !sawAPIBase {
		t.Fatalf("missing loopback openai env (key=%t base=%t api_base=%t): %q", sawKey, sawBase, sawAPIBase, env)
	}
}

func auditRecordContainsSecret(event audit.AuditRecord, secret string) bool {
	if secret == "" {
		return false
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return strings.Contains(event.Actor, secret)
	}
	return strings.Contains(string(raw), secret)
}
