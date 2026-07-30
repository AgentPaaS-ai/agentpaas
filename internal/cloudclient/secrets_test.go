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

func TestCloudClient_PutSecret_Success(t *testing.T) {
	var capturedName, capturedValue string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/secrets/") {
			t.Errorf("expected /v1/secrets/..., got %s", r.URL.Path)
		}
		capturedName = strings.TrimPrefix(r.URL.Path, "/v1/secrets/")

		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var body map[string]string
		if err := json.Unmarshal(bodyBytes, &body); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		capturedValue = body["value"]

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	err := client.PutSecret(ctx, "apc_test_token", "my-secret", "secret-value-xyz")
	if err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if capturedName != "my-secret" {
		t.Errorf("captured name = %q, want my-secret", capturedName)
	}
	if capturedValue != "secret-value-xyz" {
		t.Errorf("captured value = %q, want secret-value-xyz", capturedValue)
	}
}

func TestCloudClient_PutSecret_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	err := client.PutSecret(ctx, "bad_token", "my-secret", "v")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_PutSecret_InvalidName(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	err := client.PutSecret(ctx, "token", "bad/name", "v")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("error should mention invalid name, got: %v", err)
	}
}

func TestCloudClient_PutSecret_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	err := client.PutSecret(ctx, "token", "s", "v")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestCloudClient_ListSecrets_Success(t *testing.T) {
	expected := []SecretLabel{
		{Name: "secret-a", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-02T00:00:00Z"},
		{Name: "secret-b", CreatedAt: "2025-02-01T00:00:00Z", UpdatedAt: "2025-02-02T00:00:00Z"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/secrets" {
			t.Errorf("expected /v1/secrets, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		resp := listSecretsResponse{Secrets: expected}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	results, err := client.ListSecrets(ctx, "apc_test_token")
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(results))
	}
	if results[0].Name != "secret-a" {
		t.Errorf("results[0].Name = %q, want secret-a", results[0].Name)
	}
	if results[1].Name != "secret-b" {
		t.Errorf("results[1].Name = %q, want secret-b", results[1].Name)
	}
}

func TestCloudClient_ListSecrets_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := listSecretsResponse{Secrets: []SecretLabel{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	results, err := client.ListSecrets(ctx, "token")
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 secrets, got %d", len(results))
	}
}

func TestCloudClient_ListSecrets_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.ListSecrets(ctx, "bad_token")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_DeleteSecret_Success(t *testing.T) {
	var capturedName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/secrets/") {
			t.Errorf("expected /v1/secrets/..., got %s", r.URL.Path)
		}
		capturedName = strings.TrimPrefix(r.URL.Path, "/v1/secrets/")

		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	err := client.DeleteSecret(ctx, "apc_test_token", "to-delete")
	if err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if capturedName != "to-delete" {
		t.Errorf("captured name = %q, want to-delete", capturedName)
	}
}

func TestCloudClient_DeleteSecret_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	err := client.DeleteSecret(ctx, "bad_token", "s")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_DeleteSecret_InvalidName(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	err := client.DeleteSecret(ctx, "token", "bad\nname")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("error should mention invalid name, got: %v", err)
	}
}
