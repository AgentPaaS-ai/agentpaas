package cloudclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRevokeOAuthGrant_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/oauth/revoke" {
			t.Errorf("expected /v1/oauth/revoke, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		// tenant_id MUST NOT be present in the request body.
		if _, ok := body["tenant_id"]; ok {
			t.Errorf("request body must NOT contain tenant_id, got: %s", string(bodyBytes))
		}

		requiredFields := []string{"deployment_id", "credential_id", "end_user_identity"}
		for _, field := range requiredFields {
			raw, ok := body[field]
			if !ok {
				t.Errorf("request body missing required field %q, got: %s", field, string(bodyBytes))
				continue
			}
			var val string
			if err := json.Unmarshal(raw, &val); err != nil {
				t.Errorf("decode field %q: %v", field, err)
			}
			if val == "" {
				t.Errorf("field %q must be non-empty", field)
			}
		}

		resp := OAuthRevokeResult{
			DeploymentID:    "dep-123",
			CredentialID:    "cred-abc",
			EndUserIdentity: "user@example.com",
			Revoked:         true,
			RevokedAt:       "2025-08-10T12:00:00Z",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	req := RevokeOAuthGrantRequest{
		DeploymentID:    "dep-123",
		CredentialID:    "cred-abc",
		EndUserIdentity: "user@example.com",
	}
	result, err := client.RevokeOAuthGrant(context.Background(), "apc_test_token", req)
	if err != nil {
		t.Fatalf("RevokeOAuthGrant: %v", err)
	}
	if result.DeploymentID != "dep-123" {
		t.Errorf("DeploymentID = %q, want dep-123", result.DeploymentID)
	}
	if result.CredentialID != "cred-abc" {
		t.Errorf("CredentialID = %q, want cred-abc", result.CredentialID)
	}
	if result.EndUserIdentity != "user@example.com" {
		t.Errorf("EndUserIdentity = %q, want user@example.com", result.EndUserIdentity)
	}
	if !result.Revoked {
		t.Errorf("Revoked = false, want true")
	}
	if result.RevokedAt == "" {
		t.Errorf("RevokedAt = empty, want non-empty")
	}
}

func TestRevokeOAuthGrant_RequestBodyExcludesTenantID(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(OAuthRevokeResult{})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	req := RevokeOAuthGrantRequest{
		DeploymentID:    "dep-456",
		CredentialID:    "cred-def",
		EndUserIdentity: "bob@example.com",
	}
	_, err := client.RevokeOAuthGrant(context.Background(), "token", req)
	if err != nil {
		t.Fatalf("RevokeOAuthGrant: %v", err)
	}

	if strings.Contains(string(capturedBody), "tenant_id") {
		t.Errorf("request body must NOT contain tenant_id, got: %s", string(capturedBody))
	}
}

func TestRevokeOAuthGrant_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	req := RevokeOAuthGrantRequest{
		DeploymentID:    "dep-789",
		CredentialID:    "cred-ghi",
		EndUserIdentity: "carol@example.com",
	}
	_, err := client.RevokeOAuthGrant(context.Background(), "bad_token", req)
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestRevokeOAuthGrant_404_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":  "not_found",
			"reason": "grant_not_found",
		})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	req := RevokeOAuthGrantRequest{
		DeploymentID:    "dep-missing",
		CredentialID:    "cred-missing",
		EndUserIdentity: "dave@example.com",
	}
	_, err := client.RevokeOAuthGrant(context.Background(), "token", req)
	if err == nil {
		t.Fatal("expected error for 404 status")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status 404, got: %v", err)
	}
}

func TestRevokeOAuthGrant_500_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	req := RevokeOAuthGrantRequest{
		DeploymentID:    "dep-500",
		CredentialID:    "cred-500",
		EndUserIdentity: "eve@example.com",
	}
	_, err := client.RevokeOAuthGrant(context.Background(), "token", req)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}
