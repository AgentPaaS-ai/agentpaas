package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudImagesDelete_CommandRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	if _, _, err := cmd.Find([]string{"cloud", "images", "delete"}); err != nil {
		t.Fatalf("Find cloud images delete: %v", err)
	}
}

func TestCloudImagesHelp_ListsDelete(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "images", "--help")
	if err != nil {
		t.Fatalf("cloud images --help: %v", err)
	}
	if !strings.Contains(stdout, "delete") {
		t.Errorf("cloud images --help should list delete, got: %s", stdout)
	}
}

func TestCloudImagesDelete_RequiresYes(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_delete_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("cloud images delete without --yes must not call API; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "images", "delete", "img_unused")
	if err == nil {
		t.Fatal("expected error without --yes")
	}
	combined := err.Error() + stderr
	want := "cloud images delete: refusing without --yes (this deletes an admitted image)"
	if !strings.Contains(combined, want) {
		t.Errorf("error = %q, want containing %q", combined, want)
	}
}

func TestCloudImagesDelete_YesConfirmIDMismatch_NonTTY(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_delete_test")

	deleted := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
		}
		t.Errorf("cloud images delete with mismatched --confirm-id must not call DELETE; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "images", "delete", "img_unused", "--yes", "--confirm-id", "other-img")
	if err == nil {
		t.Fatal("expected error for mismatched --confirm-id")
	}
	if deleted {
		t.Fatal("images delete with mismatched --confirm-id deleted the image")
	}
	combined := err.Error() + stderr
	want := "cloud images delete: confirmation failed (run this in your own terminal, not via an agent)"
	if !strings.Contains(combined, want) {
		t.Errorf("error = %q, want containing %q", combined, want)
	}
}

func TestCloudImagesDelete_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_delete_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/v1/images/img_unused" {
			t.Errorf("expected /v1/images/img_unused, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer apc_images_delete_test" {
			t.Errorf("Authorization = %q, want Bearer apc_images_delete_test", auth)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "images", "delete", "img_unused", "--yes", "--confirm-id", "img_unused")
	if err != nil {
		t.Fatalf("images delete: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Deleted") {
		t.Errorf("expected 'Deleted' in output, got: %q", stdout)
	}
}

func TestCloudImagesDelete_ConflictPrintsUndeploy(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_delete_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":          "image_in_use",
			"message":        "Image is still referenced by deployments",
			"deployment_ids": []string{"dep_4f8557", "dep_live"},
		})
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "images", "delete", "img_live", "--yes", "--confirm-id", "img_live")
	if err == nil {
		t.Fatal("expected 409 error")
	}
	combined := err.Error() + stdout + stderr
	if !strings.Contains(combined, "undeploy dep_4f8557") {
		t.Errorf("409 should print undeploy dep_4f8557, got: %q", combined)
	}
	if !strings.Contains(combined, "undeploy dep_live") {
		t.Errorf("409 should print undeploy dep_live, got: %q", combined)
	}
	if strings.Contains(combined, "apc_images_delete_test") {
		t.Error("token leaked in 409 path")
	}
}

func TestCloudImagesDelete_DigestPath(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_delete_test")

	digest := "sha256:fa80e33aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/"+digest {
			t.Errorf("path = %s, want /v1/images/%s", r.URL.Path, digest)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() { apiServer.Close() }()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	if _, _, err := executeCloudCmd(t, "", "cloud", "images", "delete", digest, "--yes", "--confirm-id", digest); err != nil {
		t.Fatalf("images delete by digest: %v", err)
	}
}
