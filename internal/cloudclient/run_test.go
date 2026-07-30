package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_CreateRun_Success(t *testing.T) {
	expectedResp := RunRecord{
		ID:               "run-abc123",
		TenantID:         "tenant-42",
		DeploymentID:     "dep-xyz",
		Status:           "pending",
		QueuePosition:    intPtr(0),
		ConcurrencyLimit: 1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/runs" {
			t.Errorf("expected /v1/runs, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		var req CreateRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.DeploymentID != "dep-xyz" {
			t.Errorf("DeploymentID = %q, want dep-xyz", req.DeploymentID)
		}
		if len(req.AllowedHosts) != 2 {
			t.Errorf("AllowedHosts length = %d, want 2", len(req.AllowedHosts))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expectedResp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateRunRequest{
		DeploymentID: "dep-xyz",
		AllowedHosts: []string{"api.example.com", "db.example.com"},
	}
	result, err := client.CreateRun(ctx, "apc_test_token", req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if result.ID != "run-abc123" {
		t.Errorf("ID = %q, want run-abc123", result.ID)
	}
	if result.Status != "pending" {
		t.Errorf("Status = %q, want pending", result.Status)
	}
	if result.DeploymentID != "dep-xyz" {
		t.Errorf("DeploymentID = %q, want dep-xyz", result.DeploymentID)
	}
}

func TestCloudClient_CreateRun_WithoutAllowedHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CreateRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if len(req.AllowedHosts) != 0 {
			t.Errorf("AllowedHosts should be empty, got %v", req.AllowedHosts)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(RunRecord{
			ID:           "run-no-hosts",
			DeploymentID: req.DeploymentID,
			Status:       "pending",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateRunRequest{
		DeploymentID: "dep-abc",
	}
	result, err := client.CreateRun(ctx, "token", req)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if result.ID != "run-no-hosts" {
		t.Errorf("ID = %q, want run-no-hosts", result.ID)
	}
}

func TestCloudClient_CreateRun_400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid_deployment",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateRunRequest{
		DeploymentID: "dep-invalid",
	}
	_, err := client.CreateRun(ctx, "apc_test_token", req)
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400, got: %v", err)
	}
}

func TestCloudClient_CreateRun_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateRunRequest{
		DeploymentID: "dep-xyz",
	}
	_, err := client.CreateRun(ctx, "bad_token", req)
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_ListRuns_Success(t *testing.T) {
	expected := []RunRecord{
		{ID: "run-1", DeploymentID: "dep-1", Status: "running"},
		{ID: "run-2", DeploymentID: "dep-2", Status: "failed", Error: strPtr("timeout")},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/runs" {
			t.Errorf("expected /v1/runs, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	results, err := client.ListRuns(ctx, "apc_test_token")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(results))
	}
	if results[0].ID != "run-1" {
		t.Errorf("results[0].ID = %q, want run-1", results[0].ID)
	}
	if results[1].Status != "failed" {
		t.Errorf("results[1].Status = %q, want failed", results[1].Status)
	}
	if results[1].Error == nil || *results[1].Error != "timeout" {
		t.Errorf("results[1].Error = %v, want timeout", results[1].Error)
	}
}

func TestCloudClient_ListRuns_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.ListRuns(ctx, "bad_token")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_GetRun_Success(t *testing.T) {
	expected := RunRecord{
		ID:               "run-abc",
		TenantID:         "tenant-42",
		DeploymentID:     "dep-xyz",
		Status:           "running",
		CreatedAt:        "2025-01-15T10:30:00Z",
		ConcurrencyLimit: 5,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/runs/run-abc" {
			t.Errorf("expected /v1/runs/run-abc, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	result, err := client.GetRun(ctx, "apc_test_token", "run-abc")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if result.ID != "run-abc" {
		t.Errorf("ID = %q, want run-abc", result.ID)
	}
	if result.Status != "running" {
		t.Errorf("Status = %q, want running", result.Status)
	}
}

func TestCloudClient_GetRun_InvalidID(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	_, err := client.GetRun(ctx, "token", "bad/id")
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("error should mention invalid id, got: %v", err)
	}
}

func TestCloudClient_GetRun_InvalidID_Newline(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	_, err := client.GetRun(ctx, "token", "run\nabc")
	if err == nil {
		t.Fatal("expected error for invalid id with newline")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("error should mention invalid id, got: %v", err)
	}
}

func TestCloudClient_GetRun_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.GetRun(ctx, "bad_token", "run-abc")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_CancelRun_Success(t *testing.T) {
	expected := RunRecord{
		ID:           "run-abc",
		DeploymentID: "dep-xyz",
		Status:       "cancelled",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/runs/run-abc/cancel" {
			t.Errorf("expected /v1/runs/run-abc/cancel, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	result, err := client.CancelRun(ctx, "apc_test_token", "run-abc")
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if result.ID != "run-abc" {
		t.Errorf("ID = %q, want run-abc", result.ID)
	}
	if result.Status != "cancelled" {
		t.Errorf("Status = %q, want cancelled", result.Status)
	}
}

func TestCloudClient_CancelRun_InvalidID(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	_, err := client.CancelRun(ctx, "token", "bad/id")
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("error should mention invalid id, got: %v", err)
	}
}

func TestCloudClient_CancelRun_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.CancelRun(ctx, "bad_token", "run-abc")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_CancelRun_500_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.CancelRun(ctx, "token", "run-abc")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string { return &s }

// intPtr returns a pointer to the given int.
func intPtr(i int) *int { return &i }
