package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_CreateDeployment_Success(t *testing.T) {
	slotID := "slot-42"
	expectedResp := DeploymentRecord{
		ID:          "dep-abc123",
		ImageDigest: "sha256:deadbeef",
		SlotID:      &slotID,
		Status:      "pending",
		CreatedAt:   "2025-01-15T10:30:00Z",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments" {
			t.Errorf("expected /v1/deployments, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		var req CreateDeploymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.ImageDigest != "sha256:deadbeef" {
			t.Errorf("ImageDigest = %q, want sha256:deadbeef", req.ImageDigest)
		}
		if req.SlotID == nil || *req.SlotID != "slot-42" {
			t.Errorf("SlotID = %v, want slot-42", req.SlotID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expectedResp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateDeploymentRequest{
		ImageDigest: "sha256:deadbeef",
		SlotID:      &slotID,
	}
	result, err := client.CreateDeployment(ctx, "apc_test_token", req)
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if result.ID != "dep-abc123" {
		t.Errorf("ID = %q, want dep-abc123", result.ID)
	}
	if result.ImageDigest != "sha256:deadbeef" {
		t.Errorf("ImageDigest = %q, want sha256:deadbeef", result.ImageDigest)
	}
	if result.Status != "pending" {
		t.Errorf("Status = %q, want pending", result.Status)
	}
	if result.SlotID == nil || *result.SlotID != "slot-42" {
		t.Errorf("SlotID = %v, want slot-42", result.SlotID)
	}
}

func TestCloudClient_CreateDeployment_WithoutSlotID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req CreateDeploymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.SlotID != nil {
			t.Errorf("SlotID should be nil, got %v", req.SlotID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(DeploymentRecord{
			ID:          "dep-no-slot",
			ImageDigest: req.ImageDigest,
			Status:      "pending",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateDeploymentRequest{
		ImageDigest: "sha256:abcdef",
	}
	result, err := client.CreateDeployment(ctx, "token", req)
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if result.ID != "dep-no-slot" {
		t.Errorf("ID = %q, want dep-no-slot", result.ID)
	}
}

func TestCloudClient_CreateDeployment_InstanceTypeJSON(t *testing.T) {
	instanceType := "lite"
	requests := []CreateDeploymentRequest{
		{
			ImageDigest: "sha256:with-instance-type",
			InstanceType: &instanceType,
		},
		{
			ImageDigest: "sha256:without-instance-type",
		},
	}
	requestNumber := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}

		if requestNumber == 0 {
			raw, ok := body["instance_type"]
			if !ok {
				t.Error("instance_type should be present when set")
			} else {
				var got string
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Errorf("decode instance_type: %v", err)
				} else if got != "lite" {
					t.Errorf("instance_type = %q, want lite", got)
				}
			}
		} else if _, ok := body["instance_type"]; ok {
			t.Error("instance_type should be omitted when nil")
		}
		requestNumber++

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(DeploymentRecord{ID: "dep-instance-type", Status: "pending"})
	}))
	defer func() { server.Close() }()

	client := NewCloudClient(server.URL)
	for _, req := range requests {
		if _, err := client.CreateDeployment(context.Background(), "token", req); err != nil {
			t.Fatalf("CreateDeployment: %v", err)
		}
	}
}

func TestCloudClient_CreateDeployment_400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "image_not_admitted",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateDeploymentRequest{
		ImageDigest: "sha256:deadbeef",
	}
	_, err := client.CreateDeployment(ctx, "apc_test_token", req)
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400, got: %v", err)
	}
}

func TestCloudClient_CreateDeployment_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateDeploymentRequest{
		ImageDigest: "sha256:deadbeef",
	}
	_, err := client.CreateDeployment(ctx, "bad_token", req)
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_CreateDeployment_500_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := CreateDeploymentRequest{
		ImageDigest: "sha256:deadbeef",
	}
	_, err := client.CreateDeployment(ctx, "token", req)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestCloudClient_ListDeployments_Success(t *testing.T) {
	expected := []DeploymentRecord{
		{ID: "dep-1", ImageDigest: "sha256:aaa", Status: "running"},
		{ID: "dep-2", ImageDigest: "sha256:bbb", Status: "pending"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments" {
			t.Errorf("expected /v1/deployments, got %s", r.URL.Path)
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

	results, err := client.ListDeployments(ctx, "apc_test_token")
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(results))
	}
	if results[0].ID != "dep-1" {
		t.Errorf("results[0].ID = %q, want dep-1", results[0].ID)
	}
	if results[1].Status != "pending" {
		t.Errorf("results[1].Status = %q, want pending", results[1].Status)
	}
}

func TestCloudClient_ListDeployments_400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "bad_request",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	_, err := client.ListDeployments(ctx, "token")
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400, got: %v", err)
	}
}

func TestCloudClient_GetDeployment_Success(t *testing.T) {
	expected := DeploymentRecord{
		ID:          "dep-abc",
		ImageDigest: "sha256:deadbeef",
		Status:      "running",
		CreatedAt:   "2025-01-15T10:30:00Z",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/deployments/dep-abc" {
			t.Errorf("expected /v1/deployments/dep-abc, got %s", r.URL.Path)
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

	result, err := client.GetDeployment(ctx, "apc_test_token", "dep-abc")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if result.ID != "dep-abc" {
		t.Errorf("ID = %q, want dep-abc", result.ID)
	}
	if result.Status != "running" {
		t.Errorf("Status = %q, want running", result.Status)
	}
}

func TestCloudClient_GetDeployment_InvalidID(t *testing.T) {
	client := NewCloudClient("https://example.com")
	ctx := context.Background()

	_, err := client.GetDeployment(ctx, "token", "bad/id")
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
	if !strings.Contains(err.Error(), "invalid id") {
		t.Errorf("error should mention invalid id, got: %v", err)
	}
}
