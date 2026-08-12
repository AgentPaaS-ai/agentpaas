package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/spf13/cobra"
)

// setupFakeSecretStore overrides the secretStoreFactory to return a fake store.
func setupFakeSecretStore(t *testing.T) *secrets.FakeKeyStore {
	t.Helper()

	store := secrets.NewFakeKeyStore()

	oldFactory := secretStoreFactory
	secretStoreFactory = func(cmd *cobra.Command) (secrets.SecretStore, error) {
		return store, nil
	}
	t.Cleanup(func() {
		secretStoreFactory = oldFactory
		resetAgentCmd()
	})

	return store
}

func TestCloudSecrets_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "secrets"})
	if err != nil {
		t.Fatalf("Find cloud secrets: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "secrets", "push"})
	if err != nil {
		t.Fatalf("Find cloud secrets push: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "secrets", "list"})
	if err != nil {
		t.Fatalf("Find cloud secrets list: %v", err)
	}
}

func TestCloudSecrets_AliasSecret(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "secret"})
	if err != nil {
		t.Fatalf("Find cloud secret alias: %v", err)
	}
}

func TestCloudSecretsPush_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	secStore := setupFakeSecretStore(t)
	_ = secStore.Set(context.Background(), "my-key", []byte("super-secret-value"))

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/secrets/my-key" && r.Method == http.MethodPut {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_test_token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Read body to verify value is sent (but never printed by CLI).
			bodyBytes, _ := io.ReadAll(r.Body)
			var body map[string]string
			_ = json.Unmarshal(bodyBytes, &body)
			if body["value"] != "super-secret-value" {
				t.Errorf("server received value = %q, want super-secret-value", body["value"])
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "my-key")
	if err != nil {
		t.Fatalf("push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	// Check that the name was printed but NOT the value.
	if !strings.Contains(stdout, "pushed: my-key") {
		t.Errorf("expected 'pushed: my-key' in output, got: %q", stdout)
	}
	if strings.Contains(stdout, "super-secret-value") {
		t.Errorf("output LEAKED secret value: %q", stdout)
	}
	if strings.Contains(stderr, "super-secret-value") {
		t.Errorf("stderr LEAKED secret value: %q", stderr)
	}
}

func TestCloudSecretsPush_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	if err := store.Set(context.Background(), "apc_json_secret"); err != nil {
		t.Fatalf("store token: %v", err)
	}
	secStore := setupFakeSecretStore(t)
	if err := secStore.Set(context.Background(), "json-key", []byte("secret-value")); err != nil {
		t.Fatalf("store secret: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/secrets/json-key" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "json-key", "--json")
	if err != nil {
		t.Fatalf("push --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("push --json stderr = %q", stderr)
	}
	var got struct {
		Pushed []string `json:"pushed"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode push JSON: %v; stdout=%q", err, stdout)
	}
	if len(got.Pushed) != 1 || got.Pushed[0] != "json-key" {
		t.Fatalf("pushed = %#v, want json-key", got.Pushed)
	}
}

func TestCloudSecretsPush_NeverPrintsValue(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	secStore := setupFakeSecretStore(t)
	secretValue := "this-must-never-appear-in-output-xyz123"
	_ = secStore.Set(context.Background(), "safe-key", []byte(secretValue))

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/secrets/safe-key" && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "safe-key")
	if err != nil {
		t.Fatalf("push: err=%v", err)
	}

	if strings.Contains(stdout, secretValue) {
		t.Fatalf("stdout LEAKED secret value: %q", stdout)
	}
	if strings.Contains(stderr, secretValue) {
		t.Fatalf("stderr LEAKED secret value: %q", stderr)
	}
}

func TestCloudSecretsPush_MultipleNames(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	secStore := setupFakeSecretStore(t)
	_ = secStore.Set(context.Background(), "key-a", []byte("val-a"))
	_ = secStore.Set(context.Background(), "key-b", []byte("val-b"))

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/secrets/") && r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "key-a", "key-b")
	if err != nil {
		t.Fatalf("push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "pushed: key-a") {
		t.Errorf("expected 'pushed: key-a', got: %q", stdout)
	}
	if !strings.Contains(stdout, "pushed: key-b") {
		t.Errorf("expected 'pushed: key-b', got: %q", stdout)
	}
}

func TestCloudSecretsPush_LocalSecretNotFound(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t) // empty store — no secrets

	_, _, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "no-such-key")
	if err == nil {
		t.Fatal("expected error when local secret not found")
	}
	if !strings.Contains(err.Error(), "secret not found") {
		t.Errorf("error should mention 'secret not found', got: %v", err)
	}
}

func TestCloudSecretsPush_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t) // empty store

	_ = setupFakeSecretStore(t)

	_, _, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "some-key")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error should mention 'not logged in', got: %v", err)
	}
}

func TestCloudSecretsPush_InvalidName(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t)

	_, _, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "bad name")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "invalid secret name") {
		t.Errorf("error should mention 'invalid secret name', got: %v", err)
	}
}

func TestCloudSecretsList_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/secrets" && r.Method == http.MethodGet {
			resp := cloudclient.ListSecretsResponse{Secrets: []cloudclient.SecretLabel{
				{Name: "cloud-key-1", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-02T00:00:00Z"},
				{Name: "cloud-key-2", CreatedAt: "2025-02-01T00:00:00Z", UpdatedAt: "2025-02-02T00:00:00Z"},
			}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "secrets", "list")
	if err != nil {
		t.Fatalf("list: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "cloud-key-1") {
		t.Errorf("expected 'cloud-key-1' in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "cloud-key-2") {
		t.Errorf("expected 'cloud-key-2' in output, got: %q", stdout)
	}
}

func TestCloudSecretsList_NeverPrintsValues(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.ListSecretsResponse{Secrets: []cloudclient.SecretLabel{
			{Name: "key-alpha", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-02T00:00:00Z"},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "list")
	if err != nil {
		t.Fatalf("list: err=%v", err)
	}

	// The output should not contain any value field.
	if strings.Contains(stdout, `"value"`) || strings.Contains(stdout, "value_b64") {
		t.Errorf("list output should never contain value data, got: %q", stdout)
	}
}

func TestCloudSecretsList_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.ListSecretsResponse{Secrets: []cloudclient.SecretLabel{
			{Name: "json-key", CreatedAt: "2025-03-01T00:00:00Z", UpdatedAt: "2025-03-02T00:00:00Z"},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "list", "--json")
	if err != nil {
		t.Fatalf("list --json: err=%v", err)
	}

	var parsed []cloudclient.SecretLabel
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON: %v\noutput: %s", err, stdout)
	}
	if len(parsed) != 1 || parsed[0].Name != "json-key" {
		t.Errorf("expected 1 item with name 'json-key', got %v", parsed)
	}
}

func TestCloudSecretsList_Empty(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	_ = setupFakeSecretStore(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.ListSecretsResponse{Secrets: []cloudclient.SecretLabel{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "No cloud secrets") {
		t.Errorf("expected 'No cloud secrets' message, got: %q", stdout)
	}
}

func TestCloudSecretsList_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t) // empty store

	_ = setupFakeSecretStore(t)

	_, _, err := executeCloudCmd(t, "", "cloud", "secrets", "list")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error should mention 'not logged in', got: %v", err)
	}
}

func TestCloudSecretsPush_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "push", "--help")
	if err != nil {
		t.Fatalf("push --help: %v", err)
	}
	if !strings.Contains(stdout, "never") {
		t.Errorf("help should mention values are never displayed, got: %s", stdout)
	}
}

func TestCloudSecretsList_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "list", "--help")
	if err != nil {
		t.Fatalf("list --help: %v", err)
	}
	if !strings.Contains(stdout, "never") {
		t.Errorf("help should mention values are never displayed, got: %s", stdout)
	}
}

func TestCloudSecretsBind_OAuthDelegated_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	var captured []byte
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/deployments/dep_abc/secrets":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"bindings": []any{}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/deployments/dep_abc/secrets":
			body, _ := io.ReadAll(r.Body)
			captured = body
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "",
		"cloud", "secrets", "bind", "dep_abc", "gmail-oauth",
		"--as", "oauth_delegated",
		"--end-user-identity", "alice@example.com",
		"--oauth-provider", "google",
		"--oauth-client-id-credential", "google-client-id",
		"--oauth-client-secret-credential", "google-client-secret",
		"--oauth-scopes", "https://www.googleapis.com/auth/gmail.readonly",
		"--only",
	)
	if err != nil {
		t.Fatalf("bind oauth_delegated: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if len(captured) == 0 {
		t.Fatal("expected PUT body to be captured")
	}

	var req cloudclient.SetDeploymentSecretsRequest
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal request: %v body=%s", err, captured)
	}
	if len(req.Bindings) != 1 {
		t.Fatalf("bindings len = %d, want 1", len(req.Bindings))
	}
	b := req.Bindings[0]
	if b.SecretName != "gmail-oauth" {
		t.Errorf("SecretName = %q", b.SecretName)
	}
	if b.InjectAs != "oauth_delegated" {
		t.Errorf("InjectAs = %q", b.InjectAs)
	}
	if b.EndUserIdentity == nil || *b.EndUserIdentity != "alice@example.com" {
		t.Errorf("EndUserIdentity = %v", b.EndUserIdentity)
	}
	if b.OAuthConfig == nil {
		t.Fatal("OAuthConfig is nil")
	}
	if b.OAuthConfig.Provider != "google" {
		t.Errorf("Provider = %q", b.OAuthConfig.Provider)
	}
	if b.OAuthConfig.ClientIDCredential != "google-client-id" {
		t.Errorf("ClientIDCredential = %q", b.OAuthConfig.ClientIDCredential)
	}
	if b.OAuthConfig.ClientSecretCred != "google-client-secret" {
		t.Errorf("ClientSecretCred = %q", b.OAuthConfig.ClientSecretCred)
	}
	if len(b.OAuthConfig.Scopes) != 1 || b.OAuthConfig.Scopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Errorf("Scopes = %v", b.OAuthConfig.Scopes)
	}
	// MaxScopes defaults to Scopes when omitted.
	if len(b.OAuthConfig.MaxScopes) != 1 || b.OAuthConfig.MaxScopes[0] != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Errorf("MaxScopes = %v (want default to scopes)", b.OAuthConfig.MaxScopes)
	}
}

func TestCloudSecretsBind_OAuthDelegated_MaxScopesOverride(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	var captured []byte
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			body, _ := io.ReadAll(r.Body)
			captured = body
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"bindings": []any{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, _, err := executeCloudCmd(t, "",
		"cloud", "secrets", "bind", "dep_abc", "gmail-oauth",
		"--as", "oauth_delegated",
		"--end-user-identity", "alice@example.com",
		"--oauth-provider", "google",
		"--oauth-client-id-credential", "cid",
		"--oauth-client-secret-credential", "csec",
		"--oauth-scopes", "scope-a",
		"--oauth-max-scopes", "scope-a,scope-b",
		"--only",
	)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	var req cloudclient.SetDeploymentSecretsRequest
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	max := req.Bindings[0].OAuthConfig.MaxScopes
	if len(max) != 2 || max[0] != "scope-a" || max[1] != "scope-b" {
		t.Errorf("MaxScopes = %v", max)
	}
}

func TestCloudSecretsBind_OAuthDelegated_RequiresEndUserIdentity(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	_, _, err := executeCloudCmd(t, "",
		"cloud", "secrets", "bind", "dep_abc", "gmail-oauth",
		"--as", "oauth_delegated",
		"--oauth-provider", "google",
		"--oauth-client-id-credential", "cid",
		"--oauth-client-secret-credential", "csec",
		"--oauth-scopes", "scope-a",
	)
	if err == nil {
		t.Fatal("expected error when --end-user-identity missing")
	}
	if !strings.Contains(err.Error(), "--end-user-identity") {
		t.Errorf("error should mention --end-user-identity, got: %v", err)
	}
}

func TestCloudSecretsBind_OAuthDelegated_RequiresProviderAndCreds(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	_, _, err := executeCloudCmd(t, "",
		"cloud", "secrets", "bind", "dep_abc", "gmail-oauth",
		"--as", "oauth_delegated",
		"--end-user-identity", "alice@example.com",
		"--oauth-scopes", "scope-a",
	)
	if err == nil {
		t.Fatal("expected error when oauth provider/creds missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--oauth-provider") || !strings.Contains(msg, "--oauth-client-id-credential") {
		t.Errorf("error should mention oauth flags, got: %v", err)
	}
}

func TestCloudSecretsBind_OAuthDelegated_RequiresScopes(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_test_token")

	_, _, err := executeCloudCmd(t, "",
		"cloud", "secrets", "bind", "dep_abc", "gmail-oauth",
		"--as", "oauth_delegated",
		"--end-user-identity", "alice@example.com",
		"--oauth-provider", "google",
		"--oauth-client-id-credential", "cid",
		"--oauth-client-secret-credential", "csec",
	)
	if err == nil {
		t.Fatal("expected error when --oauth-scopes missing")
	}
	if !strings.Contains(err.Error(), "--oauth-scopes") {
		t.Errorf("error should mention --oauth-scopes, got: %v", err)
	}
}

func TestCloudSecretsBind_HelpMentionsOAuthDelegated(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "secrets", "bind", "--help")
	if err != nil {
		t.Fatalf("bind --help: %v", err)
	}
	if !strings.Contains(stdout, "oauth_delegated") {
		t.Errorf("help should mention oauth_delegated, got: %s", stdout)
	}
}
