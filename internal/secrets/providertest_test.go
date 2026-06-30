package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTestProvider_OpenAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	orig := openAIEndpoint
	openAIEndpoint = srv.URL
	t.Cleanup(func() { openAIEndpoint = orig })

	result := TestProvider(context.Background(), "openai", []byte("sk-test-key-12345"))
	if result.Status != "ok" {
		t.Fatalf("expected status ok, got %q: %s", result.Status, result.Detail)
	}
	if result.HTTPStatus != 200 {
		t.Fatalf("expected HTTP 200, got %d", result.HTTPStatus)
	}
	if result.Provider != "openai" {
		t.Fatalf("expected provider openai, got %q", result.Provider)
	}
}

func TestTestProvider_OpenAI_InvalidKey(t *testing.T) {
	secretValue := "sk-invalid-key-that-should-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid api key"})
	}))
	defer srv.Close()

	orig := openAIEndpoint
	openAIEndpoint = srv.URL
	t.Cleanup(func() { openAIEndpoint = orig })

	result := TestProvider(context.Background(), "openai", []byte(secretValue))
	if result.Status != "error" {
		t.Fatalf("expected status error, got %q", result.Status)
	}
	if !strings.Contains(result.Detail, "401") {
		t.Fatalf("expected Detail to mention 401, got: %s", result.Detail)
	}
	if strings.Contains(result.Detail, secretValue) {
		t.Fatalf("Detail leaked secret value: %s", result.Detail)
	}
}

func TestTestProvider_Anthropic_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version 2023-06-01, got %q", r.Header.Get("anthropic-version"))
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	orig := anthropicEndpoint
	anthropicEndpoint = srv.URL
	t.Cleanup(func() { anthropicEndpoint = orig })

	result := TestProvider(context.Background(), "anthropic", []byte("sk-ant-test-key"))
	if result.Status != "ok" {
		t.Fatalf("expected status ok, got %q: %s", result.Status, result.Detail)
	}
}

func TestTestProvider_XAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	orig := xaiEndpoint
	xaiEndpoint = srv.URL
	t.Cleanup(func() { xaiEndpoint = orig })

	result := TestProvider(context.Background(), "xiai", []byte("xai-test-key"))
	if result.Status != "ok" {
		t.Fatalf("expected status ok, got %q: %s", result.Status, result.Detail)
	}
}

func TestTestProvider_UnknownProvider(t *testing.T) {
	result := TestProvider(context.Background(), "unknown-provider", []byte("some-key"))
	if result.Status != "error" {
		t.Fatalf("expected status error, got %q", result.Status)
	}
	if !strings.Contains(result.Detail, "unknown provider") {
		t.Fatalf("expected Detail to mention unknown provider, got: %s", result.Detail)
	}
}

func TestTestProvider_Unreachable(t *testing.T) {
	orig := openAIEndpoint
	openAIEndpoint = "http://127.0.0.1:0"
	t.Cleanup(func() { openAIEndpoint = orig })

	result := TestProvider(context.Background(), "openai", []byte("sk-test-key"))
	if result.Status != "error" {
		t.Fatalf("expected status error, got %q", result.Status)
	}
	if !strings.Contains(strings.ToLower(result.Detail), "connection") &&
		!strings.Contains(strings.ToLower(result.Detail), "refused") &&
		!strings.Contains(strings.ToLower(result.Detail), "failed to reach") {
		t.Fatalf("expected Detail to mention connection failure, got: %s", result.Detail)
	}
}

func TestProviderTestResult_NeverContainsSecret(t *testing.T) {
	secretValue := "sk-this-is-a-very-secret-api-key-abc123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := make(map[string]string)
		for name, vals := range r.Header {
			resp[name] = strings.Join(vals, ", ")
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	orig := openAIEndpoint
	openAIEndpoint = srv.URL
	t.Cleanup(func() { openAIEndpoint = orig })

	result := TestProvider(context.Background(), "openai", []byte(secretValue))
	if strings.Contains(result.Detail, secretValue) {
		t.Fatalf("result.Detail leaked secret value: %s", result.Detail)
	}
	resultJSON, _ := json.Marshal(result)
	if strings.Contains(string(resultJSON), secretValue) {
		t.Fatalf("result JSON leaked secret value: %s", string(resultJSON))
	}
}

func TestTestProvider_Anthropic_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	orig := anthropicEndpoint
	anthropicEndpoint = srv.URL
	t.Cleanup(func() { anthropicEndpoint = orig })

	result := TestProvider(context.Background(), "anthropic", []byte("bad-key"))
	if result.Status != "error" {
		t.Fatalf("expected status error, got %q", result.Status)
	}
	if !strings.Contains(result.Detail, fmt.Sprintf("%d", http.StatusForbidden)) {
		t.Fatalf("expected Detail to mention 403, got: %s", result.Detail)
	}
}

func TestSetTestEndpoints_Restores(t *testing.T) {
	origOpenAI := openAIEndpoint
	origAnthropic := anthropicEndpoint
	origXAI := xaiEndpoint

	restore := SetTestEndpoints("http://test-openai.local", "http://test-anthropic.local", "http://test-xai.local")
	if restore == nil {
		t.Fatal("expected restore function, got nil")
	}

	if openAIEndpoint != "http://test-openai.local" {
		t.Fatalf("expected openAIEndpoint to be updated, got %q", openAIEndpoint)
	}
	if anthropicEndpoint != "http://test-anthropic.local" {
		t.Fatalf("expected anthropicEndpoint to be updated, got %q", anthropicEndpoint)
	}
	if xaiEndpoint != "http://test-xai.local" {
		t.Fatalf("expected xaiEndpoint to be updated, got %q", xaiEndpoint)
	}

	restore()

	if openAIEndpoint != origOpenAI {
		t.Fatalf("expected openAIEndpoint to be restored to %q, got %q", origOpenAI, openAIEndpoint)
	}
	if anthropicEndpoint != origAnthropic {
		t.Fatalf("expected anthropicEndpoint to be restored to %q, got %q", origAnthropic, anthropicEndpoint)
	}
	if xaiEndpoint != origXAI {
		t.Fatalf("expected xaiEndpoint to be restored to %q, got %q", origXAI, xaiEndpoint)
	}
}
