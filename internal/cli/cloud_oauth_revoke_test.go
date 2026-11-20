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
)

func TestCloudOauthRevoke_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "oauth"})
	if err != nil {
		t.Fatalf("Find cloud oauth: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "oauth", "revoke"})
	if err != nil {
		t.Fatalf("Find cloud oauth revoke: %v", err)
	}
}

func TestCloudOauthRevoke_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_revoke_token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/oauth/revoke" {
			t.Errorf("expected /v1/oauth/revoke, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_revoke_token" {
			t.Errorf("Authorization = %q, want Bearer apc_revoke_token", auth)
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		// tenant_id MUST NOT appear in the request body.
		if _, ok := body["tenant_id"]; ok {
			t.Errorf("request body must NOT contain tenant_id, got: %s", string(bodyBytes))
		}

		// Verify the three identifying fields.
		checks := map[string]string{
			"deployment_id":     "dep-revoke-1",
			"credential_id":     "cred-revoke-1",
			"end_user_identity": "alice@example.com",
		}
		for field, want := range checks {
			raw, ok := body[field]
			if !ok {
				t.Errorf("body missing %q, got: %s", field, string(bodyBytes))
				continue
			}
			var got string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Errorf("decode %q: %v", field, err)
				continue
			}
			if got != want {
				t.Errorf("%s = %q, want %q", field, got, want)
			}
		}

		resp := cloudclient.OAuthRevokeResult{
			DeploymentID:    "dep-revoke-1",
			CredentialID:    "cred-revoke-1",
			EndUserIdentity: "alice@example.com",
			Revoked:         true,
			RevokedAt:       "2025-08-10T12:00:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-revoke-1", "cred-revoke-1", "alice@example.com")
	if err != nil {
		t.Fatalf("revoke: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "dep-revoke-1") {
		t.Errorf("expected deployment id in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "cred-revoke-1") {
		t.Errorf("expected credential id in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "alice@example.com") {
		t.Errorf("expected end-user identity in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Revoked:") {
		t.Errorf("expected 'Revoked:' status line in output, got: %q", stdout)
	}
}

func TestCloudOauthRevoke_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_json_revoke")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.OAuthRevokeResult{
			DeploymentID:    "dep-json",
			CredentialID:    "cred-json",
			EndUserIdentity: "bob@example.com",
			Revoked:         true,
			RevokedAt:       "2025-08-10T12:30:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-json", "cred-json", "bob@example.com", "--json")
	if err != nil {
		t.Fatalf("revoke --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("revoke --json stderr = %q", stderr)
	}

	var parsed cloudclient.OAuthRevokeResult
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON: %v; stdout=%s", err, stdout)
	}
	if parsed.DeploymentID != "dep-json" {
		t.Errorf("DeploymentID = %q, want dep-json", parsed.DeploymentID)
	}
	if parsed.CredentialID != "cred-json" {
		t.Errorf("CredentialID = %q, want cred-json", parsed.CredentialID)
	}
	if parsed.EndUserIdentity != "bob@example.com" {
		t.Errorf("EndUserIdentity = %q, want bob@example.com", parsed.EndUserIdentity)
	}
	if !parsed.Revoked {
		t.Errorf("Revoked = false, want true")
	}
}

func TestCloudOauthRevoke_NeverPrintsToken(t *testing.T) {
	store := setupFakeTokenStore(t)
	secretToken := "apc_super_secret_never_print_xyz789"
	_ = store.Set(context.Background(), secretToken)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloudclient.OAuthRevokeResult{
			DeploymentID:    "dep-tok",
			CredentialID:    "cred-tok",
			EndUserIdentity: "carol@example.com",
			Revoked:         true,
			RevokedAt:       "2025-08-10T13:00:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-tok", "cred-tok", "carol@example.com")
	if err != nil {
		t.Fatalf("revoke: err=%v", err)
	}
	if strings.Contains(stdout, secretToken) {
		t.Errorf("stdout LEAKED token: %q", stdout)
	}
	if strings.Contains(stderr, secretToken) {
		t.Errorf("stderr LEAKED token: %q", stderr)
	}
}

func TestCloudOauthRevoke_RequestBodyExcludesTenantID(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cloudclient.OAuthRevokeResult{})
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-x", "cred-x", "dave@example.com")
	if err != nil {
		t.Fatalf("revoke: err=%v", err)
	}

	if strings.Contains(string(capturedBody), "tenant_id") {
		t.Errorf("request body must NOT contain tenant_id, got: %s", string(capturedBody))
	}
}

func TestCloudOauthRevoke_401_IsError(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_expired")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", server.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-401", "cred-401", "eve@example.com")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not authenticated") && !strings.Contains(combined, "not logged in") {
		t.Errorf("expected not-authenticated/not-logged-in message, got: err=%q stderr=%q", err, stderr)
	}
}

func TestCloudOauthRevoke_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t) // empty store

	_, _, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-nli", "cred-nli", "frank@example.com")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error should mention 'not logged in', got: %v", err)
	}
}

func TestCloudOauthRevoke_InvalidArgs(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "token")

	// Too few args.
	_, _, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "dep-only", "cred-only")
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestCloudOauthRevoke_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "oauth", "revoke", "--help")
	if err != nil {
		t.Fatalf("revoke --help: %v", err)
	}
	if !strings.Contains(stdout, "revoke") {
		t.Errorf("help should mention revoke, got: %s", stdout)
	}
	if !strings.Contains(stdout, "tenant") {
		t.Errorf("help should mention tenant_id exclusion, got: %s", stdout)
	}
}
