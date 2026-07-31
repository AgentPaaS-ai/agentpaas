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

func TestCloudClient_MintInvokeToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep-abc/invoke-token" {
			t.Errorf("expected invoke-token path, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_tenant_token" {
			t.Errorf("Authorization = %q, want Bearer apc_tenant_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"deployment_id":"dep-abc",
			"invoke_token":"inv_secret_token",
			"invoke_token_prefix":"inv_secret",
			"message":"Store this token securely."
		}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).MintInvokeToken(context.Background(), "apc_tenant_token", "dep-abc")
	if err != nil {
		t.Fatalf("MintInvokeToken: %v", err)
	}
	if result.DeploymentID != "dep-abc" {
		t.Errorf("DeploymentID = %q, want dep-abc", result.DeploymentID)
	}
	if result.InvokeToken != "inv_secret_token" {
		t.Errorf("InvokeToken = %q, want inv_secret_token", result.InvokeToken)
	}
	if result.InvokeTokenPrefix != "inv_secret" {
		t.Errorf("InvokeTokenPrefix = %q, want inv_secret", result.InvokeTokenPrefix)
	}
	if result.Message != "Store this token securely." {
		t.Errorf("Message = %q, want token message", result.Message)
	}
}

func TestCloudClient_InvokeDeployment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep-abc/invoke" {
			t.Errorf("expected invoke path, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer inv_secret_token" {
			t.Errorf("Authorization = %q, want Bearer inv_secret_token", auth)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if string(requestBody) != `{"input":"hello"}` {
			t.Errorf("request body = %q, want JSON body", requestBody)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"run-123","status":"queued","custom":true}`))
	}))
	defer func() { server.Close() }()

	body := json.RawMessage(`{"input":"hello"}`)
	result, err := NewCloudClient(server.URL).InvokeDeployment(context.Background(), "inv_secret_token", "dep-abc", body)
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if string(result) != `{"id":"run-123","status":"queued","custom":true}` {
		t.Errorf("response = %s, want raw run response", result)
	}
}

func TestCloudClient_InvokeDeployment_EmptyBodyUsesObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if string(requestBody) != `{}` {
			t.Errorf("request body = %q, want {}", requestBody)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"run-empty","status":"queued"}`))
	}))
	defer func() { server.Close() }()

	result, err := NewCloudClient(server.URL).InvokeDeployment(context.Background(), "inv_secret_token", "dep-abc", nil)
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if !strings.Contains(string(result), `"run-empty"`) {
		t.Errorf("response = %s, want run-empty response", result)
	}
}

func TestCloudClient_MintInvokeToken_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).MintInvokeToken(context.Background(), "bad_token", "dep-abc")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_InvokeDeployment_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer func() { server.Close() }()

	_, err := NewCloudClient(server.URL).InvokeDeployment(context.Background(), "bad_token", "dep-abc", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_InvokeDeployment_InvalidDeploymentID(t *testing.T) {
	client := NewCloudClient("https://example.com")

	if _, err := client.MintInvokeToken(context.Background(), "token", "bad/id"); err == nil {
		t.Fatal("MintInvokeToken should reject deployment IDs containing slash")
	}
	if _, err := client.InvokeDeployment(context.Background(), "token", "bad/id", json.RawMessage(`{}`)); err == nil {
		t.Fatal("InvokeDeployment should reject deployment IDs containing slash")
	}
}
