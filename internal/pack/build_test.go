package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func TestComputeBuildInputDigestDeterministic(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	writeBuildTestFile(t, projectDir, "requirements.txt", []byte("idna==3.7\n"))

	digest1, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}
	digest2, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}

	if digest1 != digest2 {
		t.Fatalf("digest mismatch: %s != %s", digest1, digest2)
	}
}

func TestComputeBuildInputDigestChangesWithContent(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))

	digest1, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('goodbye')\n"))
	digest2, err := ComputeBuildInputDigest(projectDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}

	if digest1 == digest2 {
		t.Fatalf("digest did not change after content changed: %s", digest1)
	}
}

func TestComputeBuildInputDigestRespectsIgnore(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	writeBuildTestFile(t, projectDir, "ignored.txt", []byte("one\n"))
	ignore := NewIgnoreMatcher("ignored.txt\n")

	digest1, err := ComputeBuildInputDigest(projectDir, ignore)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}
	writeBuildTestFile(t, projectDir, "ignored.txt", []byte("two\n"))
	digest2, err := ComputeBuildInputDigest(projectDir, ignore)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}

	if digest1 != digest2 {
		t.Fatalf("ignored file changed digest: %s != %s", digest1, digest2)
	}
}

func TestComputeBuildInputDigestSortedOrder(t *testing.T) {
	projectA := t.TempDir()
	writeBuildTestFile(t, projectA, "z.py", []byte("z\n"))
	writeBuildTestFile(t, projectA, "a.py", []byte("a\n"))

	projectB := t.TempDir()
	writeBuildTestFile(t, projectB, "a.py", []byte("a\n"))
	writeBuildTestFile(t, projectB, "z.py", []byte("z\n"))

	digestA, err := ComputeBuildInputDigest(projectA, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest(projectA) error = %v", err)
	}
	digestB, err := ComputeBuildInputDigest(projectB, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest(projectB) error = %v", err)
	}

	if digestA != digestB {
		t.Fatalf("digest depends on creation order: %s != %s", digestA, digestB)
	}
}

func TestComputeBuildInputDigestRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	if err := os.Symlink(filepath.Join(projectDir, "main.py"), filepath.Join(projectDir, "link.py")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	_, err := ComputeBuildInputDigest(projectDir, nil)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("ComputeBuildInputDigest() error = %v, want symlink rejection", err)
	}
}

func TestComputeBuildInputDigestEmptyDir(t *testing.T) {
	digest, err := ComputeBuildInputDigest(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest() error = %v", err)
	}

	const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if digest != emptySHA256 {
		t.Fatalf("empty digest = %s, want %s", digest, emptySHA256)
	}
}

func TestDefaultBaseImage(t *testing.T) {
	for _, runtimeType := range []RuntimeType{RuntimePython, RuntimeLangGraph, RuntimeCrewAI} {
		if got := defaultBaseImage(runtimeType); got != "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f" {
			t.Fatalf("defaultBaseImage(%q) = %q", runtimeType, got)
		}
	}
}

func TestResolveDependenciesRequirementsTxt(t *testing.T) {
	requireUV(t)
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "requirements.txt", []byte("idna==3.7\ncertifi==2024.2.2\n"))

	deps, err := ResolveDependencies(context.Background(), projectDir, RuntimePython)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	requireContains(t, deps, "idna@3.7")
	requireContains(t, deps, "certifi@2024.2.2")
}

func TestResolveDependenciesConflict(t *testing.T) {
	requireUV(t)
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "requirements.txt", []byte("requests==2.31.0\nrequests==2.30.0\n"))

	_, err := ResolveDependencies(context.Background(), projectDir, RuntimePython)
	if err == nil {
		t.Fatal("ResolveDependencies() error = nil, want conflict")
	}
	if !strings.Contains(err.Error(), "uv pip compile failed:\n") {
		t.Fatalf("ResolveDependencies() error = %q, want uv output prefix", err)
	}
	if !strings.Contains(err.Error(), "requests") {
		t.Fatalf("ResolveDependencies() error = %q, want verbatim dependency output", err)
	}
}

func TestResolveDependenciesPyproject(t *testing.T) {
	requireUV(t)
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "pyproject.toml", []byte(`[project]
name = "agentpaas-test"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = ["idna==3.7"]
`))

	deps, err := ResolveDependencies(context.Background(), projectDir, RuntimePython)
	if err != nil {
		t.Fatalf("ResolveDependencies() error = %v", err)
	}

	requireContains(t, deps, "idna@3.7")
}

func TestCreateBuildContextDeterministic(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "b.py", []byte("b\n"))
	writeBuildTestFile(t, projectDir, "a.py", []byte("a\n"))

	tar1 := readAllBuildContext(t, projectDir, nil)
	tar2 := readAllBuildContext(t, projectDir, nil)

	if !bytes.Equal(tar1, tar2) {
		t.Fatal("CreateBuildContext() produced different tar bytes for unchanged input")
	}
}

func TestCreateBuildContextRespectsIgnore(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	writeBuildTestFile(t, projectDir, "ignored.txt", []byte("ignored\n"))

	entries := tarEntries(t, readAllBuildContext(t, projectDir, NewIgnoreMatcher("ignored.txt\n")))
	if reflect.DeepEqual(entries, []string{"main.py"}) {
		return
	}
	t.Fatalf("tar entries = %v, want only main.py", entries)
}

func TestCreateBuildContextSortedOrder(t *testing.T) {
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "z.py", []byte("z\n"))
	writeBuildTestFile(t, projectDir, "a.py", []byte("a\n"))

	entries := tarEntries(t, readAllBuildContext(t, projectDir, nil))
	want := []string{"a.py", "z.py"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("tar entries = %v, want %v", entries, want)
	}
}

func TestCreateBuildContextRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	if err := os.Symlink(filepath.Join(projectDir, "main.py"), filepath.Join(projectDir, "link.py")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	_, err := CreateBuildContext(projectDir, nil)
	if err == nil || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("CreateBuildContext() error = %v, want symlink rejection", err)
	}
}

func TestBuildImageReproducible(t *testing.T) {
	requireDocker(t)
	requireUV(t)
	projectDir := minimalDockerProject(t)
	harnessPath := writeHarness(t)

	cfg := dockerBuildConfig(t, projectDir, harnessPath, "agentpaas/build-test-repro:one")
	result1, err := BuildImage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildImage(first) error = %v", err)
	}

	cfg.ImageTag = "agentpaas/build-test-repro:two"
	result2, err := BuildImage(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildImage(second) error = %v", err)
	}

	// BuildInputDigest is computed deterministically from the sorted build context
	// (file paths + contents + deps). Same input -> same digest. This is the
	// reproducibility guarantee AgentPaaS provides; the final Docker ImageDigest
	// depends on Docker's build engine and is not fully deterministic.
	if result1.BuildInputDigest != result2.BuildInputDigest {
		t.Fatalf("build input digest mismatch: %s != %s", result1.BuildInputDigest, result2.BuildInputDigest)
	}
	// Verify both builds produced the same locked dependencies.
	if len(result1.DepsLocked) != len(result2.DepsLocked) {
		t.Fatalf("deps count mismatch: %d != %d", len(result1.DepsLocked), len(result2.DepsLocked))
	}
	for i, dep := range result1.DepsLocked {
		if dep != result2.DepsLocked[i] {
			t.Fatalf("dep mismatch at index %d: %s != %s", i, dep, result2.DepsLocked[i])
		}
	}
}

func TestBuildImageNonRoot(t *testing.T) {
	requireDocker(t)
	requireUV(t)
	projectDir := minimalDockerProject(t)
	harnessPath := writeHarness(t)
	tag := "agentpaas/build-test-nonroot:latest"

	if _, err := BuildImage(context.Background(), dockerBuildConfig(t, projectDir, harnessPath, tag)); err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}

	inspect := inspectImage(t, tag)
	if inspect.Config == nil || inspect.Config.User != "64000:64000" {
		t.Fatalf("image user = %q, want 64000:64000", inspect.Config.User)
	}
}

func TestBuildImageNoShell(t *testing.T) {
	requireDocker(t)
	requireUV(t)
	projectDir := minimalDockerProject(t)
	harnessPath := writeHarness(t)
	tag := "agentpaas/build-test-noshell:latest"

	if _, err := BuildImage(context.Background(), dockerBuildConfig(t, projectDir, harnessPath, tag)); err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}

	inspect := inspectImage(t, tag)
	if inspect.Config != nil && strings.Contains(strings.Join(inspect.Config.Env, "\n"), "/bin/sh") {
		t.Fatalf("image config env unexpectedly references shell: %v", inspect.Config.Env)
	}
}

func TestBuildImageHarnessAsPID1(t *testing.T) {
	requireDocker(t)
	requireUV(t)
	projectDir := minimalDockerProject(t)
	harnessPath := writeHarness(t)
	tag := "agentpaas/build-test-entrypoint:latest"

	if _, err := BuildImage(context.Background(), dockerBuildConfig(t, projectDir, harnessPath, tag)); err != nil {
		t.Fatalf("BuildImage() error = %v", err)
	}

	inspect := inspectImage(t, tag)
	if inspect.Config == nil || !reflect.DeepEqual(inspect.Config.Entrypoint, []string{"/agentpaas/harness"}) {
		t.Fatalf("entrypoint = %v, want [/agentpaas/harness]", inspect.Config.Entrypoint)
	}
}

func TestBuildImageDependencyConflict(t *testing.T) {
	requireDocker(t)
	requireUV(t)
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	writeBuildTestFile(t, projectDir, "requirements.txt", []byte("requests==2.31.0\nrequests==2.30.0\n"))

	_, err := BuildImage(context.Background(), dockerBuildConfig(t, projectDir, writeHarness(t), "agentpaas/build-test-conflict:latest"))
	if err == nil {
		t.Fatal("BuildImage() error = nil, want dependency conflict")
	}
	if !strings.Contains(err.Error(), "uv pip compile failed:\n") || !strings.Contains(err.Error(), "requests") {
		t.Fatalf("BuildImage() error = %q, want verbatim uv output", err)
	}
}

func writeBuildTestFile(t *testing.T, dir string, relPath string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
}

func readAllBuildContext(t *testing.T, projectDir string, ignore *IgnoreMatcher) []byte {
	t.Helper()
	reader, err := CreateBuildContext(projectDir, ignore)
	if err != nil {
		t.Fatalf("CreateBuildContext() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	return data
}

func tarEntries(t *testing.T, data []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	var entries []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatalf("tar.Next() error = %v", err)
		}
		entries = append(entries, header.Name)
	}
}

func requireContains(t *testing.T, got []string, want string) {
	t.Helper()
	for _, item := range got {
		if item == want {
			return
		}
	}
	t.Fatalf("deps = %v, want item %q", got, want)
}

func requireUV(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv binary is not available")
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") == "" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker image build tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker client unavailable: %v", err)
	}
	defer func() { _ = cli.Close() }()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}
}

func minimalDockerProject(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	writeBuildTestFile(t, projectDir, "main.py", []byte("print('hello')\n"))
	writeBuildTestFile(t, projectDir, "requirements.txt", []byte("idna==3.7\n"))

	return projectDir
}

func writeHarness(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harness")
	if err := os.WriteFile(path, []byte("not executed by image build tests\n"), 0o755); err != nil {
		t.Fatalf("os.WriteFile(harness) error = %v", err)
	}

	return path
}

func dockerBuildConfig(t *testing.T, projectDir string, harnessPath string, tag string) BuildConfig {
	t.Helper()

	return BuildConfig{
		ProjectDir:      projectDir,
		Runtime:         RuntimePython,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f",
		HarnessPath:     harnessPath,
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
		ImageTag:        tag,
	}
}

func inspectImage(t *testing.T, tag string) image.InspectResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Docker client unavailable: %v", err)
	}
	defer func() { _ = cli.Close() }()
	inspect, err := cli.ImageInspect(ctx, tag)
	if err != nil {
		t.Fatalf("ImageInspectWithRaw(%q) error = %v", tag, err)
	}

	return inspect
}
