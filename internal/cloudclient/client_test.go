package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_StartCLIAuth(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"StatusOK", http.StatusOK},
		{"StatusCreated", http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected POST, got %s", r.Method)
				}
				if r.URL.Path != "/v1/auth/cli/start" {
					t.Errorf("expected /v1/auth/cli/start, got %s", r.URL.Path)
				}

				var req StartCLIAuthRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("decode body: %v", err)
				}
				if req.RedirectURI == "" {
					t.Error("redirect_uri must not be empty")
				}

				resp := StartCLIAuthResponse{
					State:      "state-abc123",
					ApproveURL: "/approve?state=state-abc123",
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client := NewCloudClient(server.URL)
			ctx := context.Background()

			result, err := client.StartCLIAuth(ctx, "http://127.0.0.1:12345/callback")
			if err != nil {
				t.Fatalf("StartCLIAuth: %v", err)
			}
			if result.State != "state-abc123" {
				t.Errorf("State = %q, want state-abc123", result.State)
			}
			if result.ApproveURL != "/approve?state=state-abc123" {
				t.Errorf("ApproveURL = %q, want /approve?state=state-abc123", result.ApproveURL)
			}
		})
	}
}

func TestCloudClient_StartCLIAuth_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.StartCLIAuth(ctx, "http://127.0.0.1:12345/callback")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}

func TestCloudClient_Whoami_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/whoami" {
			t.Errorf("expected /v1/whoami, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		resp := WhoamiResponse{
			TenantID:         "tenant-42",
			Tier:             "pro",
			ConcurrencyLimit: 10,
			SecretsBackend:   "vault",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	result, err := client.Whoami(ctx, "apc_test_token")
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if result.TenantID != "tenant-42" {
		t.Errorf("TenantID = %q, want tenant-42", result.TenantID)
	}
	if result.Tier != "pro" {
		t.Errorf("Tier = %q, want pro", result.Tier)
	}
	if result.ConcurrencyLimit != 10 {
		t.Errorf("ConcurrencyLimit = %d, want 10", result.ConcurrencyLimit)
	}
	if result.SecretsBackend != "vault" {
		t.Errorf("SecretsBackend = %q, want vault", result.SecretsBackend)
	}
}

func TestCloudClient_Whoami_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.Whoami(ctx, "bad_token")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_Whoami_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.Whoami(ctx, "token")
	if err == nil {
		t.Fatal("expected error for 502 status")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should mention status 502, got: %v", err)
	}
}

func TestResolveApproveURL(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		approveURL string
		want       string
	}{
		{
			name:       "absolute approve URL",
			baseURL:    "https://cloud.agentpaas.ai",
			approveURL: "https://auth.agentpaas.ai/approve?state=abc",
			want:       "https://auth.agentpaas.ai/approve?state=abc",
		},
		{
			name:       "relative approve URL",
			baseURL:    "https://cloud.agentpaas.ai",
			approveURL: "/approve?state=abc",
			want:       "https://cloud.agentpaas.ai/approve?state=abc",
		},
		{
			name:       "base with trailing slash",
			baseURL:    "https://cloud.agentpaas.ai/",
			approveURL: "/approve?state=abc",
			want:       "https://cloud.agentpaas.ai/approve?state=abc",
		},
		{
			name:       "relative without leading slash",
			baseURL:    "https://cloud.agentpaas.ai",
			approveURL: "approve?state=abc",
			want:       "https://cloud.agentpaas.ai/approve?state=abc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveApproveURL(tt.baseURL, tt.approveURL)
			if got != tt.want {
				t.Errorf("ResolveApproveURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewCloudClient_DefaultURL(t *testing.T) {
	const want = "https://cloud.agentpaas.ai"

	client := NewCloudClient("")
	if client.BaseURL != want {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, want)
	}
}

func TestNewCloudClient_CustomURL(t *testing.T) {
	client := NewCloudClient("https://custom.example.com")
	if client.BaseURL != "https://custom.example.com" {
		t.Errorf("BaseURL = %q, want https://custom.example.com", client.BaseURL)
	}
}

func TestJSONOK(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{200, true},
		{201, true},
		{204, true},
		{299, true},
		{199, false},
		{300, false},
		{301, false},
		{400, false},
		{401, false},
		{500, false},
	}
	for _, tt := range tests {
		got := jsonOK(tt.code)
		if got != tt.want {
			t.Errorf("jsonOK(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
