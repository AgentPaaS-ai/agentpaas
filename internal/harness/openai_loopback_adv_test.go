package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
)

// Synthetic sentinels — never print the raw value in failure messages.
const (
	advM160ProviderSentinel  = "ADV-SYNTHETIC-PROVIDER-KEY-M160"
	advM160AnthropicSentinel = "sk-ant-ADV-SYNTHETIC-M160-not-a-real-key"
	advM160AttackerBearer    = "sk-attacker-ADV-M160-passthrough"
)

func advEnvName(item string) string {
	if i := strings.IndexByte(item, '='); i >= 0 {
		return item[:i]
	}
	return item
}

func advContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

func advLoopbackPOST(t *testing.T, lb *openaiLoopback, path string, auth string, extraHeaders map[string]string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+lb.Addr()+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	return resp
}

func advReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(raw)
}

func advStartLoopback(t *testing.T, rpc *harnessRPCServer) *openaiLoopback {
	t.Helper()
	lb, err := startOpenAILoopback(rpc)
	if err != nil {
		t.Fatalf("startOpenAILoopback: %v", err)
	}
	t.Cleanup(func() { _ = lb.Close() })
	return lb
}

func advChatBody(stream bool) []byte {
	msg := map[string]any{
		"model": "gpt-4o",
		"messages": []map[string]any{
			{"role": "user", "content": "adv-m16-0"},
		},
	}
	if stream {
		msg["stream"] = true
	}
	raw, _ := json.Marshal(msg)
	return raw
}

func TestADV_M16_0_SC1_ProviderKeyAbsentFromWorkerEnvMemoryAudit(t *testing.T) {
	// python_worker.go:152 feeds os.Environ() into workerEnvOpenAI.
	// isWorkloadOpenAIEnv only strips OPENAI_API_KEY / OPENAI_BASE_URL / OPENAI_API_BASE.
	t.Setenv("OPENAI_API_KEY", advM160ProviderSentinel)
	t.Setenv("ANTHROPIC_API_KEY", advM160AnthropicSentinel)
	t.Setenv("AZURE_OPENAI_API_KEY", advM160ProviderSentinel)
	t.Setenv("GOOGLE_API_KEY", advM160ProviderSentinel)
	t.Setenv("GEMINI_API_KEY", advM160ProviderSentinel)

	hostile := []string{
		"PATH=/usr/bin",
		"OPENAI_API_KEY=" + advM160ProviderSentinel,
		"OPENAI_API_KEY=" + advM160ProviderSentinel, // duplicate — do not collapse to map
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENAI_API_BASE=https://api.openai.com/v1",
		"ANTHROPIC_API_KEY=" + advM160AnthropicSentinel,
		"AZURE_OPENAI_API_KEY=" + advM160ProviderSentinel,
		"GOOGLE_API_KEY=" + advM160ProviderSentinel,
		"GEMINI_API_KEY=" + advM160ProviderSentinel,
		"openai_api_key=" + advM160ProviderSentinel, // case variant
		"OpenAI_API_KEY=" + advM160ProviderSentinel,
	}

	envs := [][]string{
		workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
		workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
	}

	sentinels := []string{advM160ProviderSentinel, advM160AnthropicSentinel}
	var dummySeen bool
	for _, env := range envs {
		for _, item := range env { // preserve duplicates; do not map-collapse
			if advContainsSentinel(item, sentinels...) {
				// ADVERSARY BREAK: sibling/alias provider keys inherit into workload env
				t.Errorf("ADVERSARY BREAK: SC1 planted provider secret survived in workload env name=%s", advEnvName(item))
			}
			if strings.HasPrefix(item, "OPENAI_API_KEY=") {
				val := strings.TrimPrefix(item, "OPENAI_API_KEY=")
				if val == "" {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY is empty (SDK will fall back)")
				}
				if advContainsSentinel(val, sentinels...) {
					t.Errorf("ADVERSARY BREAK: SC1 dummy OPENAI_API_KEY equals planted parent secret")
				}
				if val == openaiLoopbackAPIKey {
					dummySeen = true
				}
			}
		}
	}
	if !dummySeen {
		t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key missing from worker env")
	}

	const gatewaySidecar = "gateway-sidecar-not-env-sentinel"
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		if advContainsSentinel(apiKey, sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted env secret reached handleLLM apiKey")
		}
		if apiKey == openaiLoopbackAPIKey {
			t.Errorf("ADVERSARY BREAK: SC1 dummy loopback key forwarded as handleLLM apiKey")
		}
		return &llm.LLMResult{Text: "ok", Tokens: 1, Model: "gpt-4o"}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: gatewaySidecar},
	})
	rpc.SetInvoke(map[string]any{
		"llm": map[string]any{"provider": "openai", "model": "gpt-4o", "credential": testCredID},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb := advStartLoopback(t, rpc)
	resp := advLoopbackPOST(t, lb, "/v1/chat/completions", "Bearer "+openaiLoopbackAPIKey, nil, advChatBody(false))
	body := advReadBody(t, resp)
	if advContainsSentinel(body, sentinels...) {
		t.Errorf("ADVERSARY BREAK: SC1 planted secret reflected in loopback response body")
	}
	for _, event := range recorder.events() {
		raw, _ := json.Marshal(event)
		if advContainsSentinel(string(raw), sentinels...) {
			t.Errorf("ADVERSARY BREAK: SC1 planted secret leaked into audit event_type=%s", event.EventType)
		}
	}
}

func TestADV_M16_0_SC2_NoWildcardBindNoLocalhostAliasNoIPv6Any(t *testing.T) {
	rpc := &harnessRPCServer{}
	lb := advStartLoopback(t, rpc)

	host, port, err := net.SplitHostPort(lb.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		t.Fatalf("ADVERSARY BREAK: SC2 wildcard bind host=%q", host)
	}
	if host != "127.0.0.1" {
		// ADVERSARY BREAK: advertised bind is not canonical IPv4 loopback
		t.Errorf("ADVERSARY BREAK: SC2 bind host=%q want 127.0.0.1 (localhost/::1 alias rejected)", host)
	}
	if port == "" || port == "0" {
		t.Errorf("ADVERSARY BREAK: SC2 ephemeral port missing")
	}

	base := lb.baseURL()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse baseURL: %v", err)
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Errorf("ADVERSARY BREAK: SC2 advertised baseURL=%q is not http://127.0.0.1", base)
	}
	if strings.EqualFold(u.Hostname(), "localhost") || u.Hostname() == "0.0.0.0" || u.Hostname() == "::1" {
		t.Errorf("ADVERSARY BREAK: SC2 advertised host alias %q", u.Hostname())
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Errorf("ADVERSARY BREAK: SC2 baseURL has userinfo/query/fragment")
	}

	env := workerEnvOpenAI(nil, "127.0.0.1:1", base)
	for _, item := range env {
		if strings.HasPrefix(item, "OPENAI_BASE_URL=") || strings.HasPrefix(item, "OPENAI_API_BASE=") {
			val := item[strings.IndexByte(item, '=')+1:]
			uu, perr := url.Parse(val)
			if perr != nil || uu.Hostname() != "127.0.0.1" {
				t.Errorf("ADVERSARY BREAK: SC2 worker env %s host is not 127.0.0.1", advEnvName(item))
			}
		}
	}

	// Residual: bind-only is not a RemoteAddr filter. Outside-container claim
	// needs ConnContext/BaseContext (or tcp4 + explicit peer check).
	if lb.server == nil || (lb.server.ConnContext == nil && lb.server.BaseContext == nil) {
		// ADVERSARY BREAK: no RemoteAddr filter on loopback http.Server
		t.Errorf("ADVERSARY BREAK: SC2 loopback http.Server has no ConnContext/BaseContext RemoteAddr filter")
	}
}

func TestADV_M16_0_SC3_BypassLoopbackToProviderHostDenied(t *testing.T) {
	// Harness-level named test. Not a golden. Never dial the public network.
	// Property: a workload that ignores OPENAI_BASE_URL and targets a provider
	// host is default-denied. BASE_URL rewrite alone is not deny.
	forbidden := []string{
		"api.openai.com",
		"api.anthropic.com",
		"openai.azure.com",
		"generativelanguage.googleapis.com",
	}

	t.Setenv("OPENAI_API_KEY", advM160ProviderSentinel)
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1")

	env := workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1")
	env = append(env, workerEnvOpenAI([]string{
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
		"AZURE_OPENAI_ENDPOINT=https://contoso.openai.azure.com",
	}, "127.0.0.1:1", "http://127.0.0.1:54321/v1")...)

	var denyMarker bool
	for _, item := range env {
		upper := strings.ToUpper(item)
		if strings.Contains(upper, "AGENTPAAS_DENY") ||
			strings.Contains(upper, "FORBID_HOST") ||
			strings.Contains(upper, "EGRESS_DENY") ||
			strings.Contains(upper, "LOOPBACK_PIN") {
			denyMarker = true
		}
	}

	// A real default-deny would expose a harness predicate. Absence is the break.
	if !denyMarker {
		// ADVERSARY BREAK: no harness default-deny of provider-host bypass
		t.Errorf("ADVERSARY BREAK: SC3 worker env has no deny/pin for provider-host bypass; OPENAI_BASE_URL rewrite is advisory only")
	}

	// Confirm we are not claiming a network timeout as deny: no dial is issued.
	for _, host := range forbidden {
		if host == "" {
			t.Fatalf("empty forbidden host")
		}
	}

	rpc := &harnessRPCServer{}
	lb := advStartLoopback(t, rpc)
	_ = lb
	if denyProviderHostBypassExists() {
		return
	}
	// ADVERSARY BREAK: missing harness-level provider-host default-deny
	t.Errorf("ADVERSARY BREAK: SC3 no harness predicate denies bypass to api.openai.com/api.anthropic.com/openai.azure.com/generativelanguage.googleapis.com")
}

// denyProviderHostBypassExists is the SC3 control the worker did not ship.
// Always false on this construction — kept as a named seam so the fix worker
// can flip it without rewriting the test.
func denyProviderHostBypassExists() bool {
	return loopbackDeniesProviderHostBypass()
}

func TestADV_M16_0_V1_ProxyEnvEscapeBypassesLoopback(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:9")
	t.Setenv("http_proxy", "http://127.0.0.1:9")
	t.Setenv("https_proxy", "http://127.0.0.1:9")
	t.Setenv("all_proxy", "socks5://127.0.0.1:9")

	hostile := []string{
		"PATH=/usr/bin",
		"HTTP_PROXY=http://127.0.0.1:9",
		"HTTPS_PROXY=http://127.0.0.1:9",
		"ALL_PROXY=socks5://127.0.0.1:9",
		"http_proxy=http://127.0.0.1:9",
		"https_proxy=http://127.0.0.1:9",
		"all_proxy=socks5://127.0.0.1:9",
		"NO_PROXY=",
	}

	envs := [][]string{
		workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
		workerEnvOpenAI(os.Environ(), "127.0.0.1:1", "http://127.0.0.1:54321/v1"),
	}

	proxyNames := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true,
		"NO_PROXY": true, "no_proxy": true,
	}
	for _, env := range envs {
		var forcedNoProxyAll bool
		for _, item := range env {
			name := advEnvName(item)
			if !proxyNames[name] {
				continue
			}
			val := ""
			if i := strings.IndexByte(item, '='); i >= 0 {
				val = item[i+1:]
			}
			if name == "NO_PROXY" || name == "no_proxy" {
				if val == "*" {
					forcedNoProxyAll = true
				}
				continue
			}
			if val != "" {
				// ADVERSARY BREAK: proxy env inherited into workload
				t.Errorf("ADVERSARY BREAK: V1 proxy env %s survived into workload (loopback bypass channel)", name)
			}
		}
		_ = forcedNoProxyAll
	}
}

func TestADV_M16_0_V2_NonDummyAuthorizationMustNotPassthrough(t *testing.T) {
	var seenAPIKey string
	var reached bool
	recorder := &recordingAuditAppender{}
	rpc := &harnessRPCServer{audit: recorder}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		reached = true
		seenAPIKey = apiKey
		return &llm.LLMResult{Text: "ok", Tokens: 1}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: "gateway-sidecar-not-dummy"},
	})
	rpc.SetInvoke(map[string]any{
		"llm": map[string]any{"provider": "openai", "model": "gpt-4o", "credential": testCredID},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb := advStartLoopback(t, rpc)

	// Planted real/attacker bearer must not reach handleLLM.
	resp := advLoopbackPOST(t, lb, "/v1/chat/completions", "Bearer "+advM160AttackerBearer, nil, advChatBody(false))
	body := advReadBody(t, resp)
	if reached {
		t.Errorf("ADVERSARY BREAK: V2 non-dummy Authorization reached handleLLM")
	}
	if advContainsSentinel(body, advM160AttackerBearer, advM160ProviderSentinel) {
		t.Errorf("ADVERSARY BREAK: V2 planted bearer reflected in error body")
	}
	if resp.StatusCode == http.StatusOK {
		t.Errorf("ADVERSARY BREAK: V2 non-dummy Authorization accepted")
	}

	reached = false
	seenAPIKey = ""
	resp2 := advLoopbackPOST(t, lb, "/v1/chat/completions", "Bearer "+openaiLoopbackAPIKey, map[string]string{
		"X-Api-Key":           advM160AttackerBearer,
		"Proxy-Authorization": "Bearer " + advM160AttackerBearer,
		"X-OpenAI-Api-Key":    advM160AttackerBearer,
	}, advChatBody(false))
	body2 := advReadBody(t, resp2)
	if advContainsSentinel(body2, advM160AttackerBearer) {
		t.Errorf("ADVERSARY BREAK: V2 extra credential headers reflected")
	}
	if advContainsSentinel(seenAPIKey, advM160AttackerBearer) {
		t.Errorf("ADVERSARY BREAK: V2 planted header forwarded as handleLLM apiKey")
	}
	for _, event := range recorder.events() {
		raw, _ := json.Marshal(event)
		if advContainsSentinel(string(raw), advM160AttackerBearer, advM160ProviderSentinel) {
			t.Errorf("ADVERSARY BREAK: V2 planted bearer leaked into audit")
		}
	}
}

func TestADV_M16_0_V3_StreamTrueMustNotForceProviderFallback(t *testing.T) {
	rpc := &harnessRPCServer{}
	lb := advStartLoopback(t, rpc)

	resp := advLoopbackPOST(t, lb, "/v1/chat/completions", "Bearer "+openaiLoopbackAPIKey, nil, advChatBody(true))
	body := advReadBody(t, resp)
	if advContainsSentinel(body, advM160ProviderSentinel, advM160AnthropicSentinel, "api.openai.com", "api.anthropic.com") {
		t.Errorf("ADVERSARY BREAK: V3 stream:true error leaked secret or provider host")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("ADVERSARY BREAK: V3 stream:true status=%d want 400", resp.StatusCode)
	}

	// Generic OpenAI invalid_request_error with no loopback pin teaches SDKs
	// to retry the default provider host. That is the fallback break.
	pin := resp.Header.Get("X-AgentPaaS-Loopback")
	if pin == "" {
		pin = resp.Header.Get("X-AgentPaaS-Egress")
	}
	if !strings.Contains(body, "loopback") && pin == "" {
		// ADVERSARY BREAK: stream 400 is unpinned; SDK may retry default host
		t.Errorf("ADVERSARY BREAK: V3 stream:true 400 has no loopback-only pin (header or error type); SDK may fall back to provider host")
	}
}

func TestADV_M16_0_V4_InheritedProviderEndpointEnvBypassesLoopback(t *testing.T) {
	hostile := []string{
		"PATH=/usr/bin",
		"AZURE_OPENAI_ENDPOINT=https://contoso.openai.azure.com",
		"AZURE_OPENAI_API_KEY=" + advM160ProviderSentinel,
		"ANTHROPIC_BASE_URL=https://api.anthropic.com",
		"ANTHROPIC_API_KEY=" + advM160AnthropicSentinel,
		"OPENAI_API_HOST=api.openai.com",
		"GEMINI_API_ENDPOINT=https://generativelanguage.googleapis.com",
		"GOOGLE_API_KEY=" + advM160ProviderSentinel,
	}
	env := workerEnvOpenAI(hostile, "127.0.0.1:1", "http://127.0.0.1:54321/v1")
	for _, item := range env {
		name := advEnvName(item)
		switch name {
		case "AZURE_OPENAI_ENDPOINT", "ANTHROPIC_BASE_URL", "OPENAI_API_HOST", "GEMINI_API_ENDPOINT":
			// ADVERSARY BREAK: provider endpoint env not rewritten to loopback
			t.Errorf("ADVERSARY BREAK: V4 inherited provider endpoint %s survived (OPENAI_BASE_URL rewrite does not cover this SDK)", name)
		}
		if advContainsSentinel(item, advM160ProviderSentinel, advM160AnthropicSentinel) {
			t.Errorf("ADVERSARY BREAK: V4 planted secret survived in %s", name)
		}
	}
}

func TestADV_M16_0_V5_StaticDummyKeyConfusedDeputy(t *testing.T) {
	rpc := &harnessRPCServer{}
	lb1 := advStartLoopback(t, rpc)
	lb2 := advStartLoopback(t, rpc)
	_ = lb1
	_ = lb2

	if openaiLoopbackAPIKey == "" {
		t.Fatal("ADVERSARY BREAK: V5 dummy key empty")
	}
	if openaiLoopbackAPIKey == "sk-agentpaas-loopback" {
		// ADVERSARY BREAK: dummy is a repo-visible process-global const
		t.Errorf("ADVERSARY BREAK: V5 dummy API key is a static well-known const; any local PID can mint Bearer during an active invoke")
	}

	env1 := workerEnvOpenAI(nil, "127.0.0.1:1", lb1.baseURL())
	env2 := workerEnvOpenAI(nil, "127.0.0.1:1", lb2.baseURL())
	var k1, k2 string
	for _, item := range env1 {
		if strings.HasPrefix(item, "OPENAI_API_KEY=") {
			k1 = strings.TrimPrefix(item, "OPENAI_API_KEY=")
		}
	}
	for _, item := range env2 {
		if strings.HasPrefix(item, "OPENAI_API_KEY=") {
			k2 = strings.TrimPrefix(item, "OPENAI_API_KEY=")
		}
	}
	if k1 != "" && k1 == k2 && k1 == openaiLoopbackAPIKey {
		t.Errorf("ADVERSARY BREAK: V5 two loopback starts share the same dummy key (not a per-start nonce)")
	}
}

func TestADV_M16_0_V6_AlternateOpenAIRoutesMustPinOrRewrite(t *testing.T) {
	var reached int
	rpc := &harnessRPCServer{}
	rpc.llmChatCompletion = func(ctx context.Context, baseURL, originalHost, apiKey, model, prompt string, maxTokens int, provider string) (*llm.LLMResult, error) {
		reached++
		return &llm.LLMResult{Text: "ok", Tokens: 1}, nil
	}
	rpc.SetCredentialsForTest(map[string]rpcCredential{
		testCredID: {Header: "Authorization", Value: "gateway-sidecar-not-dummy"},
	})
	rpc.SetInvoke(map[string]any{
		"llm": map[string]any{"provider": "openai", "model": "gpt-4o", "credential": testCredID},
	}, NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}), nil)

	lb := advStartLoopback(t, rpc)
	auth := "Bearer " + openaiLoopbackAPIKey
	body := advChatBody(false)

	paths := []string{
		"/v1/chat/completions/",
		"/v1/completions",
		"/v1/embeddings",
		"/v1/models",
		"/v1/messages",
		"/v1/responses",
	}
	for _, p := range paths {
		reachedBefore := reached
		resp := advLoopbackPOST(t, lb, p, auth, nil, body)
		raw := advReadBody(t, resp)
		rewritten := reached > reachedBefore
		pinned := resp.Header.Get("X-AgentPaaS-Loopback") != "" || strings.Contains(raw, "loopback")
		if !rewritten && resp.StatusCode == http.StatusNotFound && !pinned {
			// ADVERSARY BREAK: alternate OpenAI route 404s without rewrite or pin
			t.Errorf("ADVERSARY BREAK: V6 path %s returned unpinned 404 (not rewritten through handleLLM); SDK may fall back to provider host", p)
		}
		if advContainsSentinel(raw, advM160ProviderSentinel, "api.openai.com") {
			t.Errorf("ADVERSARY BREAK: V6 path %s leaked secret or provider host", p)
		}
	}
}
