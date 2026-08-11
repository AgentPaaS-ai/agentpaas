package secrets

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/oauth"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const testOAuthTokenValue = "ya29.test-oauth-access-token"

func newOAuthDelegatedPolicy(domain string, port int) *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: domain, Ports: []int{port}, Credential: "gmail_oauth"},
		},
		Credentials: []policy.Credential{
			{
				ID:           "gmail_oauth",
				Type:         "oauth_delegated",
				Provider:     "google",
				Header:       "Authorization",
				Scopes:       []string{"gmail.readonly"},
				MaxScopes:    []string{"gmail.readonly"},
				AuthEndpoint: "https://accounts.google.com/o/oauth2/v2/auth",
			},
		},
	}
}

func newOAuthBroker(t *testing.T, store oauth.Store, auditSink *recordingAuditSink, p *policy.Policy) *Broker {
	t.Helper()
	b, err := NewBroker(BrokerConfig{
		Store:            NewFakeKeyStore(), // static store present but must NOT be used for oauth_delegated
		Policy:           p,
		ActiveRuns:       []string{"run-active"},
		RuleMethods:      map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:            auditSink,
		OAuthStore:       store,
		OAuthEndUser:     func(runID string) string { return "user@example.com" },
		OAuthConsentBase: "http://127.0.0.1:18999",
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	return b
}

func putValidOAuthToken(t *testing.T, store oauth.Store, credentialID, endUser string) {
	t.Helper()
	_, err := store.Put(
		oauth.TokenKey{CredentialID: credentialID, EndUserIdentity: endUser},
		oauth.TokenPayload{
			AccessToken:     testOAuthTokenValue,
			RefreshToken:    "refresh-canary",
			AccessExpiresAt: time.Now().Add(time.Hour),
			GrantedScopes:   []string{"gmail.readonly"},
		},
	)
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}
}

// Test 1: No token stored -> typed AuthorizationRequiredError with consent URL.
func TestBrokerOAuthNoTokenReturnsAuthorizationRequired(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("RequestCredential returned nil error; want AuthorizationRequiredError")
	}
	authReq, ok := oauth.IsAuthorizationRequired(err)
	if !ok {
		t.Fatalf("RequestCredential error = %v; want *oauth.AuthorizationRequiredError", err)
	}
	if authReq.CredentialID != "gmail_oauth" {
		t.Fatalf("CredentialID = %q, want gmail_oauth", authReq.CredentialID)
	}
	if authReq.ConsentURL == "" {
		t.Fatal("ConsentURL is empty; want loopback consent URL")
	}
	if !strings.HasPrefix(authReq.ConsentURL, "http://127.0.0.1:") {
		t.Fatalf("ConsentURL = %q; want loopback URL", authReq.ConsentURL)
	}
	// Must never fall through to SecretStore — error should not be ErrSecretNotFound.
	if strings.Contains(err.Error(), "not set in the secret store") {
		t.Fatal("broker fell through to static SecretStore for oauth_delegated credential")
	}
	// Audit should record denial, no token value.
	rec := auditSink.last(t)
	if rec.Payload["status"] != "denied" {
		t.Fatalf("audit status = %v, want denied", rec.Payload["status"])
	}
	if strings.Contains(fmt.Sprint(rec.Payload), testOAuthTokenValue) {
		t.Fatalf("audit payload leaked token value: %#v", rec.Payload)
	}
}

// Test 2: Stored valid token -> Bearer injection, no token leak in audit.
func TestBrokerOAuthValidTokenInjectsBearer(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	injection, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if injection.HeaderName != "Authorization" {
		t.Fatalf("HeaderName = %q, want Authorization", injection.HeaderName)
	}
	if injection.HeaderValue != "Bearer "+testOAuthTokenValue {
		t.Fatalf("HeaderValue = %q, want Bearer <token>", injection.HeaderValue)
	}
	// Audit must not leak token.
	rec := auditSink.last(t)
	if rec.Payload["status"] != "injected" {
		t.Fatalf("audit status = %v, want injected", rec.Payload["status"])
	}
	if strings.Contains(fmt.Sprint(rec.Payload), testOAuthTokenValue) {
		t.Fatalf("audit payload leaked token value: %#v", rec.Payload)
	}
	// String representation must not leak.
	if strings.Contains(injection.String(), testOAuthTokenValue) {
		t.Fatalf("CredentialInjection string leaked token: %s", injection.String())
	}
}

// Test 3: Expired token -> deny with AuthorizationRequiredError.
func TestBrokerOAuthExpiredTokenReturnsAuthorizationRequired(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	// Put an expired token.
	_, err := store.Put(
		oauth.TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "user@example.com"},
		oauth.TokenPayload{
			AccessToken:     testOAuthTokenValue,
			RefreshToken:    "refresh-canary",
			AccessExpiresAt: time.Now().Add(-time.Minute), // expired
			GrantedScopes:   []string{"gmail.readonly"},
		},
	)
	if err != nil {
		t.Fatalf("store.Put: %v", err)
	}
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("RequestCredential returned nil; want AuthorizationRequiredError for expired token")
	}
	authReq, ok := oauth.IsAuthorizationRequired(err)
	if !ok {
		t.Fatalf("error = %v; want *AuthorizationRequiredError", err)
	}
	if authReq.CredentialID != "gmail_oauth" {
		t.Fatalf("CredentialID = %q", authReq.CredentialID)
	}
	// Expired token should have been deleted from store.
	_, getErr := store.Get(oauth.TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "user@example.com"})
	if getErr != oauth.ErrNotFound {
		t.Fatalf("expected expired token deleted from store, got err=%v", getErr)
	}
}

// Test 4: Revoked credential -> deny (not AuthorizationRequired, but revoked denial).
func TestBrokerOAuthRevokedDenies(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	if err := broker.Revoke(ctx, "gmail_oauth"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("RequestCredential returned nil; want revoked denial")
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("error = %v; want revoked denial", err)
	}
	// Should NOT be AuthorizationRequiredError — revocation takes precedence.
	if _, ok := oauth.IsAuthorizationRequired(err); ok {
		t.Fatal("revoked credential returned AuthorizationRequiredError instead of revoked denial")
	}
}

// Test 5: Existing brokered tests unchanged — oauth_delegated should not interfere.
func TestBrokerOAuthDoesNotAffectBrokeredCredentials(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	// Use a standard brokered policy with an oauth store configured.
	b, err := NewBroker(BrokerConfig{
		Store:            newSecretStore(t),
		Policy:           newBrokeredPolicy("api.example.com", 443),
		ActiveRuns:       []string{"run-active"},
		RuleMethods:      map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:            auditSink,
		OAuthStore:       store,
		OAuthEndUser:     func(runID string) string { return "user@example.com" },
		OAuthConsentBase: "http://127.0.0.1:18999",
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	injection, err := b.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if injection.HeaderValue != testSecretValue {
		t.Fatalf("HeaderValue = %q, want %q", injection.HeaderValue, testSecretValue)
	}
	if injection.HeaderName != "Authorization" {
		t.Fatalf("HeaderName = %q, want Authorization", injection.HeaderName)
	}
}

// Test 6: RevokeOAuthToken deletes token from the local OAuth store.
func TestBrokerRevokeOAuthTokenDeletesFromStore(t *testing.T) {
	ctx := context.Background()
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, &recordingAuditSink{}, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	// Verify token exists.
	_, err := store.Get(oauth.TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "user@example.com"})
	if err != nil {
		t.Fatalf("pre-condition: token not in store: %v", err)
	}

	if err := broker.RevokeOAuthToken(ctx, "gmail_oauth", "user@example.com"); err != nil {
		t.Fatalf("RevokeOAuthToken: %v", err)
	}

	_, err = store.Get(oauth.TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "user@example.com"})
	if err != oauth.ErrNotFound {
		t.Fatalf("after revoke: err = %v, want ErrNotFound", err)
	}
}

// Test 7: OAuth egress checks still apply (wrong domain denied).
func TestBrokerOAuthWrongDomainDenied(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://wrong.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("RequestCredential error = %v, want domain denial", err)
	}
}

// Test 8: OAuth inactive run denied.
func TestBrokerOAuthInactiveRunDenied(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	_, err := broker.RequestCredential(ctx, "run-stale", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "active run") {
		t.Fatalf("RequestCredential error = %v, want active run denial", err)
	}
}

// Test 9: OAuth wrong method denied.
func TestBrokerOAuthWrongMethodDenied(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := oauth.NewMemoryStore()
	putValidOAuthToken(t, store, "gmail_oauth", "user@example.com")
	broker := newOAuthBroker(t, store, auditSink, newOAuthDelegatedPolicy("gmail.googleapis.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://gmail.googleapis.com/v1", http.MethodPost)
	if err == nil || !strings.Contains(err.Error(), "method") {
		t.Fatalf("RequestCredential error = %v, want method denial", err)
	}
}
