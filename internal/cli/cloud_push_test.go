package cli

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestCloudPush_Success verifies a successful tenant-token upload and admission.
func TestCloudPush_Success(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_push_test")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)
	tarData := append(bytes.Repeat([]byte("a"), 8<<20), []byte("tail")...)
	var savedRef string
	oldDockerSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		savedRef = imageRef
		return io.NopCloser(bytes.NewReader(tarData)), nil
	}
	t.Cleanup(func() { dockerSaveImage = oldDockerSave })
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	t.Cleanup(func() { dockerImageInspect = oldInspect })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer apc_push_test" {
			t.Errorf("Authorization = %q, want Bearer apc_push_test", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload-start":
			var req cloudclient.UploadImageStartRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode upload-start: %v", err)
			}
			if req.ImageDigest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
				t.Errorf("ImageDigest = %q", req.ImageDigest)
			}
			if req.Platform != "linux/amd64" {
				t.Errorf("Platform = %q, want linux/amd64", req.Platform)
			}
			lockMap, ok := req.AgentLock.(map[string]interface{})
			if !ok || lockMap["agent_name"] != "test-agent" || lockMap["lockfile_signature"] == "" {
				t.Errorf("AgentLock = %#v, want signed test lock", req.AgentLock)
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(cloudclient.UploadImageStartResponse{
				UploadID:       "upload-pushed-001",
				ImageID:        "img-pushed-001",
				ChunkSizeBytes: 8 << 20,
			}); err != nil {
				t.Errorf("encode upload-start: %v", err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/images/upload/upload-pushed-001/chunk/1":
			chunk, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read chunk 1: %v", err)
			}
			if !bytes.Equal(chunk, tarData[:8<<20]) {
				t.Errorf("chunk 1 has %d bytes, want first 8 MiB", len(chunk))
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/images/upload/upload-pushed-001/chunk/2":
			chunk, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read chunk 2: %v", err)
			}
			if !bytes.Equal(chunk, tarData[8<<20:]) {
				t.Errorf("chunk 2 = %q, want tail", chunk)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload/upload-pushed-001/complete":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(cloudclient.AdmitImageResponse{
				ID:          "img-pushed-001",
				ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Status:      "admitted",
				RegistryRef: "registry.example.com/test-agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}); err != nil {
				t.Errorf("encode complete: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push",
		"--lock", lockPath,
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
	if !strings.Contains(stdout, "Registry:") {
		t.Errorf("expected registry in output, got: %q", stdout)
	}
	if savedRef != "agentpaas/test-agent:1.0.0" {
		t.Errorf("docker save ref = %q, want agentpaas/test-agent:1.0.0", savedRef)
	}
	for _, want := range []string{"Saving image…", "Uploading…", "chunk 1", "chunk 2", "Admitting image…"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q, got %q", want, stderr)
		}
	}
	if strings.Contains(stdout, "Saving image") || strings.Contains(stdout, "Uploading") || strings.Contains(stdout, "Admitting image") {
		t.Errorf("stdout must not contain progress, got %q", stdout)
	}
}

func TestCloudPush_SkipRegistryAdmitsWithNullRegistryRef(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_skip_test")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)
	oldDockerSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		t.Fatal("docker save must not run with --skip-registry")
		return nil, nil
	}
	t.Cleanup(func() { dockerSaveImage = oldDockerSave })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/admit" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode admit: %v", err)
		}
		if registryRef, ok := body["registry_ref"]; !ok || registryRef != nil {
			t.Errorf("registry_ref = %#v, want explicit null", registryRef)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cloudclient.AdmitImageResponse{
			ID:          "img-skip-001",
			ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Status:      "admitted",
		}); err != nil {
			t.Errorf("encode admit: %v", err)
		}
	}))
	defer apiServer.Close()

	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)
	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath, "--skip-registry")
	if err != nil {
		t.Fatalf("push --skip-registry: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Image admitted: img-skip-001") {
		t.Errorf("output = %q, want admitted image", stdout)
	}
}

func TestCloudPush_StartUnauthorizedPrintsNotLoggedIn(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_start_unauthorized")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)
	oldDockerSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		t.Fatal("docker save must not run when upload-start is unauthorized")
		return nil, nil
	}
	t.Cleanup(func() { dockerSaveImage = oldDockerSave })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/upload-start" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath, "--image", "custom/image:tag")
	if err == nil {
		t.Fatal("expected not-logged-in error")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "not logged in") {
		t.Errorf("error = %q, want not logged in", combined)
	}
}

func TestCloudPush_ChunkRetryOnServerError(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_retry_test")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)
	oldDockerSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader([]byte("retryable tar"))), nil
	}
	t.Cleanup(func() { dockerSaveImage = oldDockerSave })

	chunkAttempts := 0
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload-start":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(cloudclient.UploadImageStartResponse{
				UploadID:       "upload-retry-001",
				ChunkSizeBytes: 8 << 20,
			}); err != nil {
				t.Errorf("encode start: %v", err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/images/upload/upload-retry-001/chunk/1":
			chunkAttempts++
			if chunkAttempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload/upload-retry-001/complete":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(cloudclient.AdmitImageResponse{
				ID:          "img-retry-001",
				ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Status:      "admitted",
			}); err != nil {
				t.Errorf("encode complete: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath, "--image", "custom/image:tag")
	if err != nil {
		t.Fatalf("push retry: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if chunkAttempts != 2 {
		t.Errorf("chunk attempts = %d, want 2", chunkAttempts)
	}
	if !strings.Contains(stdout, "Image admitted: img-retry-001") {
		t.Errorf("output = %q, want admitted image", stdout)
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

// TestCloudPush_HelpAbstractsRegistryTransport verifies help does not expose
// the registry transport or its implementation credential.
func TestCloudPush_HelpAbstractsRegistryTransport(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "push", "--help")
	if err != nil {
		t.Fatalf("push --help: %v", err)
	}

	if !strings.Contains(stdout, "Upload a locally built agent image to AgentPaaS Cloud and admit it for deployment.") {
		t.Errorf("help should describe the tenant-token upload workflow, got: %s", stdout)
	}
	if !strings.Contains(stdout, "--target linux/amd64") {
		t.Errorf("help should require linux/amd64 packs, got: %s", stdout)
	}
	for _, internalDetail := range []string{"wrangler", "CLOUDFLARE_API_TOKEN"} {
		if strings.Contains(strings.ToLower(stdout), strings.ToLower(internalDetail)) {
			t.Errorf("help should not expose %q, got: %s", internalDetail, stdout)
		}
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

// ---- resolveLocalImageRef tests ----

func TestResolveLocalImageRef_PrefersTagWhenDigestMatches(t *testing.T) {
	lockDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	preferredRef := "agentpaas/test-agent:1.0.0"

	// Mock dockerImageInspect: preferred tag exists and digest matches.
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		if ref == preferredRef {
			return lockDigest, nil
		}
		t.Fatalf("unexpected dockerImageInspect ref: %s", ref)
		return "", fmt.Errorf("not found")
	}
	defer func() { dockerImageInspect = oldInspect }()

	lock := &pack.AgentLock{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		ImageDigest:  lockDigest,
	}

	ref, err := resolveLocalImageRef(context.Background(), lock, "")
	if err != nil {
		t.Fatalf("resolveLocalImageRef: %v", err)
	}
	if ref != preferredRef {
		t.Errorf("expected ref %q, got %q", preferredRef, ref)
	}
}

func TestResolveLocalImageRef_TagsDigestWhenTagMissing(t *testing.T) {
	lockDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	preferredRef := "agentpaas/test-agent:1.0.0"

	var taggedSrc, taggedDst string

	// Mock dockerImageInspect: preferred tag fails, digest succeeds.
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		if ref == preferredRef {
			return "", fmt.Errorf("not found")
		}
		if ref == lockDigest {
			return lockDigest, nil
		}
		return "", fmt.Errorf("unexpected ref: %s", ref)
	}
	defer func() { dockerImageInspect = oldInspect }()

	// Mock dockerTag to capture args.
	oldTag := dockerTag
	dockerTag = func(ctx context.Context, src, dst string) error {
		taggedSrc = src
		taggedDst = dst
		return nil
	}
	defer func() { dockerTag = oldTag }()

	lock := &pack.AgentLock{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		ImageDigest:  lockDigest,
	}

	ref, err := resolveLocalImageRef(context.Background(), lock, "")
	if err != nil {
		t.Fatalf("resolveLocalImageRef: %v", err)
	}
	if ref != preferredRef {
		t.Errorf("expected ref %q, got %q", preferredRef, ref)
	}
	if taggedSrc != lockDigest {
		t.Errorf("expected dockerTag src %q, got %q", lockDigest, taggedSrc)
	}
	if taggedDst != preferredRef {
		t.Errorf("expected dockerTag dst %q, got %q", preferredRef, taggedDst)
	}
}

func TestResolveLocalImageRef_ErrorWhenNeitherExists(t *testing.T) {
	lockDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	// Mock dockerImageInspect: both fail.
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	defer func() { dockerImageInspect = oldInspect }()

	lock := &pack.AgentLock{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		ImageDigest:  lockDigest,
	}

	_, err := resolveLocalImageRef(context.Background(), lock, "")
	if err == nil {
		t.Fatal("expected error when neither image exists")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
	if !strings.Contains(err.Error(), "pack --target linux/amd64 first") {
		t.Errorf("error should hint to pack, got: %v", err)
	}
}

func TestResolveLocalImageRef_ImageOverride(t *testing.T) {
	lock := &pack.AgentLock{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		ImageDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	// dockerImageInspect should NOT be called when --image is set.
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		t.Fatal("dockerImageInspect should not be called with --image override")
		return "", nil
	}
	defer func() { dockerImageInspect = oldInspect }()

	ref, err := resolveLocalImageRef(context.Background(), lock, "custom/image:tag")
	if err != nil {
		t.Fatalf("resolveLocalImageRef: %v", err)
	}
	if ref != "custom/image:tag" {
		t.Errorf("expected override ref, got %q", ref)
	}
}

func TestResolveLocalImageRef_DigestMatchNormalized(t *testing.T) {
	// Lock has digest without sha256: prefix; docker inspect returns with prefix.
	lockDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inspectDigest := "sha256:" + lockDigest
	preferredRef := "agentpaas/test-agent:1.0.0"

	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		return inspectDigest, nil
	}
	defer func() { dockerImageInspect = oldInspect }()

	lock := &pack.AgentLock{
		AgentName:    "test-agent",
		AgentVersion: "1.0.0",
		ImageDigest:  lockDigest,
	}

	ref, err := resolveLocalImageRef(context.Background(), lock, "")
	if err != nil {
		t.Fatalf("resolveLocalImageRef: %v", err)
	}
	if ref != preferredRef {
		t.Errorf("expected ref %q, got %q", preferredRef, ref)
	}
}

func TestCloudPush_JSONProgressBeforeFinalBlob(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_push_json")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)
	tarData := append(bytes.Repeat([]byte("a"), 8<<20), []byte("tail")...)
	oldDockerSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(tarData)), nil
	}
	t.Cleanup(func() { dockerSaveImage = oldDockerSave })
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	t.Cleanup(func() { dockerImageInspect = oldInspect })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload-start":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cloudclient.UploadImageStartResponse{
				UploadID:       "upload-json-001",
				ImageID:        "img-json-001",
				ChunkSizeBytes: 8 << 20,
			})
		case strings.HasPrefix(r.URL.Path, "/v1/images/upload/upload-json-001/chunk/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload/upload-json-001/complete":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cloudclient.AdmitImageResponse{
				ID:          "img-json-001",
				ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Status:      "admitted",
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "--json", "cloud", "push",
		"--lock", lockPath, "--platform", "linux/amd64")
	if err != nil {
		t.Fatalf("push --json: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stderr, `"event":"progress"`) {
		t.Fatalf("stderr missing JSON progress event before final blob: %q", stderr)
	}
	if !strings.Contains(stderr, `"chunk":1`) && !strings.Contains(stderr, `"chunk": 1`) {
		t.Fatalf("stderr missing chunk progress: %q", stderr)
	}
	var final map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &final); err != nil {
		t.Fatalf("stdout must be a single final JSON blob: %v stdout=%q", err, stdout)
	}
	if final["id"] != "img-json-001" {
		t.Fatalf("final blob = %#v", final)
	}
	if strings.Contains(stdout, `"event":"progress"`) {
		t.Fatalf("progress must not mix into stdout blob: %q", stdout)
	}
}

// newBundleLock writes run.bundle.tar beside a signed lock that carries the bundle keys.
func newBundleLock(t *testing.T, dir string, bundle []byte) (string, string) {
	t.Helper()
	sum := sha256.Sum256(bundle)
	digest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(dir, pack.RunBundleName), bundle, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	lock := &pack.AgentLock{
		SchemaVersion: 2,
		AgentName:     "test-agent",
		AgentVersion:  "1.0.0",
		Runtime:       "python",
		Platform:      "linux/amd64",
		ImageDigest:   "sha256:" + strings.Repeat("a", 64),
		BundleDigest:  digest,
		BundleSize:    int64(len(bundle)),
		RuntimeID:     pack.RuntimeIDPy311R1,
	}
	_ = signLockForTest(t, lock)
	return writeLockFile(t, dir, lock), digest
}

// TestCloudPush_BundleLockUploadsBundleBytes pins bundle push behavior: a bundle
// lock uploads bundle-sized bytes through the chunked upload and skips the
// image archive. A second push stores nothing new.
func TestCloudPush_BundleLockUploadsBundleBytes(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_bundle_push")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	bundle := []byte("run-bundle-s13-bytes")
	lockPath, digest := newBundleLock(t, dir, bundle)

	oldSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		t.Errorf("docker save must not run for a bundle lock, ref=%s", imageRef)
		return nil, fmt.Errorf("docker save must not run")
	}
	t.Cleanup(func() { dockerSaveImage = oldSave })
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		t.Errorf("docker inspect must not run for a bundle lock, ref=%s", ref)
		return "", fmt.Errorf("docker inspect must not run")
	}
	t.Cleanup(func() { dockerImageInspect = oldInspect })

	var mu sync.Mutex
	var stored []byte
	admitted := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer apc_bundle_push" {
			t.Errorf("Authorization = %q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			defer mu.Unlock()
			if !admitted {
				_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{})
				return
			}
			_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{{
				ID:           "img-bundle-001",
				ImageDigest:  "sha256:" + digest,
				BundleDigest: digest,
				Kind:         "bundle",
				Status:       "admitted",
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload-start":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cloudclient.UploadImageStartResponse{
				UploadID:       "upload-bundle-001",
				ImageID:        "img-bundle-001",
				ChunkSizeBytes: 8,
			})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/images/upload/upload-bundle-001/chunk/"):
			chunk, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read chunk: %v", err)
			}
			mu.Lock()
			stored = append(stored, chunk...)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload/upload-bundle-001/complete":
			mu.Lock()
			admitted = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cloudclient.AdmitImageResponse{
				ID:           "img-bundle-001",
				ImageDigest:  "sha256:" + digest,
				Status:       "admitted",
				Kind:         "bundle",
				BundleDigest: digest,
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath)
	if err != nil {
		t.Fatalf("push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	mu.Lock()
	got := append([]byte(nil), stored...)
	mu.Unlock()
	if !bytes.Equal(got, bundle) {
		t.Fatalf("stored %d bytes (%q), want bundle-sized %d bytes", len(got), got, len(bundle))
	}
	if strings.Contains(stderr, "Saving image") {
		t.Fatalf("bundle push must skip the image archive, stderr=%q", stderr)
	}

	stdout, stderr, err = executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath)
	if err != nil {
		t.Fatalf("second push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	mu.Lock()
	second := append([]byte(nil), stored...)
	mu.Unlock()
	if !bytes.Equal(second, bundle) {
		t.Fatalf("second push stored new bytes: got %d, want %d", len(second), len(bundle))
	}
}

// TestCloudPush_OlderCloudRejectsBundles pins the no-fallback line: a stub that
// answers that it does not accept bundles prints one line and exits non-zero.
func TestCloudPush_OlderCloudRejectsBundles(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_bundle_old")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	dir := t.TempDir()
	lockPath, _ := newBundleLock(t, dir, []byte("older-cloud-bundle"))

	oldSave := dockerSaveImage
	dockerSaveImage = func(ctx context.Context, imageRef string) (io.ReadCloser, error) {
		t.Errorf("docker save must not run when the cloud rejects bundles, ref=%s", imageRef)
		return nil, fmt.Errorf("docker save must not run")
	}
	t.Cleanup(func() { dockerSaveImage = oldSave })

	var stored int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/upload-start":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "bundles_not_accepted",
				"message": "does not accept bundles",
			})
		case r.Method == http.MethodPut:
			stored += 1
			t.Errorf("chunk upload after bundle rejection: %s", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath)
	if err == nil {
		t.Fatal("expected non-zero exit when the cloud rejects bundles")
	}
	if err.Error() != "the cloud is older than the CLI" {
		t.Fatalf("error = %q, want one line %q", err.Error(), "the cloud is older than the CLI")
	}
	if strings.Contains(err.Error(), "\n") || strings.Contains(stdout, "the cloud is older") || strings.Contains(stderr, "the cloud is older") {
		t.Fatalf("older-cloud line must be exactly one line, stdout=%q stderr=%q err=%q", stdout, stderr, err)
	}
	if stored != 0 {
		t.Fatalf("rejection stored %d chunks; want no fallback upload", stored)
	}
}

func TestCloudImages_ShowsKind(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_images_kind")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/images" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]cloudclient.ImageRecord{{
			ID:           "img-bundle-001",
			ImageDigest:  "sha256:" + strings.Repeat("ab", 32),
			BundleDigest: strings.Repeat("ab", 32),
			Kind:         "bundle",
			Status:       "admitted",
		}})
	}))
	defer apiServer.Close()
	t.Setenv("AGENTPAAS_CLOUD_API_URL", apiServer.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "images")
	if err != nil {
		t.Fatalf("images: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "KIND") || !strings.Contains(stdout, "bundle") {
		t.Fatalf("cloud images must show the kind, got %q", stdout)
	}
}
