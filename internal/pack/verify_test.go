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

func TestVerifyBuildOutput_ValidImage(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := minimalDockerProject(t)
	harnessPath := writeHarness(t)
	tag := "agentpaas/verify-test-valid:latest"

	cfg := dockerBuildConfig(t, projectDir, harnessPath, tag)
	// Set SDKDir to a valid temp dir with agentpaas_sdk/__init__.py
	sdkDir := filepath.Join(t.TempDir(), "python")
	if err := os.MkdirAll(filepath.Join(sdkDir, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("MkdirAll(sdk) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(sdkDir, "agentpaas_sdk", "__init__.py"), []byte("# SDK"), 0o644); err != nil {
		t.Fatalf("WriteFile(sdk __init__) error = %v", err)
	}
	cfg.SDKDir = sdkDir

	// Build the image first.
	result, err := BuildImage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}
	t.Logf("built image: %s (digest: %s)", result.ImageRef, result.ImageDigest)

	// Now run verification.
	err = VerifyBuildOutput(context.Background(), cfg.ImageTag, cfg)
	if err != nil {
		t.Fatalf("VerifyBuildOutput() error = %v", err)
	}
}

func TestVerifyBuildOutput_HarnessMissing(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := minimalDockerProject(t)
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

	// Build the image first.
	result, err := BuildImage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}
	t.Logf("built image: %s (digest: %s)", result.ImageRef, result.ImageDigest)

	// Now run verification. The smoke test should fail because the dummy
	// harness doesn't serve HTTP.
	err = VerifyBuildOutput(context.Background(), tag, cfg)
	if err == nil {
		t.Fatal("VerifyBuildOutput() error = nil, want smoke test failure")
	}
	t.Logf("VerifyBuildOutput() error = %q (expected)", err)
}

func TestVerifyBuildOutput_StaleHarness(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker verification tests")
	}
	requireDocker(t)
	requireUV(t)

	projectDir := minimalDockerProject(t)
	tag := "agentpaas/verify-test-stale:latest"

	// Create harness A — build the image with this one.
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

	cfgA := BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessA,
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
		SDKDir:          sdkDir,
	}

	// Build the image with harness A.
	_, err := BuildImage(context.Background(), cfgA)
	if err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}

	// Now create harness B at the same path (different content).
	if err := os.WriteFile(harnessA, []byte("harness binary B\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(harnessB) error = %v", err)
	}

	// Run verification with cfg pointing to harness B (different MD5).
	err = VerifyBuildOutput(context.Background(), tag, cfgA)
	if err == nil {
		t.Fatal("VerifyBuildOutput() error = nil, want stale harness error")
	}
	if !strings.Contains(err.Error(), "stale embedded binary") {
		t.Fatalf("VerifyBuildOutput() error = %q, want stale harness error", err)
	}
	t.Logf("VerifyBuildOutput() error = %q (expected)", err)
}