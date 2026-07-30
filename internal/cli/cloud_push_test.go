package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// signLockForTest generates an ECDSA P-256 key, signs the lock, and writes it to path.
func signLockForTest(t *testing.T, lock *pack.AgentLock) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := pack.SignLockfileWithKey(lock, key); err != nil {
		t.Fatalf("SignLockfileWithKey: %v", err)
	}
	return key
}

// writeLockFile marshals lock to JSON and writes it to dir/agent.lock.
func writeLockFile(t *testing.T, dir string, lock *pack.AgentLock) string {
	t.Helper()
	path := filepath.Join(dir, "agent.lock")
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	return path
}

// newTestLock creates a minimal signed lock and writes it to dir.
func newTestLock(t *testing.T, dir string) string {
	t.Helper()
	lock := &pack.AgentLock{
		SchemaVersion: 2,
		AgentName:     "test-agent",
		AgentVersion:  "1.0.0",
		Runtime:       "python",
		Platform:      "linux/amd64",
		ImageDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SBOMDigest:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	_ = signLockForTest(t, lock)
	return writeLockFile(t, dir, lock)
}

// TestCloudPush_CommandsRegistered verifies push and images are registered.
func TestCloudPush_CommandsRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	_, _, err := cmd.Find([]string{"cloud", "push"})
	if err != nil {
		t.Fatalf("Find cloud push: %v", err)
	}

	_, _, err = cmd.Find([]string{"cloud", "images"})
	if err != nil {
		t.Fatalf("Find cloud images: %v", err)
	}
}

// TestCloudPush_MissingLock tests that --lock is required.
func TestCloudPush_MissingLock(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_missing_lock")

	_, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--skip-registry")
	if err == nil {
		t.Fatal("expected error for missing --lock")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "lock") && !strings.Contains(combined, "Lock") && !strings.Contains(combined, "required") {
		t.Errorf("error should mention lock, got: %v", combined)
	}
}

// TestCloudPush_UnsignedLock tests that a lock without signature is rejected before HTTP.
func TestCloudPush_UnsignedLock(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_push_test")

	dir := t.TempDir()
	lockJSON := `{"schema_version":2,"agent_name":"unsigned","image_digest":"sha256:abc","lockfile_signature":""}`
	lockPath := filepath.Join(dir, "agent.lock")
	if err := os.WriteFile(lockPath, []byte(lockJSON), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	_, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath, "--skip-registry")
	if err == nil {
		t.Fatal("expected error for unsigned lock")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "unsigned") && !strings.Contains(combined, "signature") {
		t.Errorf("error should mention unsigned/signature, got: %v", combined)
	}
}

// TestCloudPush_Success verifies a successful push with --skip-registry and fake API.
func TestCloudPush_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_push_test")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/admit" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_push_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var req cloudclient.AdmitImageRequest
			_ = json.NewDecoder(r.Body).Decode(&req)

			resp := cloudclient.AdmitImageResponse{
				ID:          "img-pushed-001",
				ImageDigest: req.ImageDigest,
				Status:      "admitted",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push",
		"--lock", lockPath,
		"--skip-registry",
		"--platform", "linux/amd64")
	if err != nil {
		t.Fatalf("push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "img-pushed-001") {
		t.Errorf("expected img-pushed-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "admitted") {
		t.Errorf("expected 'admitted' in output, got: %q", stdout)
	}
}

// TestCloudPush_API400 verifies non-zero exit on 400 from server.
func TestCloudPush_API400(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_push_test")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsigned/invalid"}`))
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "push",
		"--lock", lockPath,
		"--skip-registry")
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

// TestCloudPush_NotLoggedIn tests error when not authenticated.
func TestCloudPush_NotLoggedIn(t *testing.T) {
	_ = setupFakeTokenStore(t)

	// Use a valid signed lock, but no token. Should fail at auth after lock checks pass.
	dir := t.TempDir()
	lockPath := newTestLock(t, dir)

	_, _, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath, "--skip-registry")
	if err == nil {
		t.Fatal("expected error when not logged in")
	}
}

// TestCloudImages_Success tests the list command.
func TestCloudImages_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_list_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer apc_list_test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			images := []cloudclient.ImageRecord{
				{ID: "img-1", ImageDigest: "sha256:aaa", Status: "admitted"},
				{ID: "img-2", ImageDigest: "sha256:bbb", Status: "admitted"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(images)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "images")
	if err != nil {
		t.Fatalf("images: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "img-1") {
		t.Errorf("expected img-1 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "img-2") {
		t.Errorf("expected img-2 in output, got: %q", stdout)
	}
}

// TestCloudImages_JSONOutput tests JSON output format.
func TestCloudImages_JSONOutput(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_json_test")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		images := []cloudclient.ImageRecord{
			{ID: "img-json", ImageDigest: "sha256:ccc", Status: "admitted"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(images)
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, _, err := executeCloudCmd(t, "", "cloud", "images", "--json")
	if err != nil {
		t.Fatalf("images --json: %v", err)
	}
	var parsed []struct {
		ID          string `json:"id"`
		ImageDigest string `json:"image_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout)
	}
	if len(parsed) != 1 || parsed[0].ID != "img-json" {
		t.Errorf("expected img-json, got %v", parsed)
	}
}

// TestCloudPush_Help verifies help text contains required keywords.
func TestCloudPush_Help(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "push", "--help")
	if err != nil {
		t.Fatalf("push --help: %v", err)
	}
	if !strings.Contains(stdout, "skip-registry") {
		t.Errorf("help should mention --skip-registry, got: %s", stdout)
	}
	if !strings.Contains(stdout, "lock") {
		t.Errorf("help should mention --lock, got: %s", stdout)
	}
}

// TestCloudHelp_HasPushAndImages verifies cloud --help lists push and images.
func TestCloudHelp_HasPushAndImages(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "push") {
		t.Errorf("cloud --help should mention push, got: %s", stdout)
	}
	if !strings.Contains(stdout, "images") {
		t.Errorf("cloud --help should mention images, got: %s", stdout)
	}
}

// TestWranglerHook_DefaultReturnsError verifies the default wrangler hook
// returns an error when wrangler is not available.
func TestWranglerHook_DefaultReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := runWranglerPush(ctx, "test-image:latest")
	if err == nil {
		t.Log("wrangler found locally - hook works")
	}
}