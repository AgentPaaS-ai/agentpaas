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

	var resp cloudclient.WhoamiResponse
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("unmarshal JSON: %v\noutput: %s", err, stdout)
	}
	if resp.TenantID != "json-tenant" {
		t.Errorf("TenantID = %q, want json-tenant", resp.TenantID)
	}
	if resp.Tier != "free" {
		t.Errorf("Tier = %q, want free", resp.Tier)
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
