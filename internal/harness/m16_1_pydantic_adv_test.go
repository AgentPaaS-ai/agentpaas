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
	advM161ProviderSentinel   = "ADV-SYNTHETIC-PROVIDER-KEY-M161"
	advM161OpenRouterSentinel = "ADV-SYNTHETIC-OPENROUTER-KEY-M161"
	advM161LogfireSentinel    = "ADV-SYNTHETIC-LOGFIRE-TOKEN-M161"
	advM161DeepseekSentinel   = "ADV-SYNTHETIC-DEEPSEEK-KEY-M161"
	advM161XAISentinel        = "ADV-SYNTHETIC-XAI-KEY-M161"
	advM161GroqSentinel       = "ADV-SYNTHETIC-GROQ-KEY-M161"
	advM161OTelSentinel       = "ADV-SYNTHETIC-OTEL-HEADER-M161"
)

func advM161EnvName(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

func advM161ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

func advM161Sentinels() []string {
	return []string{
		advM161ProviderSentinel,
		advM161OpenRouterSentinel,
		advM161LogfireSentinel,
		advM161DeepseekSentinel,
		advM161XAISentinel,
		advM161GroqSentinel,
		advM161OTelSentinel,
	}
}

func advM161PydanticChatBody() []byte {
	raw, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "system", "content": "Extract the city name from this question."},
			{"role": "user", "content": "What's the weather in Folsom?"},
		},
	})
	return raw
}

func advM161ReferenceLLMKeys() []string {
	return []string{"provider", "model", "input_tokens", "output_tokens", "total_tokens", "estimated_cost_usd"}
}

func advM161ReferenceEgressKeys() []string {
	return []string{"destination", "method", "decision"}
}

// TestADV_M16_1_SC1_OpenRouterAndFrameworkKeysAbsentFromPydanticWorkloadEnv
// plants the pydantic golden's actual provider key (openrouter) plus
// pydantic-ai telemetry aliases. workerEnvOpenAI must not inherit them.
func TestADV_M16_1_SC1_OpenRouterAndFrameworkKeysAbsentFromPydanticWorkloadEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", advM161OpenRouterSentinel)
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("LOGFIRE_TOKEN", advM161LogfireSentinel)
	t.Setenv("DEEPSEEK_API_KEY", advM161DeepseekSentinel)
	t.Setenv("XAI_API_KEY", advM161XAISentinel)
	t.Setenv("GROQ_API_KEY", advM161GroqSentinel)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization="+advM161OTelSentinel)
	t.Setenv("OPENAI_PROJECT", advM161ProviderSentinel)
	t.Setenv("OPENAI_API_TYPE", "openai")

	hostile := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + advM161ProviderSentinel,
		"OPENROUTER_API_KEY=" + advM161OpenRouterSentinel,
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"LOGFIRE_TOKEN=" + advM161LogfireSentinel,
		"DEEPSEEK_API_KEY=" + advM161DeepseekSentinel,
		"XAI_API_KEY=" + advM161XAISentinel,
		"GROQ_API_KEY=" + advM161GroqSentinel,
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=" + advM161OTelSentinel,
		"OPENAI_PROJECT=" + advM161ProviderSentinel,
		"OPENAI_API_TYPE=azure",
		"PYDANTIC_AI_GATEWAY_API_KEY=" + advM161ProviderSentinel,
	}

	envs := [][]string{
		workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
		workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
	}

	sentinels := advM161Sentinels()
	pydanticProviderAliases := map[string]bool{
		"OPENROUTER_API_KEY":          true,
		"OPENROUTER_BASE_URL":         true,
		"LOGFIRE_TOKEN":               true,
		"DEEPSEEK_API_KEY":            true,
		"XAI_API_KEY":                 true,
		"GROQ_API_KEY":                true,
		"OTEL_EXPORTER_OTLP_HEADERS":  true,
		"OPENAI_PROJECT":              true,
		"OPENAI_API_TYPE":             true,
		"PYDANTIC_AI_GATEWAY_API_KEY": true,
	}

	var dummySeen bool
	for _, env := range envs {
		for _, item := range env {
			name := advM161EnvName(item)
			if advM161ContainsSentinel(item, sentinels...) {
				// ADVERSARY BREAK: pydantic provider/telemetry secret inherited into workload env
				t.Errorf("ADVERSARY BREAK: SC1 planted pydantic-adjacent secret survived in workload env name=%s", name)
			}
			if pydanticProviderAliases[name] {
				// ADVERSARY BREAK: pydantic SDK/provider alias not stripped
				t.Errorf("ADVERSARY BREAK: SC1 pydantic provider/telemetry env %s survived into workload (openrouter is this golden's agent.yaml provider)", name)
			}
			if strings.HasPrefix(item, "OPENAI_API_KEY=") {
				val := strings.TrimPrefix(item, "OPENAI_API_KEY=")
				if val == "" {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY is empty (pydantic OpenAIProvider will fall back)")
				}
				if advM161ContainsSentinel(val, sentinels...) {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY equals planted parent secret")
				}
				if val == openaiLoopbackAPIKey {
					dummySeen = true
				}
			}
		}
	}
	if !dummySeen {
		t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key missing from pydantic worker env")
	}
}

// TestADV_M16_1_SC1_PydanticLoopbackAuditAndMemoryOmitProviderSecrets
// drives the pydantic-shaped chat body through the loopback and asserts
// gateway + planted secrets never appear in handleLLM apiKey, response, or audit.
func TestADV_M16_1_SC1_PydanticLoopbackAuditAndMemoryOmitProviderSecrets(t *testing.T) {
	const gatewaySidecar = "gateway-sidecar-not-env-sentinel-m161"
	sentinels := advM161Sentinels()
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		if advM161ContainsSentinel(apiKey, sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted env secret reached handleLLM apiKey")
		}
		if apiKey == openaiLoopbackAPIKey {
			t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key forwarded as handleLLM apiKey")
		}
		if apiKey != gatewaySidecar {
			t.Errorf("ADVERSARY BREAK: SC1 handleLLM apiKey was not the gateway sidecar credential")
		}
		if advM161ContainsSentinel(prompt, sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted secret reached handleLLM prompt")
		}
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model, InputTokens: 8, OutputTokens: 2}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	rpc.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openrouter", "model": "deepseek/deepseek-v4-flash", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(advM161PydanticChatBody()))
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if advM161ContainsSentinel(string(raw), sentinels...) || strings.Contains(string(raw), gatewaySidecar) {
		t.Errorf("ADVERSARY BREAK: SC1 planted or gateway secret reflected in pydantic loopback response")
	}

	for _, event := range recorder.events() {
		b, _ := json.Marshal(event)
		blob := string(b)
		if advM161ContainsSentinel(blob, sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted secret leaked into audit event_type=%s", event.EventType)
		}
		if strings.Contains(blob, gatewaySidecar) {
			t.Errorf("ADVERSARY BREAK: SC1 gateway sidecar leaked into audit event_type=%s", event.EventType)
		}
		if strings.Contains(blob, openaiLoopbackAPIKey) {
			t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key leaked into audit event_type=%s", event.EventType)
		}
	}
}

// TestADV_M16_1_SC3_PydanticDirectProviderHostDenied is the required
// golden-level default-deny case. Never dials the public network.
func TestADV_M16_1_SC3_PydanticDirectProviderHostDenied(t *testing.T) {
	forbidden := []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"generativelanguage.googleapis.com",
	}

	env := workerEnvOpenAI([]string{
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"OPENAI_API_KEY=" + advM161ProviderSentinel,
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
		for _, host := range forbidden {
			if strings.Contains(strings.ToLower(item), host) {
				// ADVERSARY BREAK: rewritten env still names a provider host
				t.Errorf("ADVERSARY BREAK: SC3 pydantic worker env still names provider host (env name=%s)", advM161EnvName(item))
			}
		}
	}
	if !denyMarker {
		t.Errorf("ADVERSARY BREAK: SC3 pydantic worker env has no deny/pin; OPENAI_BASE_URL rewrite is advisory only")
	}
	if !loopbackBase {
		t.Errorf("ADVERSARY BREAK: SC3 pydantic OPENAI_BASE_URL was not rewritten to loopback")
	}

	// Env flags are not deny. A pydantic_ai Agent that ignores base_url and
	// hits the SDK default host must be fail-closed without a public dial.
	if !pydanticGoldenDirectHostDenied("api.openai.com") {
		// ADVERSARY BREAK: no harness predicate denies pydantic direct provider host
		t.Errorf("ADVERSARY BREAK: SC3 pydantic golden has no pre-dial deny for api.openai.com (AGENTPAAS_EGRESS_DENY is unread; loopbackDeniedProviderHosts is unused by serveHTTP)")
	}
	if !pydanticGoldenDirectHostDenied("openrouter.ai") {
		t.Errorf("ADVERSARY BREAK: SC3 pydantic golden has no pre-dial deny for openrouter.ai (this golden's agent.yaml provider)")
	}

	// Confirm we never issued a public dial as the deny proof.
	for _, host := range forbidden {
		if host == "" {
			t.Fatalf("empty forbidden host")
		}
	}
}

// pydanticGoldenDirectHostDenied is the SC3 control the M16.1 worker did not
// ship for this golden. AGENTPAAS_EGRESS_DENY is injected into worker env
// but never read. loopbackDeniedProviderHosts is a list whose length is
// checked, not a host matcher consulted on the pydantic HTTP path.
func pydanticGoldenDirectHostDenied(host string) bool {
	_ = host
	return false
}

// TestADV_M16_1_SC3_EmptyLoopbackURLMustNotFallOpenToSDKDefault
// covers the pydantic OpenAIProvider default (api.openai.com) when the
// harness fails to start loopback and workerEnvOpenAI gets an empty base URL.
func TestADV_M16_1_SC3_EmptyLoopbackURLMustNotFallOpenToSDKDefault(t *testing.T) {
	env := workerEnvOpenAI([]string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + advM161ProviderSentinel,
		"OPENAI_BASE_URL=https://api.openai.com/v1",
	}, "127.0.0.1:1", "")

	var sawBase, sawKey, sawDeny bool
	for _, item := range env {
		name := advM161EnvName(item)
		val := ""
		if i := strings.IndexByte(item, '='); i >= 0 {
			val = item[i+1:]
		}
		switch name {
		case "OPENAI_BASE_URL", "OPENAI_API_BASE":
			sawBase = true
			if strings.Contains(strings.ToLower(val), "api.openai.com") || strings.Contains(strings.ToLower(val), "openrouter.ai") {
				t.Errorf("ADVERSARY BREAK: SC3 empty loopback left provider host in %s", name)
			}
		case "OPENAI_API_KEY":
			sawKey = true
			if advM161ContainsSentinel(val, advM161ProviderSentinel) {
				t.Errorf("ADVERSARY BREAK: SC3 empty loopback inherited planted key")
			}
		case "AGENTPAAS_EGRESS_DENY", "AGENTPAAS_LOOPBACK_PIN":
			sawDeny = true
		}
		if advM161ContainsSentinel(item, advM161Sentinels()...) {
			t.Errorf("ADVERSARY BREAK: SC3 empty loopback leaked planted secret in %s", name)
		}
	}
	if sawBase && !stringsContainLoopback(env) {
		t.Errorf("ADVERSARY BREAK: SC3 empty loopback advertised a non-loopback OPENAI_BASE_URL")
	}
	if !sawKey || !sawDeny {
		// ADVERSARY BREAK: missing dummy/deny when loopback URL is empty; pydantic SDK defaults to api.openai.com
		t.Errorf("ADVERSARY BREAK: SC3 empty openaiBaseURL omits dummy key and/or egress deny (pydantic OpenAIProvider defaults to api.openai.com); sawKey=%t sawDeny=%t", sawKey, sawDeny)
	}
}

func stringsContainLoopback(env []string) bool {
	for _, item := range env {
		if strings.Contains(item, "127.0.0.1") {
			return true
		}
	}
	return false
}

// TestADV_M16_1_SC3_TrailingDotCaseAndIPBypassMustDeny
// hostname-string allowlists miss these. Never dials.
func TestADV_M16_1_SC3_TrailingDotCaseAndIPBypassMustDeny(t *testing.T) {
	variants := []string{
		"api.openai.com.",
		"API.OPENAI.COM",
		"openrouter.ai.",
		"OpenRouter.AI",
	}
	for _, host := range variants {
		if !pydanticGoldenDirectHostDenied(host) {
			t.Errorf("ADVERSARY BREAK: SC3 pydantic direct-host variant not denied host-class=%s", hostClass(host))
		}
	}
}

func hostClass(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "openai"):
		return "openai-provider"
	case strings.Contains(h, "openrouter"):
		return "openrouter-provider"
	case strings.Contains(h, "anthropic"):
		return "anthropic-provider"
	default:
		return "provider-host"
	}
}

// TestADV_M16_1_SC6_PydanticAuditShapeMatchesWeatherAgentReference
// compares LLM/egress event types and payload keys produced by the pydantic
// loopback path against the handleLLM shape the reference weather agent uses.
func TestADV_M16_1_SC6_PydanticAuditShapeMatchesWeatherAgentReference(t *testing.T) {
	const gatewaySidecar = "gateway-sidecar-not-env-sentinel-m161"
	pydanticRec := &recordingAuditAppender{}
	pydanticRPC := &harnessRPCServer{audit: pydanticRec}
	pydanticRPC.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model, InputTokens: 8, OutputTokens: 2}, nil
	}
	pydanticRPC.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	pydanticRPC.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openrouter", "model": "deepseek/deepseek-v4-flash", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(pydanticRPC)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(advM161PydanticChatBody()))
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
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()

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

	pydanticShape := advM161EventShape(pydanticRec.events())
	refShape := advM161EventShape(refRec.events())

	if !pydanticShape["llm_result"] {
		t.Errorf("ADVERSARY BREAK: SC6 pydantic loopback path missing llm_result (reference weather agent emits it)")
	}
	if !pydanticShape["egress_allowed"] && !pydanticShape["egress_denied"] {
		t.Errorf("ADVERSARY BREAK: SC6 pydantic loopback path missing egress_* event (reference weather agent emits egress_allowed/denied)")
	}
	for k := range refShape {
		if !pydanticShape[k] {
			t.Errorf("ADVERSARY BREAK: SC6 pydantic audit missing reference event_type=%s", k)
		}
	}

	pydanticLLMKeys := advM161PayloadKeys(pydanticRec.events(), "llm_result")
	refLLMKeys := advM161PayloadKeys(refRec.events(), "llm_result")
	for _, k := range advM161ReferenceLLMKeys() {
		if !pydanticLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 pydantic llm_result missing field %s", k)
		}
		if refLLMKeys[k] && !pydanticLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 pydantic llm_result field set diverges from weather-agent reference field=%s", k)
		}
	}
	pydanticEgressKeys := advM161PayloadKeys(pydanticRec.events(), "egress_allowed")
	if len(pydanticEgressKeys) == 0 {
		pydanticEgressKeys = advM161PayloadKeys(pydanticRec.events(), "egress_denied")
	}
	for _, k := range advM161ReferenceEgressKeys() {
		if !pydanticEgressKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 pydantic egress event missing field %s", k)
		}
	}

	for _, event := range pydanticRec.events() {
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), gatewaySidecar) || strings.Contains(string(b), openaiLoopbackAPIKey) {
			t.Errorf("ADVERSARY BREAK: SC6 pydantic audit leaked credential material event_type=%s", event.EventType)
		}
		if event.EventType == "egress_allowed" || event.EventType == "egress_denied" {
			dest, _ := event.Payload["destination"].(string)
			if destClass := egressDestClass(dest); destClass == "provider-host" {
				// ADVERSARY BREAK: loopback LLM hop audited as public provider destination
				t.Errorf("ADVERSARY BREAK: SC6 pydantic loopback LLM egress destination class=provider-host (want loopback); actual packets are 127.0.0.1")
			}
			if dest == "" {
				t.Errorf("ADVERSARY BREAK: SC6 pydantic egress event missing destination")
			}
		}
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
	after := advM161EventShape(pydanticRec.events())
	if modelsResp.StatusCode == http.StatusOK && len(after) == len(pydanticShape) {
		// ADVERSARY BREAK: pydantic-ai GET /v1/models is unaudited LLM HTTP
		t.Errorf("ADVERSARY BREAK: SC6 pydantic GET /v1/models produced no additional LLM/egress audit event (weather-agent analog has no unaudited LLM HTTP)")
	}
}

func advM161EventShape(events []audit.AuditRecord) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.EventType != "" {
			out[ev.EventType] = true
		}
	}
	return out
}

func advM161PayloadKeys(events []audit.AuditRecord, eventType string) map[string]bool {
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

func egressDestClass(dest string) string {
	d := strings.ToLower(dest)
	switch {
	case d == "":
		return "empty"
	case strings.Contains(d, "127.0.0.1") || strings.Contains(d, "localhost") || strings.Contains(d, "::1"):
		return "loopback"
	case strings.Contains(d, "openai.com") || strings.Contains(d, "openrouter.ai") ||
		strings.Contains(d, "anthropic.com") || strings.Contains(d, "googleapis.com") ||
		strings.Contains(d, "openai.azure.com"):
		return "provider-host"
	default:
		return "other"
	}
}
