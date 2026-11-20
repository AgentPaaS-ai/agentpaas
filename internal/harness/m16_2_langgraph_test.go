package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

func TestM162LangGraphAuditExportShape(t *testing.T) {
	const gatewayKey = "gateway-sidecar-not-in-workload"
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		if apiKey != gatewayKey {
			t.Errorf("handleLLM apiKey leaked loopback dummy or wrong key")
		}
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewayKey},
	})
	rpc.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openai", "model": "gpt-4o", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	body, err := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "Extract the city name"},
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
	req.Header.Set("Authorization", "Bearer "+openaiLoopbackAPIKey)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}

	modelsReq, err := http.NewRequest(http.MethodGet, "http://"+lb.Addr()+"/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequest models: %v", err)
	}
	modelsReq.Header.Set("Authorization", "Bearer "+openaiLoopbackAPIKey)
	modelsResp, err := client.Do(modelsReq)
	if err != nil {
		t.Fatalf("Do models: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(modelsResp.Body, 1<<20))
	_ = modelsResp.Body.Close()
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/models status=%d", modelsResp.StatusCode)
	}

	var types []string
	var sawLLM, sawEgress bool
	llmKeys := map[string]bool{}
	egressKeys := map[string]bool{}
	for _, event := range recorder.events() {
		types = append(types, event.EventType)
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), gatewayKey) {
			t.Fatalf("audit leaked gateway key in %s", event.EventType)
		}
		if strings.Contains(string(b), openaiLoopbackAPIKey) {
			t.Fatalf("audit leaked loopback dummy key in %s", event.EventType)
		}
		switch event.EventType {
		case "llm_result":
			sawLLM = true
			for k := range event.Payload {
				llmKeys[k] = true
			}
		case "egress_allowed", "egress_denied":
			sawEgress = true
			for k := range event.Payload {
				egressKeys[k] = true
			}
		}
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "llm_result") || !sawLLM {
		t.Fatalf("loopback langgraph path missing llm_result events: %q", types)
	}
	if !sawEgress {
		t.Fatalf("loopback langgraph path missing egress events: %q", types)
	}
	for _, k := range []string{"provider", "model"} {
		if !llmKeys[k] {
			t.Fatalf("llm_result missing weather-golden field %s (keys=%v)", k, llmKeys)
		}
	}
	for _, k := range []string{"destination", "method", "decision"} {
		if !egressKeys[k] {
			t.Fatalf("egress event missing weather-golden field %s (keys=%v)", k, egressKeys)
		}
	}
}

func TestM162LoopbackModels(t *testing.T) {
	rpc := &harnessRPCServer{}
	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	req, err := http.NewRequest(http.MethodGet, "http://"+lb.Addr()+"/v1/models", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+openaiLoopbackAPIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/models status=%d", resp.StatusCode)
	}
}

func TestM162WorkloadEnvOmitsProviderKey(t *testing.T) {
	const planted = "M162-PLANTED-PROVIDER-KEY-NOT-DUMMY"
	const gatewayFixture = "gateway-sidecar-not-in-workload"
	env := workerEnvOpenAI([]string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + planted,
		"OPENROUTER_API_KEY=" + planted,
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENAI_API_BASE=https://openrouter.ai/api/v1",
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")

	joined := strings.Join(env, "\n")
	if strings.Contains(joined, planted) {
		t.Fatalf("worker env leaked planted provider key")
	}
	if strings.Contains(joined, gatewayFixture) {
		t.Fatalf("worker env leaked gateway fixture string")
	}

	var sawDummy, sawLoopback bool
	for _, item := range env {
		if strings.HasPrefix(item, "OPENAI_API_KEY=") {
			val := strings.TrimPrefix(item, "OPENAI_API_KEY=")
			if val != openaiLoopbackAPIKey {
				t.Fatalf("OPENAI_API_KEY is not the dummy loopback key")
			}
			if val == planted || val == gatewayFixture {
				t.Fatal("dummy key collided with planted/gateway fixture")
			}
			sawDummy = true
		}
		if strings.HasPrefix(item, "OPENAI_BASE_URL=") {
			if !strings.Contains(item, "127.0.0.1") {
				t.Fatalf("OPENAI_BASE_URL is not loopback: %s", item)
			}
			sawLoopback = true
		}
	}
	if !sawDummy || !sawLoopback {
		t.Fatalf("missing dummy key or loopback base_url (dummy=%t loopback=%t): %q", sawDummy, sawLoopback, env)
	}
}

func TestM162DirectProviderHostDenied(t *testing.T) {
	forbidden := []string{"api.openai.com", "openrouter.ai", "api.anthropic.com", "openai.azure.com"}
	env := workerEnvOpenAI([]string{
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"OPENAI_API_KEY=M162-PLANTED-PROVIDER-KEY-NOT-DUMMY",
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")
	for _, item := range env {
		lower := strings.ToLower(item)
		for _, host := range forbidden {
			if strings.Contains(lower, host) {
				t.Fatalf("worker env names provider host %s", host)
			}
		}
	}
	for _, host := range forbidden {
		if !pydanticDirectHostDenied(host) {
			t.Fatalf("direct provider host not denied: %s", host)
		}
	}
}
