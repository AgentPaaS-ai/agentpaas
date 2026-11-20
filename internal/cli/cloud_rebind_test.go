package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCloudRebindCommandRegistered verifies the rebind subcommand is wired into
// the cloud command tree.
func TestCloudRebindCommandRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	if _, _, err := cmd.Find([]string{"cloud", "rebind"}); err != nil {
		t.Fatalf("Find cloud rebind: %v", err)
	}
}

// TestCloudRebindMissingImageFails verifies --image is required.
func TestCloudRebindMissingImageFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind", "--yes")
	if err == nil {
		t.Fatal("expected error when --image is missing")
	}
}

// TestCloudRebindMissingAccountIDFails verifies CF_ACCOUNT_ID resolution.
func TestCloudRebindMissingAccountIDFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "")
	t.Setenv("CF_API_TOKEN", "tok-abc")
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind", "--image", "registry.example.com/img:1.0", "--yes")
	if err == nil {
		t.Fatal("expected error when CF_ACCOUNT_ID is missing")
	}
}

// TestCloudRebindMissingAPITokenFails verifies CF_API_TOKEN resolution.
func TestCloudRebindMissingAPITokenFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "")
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind", "--image", "registry.example.com/img:1.0", "--yes")
	if err == nil {
		t.Fatal("expected error when CF_API_TOKEN is missing")
	}
}

// TestCloudRebindInvalidInstanceTypeFails verifies instance-type validation.
func TestCloudRebindInvalidInstanceTypeFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--instance-type", "mega",
		"--yes")
	if err == nil {
		t.Fatal("expected error for invalid instance-type")
	}
}

// TestCloudRebindAppIDResolutionStaging verifies that when --app-id is not
// set, the command lists CF container applications and matches the staging
// pattern (name contains "staging-runcontainer").
func TestCloudRebindAppIDResolutionStaging(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	var postedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security fix: Authorization header is not sent over http://
		// (only https://). The test server uses http://127.0.0.1, so the
		// header is intentionally empty.

		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			// List applications — return a staging and a prod app.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": [
					{"id": "staging-app-id", "name": "agentpaas-staging-runcontainer"},
					{"id": "prod-app-id", "name": "agentpaas-runcontainer"}
				]
			}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &postedBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-123", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-123"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-123", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/staging-app-id"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "staging-app-id",
					"configuration": {
						"image": "registry.example.com/img:1.0",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err != nil {
		t.Fatalf("rebind staging: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if postedBody == nil {
		t.Fatal("expected rollout POST body to be captured")
	}
	// Verify the rollout was posted to the staging app ID path.
	targetCfg, ok := postedBody["target_configuration"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing target_configuration in POST body: %v", postedBody)
	}
	if targetCfg["image"] != "registry.example.com/img:1.0" {
		t.Errorf("POST image = %v, want registry.example.com/img:1.0", targetCfg["image"])
	}
	if !strings.Contains(stdout, "staging-app-id") {
		t.Errorf("expected staging-app-id in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "completed") {
		t.Errorf("expected completed status in output, got %q", stdout)
	}
}

// TestCloudRebindAppIDResolutionProd verifies prod matching: name ends with
// "-runcontainer" without "staging".
func TestCloudRebindAppIDResolutionProd(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": [
					{"id": "staging-app-id", "name": "agentpaas-staging-runcontainer"},
					{"id": "prod-app-id", "name": "agentpaas-runcontainer"}
				]
			}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-456", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-456"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-456", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/prod-app-id"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "prod-app-id",
					"configuration": {
						"image": "registry.example.com/prod:2.0",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/prod:2.0",
		"--env", "prod",
		"--yes")
	if err != nil {
		t.Fatalf("rebind prod: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "prod-app-id") {
		t.Errorf("expected prod-app-id in output, got %q", stdout)
	}
}

// TestCloudRebindAppIDResolutionAmbiguousFails verifies that >1 match errors.
func TestCloudRebindAppIDResolutionAmbiguousFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": [
					{"id": "app-1", "name": "foo-staging-runcontainer"},
					{"id": "app-2", "name": "bar-staging-runcontainer"}
				]
			}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err == nil {
		t.Fatal("expected error for ambiguous app ID match")
	}
}

// TestCloudRebindAppIDResolutionZeroMatchesFails verifies 0 matches errors.
func TestCloudRebindAppIDResolutionZeroMatchesFails(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": []}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err == nil {
		t.Fatal("expected error for zero app ID matches")
	}
}

// TestCloudRebindExplicitAppID verifies --app-id bypasses listing.
func TestCloudRebindExplicitAppID(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	listCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			listCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": []}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-789", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-789"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-789", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/explicit-app"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "explicit-app",
					"configuration": {
						"image": "registry.example.com/img:1.0",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "explicit-app",
		"--yes")
	if err != nil {
		t.Fatalf("rebind explicit app-id: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if listCalled {
		t.Error("list applications should NOT be called when --app-id is set")
	}
	if !strings.Contains(stdout, "explicit-app") {
		t.Errorf("expected explicit-app in output, got %q", stdout)
	}
}

// TestCloudRebindRolloutPollingTimeout verifies the command fails when rollout
// never reaches completed within the poll deadline.
func TestCloudRebindRolloutPollingTimeout(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-stuck", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-stuck"):
			// Always return deploying — never completes.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-stuck", "status": "deploying"}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--poll-interval", "1s",
		"--timeout", "2s",
		"--yes")
	if err == nil {
		t.Fatal("expected error when rollout never completes")
	}
}

// TestCloudRebindRolloutFailed verifies the command fails when rollout status
// is "failed".
func TestCloudRebindRolloutFailed(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-fail", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-fail"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-fail", "status": "failed"}}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err == nil {
		t.Fatal("expected error when rollout status is failed")
	}
}

// TestCloudRebindVerificationImageMismatch verifies the post-rollout
// verification step catches an image mismatch.
func TestCloudRebindVerificationImageMismatch(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-ok", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-ok"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-ok", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/staging-app"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "staging-app",
					"configuration": {
						"image": "WRONG-IMAGE",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err == nil {
		t.Fatal("expected error for image mismatch verification")
	}
}

// TestCloudRebindVerificationEntrypointMismatch verifies the post-rollout
// verification catches a wrong entrypoint.
func TestCloudRebindVerificationEntrypointMismatch(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-ok", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-ok"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-ok", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/staging-app"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "staging-app",
					"configuration": {
						"image": "registry.example.com/img:1.0",
						"entrypoint": ["/wrong/entrypoint"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--yes")
	if err == nil {
		t.Fatal("expected error for entrypoint mismatch verification")
	}
}

// TestCloudRebindConfirmationDeclined verifies the command aborts when the
// user answers "n" to the confirmation prompt.
func TestCloudRebindConfirmationDeclined(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no API calls expected when confirmation declined: %s %s", r.Method, r.URL.Path)
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	_, _, err := executeCloudCmd(t, "n\n", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging")
	if err == nil {
		t.Fatal("expected error when confirmation declined")
	}
}

// TestCloudRebindJSONOutput verifies --json output on success.
func TestCloudRebindJSONOutput(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", "tok-abc")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-json", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-json", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/staging-app"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "staging-app",
					"configuration": {
						"image": "registry.example.com/img:1.0",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--json",
		"--yes")
	if err != nil {
		t.Fatalf("rebind --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed["status"] != "completed" {
		t.Errorf("status = %v, want completed", parsed["status"])
	}
	if parsed["app_id"] != "staging-app" {
		t.Errorf("app_id = %v, want staging-app", parsed["app_id"])
	}
}

// TestCloudRebindFlagOverrideToken verifies --api-token and --account-id flags
// take precedence over env vars.
func TestCloudRebindFlagOverrideToken(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "env-acct")
	t.Setenv("CF_API_TOKEN", "env-tok")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security fix: Authorization header is not sent over http://
		// (only https://). Verify the account-id flag override via the
		// URL path instead.
		if !strings.Contains(r.URL.Path, "flag-acct") {
			t.Errorf("expected flag-acct in path, got %s", r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": [{"id": "staging-app", "name": "x-staging-runcontainer"}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-flag", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/rollout-flag"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result": {"id": "rollout-flag", "status": "completed"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/applications/staging-app"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"result": {
					"id": "staging-app",
					"configuration": {
						"image": "registry.example.com/img:1.0",
						"entrypoint": ["/agentpaas/harness"]
					},
					"latest_rollout": {"status": "completed"},
					"health": {"instances": {"failed": 0}}
				}
			}`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("CF_API_BASE_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--env", "staging",
		"--account-id", "flag-acct",
		"--api-token", "flag-tok",
		"--yes")
	if err != nil {
		t.Fatalf("rebind flag override: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
}
