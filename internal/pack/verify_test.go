package pack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerificationResultDefaults(t *testing.T) {
	var vr VerificationResult
	if vr.SDKPresent {
		t.Error("SDKPresent should default to false")
	}
	if vr.HarnessInImage {
		t.Error("HarnessInImage should default to false")
	}
	if vr.EntryInImage {
		t.Error("EntryInImage should default to false")
	}
	if vr.SDKInImage {
		t.Error("SDKInImage should default to false")
	}
	if vr.HarnessFresh {
		t.Error("HarnessFresh should default to false")
	}
	if vr.HealthzOK {
		t.Error("HealthzOK should default to false")
	}
	if vr.ReadyzOK {
		t.Error("ReadyzOK should default to false")
	}
}

func TestVerifyBuildOutput_EmptySDKDir(t *testing.T) {
	cfg := BuildConfig{
		ProjectDir:      t.TempDir(),
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        "test:empty-sdk",
	}
	// SDKDir is empty — the first check should fail.
	err := VerifyBuildOutput(context.Background(), cfg.ImageTag, cfg)
	if err == nil {
		t.Fatal("VerifyBuildOutput() error = nil, want error about missing SDK")
	}
	if !strings.Contains(err.Error(), "SDK directory was not resolved") {
		t.Fatalf("VerifyBuildOutput() error = %q, want SDK directory error", err)
	}
}

// realHarnessPath returns the system's linux harness binary path or skips the test.
func realHarnessPath(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		"/usr/local/bin/agentpaas-harness-linux",
		"/opt/homebrew/bin/agentpaas-harness-linux",
	} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 1000000 {
			return p
		}
	}
	t.Skip("real harness binary not found (need agentpaas-harness-linux >1MB)")
	return ""
}

// realSDKDir returns the system's Python SDK directory or skips the test.
func realSDKDir(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		"/usr/local/python",
		"/opt/homebrew/python",
	} {
		if info, err := os.Stat(filepath.Join(p, "agentpaas_sdk", "__init__.py")); err == nil && !info.IsDir() {
			return p
		}
	}
	// Try repo-relative
	if info, err := os.Stat(filepath.Join("..", "..", "python", "agentpaas_sdk", "__init__.py")); err == nil && !info.IsDir() {
		abs, _ := filepath.Abs(filepath.Join("..", "..", "python"))
		return abs
	}
	t.Skip("real SDK directory not found")
	return ""
}

// properAgentProject creates a project dir with a valid @agent.on_invoke handler.
func properAgentProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeBuildTestFile(t, dir, "main.py", []byte(`from agentpaas_sdk import agent

@agent.on_invoke
def handle_invoke(payload):
    return {"status": "OK", "answer": "test"}
`))
	writeBuildTestFile(t, dir, "agent.yaml", []byte("name: test-agent\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\n"))
	writeBuildTestFile(t, dir, "requirements.txt", []byte("# no deps\n"))
	return dir
}

func TestVerifyBuildOutput_ValidImage(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := properAgentProject(t)
	harnessPath := realHarnessPath(t)
	sdkDir := realSDKDir(t)
	tag := "agentpaas/verify-test-valid:latest"

	cfg := BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessPath,
		SDKDir:          sdkDir,
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
	}

	// BuildImage runs verification internally; if it passes, the image is good.
	result, err := BuildImage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildImage() (including verification) error = %v", err)
	}
	t.Logf("built + verified image: %s (digest: %s)", result.ImageRef, result.ImageDigest)
}

func TestVerifyBuildOutput_HarnessMissing(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := minimalDockerProject(t)
	// Use dummy harness — not a real binary
	harnessPath := writeHarness(t)
	tag := "agentpaas/verify-test-harness-missing:latest"

	// Create a minimal SDK dir so the SDK check passes.
	sdkDir := filepath.Join(t.TempDir(), "python")
	if err := os.MkdirAll(filepath.Join(sdkDir, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("MkdirAll(sdk) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "agentpaas_sdk", "__init__.py"), []byte("# SDK"), 0o644); err != nil {
		t.Fatalf("WriteFile(sdk __init__) error = %v", err)
	}

	cfg := BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessPath,
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
		SDKDir:          sdkDir,
	}

	// BuildImage includes verification — it should fail because the dummy
	// harness doesn't serve HTTP (smoke test fails).
	_, err := BuildImage(context.Background(), cfg)
	if err == nil {
		t.Fatal("BuildImage() error = nil, want smoke test failure from dummy harness")
	}
	t.Logf("BuildImage() error = %q (expected smoke test failure)", err)
	if !strings.Contains(err.Error(), "healthz") && !strings.Contains(err.Error(), "verification") {
		t.Fatalf("BuildImage() error = %q, want healthz/verification failure", err)
	}
}

func TestVerifyBuildOutput_StaleHarness(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := minimalDockerProject(t)
	// Use dummy harness
	harnessDir := t.TempDir()
	harnessA := filepath.Join(harnessDir, "harness")
	if err := os.WriteFile(harnessA, []byte("harness binary A\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(harnessA) error = %v", err)
	}

	// Create minimal SDK.
	sdkDir := filepath.Join(t.TempDir(), "python")
	if err := os.MkdirAll(filepath.Join(sdkDir, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("MkdirAll(sdk) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "agentpaas_sdk", "__init__.py"), []byte("# SDK"), 0o644); err != nil {
		t.Fatalf("WriteFile(sdk __init__) error = %v", err)
	}

	tag := "agentpaas/verify-test-stale:latest"
	cfg := BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessA,
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
		SDKDir:          sdkDir,
	}

	// Build the image with harness A (dummy — will fail smoke test, which is
	// expected). We just need the image to exist for the freshness test.
	_, buildErr := BuildImage(context.Background(), cfg)
	// Build will fail at smoke test — that's expected since harness is dummy.
	// But the image IS built. We need to test the freshness check separately.
	// Since BuildImage now runs VerifyBuildOutput internally, and the dummy
	// harness fails at smoke test, we test the freshness check by calling
	// VerifyBuildOutput directly after modifying the host file.

	if buildErr != nil {
		t.Logf("BuildImage() failed as expected (dummy harness smoke test): %v", buildErr)
		// The image was still built before verification failed. We can still
		// test the freshness check by calling VerifyBuildOutput directly.
	}

	// Now create harness B at the same path (different content).
	if err := os.WriteFile(harnessA, []byte("harness binary B DIFFERENT\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(harnessB) error = %v", err)
	}

	// Run verification directly — freshness check should fail before smoke test.
	err := VerifyBuildOutput(context.Background(), tag, cfg)
	if err == nil {
		t.Fatal("VerifyBuildOutput() error = nil, want stale harness error")
	}
	if !strings.Contains(err.Error(), "stale embedded binary") {
		t.Fatalf("VerifyBuildOutput() error = %q, want stale harness error", err)
	}
	t.Logf("VerifyBuildOutput() correctly detected stale harness: %q", err)
}
