package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

// Synthetic sentinels — never print the raw value in failure messages.
const (
	advM162ProviderSentinel   = "ADV-SYNTHETIC-PROVIDER-KEY-M162"
	advM162OpenRouterSentinel = "ADV-SYNTHETIC-OPENROUTER-KEY-M162"
	advM162LangChainSentinel  = "ADV-SYNTHETIC-LANGCHAIN-KEY-M162"
	advM162LangSmithSentinel  = "ADV-SYNTHETIC-LANGSMITH-KEY-M162"
	advM162LangGraphSentinel  = "ADV-SYNTHETIC-LANGGRAPH-KEY-M162"
	advM162FireworksSentinel  = "ADV-SYNTHETIC-FIREWORKS-KEY-M162"
	advM162TogetherSentinel   = "ADV-SYNTHETIC-TOGETHER-KEY-M162"
)

func advM162EnvName(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

func advM162ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

func advM162Sentinels() []string {
	return []string{
		advM162ProviderSentinel,
		advM162OpenRouterSentinel,
		advM162LangChainSentinel,
		advM162LangSmithSentinel,
		advM162LangGraphSentinel,
		advM162FireworksSentinel,
		advM162TogetherSentinel,
	}
}

func advM162LangGraphChatBody() []byte {
	raw, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "Extract the city name from this question. What's the weather in Folsom?"},
		},
	})
	return raw
}

func advM162ReferenceLLMKeys() []string {
	return []string{"provider", "model", "input_tokens", "output_tokens", "total_tokens", "estimated_cost_usd"}
}

func advM162ReferenceEgressKeys() []string {
	return []string{"destination", "method", "decision"}
}

func advM162LangGraphProviderAliases() map[string]bool {
	return map[string]bool{
		"LANGCHAIN_API_KEY":        true,
		"LANGCHAIN_TRACING_V2":     true,
		"LANGCHAIN_ENDPOINT":       true,
		"LANGCHAIN_PROJECT":        true,
		"LANGCHAIN_HUB_API_KEY":    true,
		"LANGSMITH_API_KEY":        true,
		"LANGSMITH_TRACING":        true,
		"LANGSMITH_ENDPOINT":       true,
		"LANGGRAPH_API_KEY":        true,
		"LANGGRAPH_CLOUD_API_KEY":  true,
		"LANGGRAPH_API_URL":        true,
		"FIREWORKS_API_KEY":        true,
		"TOGETHER_API_KEY":         true,
		"OPENROUTER_API_KEY":       true,
		"OPENROUTER_BASE_URL":      true,
	}
}

// TestADV_M16_2_SC1_LangChainLangSmithKeysAbsentFromLangGraphWorkloadEnv
// plants LangGraph-adjacent tracing/provider keys. workerEnvOpenAI must not
// inherit them — ChatOpenAI / LangSmith callbacks read these from the process
// env even when OPENAI_API_KEY is the dummy loopback value.
func TestADV_M16_2_SC1_LangChainLangSmithKeysAbsentFromLangGraphWorkloadEnv(t *testing.T) {
	t.Setenv("LANGCHAIN_API_KEY", advM162LangChainSentinel)
	t.Setenv("LANGSMITH_API_KEY", advM162LangSmithSentinel)
	t.Setenv("LANGGRAPH_API_KEY", advM162LangGraphSentinel)
	t.Setenv("LANGGRAPH_CLOUD_API_KEY", advM162LangGraphSentinel)
	t.Setenv("LANGCHAIN_HUB_API_KEY", advM162LangChainSentinel)
	t.Setenv("FIREWORKS_API_KEY", advM162FireworksSentinel)
	t.Setenv("TOGETHER_API_KEY", advM162TogetherSentinel)
	t.Setenv("OPENROUTER_API_KEY", advM162OpenRouterSentinel)
	t.Setenv("LANGCHAIN_TRACING_V2", "true")
	t.Setenv("LANGSMITH_TRACING", "true")
	t.Setenv("LANGCHAIN_ENDPOINT", "https://api.smith.langchain.com")
	t.Setenv("LANGSMITH_ENDPOINT", "https://api.smith.langchain.com")
	t.Setenv("LANGGRAPH_API_URL", "https://api.langchain.com")

	hostile := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + advM162ProviderSentinel,
		"LANGCHAIN_API_KEY=" + advM162LangChainSentinel,
		"LANGSMITH_API_KEY=" + advM162LangSmithSentinel,
		"LANGGRAPH_API_KEY=" + advM162LangGraphSentinel,
		"LANGGRAPH_CLOUD_API_KEY=" + advM162LangGraphSentinel,
		"LANGCHAIN_HUB_API_KEY=" + advM162LangChainSentinel,
		"FIREWORKS_API_KEY=" + advM162FireworksSentinel,
		"TOGETHER_API_KEY=" + advM162TogetherSentinel,
		"OPENROUTER_API_KEY=" + advM162OpenRouterSentinel,
		"LANGCHAIN_TRACING_V2=true",
		"LANGSMITH_TRACING=true",
		"LANGCHAIN_ENDPOINT=https://api.smith.langchain.com",
		"LANGSMITH_ENDPOINT=https://api.smith.langchain.com",
		"LANGGRAPH_API_URL=https://api.langchain.com",
		"LANGCHAIN_PROJECT=" + advM162LangChainSentinel,
	}

	envs := [][]string{
		workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
		workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
	}

	sentinels := advM162Sentinels()
	aliases := advM162LangGraphProviderAliases()

	var dummySeen bool
	for _, env := range envs {
		for _, item := range env {
			name := advM162EnvName(item)
			if advM162ContainsSentinel(item, sentinels...) {
				// ADVERSARY BREAK: langgraph/langchain/langsmith secret inherited into workload env
				t.Errorf("ADVERSARY BREAK: SC1 planted langgraph-adjacent secret survived in workload env name=%s", name)
			}
			if aliases[name] {
				// ADVERSARY BREAK: langgraph SDK/tracing alias not stripped
				t.Errorf("ADVERSARY BREAK: SC1 langgraph/langchain/langsmith env %s survived into workload (LangSmith tracing exfiltrates prompts+keys)", name)
			}
			if strings.HasPrefix(item, "OPENAI_API_KEY=") {
				val := strings.TrimPrefix(item, "OPENAI_API_KEY=")
				if val == "" {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY is empty (LangGraph OpenAI() / ChatOpenAI will fall back)")
				}
				if advM162ContainsSentinel(val, sentinels...) {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY equals planted parent secret")
				}
				if val == openaiLoopbackAPIKey {
					dummySeen = true
				}
			}
		}
	}
	if !dummySeen {
		t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key missing from langgraph worker env")
	}
}

// TestADV_M16_2_SC3_LangGraphDirectProviderAndLangSmithHostDenied is the
// required golden-level default-deny case. Never dials the public network.
// Worker TestM162DirectProviderHostDenied only covers openai/openrouter/
// anthropic/azure via pydanticDirectHostDenied — not LangSmith/LangGraph
// cloud, x.ai, or IP literals that ChatOpenAI / httpx would use.
func TestADV_M16_2_SC3_LangGraphDirectProviderAndLangSmithHostDenied(t *testing.T) {
	forbidden := []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"api.smith.langchain.com",
		"api.langchain.com",
		"smith.langchain.com",
		"api.x.ai",
		"inference-api.nousresearch.com",
		"api.fireworks.ai",
		"api.together.xyz",
	}

	env := workerEnvOpenAI([]string{
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"LANGCHAIN_ENDPOINT=https://api.smith.langchain.com",
		"LANGSMITH_ENDPOINT=https://api.smith.langchain.com",
		"LANGGRAPH_API_URL=https://api.langchain.com",
		"OPENAI_API_KEY=" + advM162ProviderSentinel,
		"LANGCHAIN_API_KEY=" + advM162LangChainSentinel,
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")

	var denyMarker bool
	var loopbackBase bool
	for _, item := range env {
		upper := strings.ToUpper(item)
		if strings.Contains(upper, "AGENTPAAS_EGRESS_DENY") ||
			strings.Contains(upper, "LOOPBACK_PIN") {
			denyMarker = true
		}
		if strings.HasPrefix(item, "OPENAI_BASE_URL=") && strings.Contains(item, "127.0.0.1") {
			loopbackBase = true
		}
		lower := strings.ToLower(item)
		for _, host := range forbidden {
			if strings.Contains(lower, host) {
				// ADVERSARY BREAK: rewritten env still names a provider/telemetry host
				t.Errorf("ADVERSARY BREAK: SC3 langgraph worker env still names provider/telemetry host (env name=%s)", advM162EnvName(item))
			}
		}
	}
	if !denyMarker {
		t.Errorf("ADVERSARY BREAK: SC3 langgraph worker env has no deny/pin; OPENAI_BASE_URL rewrite is advisory only")
	}
	if !loopbackBase {
		t.Errorf("ADVERSARY BREAK: SC3 langgraph OPENAI_BASE_URL was not rewritten to loopback")
	}

	// Env flags are not deny. A LangGraph node that ignores base_url and
	// hits ChatOpenAI / httpx / urllib default host must be fail-closed
	// without a public dial. AGENTPAAS_EGRESS_DENY is unread; the deny
	// list is unused by serveHTTP.
	for _, host := range []string{
		"api.smith.langchain.com",
		"api.langchain.com",
		"smith.langchain.com",
		"api.x.ai",
		"inference-api.nousresearch.com",
		"api.fireworks.ai",
		"api.together.xyz",
		"104.18.0.1",
		"2606:4700::1",
		"api.openai.com.attacker.invalid",
	} {
		if !pydanticDirectHostDenied(host) {
			// ADVERSARY BREAK: no harness predicate denies langgraph direct provider/telemetry host
			t.Errorf("ADVERSARY BREAK: SC3 langgraph golden has no pre-dial deny for host-class=%s (AGENTPAAS_EGRESS_DENY is unread; loopbackDeniedProviderHosts unused by serveHTTP)", advM162HostClass(host))
		}
	}

	for _, host := range forbidden {
		if host == "" {
			t.Fatalf("empty forbidden host")
		}
	}
}

func advM162HostClass(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "smith") || strings.Contains(h, "langchain"):
		return "langsmith-telemetry"
	case strings.Contains(h, "langgraph") || h == "api.langchain.com":
		return "langgraph-cloud"
	case strings.Contains(h, "x.ai"):
		return "xai-provider"
	case strings.Contains(h, "nous"):
		return "nous-provider"
	case strings.Contains(h, "fireworks"):
		return "fireworks-provider"
	case strings.Contains(h, "together"):
		return "together-provider"
	case strings.Contains(h, "attacker"):
		return "suffix-bypass"
	case strings.ContainsAny(h, ":") || (len(h) > 0 && h[0] >= '0' && h[0] <= '9'):
		return "ip-literal"
	default:
		return "provider-host"
	}
}

// TestADV_M16_2_SC3_TrailingDotCaseAndIPBypassMustDeny
// hostname-string allowlists miss these. Never dials.
func TestADV_M16_2_SC3_TrailingDotCaseAndIPBypassMustDeny(t *testing.T) {
	variants := []string{
		"api.smith.langchain.com.",
		"API.SMITH.LANGCHAIN.COM",
		"api.langchain.com.",
		"Api.LangChain.Com",
		"api.x.ai.",
		"API.X.AI",
	}
	for _, host := range variants {
		if !pydanticDirectHostDenied(host) {
			t.Errorf("ADVERSARY BREAK: SC3 langgraph direct-host variant not denied host-class=%s", advM162HostClass(host))
		}
	}
}

// TestADV_M16_2_SC6_LangGraphAuditShapeMatchesWeatherAgentReference
// compares LLM/egress event types and payload keys produced by the langgraph
// loopback path (chat + GET /v1/models that ChatOpenAI issues) against the
// handleLLM shape the reference weather agent uses.
func TestADV_M16_2_SC6_LangGraphAuditShapeMatchesWeatherAgentReference(t *testing.T) {
	const gatewaySidecar = "gateway-sidecar-not-env-sentinel-m162"
	lgRec := &recordingAuditAppender{}
	lgRPC := &harnessRPCServer{audit: lgRec}
	lgRPC.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model, InputTokens: 8, OutputTokens: 2}, nil
	}
	lgRPC.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	lgRPC.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openrouter", "model": "deepseek/deepseek-v4-flash", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(lgRPC)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(advM162LangGraphChatBody()))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openaiLoopbackAPIKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if advM162ContainsSentinel(string(raw), advM162Sentinels()...) || strings.Contains(string(raw), gatewaySidecar) {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph loopback response leaked credential material")
	}

	afterChat := advM162EventShape(lgRec.events())

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

	refRec := &recordingAuditAppender{}
	refRPC := &harnessRPCServer{audit: refRec}
	refRPC.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model, InputTokens: 8, OutputTokens: 2}, nil
	}
	refRPC.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	refRPC.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openrouter", "model": "deepseek/deepseek-v4-flash", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)
	_ = refRPC.handleLLM(rpcRequest{ID: "ref", Method: "llm", Params: map[string]any{"prompt": "Extract the city name"}}, refRPC.currentInvoke())

	lgShape := advM162EventShape(lgRec.events())
	refShape := advM162EventShape(refRec.events())

	if !lgShape["llm_result"] {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph loopback path missing llm_result (reference weather agent emits it)")
	}
	if !lgShape["egress_allowed"] && !lgShape["egress_denied"] {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph loopback path missing egress_* event (reference weather agent emits egress_allowed/denied)")
	}
	for k := range refShape {
		if !lgShape[k] {
			t.Errorf("ADVERSARY BREAK: SC6 langgraph audit missing reference event_type=%s", k)
		}
	}
	for k := range lgShape {
		if !refShape[k] {
			// ADVERSARY BREAK: langgraph-only audit event class (GET /v1/models → egress_loopback_models)
			t.Errorf("ADVERSARY BREAK: SC6 langgraph audit extra event_type=%s not in weather-agent reference (ChatOpenAI GET /v1/models is not weather-shaped)", k)
		}
	}

	lgLLMKeys := advM162PayloadKeys(lgRec.events(), "llm_result")
	refLLMKeys := advM162PayloadKeys(refRec.events(), "llm_result")
	for _, k := range advM162ReferenceLLMKeys() {
		if !lgLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 langgraph llm_result missing field %s", k)
		}
		if refLLMKeys[k] && !lgLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 langgraph llm_result field set diverges from weather-agent reference field=%s", k)
		}
	}
	lgEgressKeys := advM162PayloadKeys(lgRec.events(), "egress_allowed")
	if len(lgEgressKeys) == 0 {
		lgEgressKeys = advM162PayloadKeys(lgRec.events(), "egress_denied")
	}
	for _, k := range advM162ReferenceEgressKeys() {
		if !lgEgressKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 langgraph egress event missing field %s", k)
		}
	}

	for _, event := range lgRec.events() {
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), gatewaySidecar) || strings.Contains(string(b), openaiLoopbackAPIKey) {
			t.Errorf("ADVERSARY BREAK: SC6 langgraph audit leaked credential material event_type=%s", event.EventType)
		}
		if event.EventType == "egress_allowed" || event.EventType == "egress_denied" {
			dest, _ := event.Payload["destination"].(string)
			if destClass := advM162EgressDestClass(dest); destClass == "provider-host" {
				// ADVERSARY BREAK: loopback LLM hop audited as public provider destination
				t.Errorf("ADVERSARY BREAK: SC6 langgraph loopback LLM egress destination class=provider-host (want loopback); actual packets are 127.0.0.1")
			}
			if dest == "" {
				t.Errorf("ADVERSARY BREAK: SC6 langgraph egress event missing destination")
			}
		}
		if event.EventType == "egress_loopback_models" {
			// ADVERSARY BREAK: models probe is not a weather-agent LLM/egress event
			t.Errorf("ADVERSARY BREAK: SC6 langgraph GET /v1/models audited as egress_loopback_models (weather-agent analog has llm_result/egress_allowed|denied only)")
		}
	}

	if modelsResp.StatusCode == http.StatusOK {
		modelsWeather := false
		for _, event := range lgRec.events() {
			if !afterChat[event.EventType] && (event.EventType == "llm_result" || event.EventType == "egress_allowed" || event.EventType == "egress_denied") {
				modelsWeather = true
			}
		}
		if !modelsWeather {
			// ADVERSARY BREAK: ChatOpenAI GET /v1/models is not weather-class audited
			t.Errorf("ADVERSARY BREAK: SC6 langgraph GET /v1/models produced no additional weather-class LLM/egress audit event")
		}
	}
}

func advM162EventShape(events []audit.AuditRecord) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.EventType != "" {
			out[ev.EventType] = true
		}
	}
	return out
}

func advM162PayloadKeys(events []audit.AuditRecord, eventType string) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.EventType != eventType {
			continue
		}
		for k := range ev.Payload {
			out[k] = true
		}
	}
	return out
}

func advM162EgressDestClass(dest string) string {
	d := strings.ToLower(dest)
	switch {
	case d == "":
		return "empty"
	case strings.Contains(d, "127.0.0.1") || strings.Contains(d, "localhost") || strings.Contains(d, "::1"):
		return "loopback"
	case strings.Contains(d, "openai.com") || strings.Contains(d, "openrouter.ai") ||
		strings.Contains(d, "anthropic.com") || strings.Contains(d, "googleapis.com") ||
		strings.Contains(d, "openai.azure.com") || strings.Contains(d, "langchain.com") ||
		strings.Contains(d, "smith.langchain"):
		return "provider-host"
	default:
		return "other"
	}
}
