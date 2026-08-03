package cloudclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCloudClient_UploadImageStart_Success(t *testing.T) {
	lock := map[string]interface{}{
		"schema_version":     2,
		"agent_name":         "test-agent",
		"image_digest":       "sha256:deadbeef",
		"lockfile_signature": "signed",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/images/upload-start" {
			t.Errorf("path = %s, want /v1/images/upload-start", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", got)
		}

		var got UploadImageStartRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if got.ImageDigest != "sha256:deadbeef" {
			t.Errorf("ImageDigest = %q, want sha256:deadbeef", got.ImageDigest)
		}
		if got.Platform != "linux/amd64" {
			t.Errorf("Platform = %q, want linux/amd64", got.Platform)
		}
		if !reflect.DeepEqual(got.AgentLock, lock) {
			t.Errorf("AgentLock = %#v, want %#v", got.AgentLock, lock)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(UploadImageStartResponse{
			UploadID:       "upload-123",
			ImageID:        "img-123",
			ChunkSizeBytes: 8 << 20,
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(func() { server.Close() })

	client := NewCloudClient(server.URL)
	got, err := client.UploadImageStart(context.Background(), "apc_test_token", UploadImageStartRequest{
		ImageDigest: "sha256:deadbeef",
		Platform:    "linux/amd64",
		AgentLock:   lock,
	})
	if err != nil {
		t.Fatalf("UploadImageStart: %v", err)
	}
	if got.UploadID != "upload-123" || got.ImageID != "img-123" || got.ChunkSizeBytes != 8<<20 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestCloudClient_UploadImageChunk_SendsRawChunk(t *testing.T) {
	wantChunk := []byte("raw tar chunk\x00bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/v1/images/upload/upload-123/chunk/7" {
			t.Errorf("path = %s, want /v1/images/upload/upload-123/chunk/7", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("Content-Type = %q, want application/octet-stream", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if !reflect.DeepEqual(body, wantChunk) {
			t.Errorf("body = %q, want %q", body, wantChunk)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(func() { server.Close() })

	client := NewCloudClient(server.URL)
	if err := client.UploadImageChunk(context.Background(), "apc_test_token", "upload-123", 7, wantChunk); err != nil {
		t.Fatalf("UploadImageChunk: %v", err)
	}
}

func TestCloudClient_UploadImageComplete_DecodesAdmittedImage(t *testing.T) {
	want := AdmitImageResponse{
		ID:           "img-123",
		TenantID:     "tenant-123",
		ImageDigest:  "sha256:deadbeef",
		Platform:     "linux/amd64",
		RegistryRef:  "registry.example.com/test-agent@sha256:deadbeef",
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		Status:       "admitted",
		CreatedAt:    time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/images/upload/upload-123/complete" {
			t.Errorf("path = %s, want /v1/images/upload/upload-123/complete", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer apc_test_token" {
			t.Errorf("Authorization = %q, want Bearer apc_test_token", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != "{}" {
			t.Errorf("body = %q, want {}", body)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(want); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(func() { server.Close() })

	client := NewCloudClient(server.URL)
	got, err := client.UploadImageComplete(context.Background(), "apc_test_token", "upload-123")
	if err != nil {
		t.Fatalf("UploadImageComplete: %v", err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
}

func TestCloudClient_UploadImageStart_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(func() { server.Close() })

	client := NewCloudClient(server.URL)
	_, err := client.UploadImageStart(context.Background(), "bad-token", UploadImageStartRequest{})
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("error = %q, want not authenticated", err)
	}
}
