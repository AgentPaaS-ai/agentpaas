package harness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadOAuthBindingsFromJSON_LoadsEntries(t *testing.T) {
	raw := `[
		{
			"credential_id": "gmail_oauth",
			"host_pattern": "gmail.googleapis.com",
			"consent_url": "https://cloud.example/oauth/start/g1?tenant_id=t1",
			"end_user_identity": "user1"
		}
	]`
	s := &harnessRPCServer{}
	if err := s.LoadOAuthBindingsFromJSON(raw); err != nil {
		t.Fatalf("LoadOAuthBindingsFromJSON: %v", err)
	}
	if len(s.oauthBindings) != 1 {
		t.Fatalf("got %d bindings, want 1", len(s.oauthBindings))
	}
	b := s.oauthBindings[0]
	if b.CredentialID != "gmail_oauth" || b.HostPattern != "gmail.googleapis.com" {
		t.Fatalf("unexpected binding: %+v", b)
	}
	if b.ConsentURL == "" || b.EndUserIdentity != "user1" {
		t.Fatalf("missing consent/end_user: %+v", b)
	}
}

func TestLoadOAuthBindingsFromJSON_EmptyOK(t *testing.T) {
	s := &harnessRPCServer{}
	if err := s.LoadOAuthBindingsFromJSON(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := s.LoadOAuthBindingsFromJSON("[]"); err != nil {
		t.Fatalf("empty array: %v", err)
	}
}

func TestHandleHTTP_OAuthBindingRequiresConsent(t *testing.T) {
	s := &harnessRPCServer{
		oauthBindings: []oauthBinding{{
			CredentialID:    "gmail_oauth",
			HostPattern:     "gmail.googleapis.com",
			ConsentURL:      "https://cloud.example/oauth/start/grant99?tenant_id=t1",
			EndUserIdentity: "user1",
		}},
	}
	state := &rpcInvokeState{
		payload:     map[string]any{},
		credentials: map[string]rpcCredential{},
		budget:      NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleHTTP(rpcRequest{
		ID:     "1",
		Method: "http",
		Params: map[string]any{
			"method": "GET",
			"url":    "https://gmail.googleapis.com/gmail/v1/users/me/messages",
		},
	}, state, false)

	if resp.OK {
		t.Fatal("expected authorization_required error, got OK")
	}
	if resp.Code != "authorization_required" {
		t.Fatalf("code=%q want authorization_required", resp.Code)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(resp.Error), &body); err != nil {
		t.Fatalf("error payload not JSON: %q (%v)", resp.Error, err)
	}
	if body["error"] != "authorization_required" {
		t.Fatalf("error field=%v", body["error"])
	}
	if body["consent_url"] != "https://cloud.example/oauth/start/grant99?tenant_id=t1" {
		t.Fatalf("consent_url=%v", body["consent_url"])
	}
	if body["credential_id"] != "gmail_oauth" {
		t.Fatalf("credential_id=%v", body["credential_id"])
	}
	if body["grant_id"] != "grant99" {
		t.Fatalf("grant_id=%v want grant99", body["grant_id"])
	}
}

func TestHandleHTTP_OAuthBindingInjectsPreResolvedToken(t *testing.T) {
	var sawAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer func() { ts.Close() }()

	s := &harnessRPCServer{
		oauthBindings: []oauthBinding{{
			CredentialID:    "gmail_oauth",
			HostPattern:     "gmail.googleapis.com",
			ConsentURL:      "",
			EndUserIdentity: "user1",
		}},
	}
	// Host match uses the original URL host; rewrite gateway so client.Do hits test server.
	t.Setenv("AGENTPAAS_GATEWAY_URL", ts.URL)
	state := &rpcInvokeState{
		payload: map[string]any{},
		credentials: map[string]rpcCredential{
			"gmail_oauth": {Header: "Authorization", Value: "Bearer tok-pre-resolved"},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleHTTP(rpcRequest{
		ID:     "1",
		Method: "http",
		Params: map[string]any{
			"method": "GET",
			"url":    "https://gmail.googleapis.com/gmail/v1/users/me/messages",
		},
	}, state, false)
	if !resp.OK {
		t.Fatalf("expected OK, got %s (%s)", resp.Error, resp.Code)
	}
	if sawAuth != "Bearer tok-pre-resolved" {
		t.Fatalf("Authorization=%q want Bearer tok-pre-resolved", sawAuth)
	}
}

func TestHandleHTTP_OAuthBindingIgnoresUnrelatedHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization on unrelated host")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer func() { ts.Close() }()

	s := &harnessRPCServer{
		oauthBindings: []oauthBinding{{
			CredentialID: "gmail_oauth",
			HostPattern:  "gmail.googleapis.com",
			ConsentURL:   "https://cloud.example/oauth/start/g1?tenant_id=t1",
		}},
	}
	t.Setenv("AGENTPAAS_GATEWAY_URL", ts.URL)
	state := &rpcInvokeState{
		payload:     map[string]any{},
		credentials: map[string]rpcCredential{},
		budget:      NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleHTTP(rpcRequest{
		ID:     "1",
		Method: "http",
		Params: map[string]any{
			"method": "GET",
			"url":    "https://api.weather.gov/points/1,2",
		},
	}, state, false)
	if !resp.OK {
		t.Fatalf("unrelated host should succeed without oauth error: %s", resp.Error)
	}
}

func TestHandleHTTP_OAuthBindingErrorDoesNotLeakToken(t *testing.T) {
	const secret = "Bearer SUPER_SECRET_TOKEN_NEVER_LEAK"
	s := &harnessRPCServer{
		oauthBindings: []oauthBinding{{
			CredentialID: "gmail_oauth",
			HostPattern:  "gmail.googleapis.com",
			ConsentURL:   "https://cloud.example/oauth/start/g1?tenant_id=t1",
		}},
	}
	state := &rpcInvokeState{
		payload: map[string]any{},
		// Different credential present — must not appear in auth-required error.
		credentials: map[string]rpcCredential{
			"other": {Header: "Authorization", Value: secret},
		},
		budget: NewBudgetEnforcer(BudgetConfig{MaxTokens: 10000}),
	}
	resp := s.handleHTTP(rpcRequest{
		ID:     "1",
		Method: "http",
		Params: map[string]any{
			"method": "GET",
			"url":    "https://gmail.googleapis.com/x",
		},
	}, state, false)
	if resp.OK || resp.Code != "authorization_required" {
		t.Fatalf("want authorization_required, got ok=%v code=%s", resp.OK, resp.Code)
	}
	if strings.Contains(resp.Error, secret) || strings.Contains(resp.Error, "SUPER_SECRET") {
		t.Fatalf("error leaked secret: %s", resp.Error)
	}
}
