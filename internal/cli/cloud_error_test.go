package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudJSONErrorIsTypedAndRendered(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_error_test"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"concurrency","message":"quota is full","retry_after_sec":12}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami", "--json")
	if err == nil {
		t.Fatal("expected a non-zero cloud error")
	}
	if stderr != "" {
		t.Fatalf("JSON cloud error wrote to stderr: %q", stderr)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode JSON error: %v; stdout=%q", err, stdout)
	}
	if got["error"] != "quota_exceeded" || got["reason"] != "concurrency" {
		t.Fatalf("error envelope = %#v, want quota_exceeded/concurrency", got)
	}
	if got["message"] != "quota is full" || got["retry_after_sec"] != float64(12) {
		t.Fatalf("error details = %#v, want message and retry_after_sec", got)
	}
}
