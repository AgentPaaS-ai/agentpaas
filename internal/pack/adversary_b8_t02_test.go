//go:build adversary

package pack

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdversaryB8T02_SymlinkInjection(t *testing.T) {
	projectDir := t.TempDir()
	// Create a symlink in project dir pointing outside (to /etc/passwd or /tmp target)
	outsideTarget := "/etc"
	symlinkPath := filepath.Join(projectDir, "evil_link")
	if err := os.Symlink(outsideTarget, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	// Also create a regular file
	if err := os.WriteFile(filepath.Join(projectDir, "safe.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("write safe: %v", err)
	}

	ignore, _ := LoadIgnore(projectDir)
	_, err := ComputeBuildInputDigest(projectDir, ignore)
	if err == nil {
		t.Fatal("ComputeBuildInputDigest did not reject symlink injection")
	}
	if !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Errorf("expected symlink error, got: %v", err)
	}

	_, err = CreateBuildContext(projectDir, ignore)
	if err == nil {
		t.Fatal("CreateBuildContext did not reject symlink injection")
	}
}

func TestAdversaryB8T02_SymlinkInParentComponent(t *testing.T) {
	// Multi-round style: symlink in parent dir component
	tmp := t.TempDir()
	realProject := filepath.Join(tmp, "realproj")
	if err := os.MkdirAll(realProject, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realProject, "app.py"), []byte("print(1)"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink parent: /tmp/symparent -> realproj
	symParent := filepath.Join(tmp, "symparent")
	if err := os.Symlink(realProject, symParent); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}
	// Now projectDir via symlink parent
	projectDirViaSym := symParent

	ignore, _ := LoadIgnore(realProject) // ignore from real
	_, err := ComputeBuildInputDigest(projectDirViaSym, ignore)
	if err == nil {
		// ADVERSARY BREAK: parent component symlink not fully rejected by rejectSymlinkPath in some paths
		t.Log("ADVERSARY BREAK: symlink in parent directory component was accepted")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAdversaryB8T02_PathTraversal(t *testing.T) {
	projectDir := t.TempDir()
	// Validate rejects .. in projectDir name - use string containing .. not cleaned by Join
	badDir := projectDir + "/../escape"
	err := validateProjectDir(badDir)
	if err == nil {
		t.Fatal("validateProjectDir accepted path with ..")
	}

	// Test safeRelPath escape
	_, err = safeRelPath(projectDir, filepath.Join(projectDir, "..", "outside.txt"))
	if err == nil {
		t.Fatal("safeRelPath accepted traversal")
	}
	if !strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected traversal error: %v", err)
	}
}

func TestAdversaryB8T02_NonDeterminism(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "main.py"), []byte("print('hi')"), 0644); err != nil {
		t.Fatal(err)
	}
	ignore, _ := LoadIgnore(projectDir)

	d1, err := ComputeBuildInputDigest(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ComputeBuildInputDigest(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("non-deterministic digest for identical input")
	}

	r1, err := CreateBuildContext(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := CreateBuildContext(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := io.ReadAll(r1)
	b2, _ := io.ReadAll(r2)
	if !bytes.Equal(b1, b2) {
		t.Fatal("CreateBuildContext non-deterministic tar")
	}
}

func TestAdversaryB8T02_SecretLeakage(t *testing.T) {
	projectDir := t.TempDir()
	// .env is in default ignore; test that it is excluded
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("SECRET=leak"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "app.py"), []byte("print(1)"), 0644); err != nil {
		t.Fatal(err)
	}
	ignore, _ := LoadIgnore(projectDir)
	files, err := collectBuildFiles(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.relPath, ".env") {
			t.Fatal("secret .env leaked into build context")
		}
	}
	// Test custom secret pattern not covered by default? e.g. id_rsa if not ignored explicitly
}

func TestAdversaryB8T02_ContextSizeWarning(t *testing.T) {
	projectDir := t.TempDir()
	// Simulate large file (use smaller for test speed; real >100MB impractical)
	// Code has no size check/warning, so this tests the gap
	largeData := make([]byte, 1024*1024) // 1MB proxy
	if err := os.WriteFile(filepath.Join(projectDir, "large.bin"), largeData, 0644); err != nil {
		t.Fatal(err)
	}
	ignore, _ := LoadIgnore(projectDir)
	_, err := ComputeBuildInputDigest(projectDir, ignore)
	if err != nil {
		t.Fatalf("large file caused error instead of warning: %v", err)
	}
	// If >100MB check existed it would warn; absence is gap but no panic ok
}

func TestAdversaryB8T02_DependencyInjection(t *testing.T) {
	// Hard to tamper without uv; test that ResolveDependencies rejects symlink on reqs
	projectDir := t.TempDir()
	req := filepath.Join(projectDir, "requirements.txt")
	if err := os.WriteFile(req, []byte("flask"), 0644); err != nil {
		t.Fatal(err)
	}
	// Symlink the req file
	symReq := filepath.Join(projectDir, "reqs-sym.txt")
	if err := os.Symlink(req, symReq); err != nil {
		t.Fatal(err)
	}
	// But resolve uses direct path; test rejectSymlinkPath on it
	err := rejectSymlinkPath(symReq, false)
	if err == nil {
		// ADVERSARY BREAK possible if resolve called on symlinked path
		t.Log("ADVERSARY BREAK: rejectSymlinkPath allowed symlinked reqs path")
	}
}

func TestAdversaryB8T02_BaseImageNotPinned(t *testing.T) {
	cfg := BuildConfig{
		ProjectDir: t.TempDir(),
		Runtime:    RuntimePython,
		BaseImage:  "gcr.io/distroless/python3-debian12", // tag only, no @sha256:
		ImageTag:   "test:latest",
	}
	err := validateBuildConfig(&cfg)
	if err != nil {
		t.Logf("validate rejected tag-only (good): %v", err)
		return
	}
	// If no reject, document break
	// ADVERSARY BREAK: base image accepts tag-only without digest pin
	t.Log("ADVERSARY BREAK: BaseImage tag-only accepted; should require digest pin for security")
}

func TestAdversaryB8T02_FilePermissionsNonRoot(t *testing.T) {
	// Check renderDockerfile enforces non-root 64000
	cfg := BuildConfig{
		ProjectDir:      t.TempDir(),
		NonRootUID:      64000,
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:deadbeef",
		ImageTag:        "test:tag",
		SourceDateEpoch: time.Unix(0, 0),
	}
	// Need dummy deps and harness? render doesn't call exec
	dockerfile := renderDockerfile(cfg, []string{})
	if !strings.Contains(dockerfile, "USER 64000:64000") {
		t.Error("non-root UID not enforced in Dockerfile")
	}
	if !strings.Contains(dockerfile, "COPY --chown=64000:64000 project/") {
		t.Error("chown for project not set to non-root")
	}
}

func TestAdversaryB8T02_PID1HarnessEntrypoint(t *testing.T) {
	cfg := BuildConfig{
		ProjectDir:      t.TempDir(),
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:deadbeef",
		ImageTag:        "test:tag",
		SourceDateEpoch: time.Unix(0, 0),
	}
	dockerfile := renderDockerfile(cfg, nil)
	if !strings.Contains(dockerfile, `ENTRYPOINT ["/agentpaas/harness"]`) {
		t.Fatal("harness not set as PID 1 entrypoint")
	}
	if !strings.Contains(dockerfile, "COPY --chown=0:0 harness /agentpaas/harness") {
		t.Error("harness not copied to /agentpaas/harness")
	}
}

func TestAdversaryB8T02_TarOrdering(t *testing.T) {
	projectDir := t.TempDir()
	// Create files out of alpha order
	if err := os.WriteFile(filepath.Join(projectDir, "z_last.py"), []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "a_first.py"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "m_mid.py"), []byte("m"), 0644); err != nil {
		t.Fatal(err)
	}
	ignore, _ := LoadIgnore(projectDir)
	reader, err := CreateBuildContext(projectDir, ignore)
	if err != nil {
		t.Fatal(err)
	}
	// Read tar and verify entries sorted
	tr := tar.NewReader(reader.(io.Reader))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	// Expect sorted order
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Fatal("tar not sorted lexicographically")
		}
	}
}