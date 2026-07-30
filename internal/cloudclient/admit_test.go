package cloudclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudClient_AdmitImage_Success(t *testing.T) {
	expectedResp := AdmitImageResponse{
		ID:          "img-abc123",
		ImageDigest: "sha256:deadbeef",
		Status:      "admitted",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images/admit" {
			t.Errorf("expected /v1/images/admit, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", auth)
		}

		var req AdmitImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.ImageDigest != "sha256:deadbeef" {
			t.Errorf("ImageDigest = %q, want sha256:deadbeef", req.ImageDigest)
		}
		if req.Platform != "linux/amd64" {
			t.Errorf("Platform = %q, want linux/amd64", req.Platform)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(expectedResp)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := AdmitImageRequest{
		ImageDigest: "sha256:deadbeef",
		Platform:    "linux/amd64",
	}
	result, err := client.AdmitImage(ctx, "apc_test_token", req)
	if err != nil {
		t.Fatalf("AdmitImage: %v", err)
	}
	if result.ID != "img-abc123" {
		t.Errorf("ID = %q, want img-abc123", result.ID)
	}
	if result.ImageDigest != "sha256:deadbeef" {
		t.Errorf("ImageDigest = %q, want sha256:deadbeef", result.ImageDigest)
	}
	if result.Status != "admitted" {
		t.Errorf("Status = %q, want admitted", result.Status)
	}
}

func TestCloudClient_AdmitImage_WithAgentLock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req AdmitImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if req.AgentLock == nil {
			t.Error("AgentLock should not be nil")
		}
		if req.RegistryRef != "registry.example.com/deadbeef" {
			t.Errorf("RegistryRef = %q, want registry.example.com/deadbeef", req.RegistryRef)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(AdmitImageResponse{
			ID:          "img-lock-001",
			ImageDigest: req.ImageDigest,
			Status:      "admitted",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	lockJSON := map[string]interface{}{
		"schema_version": 2,
		"agent_name":     "test-agent",
		"lockfile_signature": "base64sig",
	}
	req := AdmitImageRequest{
		ImageDigest: "sha256:abcdef",
		Platform:    "linux/amd64",
		RegistryRef: "registry.example.com/deadbeef",
		AgentLock:   lockJSON,
	}
	result, err := client.AdmitImage(ctx, "apc_test_token", req)
	if err != nil {
		t.Fatalf("AdmitImage: %v", err)
	}
	if result.ID != "img-lock-001" {
		t.Errorf("ID = %q, want img-lock-001", result.ID)
	}
}

func TestCloudClient_AdmitImage_400_Unsigned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "unsigned/invalid",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := AdmitImageRequest{
		ImageDigest: "sha256:deadbeef",
		Platform:    "linux/amd64",
	}
	_, err := client.AdmitImage(ctx, "apc_test_token", req)
	if err == nil {
		t.Fatal("expected error for 400 status")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400, got: %v", err)
	}
}

func TestCloudClient_AdmitImage_401_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := AdmitImageRequest{
		ImageDigest: "sha256:deadbeef",
		Platform:    "linux/amd64",
	}
	_, err := client.AdmitImage(ctx, "bad_token", req)
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error should mention not authenticated, got: %v", err)
	}
}

func TestCloudClient_AdmitImage_500_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	req := AdmitImageRequest{
		ImageDigest: "sha256:deadbeef",
		Platform:    "linux/amd64",
	}
	_, err := client.AdmitImage(ctx, "token", req)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestCloudClient_ListImages_Success(t *testing.T) {
	expected := []ImageRecord{
		{ID: "img-1", ImageDigest: "sha256:aaa", Status: "admitted"},
		{ID: "img-2", ImageDigest: "sha256:bbb", Status: "admitted"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images" {
			t.Errorf("expected /v1/images, got %s", r.URL.Path)
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

	results, err := client.ListImages(ctx, "apc_test_token")
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 images, got %d", len(results))
	}
	if results[0].ID != "img-1" {
		t.Errorf("results[0].ID = %q, want img-1", results[0].ID)
	}
}

func TestCloudClient_GetImage_Success(t *testing.T) {
	expected := ImageRecord{
		ID:          "img-abc",
		ImageDigest: "sha256:deadbeef",
		Status:      "admitted",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images/img-abc" {
			t.Errorf("expected /v1/images/img-abc, got %s", r.URL.Path)
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

	result, err := client.GetImage(ctx, "apc_test_token", "img-abc")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if result.ID != "img-abc" {
		t.Errorf("ID = %q, want img-abc", result.ID)
	}
}

func TestCloudClient_GetImage_ByDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/sha256:deadbeef" {
			t.Errorf("expected /v1/images/sha256:deadbeef, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ImageRecord{
			ID:          "img-found",
			ImageDigest: "sha256:deadbeef",
			Status:      "admitted",
		})
	}))
	defer server.Close()

	client := NewCloudClient(server.URL)
	ctx := context.Background()

	result, err := client.GetImage(ctx, "token", "sha256:deadbeef")
	if err != nil {
		t.Fatalf("GetImage: %v", err)
	}
	if result.ID != "img-found" {
		t.Errorf("ID = %q, want img-found", result.ID)
	}
}