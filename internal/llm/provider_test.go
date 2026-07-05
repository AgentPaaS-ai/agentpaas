package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetAdapter_Valid(t *testing.T) {
	for _, p := range []string{"openai", "anthropic", "xai", "xiai"} {
		adapter := GetAdapter(p)
		if adapter == nil {
			t.Errorf("GetAdapter(%q) returned nil", p)
		}
	}
}

func TestGetAdapter_Unknown(t *testing.T) {
	if a := GetAdapter("unknown"); a != nil {
		t.Errorf("GetAdapter(%q) = %v, want nil", "unknown", a)
	}
	if a := GetAdapter(""); a != nil {
		t.Errorf("GetAdapter(%q) = %v, want nil", "", a)
	}
}

func TestSupportedProviders(t *testing.T) {
	providers := SupportedProviders()
	if len(providers) != 5 {
		t.Fatalf("SupportedProviders() len = %d, want 5", len(providers))
	}
	expected := map[string]bool{"openrouter": true, "openai": true, "anthropic": true, "xai": true, "nous": true}
	for _, p := range providers {
		if !expected[p] {
			t.Errorf("unexpected provider %q in SupportedProviders()", p)
		}
	}
}

func TestOpenAIAdapter_BuildRequest(t *testing.T) {
	adapter := &openAIAdapter{}

	// Set test endpoint
	restore := SetTestEndpoints("https://example.com/openai", anthropicEndpoint, xaiEndpoint)
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "gpt-4", "Hello, world!", "sk-test-key-123")
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}

	// Check URL
	if req.URL.String() != "https://example.com/openai" {
		t.Errorf("URL = %q, want %q", req.URL.String(), "https://example.com/openai")
	}

	// Check method
	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want %q", req.Method, http.MethodPost)
	}

	// Check Authorization header
	auth := req.Header.Get("Authorization")
	if auth != "Bearer sk-test-key-123" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-test-key-123")
	}

	// Check Content-Type
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Check body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != "gpt-4" {
		t.Errorf("body.model = %v, want %q", body["model"], "gpt-4")
	}
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatal("body.messages missing or empty")
	}
	msg := messages[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Errorf("msg.role = %v, want user", msg["role"])
	}
	if msg["content"] != "Hello, world!" {
		t.Errorf("msg.content = %v, want %q", msg["content"], "Hello, world!")
	}
}

func TestOpenAIAdapter_BuildRequest_EmptyModel(t *testing.T) {
	adapter := &openAIAdapter{}
	restore := SetTestEndpoints("https://example.com/openai", anthropicEndpoint, xaiEndpoint)
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "", "prompt", "key")
	if err != nil {
		t.Fatalf("BuildRequest with empty model: %v", err)
	}
	bodyBytes, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != defaultOpenAIModel {
		t.Errorf("empty model default = %v, want %q", body["model"], defaultOpenAIModel)
	}
}

func TestOpenAIAdapter_ParseResponse_Success(t *testing.T) {
	adapter := &openAIAdapter{}
	body := `{"choices":[{"message":{"content":"Hello back!"}}],"usage":{"total_tokens":42},"model":"gpt-4"}`
	result, err := adapter.ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if result.Text != "Hello back!" {
		t.Errorf("Text = %q, want %q", result.Text, "Hello back!")
	}
	if result.Tokens != 42 {
		t.Errorf("Tokens = %d, want 42", result.Tokens)
	}
	if result.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", result.Model, "gpt-4")
	}
}

func TestOpenAIAdapter_ParseResponse_EmptyChoices(t *testing.T) {
	adapter := &openAIAdapter{}
	body := `{"choices":[],"usage":{"total_tokens":0},"model":"gpt-4"}`
	result, err := adapter.ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatalf("ParseResponse with empty choices: %v", err)
	}
	if result.Text != "" {
		t.Errorf("Text = %q, want empty", result.Text)
	}
}

func TestOpenAIAdapter_ParseResponse_ErrorStatus(t *testing.T) {
	adapter := &openAIAdapter{}
	_, err := adapter.ParseResponse(401, []byte(`{"error":"unauthorized"}`))
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestOpenAIAdapter_ParseResponse_MalformedJSON(t *testing.T) {
	adapter := &openAIAdapter{}
	_, err := adapter.ParseResponse(200, []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestAnthropicAdapter_BuildRequest(t *testing.T) {
	adapter := &anthropicAdapter{}
	restore := SetTestEndpoints(openAIEndpoint, "https://example.com/anthropic", xaiEndpoint)
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "claude-3", "Hi there", "sk-ant-key-456")
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}

	// Check URL
	if req.URL.String() != "https://example.com/anthropic" {
		t.Errorf("URL = %q, want %q", req.URL.String(), "https://example.com/anthropic")
	}

	// Check method
	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want %q", req.Method, http.MethodPost)
	}

	// Check headers
	if req.Header.Get("x-api-key") != "sk-ant-key-456" {
		t.Errorf("x-api-key = %q, want %q", req.Header.Get("x-api-key"), "sk-ant-key-456")
	}
	if req.Header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", req.Header.Get("anthropic-version"), "2023-06-01")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", req.Header.Get("Content-Type"), "application/json")
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty, got %q", req.Header.Get("Authorization"))
	}

	// Check body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != "claude-3" {
		t.Errorf("body.model = %v, want %q", body["model"], "claude-3")
	}
	if body["max_tokens"] != float64(1024) {
		t.Errorf("body.max_tokens = %v, want 1024", body["max_tokens"])
	}
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatal("body.messages missing or empty")
	}
	msg := messages[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Errorf("msg.role = %v, want user", msg["role"])
	}
	if msg["content"] != "Hi there" {
		t.Errorf("msg.content = %v, want %q", msg["content"], "Hi there")
	}
}

func TestAnthropicAdapter_BuildRequest_EmptyModel(t *testing.T) {
	adapter := &anthropicAdapter{}
	restore := SetTestEndpoints(openAIEndpoint, "https://example.com/anthropic", xaiEndpoint)
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "", "prompt", "key")
	if err != nil {
		t.Fatalf("BuildRequest with empty model: %v", err)
	}
	bodyBytes, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != defaultAnthropicModel {
		t.Errorf("empty model default = %v, want %q", body["model"], defaultAnthropicModel)
	}
}

func TestAnthropicAdapter_ParseResponse_Success(t *testing.T) {
	adapter := &anthropicAdapter{}
	body := `{"content":[{"type":"text","text":"Bonjour!"}],"usage":{"output_tokens":99},"model":"claude-3"}`
	result, err := adapter.ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if result.Text != "Bonjour!" {
		t.Errorf("Text = %q, want %q", result.Text, "Bonjour!")
	}
	if result.Tokens != 99 {
		t.Errorf("Tokens = %d, want 99", result.Tokens)
	}
	if result.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", result.Model, "claude-3")
	}
}

func TestAnthropicAdapter_ParseResponse_EmptyContent(t *testing.T) {
	adapter := &anthropicAdapter{}
	body := `{"content":[],"usage":{"output_tokens":0},"model":"claude-3"}`
	result, err := adapter.ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatalf("ParseResponse with empty content: %v", err)
	}
	if result.Text != "" {
		t.Errorf("Text = %q, want empty", result.Text)
	}
}

func TestAnthropicAdapter_ParseResponse_ErrorStatus(t *testing.T) {
	adapter := &anthropicAdapter{}
	_, err := adapter.ParseResponse(500, []byte(`{"error":"internal"}`))
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestAnthropicAdapter_ParseResponse_MalformedJSON(t *testing.T) {
	adapter := &anthropicAdapter{}
	_, err := adapter.ParseResponse(200, []byte(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestXAIAdapter_BuildRequest(t *testing.T) {
	adapter := &xAIAdapter{}
	restore := SetTestEndpoints(openAIEndpoint, anthropicEndpoint, "https://example.com/xai")
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "grok-beta", "Hello xAI!", "sk-xai-key-789")
	if err != nil {
		t.Fatalf("BuildRequest failed: %v", err)
	}

	// Check URL
	if req.URL.String() != "https://example.com/xai" {
		t.Errorf("URL = %q, want %q", req.URL.String(), "https://example.com/xai")
	}

	// Check method
	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want %q", req.Method, http.MethodPost)
	}

	// Check Authorization header
	auth := req.Header.Get("Authorization")
	if auth != "Bearer sk-xai-key-789" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer sk-xai-key-789")
	}

	// Check Content-Type
	ct := req.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	// Check body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll body: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != "grok-beta" {
		t.Errorf("body.model = %v, want %q", body["model"], "grok-beta")
	}
}

func TestXAIAdapter_BuildRequest_EmptyModel(t *testing.T) {
	adapter := &xAIAdapter{}
	restore := SetTestEndpoints(openAIEndpoint, anthropicEndpoint, "https://example.com/xai")
	defer restore()

	req, err := adapter.BuildRequest(context.Background(), "", "prompt", "key")
	if err != nil {
		t.Fatalf("BuildRequest with empty model: %v", err)
	}
	bodyBytes, _ := io.ReadAll(req.Body)
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("Unmarshal body: %v", err)
	}
	if body["model"] != defaultXAIModel {
		t.Errorf("empty model default = %v, want %q", body["model"], defaultXAIModel)
	}
}

func TestXAIAdapter_ParseResponse_Success(t *testing.T) {
	adapter := &xAIAdapter{}
	body := `{"choices":[{"message":{"content":"Grok says hi!"}}],"usage":{"total_tokens":55},"model":"grok-beta"}`
	result, err := adapter.ParseResponse(200, []byte(body))
	if err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}
	if result.Text != "Grok says hi!" {
		t.Errorf("Text = %q, want %q", result.Text, "Grok says hi!")
	}
	if result.Tokens != 55 {
		t.Errorf("Tokens = %d, want 55", result.Tokens)
	}
	if result.Model != "grok-beta" {
		t.Errorf("Model = %q, want %q", result.Model, "grok-beta")
	}
}

func TestXAIAdapter_ParseResponse_MalformedJSON(t *testing.T) {
	adapter := &xAIAdapter{}
	_, err := adapter.ParseResponse(200, []byte(`{malformed`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSetTestEndpoints(t *testing.T) {
	// Save originals
	origOpenAI := openAIEndpoint
	origAnthropic := anthropicEndpoint
	origXAI := xaiEndpoint

	// Override
	restore := SetTestEndpoints("http://test-oai", "http://test-anth", "http://test-xai")

	if openAIEndpoint != "http://test-oai" {
		t.Errorf("openAIEndpoint = %q, want http://test-oai", openAIEndpoint)
	}
	if anthropicEndpoint != "http://test-anth" {
		t.Errorf("anthropicEndpoint = %q, want http://test-anth", anthropicEndpoint)
	}
	if xaiEndpoint != "http://test-xai" {
		t.Errorf("xaiEndpoint = %q, want http://test-xai", xaiEndpoint)
	}

	// Restore
	restore()

	if openAIEndpoint != origOpenAI {
		t.Errorf("after restore, openAIEndpoint = %q, want %q", openAIEndpoint, origOpenAI)
	}
	if anthropicEndpoint != origAnthropic {
		t.Errorf("after restore, anthropicEndpoint = %q, want %q", anthropicEndpoint, origAnthropic)
	}
	if xaiEndpoint != origXAI {
		t.Errorf("after restore, xaiEndpoint = %q, want %q", xaiEndpoint, origXAI)
	}
}

func TestSecretValueNotInError(t *testing.T) {
	secretKey := "sk-super-secret-key-that-must-not-leak-12345"

	// Set a deliberately invalid endpoint so http.NewRequestWithContext fails
	restore := SetTestEndpoints("://invalid-url", anthropicEndpoint, xaiEndpoint)
	defer restore()

	adapter := &openAIAdapter{}
	_, err := adapter.BuildRequest(context.Background(), "gpt-4", "prompt", secretKey)
	if err == nil {
		t.Skip("expected an error from invalid URL, but got none — skipping leak check")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Errorf("error message contains secret key! error: %v", err)
	}
}

func TestSecretValueNotInError_Anthropic(t *testing.T) {
	secretKey := "sk-ant-super-secret-do-not-leak-67890"

	restore := SetTestEndpoints(openAIEndpoint, "://bad-anthropic", xaiEndpoint)
	defer restore()

	adapter := &anthropicAdapter{}
	_, err := adapter.BuildRequest(context.Background(), "claude-3", "prompt", secretKey)
	if err == nil {
		t.Skip("expected an error from invalid URL, but got none — skipping leak check")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Errorf("error message contains secret key! error: %v", err)
	}
}

func TestSecretValueNotInError_XAI(t *testing.T) {
	secretKey := "sk-xai-super-secret-do-not-leak-abcdef"

	restore := SetTestEndpoints(openAIEndpoint, anthropicEndpoint, "://bad-xai")
	defer restore()

	adapter := &xAIAdapter{}
	_, err := adapter.BuildRequest(context.Background(), "grok-beta", "prompt", secretKey)
	if err == nil {
		t.Skip("expected an error from invalid URL, but got none — skipping leak check")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Errorf("error message contains secret key! error: %v", err)
	}
}

func TestAdapter_InterfaceCompliance(t *testing.T) {
	// Verify all adapters satisfy the ProviderAdapter interface
	var _ ProviderAdapter = &openAIAdapter{}
	var _ ProviderAdapter = &anthropicAdapter{}
	var _ ProviderAdapter = &xAIAdapter{}
}

func TestAdapter_EndpointsMatchDefault(t *testing.T) {
	// Verify adapter endpoints match the defaults (when not overridden)
	if ep := (&openAIAdapter{}).Endpoint(); ep != openAIEndpoint {
		t.Errorf("openAI endpoint = %q, want %q", ep, openAIEndpoint)
	}
	if ep := (&anthropicAdapter{}).Endpoint(); ep != anthropicEndpoint {
		t.Errorf("anthropic endpoint = %q, want %q", ep, anthropicEndpoint)
	}
	if ep := (&xAIAdapter{}).Endpoint(); ep != xaiEndpoint {
		t.Errorf("xai endpoint = %q, want %q", ep, xaiEndpoint)
	}
}

func TestAdapter_AuthHeaders(t *testing.T) {
	if ah := (&openAIAdapter{}).AuthHeader(); ah != "Authorization" {
		t.Errorf("openAI AuthHeader = %q, want Authorization", ah)
	}
	if ah := (&anthropicAdapter{}).AuthHeader(); ah != "x-api-key" {
		t.Errorf("anthropic AuthHeader = %q, want x-api-key", ah)
	}
	if ah := (&xAIAdapter{}).AuthHeader(); ah != "Authorization" {
		t.Errorf("xAI AuthHeader = %q, want Authorization", ah)
	}
}

func TestXAIAdapter_ParseResponse_ErrorStatus(t *testing.T) {
	adapter := &xAIAdapter{}
	_, err := adapter.ParseResponse(403, []byte(`{"error":"forbidden"}`))
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestRoundTrip_OpenAI(t *testing.T) {
	// Start a test HTTP server that mimics a minimal OpenAI response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key-openai" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hi from test server!"}}],"usage":{"total_tokens":10},"model":"gpt-4o-mini"}`))
	}))
	defer server.Close()

	restore := SetTestEndpoints(server.URL, anthropicEndpoint, xaiEndpoint)
	defer restore()

	adapter := &openAIAdapter{}
	req, err := adapter.BuildRequest(context.Background(), "gpt-4o-mini", "ping", "test-key-openai")
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	result, err := adapter.ParseResponse(resp.StatusCode, body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if result.Text != "Hi from test server!" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Tokens != 10 {
		t.Errorf("Tokens = %d", result.Tokens)
	}
}

func TestRoundTrip_Anthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key-anthropic" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Claude here!"}],"usage":{"output_tokens":20},"model":"claude-3-5-sonnet-20241022"}`))
	}))
	defer server.Close()

	restore := SetTestEndpoints(openAIEndpoint, server.URL, xaiEndpoint)
	defer restore()

	adapter := &anthropicAdapter{}
	req, err := adapter.BuildRequest(context.Background(), "claude-3-5-sonnet-20241022", "ping", "test-key-anthropic")
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	result, err := adapter.ParseResponse(resp.StatusCode, body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if result.Text != "Claude here!" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Tokens != 20 {
		t.Errorf("Tokens = %d", result.Tokens)
	}
}

func TestRoundTrip_XAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key-xai" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Grok responds!"}}],"usage":{"total_tokens":30},"model":"grok-beta"}`))
	}))
	defer server.Close()

	restore := SetTestEndpoints(openAIEndpoint, anthropicEndpoint, server.URL)
	defer restore()

	adapter := &xAIAdapter{}
	req, err := adapter.BuildRequest(context.Background(), "grok-beta", "ping", "test-key-xai")
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HTTP Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	result, err := adapter.ParseResponse(resp.StatusCode, body)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if result.Text != "Grok responds!" {
		t.Errorf("Text = %q", result.Text)
	}
	if result.Tokens != 30 {
		t.Errorf("Tokens = %d", result.Tokens)
	}
}