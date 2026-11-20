package cli

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudHelp_MentionsWebhook(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "webhook") {
		t.Fatalf("cloud --help silent on webhook:\n%s", stdout)
	}
}

func TestCloudHelp_NoEvery1m(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if strings.Contains(stdout, "every_1m") {
		t.Fatalf("cloud --help still advertises every_1m:\n%s", stdout)
	}
	cronHelp, _, err := executeCloudCmd(t, "", "cloud", "cron", "--help")
	if err != nil {
		t.Fatalf("cloud cron --help: %v", err)
	}
	if strings.Contains(cronHelp, "every_1m") {
		t.Fatalf("cloud cron --help still advertises every_1m:\n%s", cronHelp)
	}
}

func TestCloudWebhook_SetFireCompletionDelivery(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_hook")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	now := time.Unix(1700000000, 0)
	oldNow := webhookNow
	webhookNow = func() time.Time { return now }
	t.Cleanup(func() { webhookNow = oldNow })

	secret := "hook-secret"
	body := []byte(`{"ok":true}`)
	var sawSet, sawFire, sawComplete, sawDeliver bool
	var fireAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/deployments/dep_hook/webhook":
			sawSet = true
			if got := r.Header.Get("User-Agent"); got != "agentpaas-cli/0.1" {
				t.Errorf("User-Agent = %q", got)
			}
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"secret":"hook-secret"`) {
				t.Errorf("set body = %s", raw)
			}
			_, _ = w.Write([]byte(`{"configured":true,"provider":"generic_hmac","deployment_id":"dep_hook"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/deployments/dep_hook/hooks/generic_hmac":
			sawFire = true
			fireAuth = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = fmt.Fprintf(mac, "%d.", now.Unix())
			_, _ = mac.Write(raw)
			want := fmt.Sprintf("t=%d,v1=%s", now.Unix(), hex.EncodeToString(mac.Sum(nil)))
			if got := r.Header.Get("X-Agentpaas-Signature"); got != want {
				t.Errorf("signature = %q want %q", got, want)
			}
			_, _ = w.Write([]byte(`{"run_id":"run_hook"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/deployments/dep_hook/completion-webhook":
			sawComplete = true
			_, _ = w.Write([]byte(`{"configured":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/deployments/dep_hook/delivery-webhook":
			sawDeliver = true
			_, _ = w.Write([]byte(`{"configured":true}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, secret, "cloud", "webhook", "set", "dep_hook", "--secret-stdin")
	if err != nil {
		t.Fatalf("set: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout, stderr)
	}

	stdout, stderr, err = executeCloudCmd(t, secret, "cloud", "webhook", "fire", "dep_hook",
		"--body", string(body), "--secret-stdin")
	if err != nil {
		t.Fatalf("fire: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run_hook") {
		t.Fatalf("fire stdout=%q", stdout)
	}
	if fireAuth != "" {
		t.Fatalf("fire sent tenant token: %q", fireAuth)
	}

	if _, _, err := executeCloudCmd(t, "", "cloud", "webhook", "completion", "dep_hook", "--url", "https://example.com/c"); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if _, _, err := executeCloudCmd(t, "", "cloud", "webhook", "delivery", "dep_hook", "--url", "https://example.com/d"); err != nil {
		t.Fatalf("delivery: %v", err)
	}
	if !sawSet || !sawFire || !sawComplete || !sawDeliver {
		t.Fatalf("saw set=%v fire=%v complete=%v deliver=%v", sawSet, sawFire, sawComplete, sawDeliver)
	}
}

func TestCloudWebhook_RejectsHTTPDestination(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_hook")
	_, _, err := executeCloudCmd(t, "", "cloud", "webhook", "completion", "dep_hook", "--url", "http://example.com/c")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want https required", err)
	}
}

func TestCloudWebhookCommandRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	found, _, err := cmd.Find([]string{"cloud", "webhook"})
	if err != nil {
		t.Fatalf("Find cloud webhook: %v", err)
	}
	names := map[string]bool{}
	for _, c := range found.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"set", "fire", "completion", "delivery"} {
		if !names[want] {
			t.Errorf("missing webhook subcommand %s", want)
		}
	}
}

func TestCloudWebhook_JSONSetDoesNotPrintSecret(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_hook_json")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"configured":true,"provider":"generic_hmac","deployment_id":"dep_hook"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "super-secret", "--json", "cloud", "webhook", "set", "dep_hook", "--secret-stdin")
	if err != nil {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if strings.Contains(stdout, "super-secret") || strings.Contains(stderr, "super-secret") {
		t.Fatalf("secret in output stdout=%q stderr=%q", stdout, stderr)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("json: %v stdout=%q", err, stdout)
	}
}
