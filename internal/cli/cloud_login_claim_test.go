package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudLogin_HelpCoachesClaimFirst(t *testing.T) {
	cmd := newCloudLoginCmd()
	long := cmd.Long
	for _, want := range []string{"https://agentpaas.ai", "Start free trial", "claim"} {
		if !strings.Contains(long, want) {
			t.Errorf("cloud login Long missing %q:\n%s", want, long)
		}
	}
	if strings.Contains(strings.ToLower(long), "cloudflare") {
		t.Errorf("cloud login Long must not mention Cloudflare tokens:\n%s", long)
	}
}

func TestCloudWhoami_NotLoggedIn_CoachesClaimFirst(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	for _, want := range []string{"not logged in", "https://agentpaas.ai", "Start free trial", "agentpaas cloud login"} {
		if !strings.Contains(combined, want) {
			t.Errorf("not-logged-in coaching missing %q, got: %q", want, combined)
		}
	}
}

func TestCloudLogin_StartAuthCoachesClaimFirst(t *testing.T) {
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"claim_required","message":"Open your claim link first"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "login")
	if err == nil {
		t.Fatal("expected claim-first error from cloud login")
	}
	combined := err.Error() + stderr
	for _, want := range []string{"https://agentpaas.ai", "Start free trial"} {
		if !strings.Contains(combined, want) {
			t.Errorf("login start-auth error missing %q, got: %q", want, combined)
		}
	}
	if strings.Contains(strings.ToLower(combined), "cloudflare") {
		t.Errorf("login error must not mention Cloudflare: %q", combined)
	}
}

func TestCloudLogin_StartAuthJSONCoachesClaimFirst(t *testing.T) {
	_ = setupFakeTokenStore(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"claim_required","message":"Open your claim link first"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "login", "--json")
	if err == nil {
		t.Fatal("expected claim-first error from cloud login --json")
	}
	var got CloudErrorJSON
	if jsonErr := json.Unmarshal([]byte(stdout), &got); jsonErr != nil {
		t.Fatalf("decode JSON error: %v; stdout=%q", jsonErr, stdout)
	}
	if !strings.Contains(got.Message, "https://agentpaas.ai") || !strings.Contains(got.Message, "Start free trial") {
		t.Fatalf("JSON message = %q, want Start free trial coaching", got.Message)
	}
}
