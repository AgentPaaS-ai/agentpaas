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

func TestCloudLogin_TokenStdinJSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)

	stdout, stderr, err := executeCloudCmd(t, "apc_json_login_token", "cloud", "login", "--token-stdin", "--json")
	if err != nil {
		t.Fatalf("login --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("login --json stderr = %q", stderr)
	}
	if strings.Contains(stdout, "apc_json_login_token") {
		t.Fatalf("login JSON leaked token: %q", stdout)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode login JSON: %v; stdout=%q", err, stdout)
	}
	if got["message"] != "Logged in." {
		t.Fatalf("login JSON = %#v", got)
	}
	if _, err := store.Get(context.Background()); err != nil {
		t.Fatalf("stored login token: %v", err)
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

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "login", "--open-browser")
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

func TestCloudLogin_DefaultDoesNotOpenBrowser(t *testing.T) {
	cmd := newCloudLoginCmd()
	flag := cmd.Flags().Lookup("open-browser")
	if flag == nil {
		t.Fatal("cloud login missing --open-browser flag")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--open-browser default = %q, want false", flag.DefValue)
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
				AgentLimit:       25,
				AgentsUsed:       3,
				CPUMinuteLimit:   100,
				CPUMinutesUsed:   12.5,
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
	for _, want := range []string{"Agent limit: 3/25", "CPU minutes used: 12.5/100"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in output, got: %q", want, stdout)
		}
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
				AgentLimit:       2,
				AgentsUsed:       1,
				CPUMinuteLimit:   60,
				CPUMinutesUsed:   4.5,
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
		TenantID       string  `json:"tenant_id"`
		Tier           string  `json:"tier"`
		AgentLimit     int     `json:"agent_limit"`
		AgentsUsed     int     `json:"agents_used"`
		CPUMinuteLimit int     `json:"cpu_minute_limit"`
		CPUMinutesUsed float64 `json:"cpu_minutes_used"`
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
	if parsed.AgentLimit != 2 || parsed.AgentsUsed != 1 {
		t.Errorf("agent usage = %d/%d, want 1/2", parsed.AgentsUsed, parsed.AgentLimit)
	}
	if parsed.CPUMinuteLimit != 60 || parsed.CPUMinutesUsed != 4.5 {
		t.Errorf("CPU usage = %g/%d, want 4.5/60", parsed.CPUMinutesUsed, parsed.CPUMinuteLimit)
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

	_, _, err = cmd.Find([]string{"cloud", "undeploy"})
	if err != nil {
		t.Fatalf("Find cloud undeploy: %v", err)
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
			if req.InstanceType == nil || *req.InstanceType != "basic" {
				t.Errorf("InstanceType = %v, want basic", req.InstanceType)
			}

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
	if !strings.Contains(stderr, "Creating deployment…") {
		t.Errorf("stderr missing deploy progress, got %q", stderr)
	}
	if strings.Contains(stdout, "Creating deployment") {
		t.Errorf("stdout must not contain progress, got %q", stdout)
	}
}

func TestCloudDeploy_Latest_WithInstanceTypeStandard2(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_instance_type")

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images":
			if r.Method != http.MethodGet {
				t.Errorf("images method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{{
				ID:          "img-latest-001",
				ImageDigest: digest,
				Status:      "admitted",
			}})
		case "/v1/deployments":
			if r.Method != http.MethodPost {
				t.Errorf("deployments method = %s, want POST", r.Method)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode deployment body: %v", err)
			}
			var gotInstanceType string
			if err := json.Unmarshal(body["instance_type"], &gotInstanceType); err != nil {
				t.Errorf("decode instance_type: %v", err)
			}
			if gotInstanceType != "standard-2" {
				t.Errorf("instance_type = %q, want standard-2", gotInstanceType)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
				ID:          "dep-instance-type-001",
				ImageDigest: digest,
				Status:      "pending",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--instance-type", "standard-2")
	if err != nil {
		t.Fatalf("deploy latest --instance-type standard-2: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-instance-type-001") {
		t.Errorf("expected dep-instance-type-001 in output, got: %q", stdout)
	}
}

func TestCloudDeploy_DevInstanceTypeRejected(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_invalid_instance_type")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--instance-type", "dev")
	if err == nil {
		t.Fatal("expected error for dev instance type")
	}
	combined := err.Error() + stderr
	want := "instance_type 'dev' is an alias for 'lite' (256MiB) which is too small for LLM agents — use 'basic' or higher"
	if !strings.Contains(combined, want) {
		t.Errorf("error should contain %q, got: %s", want, combined)
	}
}

func TestCloudDeploy_InvalidInstanceType(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_invalid_instance_type")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--instance-type", "gigantic")
	if err == nil {
		t.Fatal("expected error for invalid instance type")
	}
	combined := err.Error() + stderr
	want := "instance_type must be one of: lite, basic, standard-1, standard-2, standard-3, standard-4"
	if !strings.Contains(combined, want) {
		t.Errorf("error should contain %q, got: %s", want, combined)
	}
}

func TestCloudDeploy_Latest_WithMaxConcurrentRuns(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_max_concurrent")

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images":
			if r.Method != http.MethodGet {
				t.Errorf("images method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{{
				ID:          "img-latest-001",
				ImageDigest: digest,
				Status:      "admitted",
			}})
		case "/v1/deployments":
			if r.Method != http.MethodPost {
				t.Errorf("deployments method = %s, want POST", r.Method)
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode deployment body: %v", err)
			}
			raw, ok := body["max_concurrent_runs"]
			if !ok {
				t.Fatal("max_concurrent_runs missing from POST body")
			}
			var got int
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Errorf("decode max_concurrent_runs: %v", err)
			}
			if got != 2 {
				t.Errorf("max_concurrent_runs = %d, want 2", got)
			}
			lock := 2
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
				ID:                "dep-max-concurrent-001",
				ImageDigest:       digest,
				Status:            "pending",
				MaxConcurrentRuns: &lock,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--max-concurrent-runs", "2")
	if err != nil {
		t.Fatalf("deploy latest --max-concurrent-runs 2: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-max-concurrent-001") {
		t.Errorf("expected dep-max-concurrent-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "2") {
		t.Errorf("expected lock value in output, got: %q", stdout)
	}
}

func TestCloudDeploy_Latest_OmitsMaxConcurrentRuns(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_no_max_concurrent")

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{{
				ID:          "img-latest-001",
				ImageDigest: digest,
				Status:      "admitted",
			}})
		case "/v1/deployments":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode deployment body: %v", err)
			}
			if _, ok := body["max_concurrent_runs"]; ok {
				t.Error("max_concurrent_runs should be omitted when flag is unset")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
				ID:          "dep-no-lock-001",
				ImageDigest: digest,
				Status:      "pending",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest")
	if err != nil {
		t.Fatalf("deploy latest: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "dep-no-lock-001") {
		t.Errorf("expected dep-no-lock-001 in output, got: %q", stdout)
	}
}

func TestCloudDeploy_MaxConcurrentRunsZeroRejected(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_zero_lock")

	hits := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--max-concurrent-runs", "0")
	if err == nil {
		t.Fatal("expected error for --max-concurrent-runs 0")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "max_concurrent_runs must be an integer >= 1") {
		t.Errorf("error should mention max_concurrent_runs must be an integer >= 1, got: %s", combined)
	}
	if hits != 0 {
		t.Errorf("expected no HTTP requests, got %d", hits)
	}
}

func TestCloudDeploy_Update_MaxConcurrentRuns(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_update_lock")

	var gotMethod, gotPath string
	var gotBody map[string]json.RawMessage
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		lock := 1
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
			ID:                "dep_x",
			Status:            "running",
			MaxConcurrentRuns: &lock,
		})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "update", "dep_x", "--max-concurrent-runs", "1")
	if err != nil {
		t.Fatalf("deploy update --max-concurrent-runs 1: err=%v stderr=%q", err, stderr)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/v1/deployments/dep_x" {
		t.Errorf("path = %s, want /v1/deployments/dep_x", gotPath)
	}
	raw, ok := gotBody["max_concurrent_runs"]
	if !ok {
		t.Fatal("max_concurrent_runs missing from PATCH body")
	}
	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode max_concurrent_runs: %v", err)
	}
	if got != 1 {
		t.Errorf("max_concurrent_runs = %d, want 1", got)
	}
}

func TestCloudDeploy_Update_UnlockConcurrency(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_update_unlock")

	var gotMethod, gotPath string
	var gotBody map[string]json.RawMessage
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cloudclient.DeploymentRecord{
			ID:     "dep_x",
			Status: "running",
		})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "update", "dep_x", "--unlock-concurrency")
	if err != nil {
		t.Fatalf("deploy update --unlock-concurrency: err=%v stderr=%q", err, stderr)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/v1/deployments/dep_x" {
		t.Errorf("path = %s, want /v1/deployments/dep_x", gotPath)
	}
	raw, ok := gotBody["max_concurrent_runs"]
	if !ok {
		t.Fatal("max_concurrent_runs missing from PATCH body")
	}
	if string(raw) != "null" {
		t.Errorf("max_concurrent_runs = %s, want null", raw)
	}
}

func TestCloudDeploy_Update_RequiresExactlyOneFlag(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_update_flags")

	hits := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "update", "dep_x")
	if err == nil {
		t.Fatal("expected error when neither flag is set")
	}
	if hits != 0 {
		t.Errorf("neither-flag case should not hit HTTP, got %d", hits)
	}
	combined := err.Error() + stderr
	if combined == "" {
		t.Error("expected an error message for neither flag")
	}

	_, stderr, err = executeCloudCmd(t, "", "cloud", "deploy", "update", "dep_x", "--max-concurrent-runs", "1", "--unlock-concurrency")
	if err == nil {
		t.Fatal("expected error when both flags are set")
	}
	if hits != 0 {
		t.Errorf("both-flags case should not hit HTTP, got %d", hits)
	}
	combined = err.Error() + stderr
	if combined == "" {
		t.Error("expected an error message for both flags")
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

func TestCloudDeploy_CalleesFlag(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_deploy_callee")

	var got cloudclient.CreateDeploymentRequest
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/deployments" {
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Errorf("decode body: %v", err)
			}
			resp := cloudclient.DeploymentRecord{
				ID:          "dep-callee-001",
				ImageDigest: got.ImageDigest,
				Status:      "pending",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy",
		"--callee", "dep_aaa000000000000000000000000aa",
		"--callee", "dep_bbb000000000000000000000000aa",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("deploy: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if len(got.Callees) != 2 {
		t.Fatalf("callees len = %d, want 2", len(got.Callees))
	}
	if got.Callees[0].DeploymentID != "dep_aaa000000000000000000000000aa" {
		t.Errorf("callees[0] = %q", got.Callees[0].DeploymentID)
	}
	if got.Callees[1].DeploymentID != "dep_bbb000000000000000000000000aa" {
		t.Errorf("callees[1] = %q", got.Callees[1].DeploymentID)
	}
	if !strings.Contains(stdout, "dep-callee-001") {
		t.Errorf("expected dep-callee-001 in output, got: %q", stdout)
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

func TestCloudDeploy_IdempotentReplayJSONIsClean(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_deploy_replay"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/images":
			_, _ = w.Write([]byte(`[{"id":"img-existing","image_digest":"` + digest + `","status":"admitted"}]`))
		case "/v1/deployments":
			_, _ = w.Write([]byte(`{"id":"dep-existing","image_digest":"` + digest + `","status":"idempotent_replay"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "deploy", "latest", "--json")
	if err != nil {
		t.Fatalf("idempotent deploy: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("idempotent deploy stderr = %q", stderr)
	}
	var got cloudclient.DeploymentRecord
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode idempotent deployment: %v; stdout=%q", err, stdout)
	}
	if got.ID != "dep-existing" || got.Status != "idempotent_replay" {
		t.Fatalf("deployment = %#v, want existing idempotent replay", got)
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

func TestCloudUndeploy_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_undeploy_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep-delete-001" {
			t.Errorf("expected /v1/deployments/dep-delete-001, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_undeploy_test" {
			t.Errorf("Authorization = %q, want Bearer apc_undeploy_test", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cloudclient.DeploymentDeleteResult{
			ID:     "dep-delete-001",
			Status: "deleted",
		})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "undeploy", "dep-delete-001", "--yes")
	if err != nil {
		t.Fatalf("undeploy: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Undeployed") {
		t.Errorf("expected 'Undeployed' in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "slot freed") {
		t.Errorf("expected 'slot freed' in output, got: %q", stdout)
	}
}

func TestCloudUndeploy_NotFound(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_undeploy_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "undeploy", "missing-deployment", "--yes")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not_found") {
		t.Errorf("error should surface not_found, got: %s", combined)
	}
}

func TestCloudUndeploy_NotFoundJSONExitCode(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_undeploy_json"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found","reason":"not_found","message":"deployment missing"}`))
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "undeploy", "missing-deployment", "--json", "--yes")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if got := CloudExitCode(err); got != cloudExitNotFound {
		t.Fatalf("CloudExitCode = %d, want %d", got, cloudExitNotFound)
	}
	if stderr != "" {
		t.Fatalf("JSON not-found error wrote stderr: %q", stderr)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode not-found error: %v; stdout=%q", err, stdout)
	}
	if got["error"] != "not_found" || got["reason"] != "not_found" || got["message"] != "deployment missing" {
		t.Fatalf("not-found envelope = %#v", got)
	}
}

func TestCloudUndeploy_RequiresYes(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_undeploy_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("cloud undeploy without --yes must not call DELETE; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "undeploy", "dep-delete-001")
	if err == nil {
		t.Fatal("expected error without --yes")
	}
	combined := err.Error() + stderr
	want := "cloud undeploy: refusing without --yes (this deletes a live deployment)"
	if !strings.Contains(combined, want) {
		t.Errorf("error = %q, want containing %q", combined, want)
	}
}

func TestCloudList_AliasToRegistry(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_alias_test"); err != nil {
		t.Fatalf("store token: %v", err)
	}

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/registry" {
			t.Errorf("path = %q, want /v1/registry", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloudclient.RegistryResponse{
			Assets: cloudclient.RegistryAssets{
				Deployments: []cloudclient.RegistryDeployment{{AgentName: "asset-alias", Kind: "agent"}},
			},
		})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "list")
	if err != nil {
		t.Fatalf("cloud list: %v", err)
	}
	if !strings.Contains(stdout, "asset-alias") {
		t.Errorf("expected asset-alias in output, got: %q", stdout)
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
	for _, preset := range []string{"lite", "basic", "standard-1", "standard-2", "standard-3", "standard-4"} {
		if !strings.Contains(stdout, preset) {
			t.Errorf("help should mention instance preset %q, got: %s", preset, stdout)
		}
	}
	if !strings.Contains(stdout, "default: basic") {
		t.Errorf("help should mention basic as the default, got: %s", stdout)
	}
	if !strings.Contains(stdout, "max-concurrent-runs") {
		t.Errorf("help should mention max-concurrent-runs, got: %s", stdout)
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

// --- Cloud run tests ---

func TestCloudRun_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "run"})
	if err != nil {
		t.Fatalf("Find cloud run: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "status"})
	if err != nil {
		t.Fatalf("Find cloud status: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "cancel"})
	if err != nil {
		t.Fatalf("Find cloud cancel: %v", err)
	}
}

func TestCloudRun_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "run", "dep-abc")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudRun_MissingArgs(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "run")
	if err == nil {
		t.Fatal("expected error for missing deployment_id")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "accepts 1 arg") {
		t.Errorf("error should mention accepts 1 arg, got: %s", combined)
	}
}

func TestCloudRun_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_run_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs" && r.Method == http.MethodPost {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_run_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var req cloudclient.CreateRunRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			resp := cloudclient.RunRecord{
				ID:           "run-created-001",
				DeploymentID: req.DeploymentID,
				Status:       "pending",
				CreatedAt:    "2025-01-15T10:30:00Z",
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

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "run", "dep-abc")
	if err != nil {
		t.Fatalf("run: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-created-001") {
		t.Errorf("expected run-created-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "pending") {
		t.Errorf("expected 'pending' status in output, got: %q", stdout)
	}
}

func TestCloudRun_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_run_json")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.RunRecord{
			ID:           "run-json-001",
			DeploymentID: "dep-abc",
			Status:       "pending",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "run", "--json", "dep-abc")
	if err != nil {
		t.Fatalf("run --json: %v", err)
	}
	var parsed struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed.ID != "run-json-001" {
		t.Errorf("ID = %q, want run-json-001", parsed.ID)
	}
}

func TestCloudRun_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "run", "--help")
	if err != nil {
		t.Fatalf("run --help: %v", err)
	}
	if !strings.Contains(stdout, "deployment_id") {
		t.Errorf("help should mention deployment_id, got: %s", stdout)
	}
}

// --- Cloud status tests ---

func TestCloudStatus_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "status")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudStatus_ListRuns(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_status_list")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs" && r.Method == http.MethodGet {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_status_list" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			runs := []cloudclient.RunRecord{
				{ID: "run-1", DeploymentID: "dep-1", Status: "running"},
				{ID: "run-2", DeploymentID: "dep-2", Status: "failed"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(runs)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "status")
	if err != nil {
		t.Fatalf("status: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-1") {
		t.Errorf("expected run-1 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "run-2") {
		t.Errorf("expected run-2 in output, got: %q", stdout)
	}
}

func TestCloudStatus_GetRun(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_status_get")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs/run-abc" && r.Method == http.MethodGet {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_status_get" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			run := cloudclient.RunRecord{
				ID:           "run-abc",
				DeploymentID: "dep-xyz",
				Status:       "running",
				CreatedAt:    "2025-01-15T10:30:00Z",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(run)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "status", "run-abc")
	if err != nil {
		t.Fatalf("status run-abc: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-abc") {
		t.Errorf("expected run-abc in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "running") {
		t.Errorf("expected 'running' status in output, got: %q", stdout)
	}
}

func TestCloudStatus_Empty(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_status_empty")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]cloudclient.RunRecord{})
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "No runs") {
		t.Errorf("expected 'No runs' message, got: %q", stdout)
	}
}

func TestCloudStatus_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_status_json")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs := []cloudclient.RunRecord{
			{ID: "run-json", DeploymentID: "dep-json", Status: "running"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(runs)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var parsed []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if len(parsed) != 1 || parsed[0].ID != "run-json" {
		t.Errorf("expected run-json, got %v", parsed)
	}
}

func TestCloudStatus_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "status", "--help")
	if err != nil {
		t.Fatalf("status --help: %v", err)
	}
	if !strings.Contains(stdout, "status") {
		t.Errorf("help should mention status, got: %s", stdout)
	}
}

// --- Cloud cancel tests ---

func TestCloudCancel_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "cancel", "run-abc")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "Not logged in") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected 'not logged in' message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudCancel_MissingArgs(t *testing.T) {
	_ = setupFakeTokenStore(t)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "cancel")
	if err == nil {
		t.Fatal("expected error for missing run_id")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "accepts 1 arg") {
		t.Errorf("error should mention accepts 1 arg, got: %s", combined)
	}
}

func TestCloudCancel_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_cancel_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs/run-abc/cancel" && r.Method == http.MethodPost {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_cancel_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			resp := cloudclient.RunRecord{
				ID:           "run-abc",
				DeploymentID: "dep-xyz",
				Status:       "cancelled",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "cancel", "run-abc")
	if err != nil {
		t.Fatalf("cancel: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "run-abc") {
		t.Errorf("expected run-abc in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "cancelled") {
		t.Errorf("expected 'cancelled' status in output, got: %q", stdout)
	}
}

func TestCloudCancel_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_cancel_json")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.RunRecord{
			ID:           "run-abc",
			DeploymentID: "dep-xyz",
			Status:       "cancelled",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "cancel", "--json", "run-abc")
	if err != nil {
		t.Fatalf("cancel --json: %v", err)
	}
	var parsed struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if parsed.ID != "run-abc" {
		t.Errorf("ID = %q, want run-abc", parsed.ID)
	}
	if parsed.Status != "cancelled" {
		t.Errorf("Status = %q, want cancelled", parsed.Status)
	}
}

func TestCloudCancel_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "cancel", "--help")
	if err != nil {
		t.Fatalf("cancel --help: %v", err)
	}
	if !strings.Contains(stdout, "run_id") {
		t.Errorf("help should mention run_id, got: %s", stdout)
	}
}

func TestCloudHelp_HasRunStatusCancel(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "run") {
		t.Errorf("cloud --help should mention run, got: %s", stdout)
	}
	if !strings.Contains(stdout, "status") {
		t.Errorf("cloud --help should mention status, got: %s", stdout)
	}
	if !strings.Contains(stdout, "cancel") {
		t.Errorf("cloud --help should mention cancel, got: %s", stdout)
	}
}
