package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

// setupFakeTokenStore overrides the token store factory to return a fake store
// and restores it on test cleanup.
func setupFakeTokenStore(t *testing.T) *cloudclient.FakeTokenStore {
	t.Helper()

	store := cloudclient.NewFakeTokenStore()

	oldFactory := cloudTokenStoreFactory
	cloudTokenStoreFactory = func() (cloudclient.TokenStore, error) {
		return store, nil
	}
	t.Cleanup(func() {
		cloudTokenStoreFactory = oldFactory
		resetAgentCmd()
	})

	return store
}

// executeCloudCmd builds a fresh command tree and executes it with the given args.
// Returns stdout, stderr, and the error from Execute. Captures both cobra writer
// output and os.Stdout (for fmt.Println calls).
func executeCloudCmd(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	resetAgentCmd()
	cmd := AgentCmd()

	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)

	// Also capture os.Stdout for commands that use fmt.Println.
	stdout, stderr, err := captureStdoutStderr(t, func() error {
		return cmd.Execute()
	})

	return outBuf.String() + stdout, errBuf.String() + stderr, err
}

// captureStdoutStderr captures os.Stdout and os.Stderr while running fn.
func captureStdoutStderr(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut
	stdoutDone := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rOut)
		stdoutDone <- buf.String()
	}()

	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr
	stderrDone := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rErr)
		stderrDone <- buf.String()
	}()

	err := fn()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	return <-stdoutDone, <-stderrDone, err
}

func TestCloudCommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud"})
	if err != nil {
		t.Fatalf("Find cloud: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "login"})
	if err != nil {
		t.Fatalf("Find cloud login: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "whoami"})
	if err != nil {
		t.Fatalf("Find cloud whoami: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "logout"})
	if err != nil {
		t.Fatalf("Find cloud logout: %v", err)
	}
}

func TestCloudLogin_TokenStdin(t *testing.T) {
	store := setupFakeTokenStore(t)

	stdout, stderr, err := executeCloudCmd(t, "apc_test_token_12345", "cloud", "login", "--token-stdin")
	if err != nil {
		t.Fatalf("login --token-stdin: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Logged in") {
		t.Errorf("expected 'Logged in' in stdout, got: %q", stdout)
	}

	// Verify token stored.
	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got != "apc_test_token_12345" {
		t.Errorf("stored token = %q, want apc_test_token_12345", got)
	}
}

func TestCloudLogin_TokenStdin_Empty(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "\n  \n", "cloud", "login", "--token-stdin")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !strings.Contains(err.Error(), "empty token") {
		t.Errorf("error should mention empty token, got: %v", err)
	}
	_ = stderr
}

func TestCloudLogin_TokenFlag(t *testing.T) {
	store := setupFakeTokenStore(t)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "login", "--token", "apc_flag_token")
	if err != nil {
		t.Fatalf("login --token: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Logged in") {
		t.Errorf("expected 'Logged in' in stdout, got: %q", stdout)
	}

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got != "apc_flag_token" {
		t.Errorf("stored token = %q, want apc_flag_token", got)
	}
}

func TestCloudLogin_BrowserFlow_CallbackSimulation(t *testing.T) {
	store := setupFakeTokenStore(t)

	// Capture the redirect_uri from the API request so we can call it back.
	redirectCh := make(chan string, 1)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/cli/start" {
			var req cloudclient.StartCLIAuthRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			redirectCh <- req.RedirectURI

			resp := map[string]string{
				"state":       "mock-state-abc",
				"approve_url": "/approve?state=mock-state-abc",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	// Override openBrowser to call the callback URL directly using the captured
	// redirect_uri from the API request.
	oldOpenBrowser := openBrowser
	openBrowser = func(url string) error {
		// Wait for the redirect URI to be captured from the API request.
		redirectURI := <-redirectCh
		// Construct callback URL: redirect_uri is http://127.0.0.1:<port>/callback
		// Append ?state=mock-state-abc&token=apc_browser_token_xyz
		callbackURL := redirectURI + "?state=mock-state-abc&token=apc_browser_token_xyz"
		resp, err := http.Get(callbackURL)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	}
	defer func() { openBrowser = oldOpenBrowser }()

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "login")
	if err != nil {
		t.Fatalf("login browser: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Logged in") {
		t.Errorf("expected 'Logged in' in stdout, got: %q", stdout)
	}

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got != "apc_browser_token_xyz" {
		t.Errorf("stored token = %q, want apc_browser_token_xyz", got)
	}
}

func TestCloudWhoami_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_whoami_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/whoami" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_whoami_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			resp := cloudclient.WhoamiResponse{
				TenantID:         "tenant-42",
				Tier:             "pro",
				ConcurrencyLimit: 10,
				SecretsBackend:   "vault",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err != nil {
		t.Fatalf("whoami: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "tenant-42") {
		t.Errorf("expected tenant-42 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "pro") {
		t.Errorf("expected pro tier in output, got: %q", stdout)
	}
}

func TestCloudWhoami_EnvToken(t *testing.T) {
	_ = setupFakeTokenStore(t)

	// Store a different token in keychain to verify env wins.
	store, _ := cloudTokenStoreFactory()
	_ = store.Set(context.Background(), "apc_keychain_token")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/whoami" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_env_token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			resp := cloudclient.WhoamiResponse{
				TenantID: "env-tenant",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "apc_env_token")

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err != nil {
		t.Fatalf("whoami: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "env-tenant") {
		t.Errorf("expected env-tenant in output, got: %q", stdout)
	}
}

func TestCloudWhoami_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_json_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/whoami" {
			resp := cloudclient.WhoamiResponse{
				TenantID:         "json-tenant",
				Tier:             "free",
				ConcurrencyLimit: 1,
				SecretsBackend:   "env",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "whoami", "--json")
	if err != nil {
		t.Fatalf("whoami --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	// Verify secrets_backend is NOT leaked in JSON output.
	if strings.Contains(stdout, "secrets_backend") {
		t.Errorf("JSON output should not contain secrets_backend, got: %s", stdout)
	}

	var parsed struct {
		TenantID         string `json:"tenant_id"`
		Tier             string `json:"tier"`
		ConcurrencyLimit int    `json:"concurrency_limit"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON: %v\noutput: %s", err, stdout)
	}
	if parsed.TenantID != "json-tenant" {
		t.Errorf("TenantID = %q, want json-tenant", parsed.TenantID)
	}
	if parsed.Tier != "free" {
		t.Errorf("Tier = %q, want free", parsed.Tier)
	}
	if parsed.ConcurrencyLimit != 1 {
		t.Errorf("ConcurrencyLimit = %d, want 1", parsed.ConcurrencyLimit)
	}
}

func TestCloudWhoami_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudWhoami_NotLoggedIn_JSON(t *testing.T) {
	_ = setupFakeTokenStore(t)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "whoami", "--json")
	// JSON error output should still have non-zero exit, but stdout has JSON.
	_ = err // error is expected for non-zero exit

	var je JSONError
	if err := json.Unmarshal([]byte(stdout), &je); err != nil {
		t.Fatalf("unmarshal JSON error: %v\noutput: %s", err, stdout)
	}
	if je.Error != "not logged in" {
		t.Errorf("Error = %q, want 'not logged in'", je.Error)
	}
}

func TestCloudWhoami_UnauthorizedToken(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_expired")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "whoami")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message for 401, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudLogout_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_to_logout")

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "logout")
	if err != nil {
		t.Fatalf("logout: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Logged out") {
		t.Errorf("expected 'Logged out' in stdout, got: %q", stdout)
	}

	// Verify token removed.
	_, err = store.Get(context.Background())
	if !cloudclient.IsTokenNotFoundErr(err) {
		t.Fatalf("expected ErrTokenNotFound after logout, got: %v", err)
	}
}

func TestCloudLogout_Idempotent(t *testing.T) {
	_ = setupFakeTokenStore(t)

	// First logout (no token stored).
	_, _, err := executeCloudCmd(t, "", "cloud", "logout")
	if err != nil {
		t.Fatalf("logout (first): %v", err)
	}

	// Second logout (still no token).
	_, _, err = executeCloudCmd(t, "", "cloud", "logout")
	if err != nil {
		t.Fatalf("logout (second): %v", err)
	}
}

func TestCloudResolveToken_EnvWinsOverKeychain(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_keychain_value")

	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "apc_env_value")

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"cloud", "whoami"}) // won't execute, just set up

	tok, err := resolveToken(cmd)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if tok != "apc_env_value" {
		t.Errorf("resolveToken = %q, want apc_env_value (env should win)", tok)
	}
}

func TestCloudResolveToken_FallsBackToKeychain(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_from_store")

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"cloud", "whoami"})

	tok, err := resolveToken(cmd)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if tok != "apc_from_store" {
		t.Errorf("resolveToken = %q, want apc_from_store", tok)
	}
}

func TestCloudResolveToken_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t) // empty store

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"cloud", "whoami"})

	_, err := resolveToken(cmd)
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error should contain 'not logged in', got: %v", err)
	}
}

// --- Cloud deploy tests ---

func TestCloudDeploy_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "deploy"})
	if err != nil {
		t.Fatalf("Find cloud deploy: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "deployments"})
	if err != nil {
		t.Fatalf("Find cloud deployments: %v", err)
	}

	// Alias: cloud list → cloud deployments
	_, _, err = cmd.Find([]string{"cloud", "list"})
	if err != nil {
		t.Fatalf("Find cloud list (alias): %v", err)
	}
}

func TestCloudDeploy_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudDeploy_BadDigest_Short(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_test")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "sha256:abc")
	if err == nil {
		t.Fatal("expected error for bad digest")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "invalid digest") {
		t.Errorf("error should mention invalid digest, got: %v", combined)
	}
}

func TestCloudDeploy_BadDigest_NoPrefix(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_test")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected error for digest without sha256: prefix")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "invalid digest") {
		t.Errorf("error should mention invalid digest, got: %v", combined)
	}
}

func TestCloudDeploy_BadDigest_NonHex(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_test")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected error for non-hex digest")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "invalid digest") {
		t.Errorf("error should mention invalid digest, got: %v", combined)
	}
}

func TestCloudDeploy_BadDigest_TooLong(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_test")

	// 65 hex chars — one too many.
	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("expected error for too-long digest")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "invalid digest") {
		t.Errorf("error should mention invalid digest, got: %v", combined)
	}
}

func TestCloudDeploy_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/deployments" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_deploy_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var req cloudclient.CreateDeploymentRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			resp := cloudclient.DeploymentRecord{
				ID:          "dep-created-001",
				ImageDigest: req.ImageDigest,
				Status:      "pending",
				CreatedAt:   "2025-01-15T10:30:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("deploy: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-created-001") {
		t.Errorf("expected dep-created-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "pending") {
		t.Errorf("expected 'pending' status in output, got: %q", stdout)
	}
}

func TestCloudDeploy_Success_WithSlotID(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_slot")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/deployments" {
			var req cloudclient.CreateDeploymentRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.SlotID == nil || *req.SlotID != "slot-99" {
				t.Errorf("SlotID = %v, want slot-99", req.SlotID)
			}

			resp := cloudclient.DeploymentRecord{
				ID:          "dep-slot-001",
				ImageDigest: req.ImageDigest,
				SlotID:      req.SlotID,
				Status:      "pending",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy",
		"--slot-id", "slot-99",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("deploy: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-slot-001") {
		t.Errorf("expected dep-slot-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "slot-99") {
		t.Errorf("expected slot-99 in output, got: %q", stdout)
	}
}

func TestCloudDeploy_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_json")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.DeploymentRecord{
			ID:          "dep-json-001",
			ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Status:      "pending",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "deploy", "--json",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("deploy --json: %v", err)
	}
	var parsed struct {
		ID          string `json:"id"`
		ImageDigest string `json:"image_digest"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed.ID != "dep-json-001" {
		t.Errorf("ID = %q, want dep-json-001", parsed.ID)
	}
}

// --- Cloud deployments tests ---

func TestCloudDeployments_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deployments")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudDeployments_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_list_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/deployments" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_list_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			deployments := []cloudclient.DeploymentRecord{
				{ID: "dep-1", ImageDigest: "sha256:aaa", Status: "running"},
				{ID: "dep-2", ImageDigest: "sha256:bbb", Status: "pending"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(deployments)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deployments")
	if err != nil {
		t.Fatalf("deployments: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-1") {
		t.Errorf("expected dep-1 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "dep-2") {
		t.Errorf("expected dep-2 in output, got: %q", stdout)
	}
}

func TestCloudDeployments_Empty(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_empty_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]cloudclient.DeploymentRecord{})
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "deployments")
	if err != nil {
		t.Fatalf("deployments: %v", err)
	}
	if !strings.Contains(stdout, "No deployments") {
		t.Errorf("expected 'No deployments' message, got: %q", stdout)
	}
}

func TestCloudDeployments_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_json_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments := []cloudclient.DeploymentRecord{
			{ID: "dep-json", ImageDigest: "sha256:ccc", Status: "running"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deployments)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "deployments", "--json")
	if err != nil {
		t.Fatalf("deployments --json: %v", err)
	}
	var parsed []struct {
		ID          string `json:"id"`
		ImageDigest string `json:"image_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if len(parsed) != 1 || parsed[0].ID != "dep-json" {
		t.Errorf("expected dep-json, got %v", parsed)
	}
}

func TestCloudList_AliasToDeployments(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_alias_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deployments := []cloudclient.DeploymentRecord{
			{ID: "dep-alias", ImageDigest: "sha256:ddd", Status: "running"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deployments)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "list")
	if err != nil {
		t.Fatalf("cloud list: %v", err)
	}
	if !strings.Contains(stdout, "dep-alias") {
		t.Errorf("expected dep-alias in output, got: %q", stdout)
	}
}

func TestCloudDeploy_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "deploy", "--help")
	if err != nil {
		t.Fatalf("deploy --help: %v", err)
	}
	if !strings.Contains(stdout, "slot-id") {
		t.Errorf("help should mention --slot-id, got: %s", stdout)
	}
	if !strings.Contains(stdout, "digest") {
		t.Errorf("help should mention digest, got: %s", stdout)
	}
}

func TestCloudDeployments_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "deployments", "--help")
	if err != nil {
		t.Fatalf("deployments --help: %v", err)
	}
	if !strings.Contains(stdout, "deployments") {
		t.Errorf("help should mention deployments, got: %s", stdout)
	}
}

func TestCloudHelp_HasDeployAndDeployments(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "deploy") {
		t.Errorf("cloud --help should mention deploy, got: %s", stdout)
	}
	if !strings.Contains(stdout, "deployments") {
		t.Errorf("cloud --help should mention deployments, got: %s", stdout)
	}
}
