package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudWhoami_CPUQuotaExhaustedCoaching(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_cpu_quota"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"cpu_quota_exhausted","message":"quota_exceeded"}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err == nil {
		t.Fatal("expected CPU quota error")
	}
	combined := err.Error() + stderr
	want := "Trial CPU limit reached. Get a new trial or convert to paid."
	if !strings.Contains(combined, want) {
		t.Fatalf("human error = %q, want %q", combined, want)
	}
	if CloudExitCode(err) != cloudExitQuota {
		t.Fatalf("CloudExitCode = %d, want %d", CloudExitCode(err), cloudExitQuota)
	}
}

func TestCloudWhoamiJSON_CPUQuotaExhaustedCoaching(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_cpu_quota_json"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"cpu_quota_exhausted","message":"quota_exceeded"}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami", "--json")
	if err == nil {
		t.Fatal("expected CPU quota error")
	}
	if stderr != "" {
		t.Fatalf("JSON cloud error wrote to stderr: %q", stderr)
	}
	var got CloudErrorJSON
	if jsonErr := json.Unmarshal([]byte(stdout), &got); jsonErr != nil {
		t.Fatalf("decode JSON error: %v; stdout=%q", jsonErr, stdout)
	}
	if got.Reason != "cpu_quota_exhausted" {
		t.Fatalf("reason = %q, want cpu_quota_exhausted", got.Reason)
	}
	if got.Message != "Trial CPU limit reached. Get a new trial or convert to paid." {
		t.Fatalf("message = %q, want trial CPU coaching", got.Message)
	}
	if CloudExitCode(err) != cloudExitQuota {
		t.Fatalf("CloudExitCode = %d, want %d", CloudExitCode(err), cloudExitQuota)
	}
}

func TestCloudWhoami_AgentLimitExceededCoaching(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_agent_limit"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"agent_limit_exceeded","message":"too many agents","limit":10}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err == nil {
		t.Fatal("expected agent limit error")
	}
	combined := err.Error() + stderr
	want := "Trial agent limit reached. Convert to paid, or remove an agent."
	if !strings.Contains(combined, want) {
		t.Fatalf("human error = %q, want %q", combined, want)
	}
	if CloudExitCode(err) != cloudExitQuota {
		t.Fatalf("CloudExitCode = %d, want %d", CloudExitCode(err), cloudExitQuota)
	}
}

func TestCloudWhoamiJSON_AgentLimitExceededCoaching(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_agent_limit_json"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota_exceeded","reason":"agent_limit_exceeded","message":"too many agents"}`))
	}))
	defer func() { server.Close() }()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami", "--json")
	if err == nil {
		t.Fatal("expected agent limit error")
	}
	if stderr != "" {
		t.Fatalf("JSON cloud error wrote to stderr: %q", stderr)
	}
	var got CloudErrorJSON
	if jsonErr := json.Unmarshal([]byte(stdout), &got); jsonErr != nil {
		t.Fatalf("decode JSON error: %v; stdout=%q", jsonErr, stdout)
	}
	if got.Reason != "agent_limit_exceeded" {
		t.Fatalf("reason = %q, want agent_limit_exceeded", got.Reason)
	}
	if got.Message != "Trial agent limit reached. Convert to paid, or remove an agent." {
		t.Fatalf("message = %q, want trial agent coaching", got.Message)
	}
	if CloudExitCode(err) != cloudExitQuota {
		t.Fatalf("CloudExitCode = %d, want %d", CloudExitCode(err), cloudExitQuota)
	}
}
