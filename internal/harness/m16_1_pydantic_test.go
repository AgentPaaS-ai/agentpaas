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

func TestM161PydanticAuditExportShape(t *testing.T) {
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

	var types []string
	for _, event := range recorder.events() {
		types = append(types, event.EventType)
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), gatewayKey) {
			t.Fatalf("audit leaked gateway key in %s", event.EventType)
		}
		if strings.Contains(string(b), openaiLoopbackAPIKey) {
			t.Fatalf("audit leaked loopback dummy key in %s", event.EventType)
		}
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "llm_result") {
		t.Fatalf("loopback pydantic path missing llm_result events: %q", types)
	}
}

func TestM161LoopbackModels(t *testing.T) {
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
