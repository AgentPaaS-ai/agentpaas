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
	advM163ProviderSentinel   = "ADV-SYNTHETIC-PROVIDER-KEY-M163"
	advM163OpenRouterSentinel = "ADV-SYNTHETIC-OPENROUTER-KEY-M163"
	advM163LiteLLMSentinel    = "ADV-SYNTHETIC-LITELLM-KEY-M163"
	advM163PosthogSentinel    = "ADV-SYNTHETIC-POSTHOG-KEY-M163"
	advM163SentrySentinel     = "ADV-SYNTHETIC-SENTRY-DSN-M163"
	advM163CrewAISentinel     = "ADV-SYNTHETIC-CREWAI-KEY-M163"
	advM163OTelSentinel       = "ADV-SYNTHETIC-OTEL-HEADER-M163"
)

func advM163EnvName(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

func advM163ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

func advM163Sentinels() []string {
	return []string{
		advM163ProviderSentinel,
		advM163OpenRouterSentinel,
		advM163LiteLLMSentinel,
		advM163PosthogSentinel,
		advM163SentrySentinel,
		advM163CrewAISentinel,
		advM163OTelSentinel,
	}
}

func advM163CrewAIChatBody() []byte {
	raw, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "Extract the city name from this question. What's the weather in Folsom?"},
		},
	})
	return raw
}

func advM163ReferenceLLMKeys() []string {
	return []string{"provider", "model", "input_tokens", "output_tokens", "total_tokens", "estimated_cost_usd"}
}

func advM163ReferenceEgressKeys() []string {
	return []string{"destination", "method", "decision"}
}

func advM163CrewAISecretAliases() map[string]bool {
	return map[string]bool{
		"LITELLM_API_KEY":                     true,
		"LITELLM_PROXY_API_KEY":               true,
		"LITELLM_LOG":                         true,
		"POSTHOG_API_KEY":                     true,
		"POSTHOG_HOST":                        true,
		"POSTHOG_DISABLED":                    true,
		"SENTRY_DSN":                          true,
		"SENTRY_ENVIRONMENT":                  true,
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": true,
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":    true,
		"OTEL_EXPORTER_OTLP_PROTOCOL":         true,
		"CREWAI_API_KEY":                      true,
		"OPENROUTER_API_KEY":                  true,
		"OPENROUTER_BASE_URL":                 true,
	}
}

func advM163HostClass(host string) string {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "sentry"):
		return "sentry-telemetry"
	case strings.Contains(h, "posthog"):
		return "posthog-telemetry"
	case strings.Contains(h, "crewai"):
		return "crewai-telemetry"
	case strings.Contains(h, "litellm"):
		return "litellm-telemetry"
	case strings.Contains(h, "openai") || strings.Contains(h, "openrouter") || strings.Contains(h, "anthropic"):
		return "provider-host"
	case strings.Contains(h, "://"):
		return "scheme-form"
	case strings.Contains(h, ":"):
		return "port-or-ipv6"
	case len(h) > 0 && h[0] >= '0' && h[0] <= '9':
		return "ip-literal"
	default:
		return "other"
	}
}

func advM163EventShape(events []audit.AuditRecord) map[string]bool {
	out := map[string]bool{}
	for _, ev := range events {
		if ev.EventType != "" {
			out[ev.EventType] = true
		}
	}
	return out
}

func advM163PayloadKeys(events []audit.AuditRecord, eventType string) map[string]bool {
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

func advM163EgressDestClass(dest string) string {
	d := strings.ToLower(dest)
	switch {
	case d == "":
		return "empty"
	case strings.Contains(d, "127.0.0.1") || strings.Contains(d, "localhost") || strings.Contains(d, "::1"):
		return "loopback"
	case strings.Contains(d, "openai.com") || strings.Contains(d, "openrouter.ai") ||
		strings.Contains(d, "anthropic.com") || strings.Contains(d, "crewai.com") ||
		strings.Contains(d, "posthog.com") || strings.Contains(d, "sentry.io"):
		return "provider-host"
	default:
		return "other"
	}
}

func advM163EnvHasName(env []string, name string) bool {
	for _, item := range env {
		if advM163EnvName(item) == name {
			return true
		}
	}
	return false
}

func advM163EnvValue(env []string, name string) (string, bool) {
	prefix := name + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix), true
		}
	}
	return "", false
}

// TestADV_M16_3_SC1_LiteLLMPosthogSentryKeysAbsentFromCrewAIWorkloadEnv
// plants CrewAI-adjacent provider/telemetry keys that LiteLLM (CrewAI's
// OpenAI backend) and CrewAI 0.80 PostHog/Sentry clients read from the
// process env even when OPENAI_API_KEY is the dummy loopback value.
// workerEnvOpenAI must not inherit them.
func TestADV_M16_3_SC1_LiteLLMPosthogSentryKeysAbsentFromCrewAIWorkloadEnv(t *testing.T) {
	t.Setenv("LITELLM_API_KEY", advM163LiteLLMSentinel)
	t.Setenv("LITELLM_PROXY_API_KEY", advM163LiteLLMSentinel)
	t.Setenv("POSTHOG_API_KEY", advM163PosthogSentinel)
	t.Setenv("POSTHOG_HOST", "https://app.posthog.com")
	t.Setenv("SENTRY_DSN", advM163SentrySentinel)
	t.Setenv("CREWAI_API_KEY", advM163CrewAISentinel)
	t.Setenv("OPENROUTER_API_KEY", advM163OpenRouterSentinel)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "https://telemetry.crewai.com")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "https://telemetry.sentry.io")

	hostile := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + advM163ProviderSentinel,
		"OPENROUTER_API_KEY=" + advM163OpenRouterSentinel,
		"LITELLM_API_KEY=" + advM163LiteLLMSentinel,
		"LITELLM_PROXY_API_KEY=" + advM163LiteLLMSentinel,
		"LITELLM_LOG=DEBUG",
		"POSTHOG_API_KEY=" + advM163PosthogSentinel,
		"POSTHOG_HOST=https://app.posthog.com",
		"SENTRY_DSN=" + advM163SentrySentinel,
		"SENTRY_ENVIRONMENT=production",
		"CREWAI_API_KEY=" + advM163CrewAISentinel,
		"OTEL_EXPORTER_OTLP_ENDPOINT=https://telemetry.crewai.com",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://app.posthog.com",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=https://telemetry.crewai.com",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://telemetry.sentry.io",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=" + advM163OTelSentinel,
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
	}

	envs := [][]string{
		workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
		workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
	}

	sentinels := advM163Sentinels()
	aliases := advM163CrewAISecretAliases()

	var dummySeen bool
	for _, env := range envs {
		for _, item := range env {
			name := advM163EnvName(item)
			if advM163ContainsSentinel(item, sentinels...) {
				// ADVERSARY BREAK: crewai/litellm/posthog/sentry secret inherited into workload env
				t.Errorf("ADVERSARY BREAK: SC1 planted crewai-adjacent secret survived in workload env name=%s", name)
			}
			if aliases[name] {
				// ADVERSARY BREAK: crewai SDK/telemetry alias not stripped
				t.Errorf("ADVERSARY BREAK: SC1 crewai/litellm/posthog/sentry env %s survived into workload (CrewAI 0.80 LiteLLM/PostHog clients read these)", name)
			}
			lower := strings.ToLower(item)
			for _, host := range []string{"telemetry.crewai.com", "app.posthog.com", "telemetry.sentry.io", "api.crewai.com"} {
				if strings.Contains(lower, host) {
					t.Errorf("ADVERSARY BREAK: SC1 workload env names telemetry host class=%s via %s", advM163HostClass(host), name)
				}
			}
			if strings.HasPrefix(item, "OPENAI_API_KEY=") {
				val := strings.TrimPrefix(item, "OPENAI_API_KEY=")
				if val == "" {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY is empty (CrewAI LLM()/LiteLLM will fall back)")
				}
				if advM163ContainsSentinel(val, sentinels...) {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY equals planted parent secret")
				}
				if val == openaiLoopbackAPIKey {
					dummySeen = true
				}
			}
		}
	}
	if !dummySeen {
		t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key missing from crewai worker env")
	}
}

// TestADV_M16_3_SC1_CrewAILoopbackAuditAndMemoryOmitProviderSecrets
// drives the crewai-shaped chat body through the loopback and asserts
// gateway + planted secrets never appear in handleLLM apiKey, response, or audit.
func TestADV_M16_3_SC1_CrewAILoopbackAuditAndMemoryOmitProviderSecrets(t *testing.T) {
	const gatewaySidecar = "gateway-sidecar-not-env-sentinel-m163"
	sentinels := advM163Sentinels()
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		if advM163ContainsSentinel(apiKey, sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted env secret reached handleLLM apiKey")
		}
		if apiKey == openaiLoopbackAPIKey {
			t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key forwarded as handleLLM apiKey")
		}
		if apiKey != gatewaySidecar {
			t.Errorf("ADVERSARY BREAK: SC1 handleLLM apiKey was not the gateway sidecar credential")
		}
		if advM163ContainsSentinel(prompt, sentinels...) {
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

	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(advM163CrewAIChatBody()))
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
	if advM163ContainsSentinel(string(raw), sentinels...) || strings.Contains(string(raw), gatewaySidecar) {
		t.Errorf("ADVERSARY BREAK: SC1 planted or gateway secret reflected in crewai loopback response")
	}

	for _, event := range recorder.events() {
		b, _ := json.Marshal(event)
		blob := string(b)
		if advM163ContainsSentinel(blob, sentinels...) {
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

// TestADV_M16_3_SC3_CrewAIDirectProviderAndTelemetryHostDenied is the
// required golden-level default-deny case. Never dials the public network.
// Worker TestM163DirectProviderHostDenied only covers openai/openrouter/
// anthropic/azure/telemetry.crewai.com/app.posthog.com/api.crewai.com via
// pydanticDirectHostDenied — not Sentry ingest, PostHog regional, LiteLLM
// collector, scheme/port forms, or the fact AGENTPAAS_EGRESS_DENY is unread.
func TestADV_M16_3_SC3_CrewAIDirectProviderAndTelemetryHostDenied(t *testing.T) {
	forbidden := []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"telemetry.crewai.com",
		"app.posthog.com",
		"api.crewai.com",
		"telemetry.sentry.io",
		"us.i.posthog.com",
		"eu.i.posthog.com",
		"o447951.ingest.sentry.io",
	}

	env := workerEnvOpenAI([]string{
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"LITELLM_API_BASE=https://api.openai.com/v1",
		"POSTHOG_HOST=https://app.posthog.com",
		"OTEL_EXPORTER_OTLP_ENDPOINT=https://telemetry.crewai.com",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=https://telemetry.sentry.io",
		"OPENAI_API_KEY=" + advM163ProviderSentinel,
		"CREWAI_API_KEY=" + advM163CrewAISentinel,
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
				t.Errorf("ADVERSARY BREAK: SC3 crewai worker env still names provider/telemetry host (env name=%s class=%s)", advM163EnvName(item), advM163HostClass(host))
			}
		}
	}
	if !denyMarker {
		t.Errorf("ADVERSARY BREAK: SC3 crewai worker env has no deny/pin; OPENAI_BASE_URL rewrite is advisory only")
	}
	if !loopbackBase {
		t.Errorf("ADVERSARY BREAK: SC3 crewai OPENAI_BASE_URL was not rewritten to loopback")
	}

	// Env flags are not deny. A CrewAI LLM()/LiteLLM client that ignores
	// base_url and hits the SDK default host, or a PostHog/Sentry SDK that
	// phones home, must be fail-closed without a public dial.
	// AGENTPAAS_EGRESS_DENY is unread; loopbackDeniedProviderHosts is unused
	// by serveHTTP.
	for _, host := range []string{
		"telemetry.sentry.io",
		"us.i.posthog.com",
		"eu.i.posthog.com",
		"o447951.ingest.sentry.io",
		"crewai.com",
		"litellm.vercel.app",
		"https://api.openai.com",
		"api.openai.com:443",
		"telemetry.crewai.com:4318",
	} {
		if !pydanticDirectHostDenied(host) {
			// ADVERSARY BREAK: no harness predicate denies crewai direct provider/telemetry host
			t.Errorf("ADVERSARY BREAK: SC3 crewai golden has no pre-dial deny for host-class=%s (AGENTPAAS_EGRESS_DENY is unread; loopbackDeniedProviderHosts unused by serveHTTP)", advM163HostClass(host))
		}
	}

	for _, host := range forbidden {
		if host == "" {
			t.Fatalf("empty forbidden host")
		}
	}
}

// TestADV_M16_3_SC3_TrailingDotCaseSchemePortAndIPBypassMustDeny
// hostname-string allowlists miss these. Never dials.
func TestADV_M16_3_SC3_TrailingDotCaseSchemePortAndIPBypassMustDeny(t *testing.T) {
	variants := []string{
		"telemetry.crewai.com.",
		"TELEMETRY.CREWAI.COM",
		"app.posthog.com.",
		"App.PostHog.Com",
		"api.crewai.com.",
		"API.CREWAI.COM",
		"https://telemetry.crewai.com",
		"http://app.posthog.com/capture",
		"telemetry.crewai.com:4318",
		"telemetry.crewai.com:4317",
		"app.posthog.com:443",
	}
	for _, host := range variants {
		if !pydanticDirectHostDenied(host) {
			t.Errorf("ADVERSARY BREAK: SC3 crewai direct-host variant not denied host-class=%s", advM163HostClass(host))
		}
	}
}

// TestADV_M16_3_SC5_OTelSdkDisabledAndSiblingTelemetryEnvKilled
// SC5 is "telemetry domains are not in egress and the disable flag is set
// at pack." Worker TestM163CrewAITelemetryOff only asserts
// CREWAI_DISABLE_TELEMETRY=true and strips OTEL_EXPORTER_OTLP_ENDPOINT.
// CrewAI 0.80 moved to OpenTelemetry/PostHog; OTEL_SDK_DISABLED and
// sibling exporters must also be killed or the disable flag is advisory.
func TestADV_M16_3_SC5_OTelSdkDisabledAndSiblingTelemetryEnvKilled(t *testing.T) {
	env := workerEnvOpenAI([]string{
		"PATH=/usr/bin",
		"CREWAI_DISABLE_TELEMETRY=false",
		"OTEL_SDK_DISABLED=false",
		"POSTHOG_HOST=https://us.i.posthog.com",
		"POSTHOG_API_KEY=" + advM163PosthogSentinel,
		"SENTRY_DSN=" + advM163SentrySentinel,
		"OTEL_EXPORTER_OTLP_ENDPOINT=https://telemetry.crewai.com",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=https://telemetry.crewai.com:4318",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://telemetry.sentry.io",
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")

	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "CREWAI_DISABLE_TELEMETRY=true") {
		t.Errorf("ADVERSARY BREAK: SC5 workerEnvOpenAI missing CREWAI_DISABLE_TELEMETRY=true")
	}
	if val, ok := advM163EnvValue(env, "CREWAI_DISABLE_TELEMETRY"); ok {
		if val == "" || strings.EqualFold(val, "false") || val == "0" || strings.EqualFold(val, "no") {
			t.Errorf("ADVERSARY BREAK: SC5 CREWAI_DISABLE_TELEMETRY is not a disabling value")
		}
	}
	if !advM163EnvHasName(env, "OTEL_SDK_DISABLED") {
		// ADVERSARY BREAK: pack/harness never sets OTEL_SDK_DISABLED (CrewAI OTel still exports)
		t.Errorf("ADVERSARY BREAK: SC5 worker env missing OTEL_SDK_DISABLED=true (CREWAI_DISABLE_TELEMETRY alone does not stop OTel/PostHog exporters)")
	} else if val, _ := advM163EnvValue(env, "OTEL_SDK_DISABLED"); !strings.EqualFold(val, "true") && val != "1" {
		t.Errorf("ADVERSARY BREAK: SC5 OTEL_SDK_DISABLED is not a disabling value")
	}

	for _, name := range []string{"POSTHOG_HOST", "POSTHOG_API_KEY", "SENTRY_DSN", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"} {
		if advM163EnvHasName(env, name) {
			t.Errorf("ADVERSARY BREAK: SC5 sibling telemetry env %s survived into crewai workload", name)
		}
	}
	lower := strings.ToLower(joined)
	for _, host := range []string{"telemetry.crewai.com", "app.posthog.com", "us.i.posthog.com", "telemetry.sentry.io", "api.crewai.com"} {
		if strings.Contains(lower, host) {
			t.Errorf("ADVERSARY BREAK: SC5 worker env still names telemetry host class=%s", advM163HostClass(host))
		}
	}
	if !pydanticDirectHostDenied("telemetry.sentry.io") || !pydanticDirectHostDenied("us.i.posthog.com") {
		t.Errorf("ADVERSARY BREAK: SC5 harness deny list omits Sentry/PostHog regional telemetry hosts")
	}
}

// TestADV_M16_3_SC6_CrewAIAuditShapeMatchesWeatherAgentReference
// compares LLM/egress event types and payload keys produced by the crewai
// loopback path (chat + GET /v1/models that CrewAI LLM()/LiteLLM issues)
// against the handleLLM shape the reference weather agent uses.
func TestADV_M16_3_SC6_CrewAIAuditShapeMatchesWeatherAgentReference(t *testing.T) {
	const gatewaySidecar = "gateway-sidecar-not-env-sentinel-m163"
	crewRec := &recordingAuditAppender{}
	crewRPC := &harnessRPCServer{audit: crewRec}
	crewRPC.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		return &llm.LLMResult{Text: "Folsom", Tokens: 2, Model: model, InputTokens: 8, OutputTokens: 2}, nil
	}
	crewRPC.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	crewRPC.SetInvoke(map[string]any{
		"llm":           map[string]any{"provider": "openrouter", "model": "deepseek/deepseek-v4-flash", "credential": testCredID},
		"observability": map[string]any{"cost_tracking": true},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb, err := startOpenAILoopback(crewRPC)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	defer func() { _ = lb.Close() }()

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+"/v1/chat/completions", bytes.NewReader(advM163CrewAIChatBody()))
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
	if advM163ContainsSentinel(string(raw), advM163Sentinels()...) || strings.Contains(string(raw), gatewaySidecar) {
		t.Errorf("ADVERSARY BREAK: SC6 crewai loopback response leaked credential material")
	}

	afterChat := advM163EventShape(crewRec.events())

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

	crewShape := advM163EventShape(crewRec.events())
	refShape := advM163EventShape(refRec.events())

	if !crewShape["llm_result"] {
		t.Errorf("ADVERSARY BREAK: SC6 crewai loopback path missing llm_result (reference weather agent emits it)")
	}
	if !crewShape["egress_allowed"] && !crewShape["egress_denied"] {
		t.Errorf("ADVERSARY BREAK: SC6 crewai loopback path missing egress_* event (reference weather agent emits egress_allowed/denied)")
	}
	for k := range refShape {
		if !crewShape[k] {
			t.Errorf("ADVERSARY BREAK: SC6 crewai audit missing reference event_type=%s", k)
		}
	}
	for k := range crewShape {
		if !refShape[k] {
			// ADVERSARY BREAK: crewai-only audit event class (GET /v1/models)
			t.Errorf("ADVERSARY BREAK: SC6 crewai audit extra event_type=%s not in weather-agent reference (CrewAI LLM() GET /v1/models is not weather-shaped)", k)
		}
	}

	crewLLMKeys := advM163PayloadKeys(crewRec.events(), "llm_result")
	refLLMKeys := advM163PayloadKeys(refRec.events(), "llm_result")
	for _, k := range advM163ReferenceLLMKeys() {
		if !crewLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 crewai llm_result missing field %s", k)
		}
		if refLLMKeys[k] && !crewLLMKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 crewai llm_result field set diverges from weather-agent reference field=%s", k)
		}
	}
	crewEgressKeys := advM163PayloadKeys(crewRec.events(), "egress_allowed")
	if len(crewEgressKeys) == 0 {
		crewEgressKeys = advM163PayloadKeys(crewRec.events(), "egress_denied")
	}
	for _, k := range advM163ReferenceEgressKeys() {
		if !crewEgressKeys[k] {
			t.Errorf("ADVERSARY BREAK: SC6 crewai egress event missing field %s", k)
		}
	}

	for _, event := range crewRec.events() {
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), gatewaySidecar) || strings.Contains(string(b), openaiLoopbackAPIKey) {
			t.Errorf("ADVERSARY BREAK: SC6 crewai audit leaked credential material event_type=%s", event.EventType)
		}
		if event.EventType == "egress_allowed" || event.EventType == "egress_denied" {
			dest, _ := event.Payload["destination"].(string)
			if destClass := advM163EgressDestClass(dest); destClass == "provider-host" {
				// ADVERSARY BREAK: loopback LLM hop audited as public provider destination
				t.Errorf("ADVERSARY BREAK: SC6 crewai loopback LLM egress destination class=provider-host (want loopback); actual packets are 127.0.0.1")
			}
			if dest == "" {
				t.Errorf("ADVERSARY BREAK: SC6 crewai egress event missing destination")
			}
		}
		if event.EventType == "egress_loopback_models" {
			// ADVERSARY BREAK: models probe is not a weather-agent LLM/egress event
			t.Errorf("ADVERSARY BREAK: SC6 crewai GET /v1/models audited as egress_loopback_models (weather-agent analog has llm_result/egress_allowed|denied only)")
		}
	}

	if modelsResp.StatusCode == http.StatusOK {
		modelsWeather := false
		for _, event := range crewRec.events() {
			if !afterChat[event.EventType] && (event.EventType == "llm_result" || event.EventType == "egress_allowed" || event.EventType == "egress_denied") {
				modelsWeather = true
			}
		}
		if !modelsWeather {
			// ADVERSARY BREAK: CrewAI LLM() GET /v1/models is not weather-class audited
			t.Errorf("ADVERSARY BREAK: SC6 crewai GET /v1/models produced no additional weather-class LLM/egress audit event")
		}
	}
}
