package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePythonPackagePathUsesImageSDKWhenCwdWalkMisses(t *testing.T) {
	if pythonSDKDirExists("/python") {
		t.Skip("/python/agentpaas_sdk exists on this host; walk from / would prefer it")
	}
	imageSDK := filepath.Join(t.TempDir(), "python")
	if err := os.MkdirAll(filepath.Join(imageSDK, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("mkdir image sdk: %v", err)
	}

	got := resolvePythonPackagePath("/", imageSDK)
	if got != imageSDK {
		t.Fatalf("resolvePythonPackagePath() = %q, want image SDK %q when cwd is /", got, imageSDK)
	}
}

func TestResolvePythonPackagePathPrefersCwdWalkOverImageSDK(t *testing.T) {
	root := t.TempDir()
	walkSDK := filepath.Join(root, "python")
	if err := os.MkdirAll(filepath.Join(walkSDK, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("mkdir walk sdk: %v", err)
	}
	imageSDK := filepath.Join(root, "app", "python")
	if err := os.MkdirAll(filepath.Join(imageSDK, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("mkdir image sdk: %v", err)
	}

	got := resolvePythonPackagePath(root, imageSDK)
	if got != walkSDK {
		t.Fatalf("resolvePythonPackagePath() = %q, want walked SDK %q", got, walkSDK)
	}
}

func TestResolvePythonPackagePathFallsBackWhenImageSDKMissing(t *testing.T) {
	if pythonSDKDirExists("/python") {
		t.Skip("/python/agentpaas_sdk exists on this host; walk from / would prefer it")
	}
	got := resolvePythonPackagePath("/", filepath.Join(t.TempDir(), "missing-python"))
	want := filepath.Join(".", "python")
	if got != want {
		t.Fatalf("resolvePythonPackagePath() = %q, want %q", got, want)
	}
}

func TestResolvePythonPackagePathFindsSDKInParentBeforeImageDir(t *testing.T) {
	root := t.TempDir()
	walkSDK := filepath.Join(root, "python")
	if err := os.MkdirAll(filepath.Join(walkSDK, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("mkdir walk sdk: %v", err)
	}
	wd := filepath.Join(root, "nested", "cwd")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}
	imageSDK := filepath.Join(root, "elsewhere", "python")
	if err := os.MkdirAll(filepath.Join(imageSDK, "agentpaas_sdk"), 0o755); err != nil {
		t.Fatalf("mkdir image sdk: %v", err)
	}

	got := resolvePythonPackagePath(wd, imageSDK)
	if got != walkSDK {
		t.Fatalf("resolvePythonPackagePath() = %q, want parent SDK %q", got, walkSDK)
	}
}

func TestImagePythonPackageDirIsPackCopyDestination(t *testing.T) {
	if imagePythonPackageDir != "/app/python" {
		t.Fatalf("imagePythonPackageDir = %q, want /app/python (pack COPY python/ /app/python/)", imagePythonPackageDir)
	}
}
