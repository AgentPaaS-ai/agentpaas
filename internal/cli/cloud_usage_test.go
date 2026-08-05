package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const usageTestJSON = `{
	"tier":"trial",
	"concurrency_limit":5,
	"concurrency_active":0,
	"agent_limit":10,
	"agents_used":1,
	"cpu_minute_limit":100,
	"cpu_minutes_used":1.25,
	"cpu_minutes_remaining":98.75,
	"usage_period_start":"2026-07-01T00:00:00Z",
	"trial_expires_at":"2026-07-28T00:00:00Z",
	"days_remaining":27,
	"meter":{
		"formula":"(finished_at-started_at)+sleep_tail",
		"sleep_tail_sec_default":30,
		"note":"awake estimate; CF invoice reconcile later"
	}
}`

func TestCloudUsage_CommandsRegistered(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "usage"})
	if err != nil {
		t.Fatalf("Find cloud usage: %v", err)
	}
}

func TestCloudUsage_HumanOutput(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_usage_test")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/usage" {
			t.Errorf("expected /v1/usage, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_usage_test" {
			t.Errorf("Authorization = %q, want Bearer apc_usage_test", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(usageTestJSON))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "usage")
	if err != nil {
		t.Fatalf("usage: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	for _, want := range []string{
		"Tier: trial",
		"Concurrency active: 0/5",
		"Agents used: 1/10",
		"CPU minutes used: 1.25/100",
		"CPU minutes remaining: 98.75",
		"Trial days remaining: 27",
		"Meter formula: (finished_at-started_at)+sleep_tail",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got: %q", want, stdout)
		}
	}
}

func TestCloudUsage_JSONOutput(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_usage_json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tier":"pro",
			"concurrency_limit":0,
			"concurrency_active":0,
			"agent_limit":0,
			"agents_used":0,
			"cpu_minute_limit":0,
			"cpu_minutes_used":0,
			"cpu_minutes_remaining":null,
			"usage_period_start":"2026-07-01T00:00:00Z",
			"trial_expires_at":"",
			"days_remaining":null,
			"meter":{"formula":"formula","sleep_tail_sec_default":30,"note":"note"}
		}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "usage", "--json")
	if err != nil {
		t.Fatalf("usage --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed["tier"] != "pro" {
		t.Errorf("tier = %v, want pro", parsed["tier"])
	}
	if parsed["cpu_minutes_remaining"] != nil {
		t.Errorf("cpu_minutes_remaining = %v, want null", parsed["cpu_minutes_remaining"])
	}
	if parsed["days_remaining"] != nil {
		t.Errorf("days_remaining = %v, want null", parsed["days_remaining"])
	}
	if !strings.Contains(stdout, "\n  \"meter\":") {
		t.Errorf("expected pretty JSON output, got: %s", stdout)
	}
	_ = stderr
}

func TestCloudUsage_HumanOutput_TrialLimitMessages(t *testing.T) {
	tests := []struct {
		name    string
		usage   string
		want    string
		notWant string
	}{
		{
			name:  "cpu exhausted",
			usage: `{"cpu_minutes_remaining":0,"days_remaining":27,"agent_limit":10,"agents_used":1}`,
			want:  "Trial limits crossed. Request a new trial or convert to a paid plan at https://agentpaas.ai (or email contact@agentpaas.ai).",
		},
		{
			name:    "trial expired",
			usage:   `{"cpu_minutes_remaining":98.75,"days_remaining":0,"agent_limit":10,"agents_used":1}`,
			want:    "Trial period ended. Request a new trial or convert to a paid plan at https://agentpaas.ai (or email contact@agentpaas.ai).",
			notWant: "Trial limits crossed.",
		},
		{
			name:  "agent limit reached",
			usage: `{"cpu_minutes_remaining":98.75,"days_remaining":27,"agent_limit":1,"agents_used":1}`,
			want:  "Trial limits crossed. Request a new trial or convert to a paid plan at https://agentpaas.ai (or email contact@agentpaas.ai).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
			store := setupFakeTokenStore(t)
			if err := store.Set(context.Background(), "apc_usage_limit_test"); err != nil {
				t.Fatalf("seed token: %v", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.usage))
			}))
			defer func() { server.Close() }()
			t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

			stdout, _, err := executeCloudCmd(t, "", "cloud", "usage")
			if err != nil {
				t.Fatalf("usage: %v", err)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("expected %q in output, got %q", tt.want, stdout)
			}
			if tt.notWant != "" && strings.Contains(stdout, tt.notWant) {
				t.Errorf("did not expect %q in output, got %q", tt.notWant, stdout)
			}
		})
	}
}

func TestCloudUsage_NotLoggedIn(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "usage")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not logged in") && !strings.Contains(combined, "Not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudUsage_UnauthorizedToken(t *testing.T) {
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_expired_usage")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	_, stderr, err := executeCloudCmd(t, "", "cloud", "usage")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not logged in") && !strings.Contains(combined, "Not logged in") {
		t.Errorf("expected 'not logged in' message for 401, got: err=%q stderr=%q", err, stderr)
	}
}
