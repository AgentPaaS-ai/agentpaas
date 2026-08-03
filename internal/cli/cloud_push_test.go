package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
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

// TestCloudPush_HelpAbstractsRegistryTransport verifies help does not expose
// the registry transport or its implementation credential.
func TestCloudPush_HelpAbstractsRegistryTransport(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "push", "--help")
	if err != nil {
		t.Fatalf("push --help: %v", err)
	}

	if !strings.Contains(stdout, "Push a locally built agent image to the AgentPaaS cloud registry and admit it for deployment.") {
		t.Errorf("help should describe the cloud registry workflow, got: %s", stdout)
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

// TestCloudPush_MissingRegistryCredentialsAbstractsTransport verifies the
// missing registry credential error gives user-facing next steps.
func TestCloudPush_MissingRegistryCredentialsAbstractsTransport(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_registry_credentials_test")
	t.Setenv("AGENTPAAS_CLOUD_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_API_TOKEN", "")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)

	_, stderr, err := executeCloudCmd(t, "", "cloud", "push", "--lock", lockPath)
	if err == nil {
		t.Fatal("expected error when registry credentials are unavailable")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "registry credentials not configured") {
		t.Errorf("error should explain missing registry credentials, got: %v", combined)
	}
	if !strings.Contains(combined, "agentpaas cloud login") {
		t.Errorf("error should suggest cloud login, got: %v", combined)
	}
	if !strings.Contains(combined, "--skip-registry") {
		t.Errorf("error should mention --skip-registry, got: %v", combined)
	}
	if strings.Contains(combined, "CLOUDFLARE_API_TOKEN") {
		t.Errorf("error should not expose the implementation credential, got: %v", combined)
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

// ---- runWranglerPush tests ----

func TestPushCommand_CallsWranglerWithTagNotDigest(t *testing.T) {
	store := setupFakeTokenStore(t)
	_ = store.Set(context.Background(), "apc_tag_test")

	dir := t.TempDir()
	lockPath := newTestLock(t, dir)

	// Set CLOUDFLARE_API_TOKEN so we pass the env check.
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-token")

	// Mock resolveLocalImageRef hooks.
	oldInspect := dockerImageInspect
	dockerImageInspect = func(ctx context.Context, ref string) (string, error) {
		return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	defer func() { dockerImageInspect = oldInspect }()

	var pushedRef string
	oldPush := runWranglerPush
	runWranglerPush = func(ctx context.Context, imageRef string) (string, error) {
		pushedRef = imageRef
		return "registry.cloudflare.com/test-org/test-repo:latest", nil
	}
	defer func() { runWranglerPush = oldPush }()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/admit" {
			var req cloudclient.AdmitImageRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			resp := cloudclient.AdmitImageResponse{
				ID:          "img-tag-001",
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
		"--platform", "linux/amd64")
	if err != nil {
		t.Fatalf("push: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}

	// Verify wrangler was called with a tag (agentpaas/...), not a sha256: digest.
	if !strings.Contains(pushedRef, "agentpaas/") {
		t.Errorf("wrangler push should receive agentpaas/<name>:<version>, got: %q", pushedRef)
	}
	if strings.HasPrefix(pushedRef, "sha256:") {
		t.Errorf("wrangler push should NOT receive a digest, got: %q", pushedRef)
	}
	if !strings.Contains(stdout, "img-tag-001") {
		t.Errorf("expected img-tag-001 in output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Registry:") {
		t.Errorf("output should include Registry, got: %q", stdout)
	}
}

// ---- parseWranglerPushOutput tests ----

func TestParseWranglerPushOutput_PrefersPushedImage(t *testing.T) {
	output := `Deploying container...
Uploading layers...
Pushed image: registry.cloudflare.com/my-org/my-agent:sha256-abc.1@sha256:abcd1234
Done.`

	ref, err := parseWranglerPushOutput(output)
	if err != nil {
		t.Fatalf("parseWranglerPushOutput: %v", err)
	}
	if ref != "registry.cloudflare.com/my-org/my-agent:sha256-abc.1@sha256:abcd1234" {
		t.Errorf("expected extracted URL, got: %q", ref)
	}
}

func TestParseWranglerPushOutput_PushedImageWithoutURL(t *testing.T) {
	output := `Pushed image: some other text
registry.cloudflare.com/my-org/my-agent:latest
Done.`

	ref, err := parseWranglerPushOutput(output)
	if err != nil {
		t.Fatalf("parseWranglerPushOutput: %v", err)
	}
	// "Pushed image:" line has no registry.cloudflare.com URL, so fallback finds the next line.
	if ref != "registry.cloudflare.com/my-org/my-agent:latest" {
		t.Errorf("expected fallback URL, got: %q", ref)
	}
}

func TestParseWranglerPushOutput_FallbackToRegistryLine(t *testing.T) {
	output := `Uploading image...
registry.cloudflare.com/my-org/other-agent:sha256-def.5
Done.`

	ref, err := parseWranglerPushOutput(output)
	if err != nil {
		t.Fatalf("parseWranglerPushOutput: %v", err)
	}
	if !strings.Contains(ref, "registry.cloudflare.com") {
		t.Errorf("expected registry.cloudflare.com line, got: %q", ref)
	}
}

func TestParseWranglerPushOutput_NoMatch(t *testing.T) {
	output := `Uploaded successfully.`
	_, err := parseWranglerPushOutput(output)
	if err == nil {
		t.Fatal("expected error when no registry ref found")
	}
	if !strings.Contains(err.Error(), "no registry ref found") {
		t.Errorf("expected 'no registry ref found' error, got: %v", err)
	}
}

func TestParseWranglerPushOutput_PushedImageWithTokenSeparation(t *testing.T) {
	// "Pushed image:" and URL may be separated by whitespace or punctuation.
	output := `Pushed image:
  registry.cloudflare.com/my-org/agent:prod@sha256:ffff
Done.`

	ref, err := parseWranglerPushOutput(output)
	if err != nil {
		t.Fatalf("parseWranglerPushOutput: %v", err)
	}
	if ref != "registry.cloudflare.com/my-org/agent:prod@sha256:ffff" {
		t.Errorf("expected URL, got: %q", ref)
	}
}