package routedrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactWorkspace_NestedRegularFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, err := NewArtifactWorkspace(root, RunID("r1"))
	if err != nil {
		t.Fatalf("NewArtifactWorkspace: %v", err)
	}

	// Create a nested file.
	nested := filepath.Join(root, "charts", "themes.json")
	os.MkdirAll(filepath.Dir(nested), 0o700)
	os.WriteFile(nested, []byte(`{"theme":"dark"}`), 0o600)

	meta, err := aw.ValidateAndAccept(context.Background(), "charts/themes.json", AttemptID("a1"))
	if err != nil {
		t.Fatalf("ValidateAndAccept: %v", err)
	}
	if meta.Digest == "" {
		t.Fatal("expected non-empty digest")
	}
	if meta.ByteSize != 16 {
		t.Fatalf("expected 16 bytes, got %d", meta.ByteSize)
	}
	if meta.MediaType != "application/json" {
		t.Fatalf("expected application/json, got %s", meta.MediaType)
	}
	if meta.RelativePath != "charts/themes.json" {
		t.Fatalf("expected charts/themes.json, got %s", meta.RelativePath)
	}
}

func TestArtifactWorkspace_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	aw, _ := NewArtifactWorkspace(filepath.Join(dir, "artifacts"), RunID("r1"))
	_, err := aw.ValidateAndAccept(context.Background(), "/etc/passwd", AttemptID("a1"))
	if err == nil {
		t.Fatal("absolute path should be rejected")
	}
}

func TestArtifactWorkspace_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	aw, _ := NewArtifactWorkspace(filepath.Join(dir, "artifacts"), RunID("r1"))
	_, err := aw.ValidateAndAccept(context.Background(), "../secret", AttemptID("a1"))
	if err == nil {
		t.Fatal("traversal path should be rejected")
	}
}

func TestArtifactWorkspace_SymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))

	// Create a real file and a symlink to it.
	target := filepath.Join(dir, "target.txt")
	os.WriteFile(target, []byte("secret"), 0o600)
	link := filepath.Join(root, "link.json")
	os.MkdirAll(root, 0o700)
	os.Symlink(target, link)

	_, err := aw.ValidateAndAccept(context.Background(), "link.json", AttemptID("a1"))
	if err == nil {
		t.Fatal("symlink should be rejected")
	}
}

func TestArtifactWorkspace_PerFileQuota(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))

	// Create a file exceeding 25 MiB (use small limit for test).
	bigData := make([]byte, 26*1024*1024)
	os.MkdirAll(root, 0o700)
	os.WriteFile(filepath.Join(root, "big.bin"), bigData, 0o600)

	_, err := aw.ValidateAndAccept(context.Background(), "big.bin", AttemptID("a1"))
	if err == nil {
		t.Fatal("file exceeding per-file quota should be rejected")
	}
	if !strings.Contains(err.Error(), "SizeCapExceeded") && !strings.Contains(err.Error(), "size") {
		t.Fatalf("expected size cap error, got: %v", err)
	}
}

func TestArtifactWorkspace_TotalQuota(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	os.MkdirAll(root, 0o700)

	// Create 5 files of 24 MiB each = 120 MiB (exceeds 100 MiB total).
	for i := 0; i < 5; i++ {
		name := filepath.Join(root, "file"+string(rune('1'+i))+".bin")
		os.WriteFile(name, make([]byte, 24*1024*1024), 0o600)
		rel := "file" + string(rune('1'+i)) + ".bin"
		_, err := aw.ValidateAndAccept(context.Background(), rel, AttemptID("a1"))
		if err != nil && i < 4 {
			t.Fatalf("file %d should be accepted: %v", i, err)
		}
		if err != nil && i == 4 {
			// 5th file exceeds total — expected.
			return
		}
	}
	t.Fatal("expected total quota exceeded on 5th file")
}

func TestArtifactWorkspace_BackslashRejected(t *testing.T) {
	dir := t.TempDir()
	aw, _ := NewArtifactWorkspace(filepath.Join(dir, "artifacts"), RunID("r1"))
	_, err := aw.ValidateAndAccept(context.Background(), "foo\\bar", AttemptID("a1"))
	if err == nil {
		t.Fatal("backslash path should be rejected")
	}
}

func TestArtifactWorkspace_TooManySegments(t *testing.T) {
	dir := t.TempDir()
	aw, _ := NewArtifactWorkspace(filepath.Join(dir, "artifacts"), RunID("r1"))
	_, err := aw.ValidateAndAccept(context.Background(), "a/b/c/d/e/f/g/i/j", AttemptID("a1"))
	if err == nil {
		t.Fatal("too many segments should be rejected")
	}
}

func TestArtifactWorkspace_DigestChangesOnMutation(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	os.MkdirAll(root, 0o700)

	fpath := filepath.Join(root, "data.json")
	os.WriteFile(fpath, []byte(`{"v":1}`), 0o600)

	meta1, err := aw.ValidateAndAccept(context.Background(), "data.json", AttemptID("a1"))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	// Mutate file.
	os.WriteFile(fpath, []byte(`{"v":2}`), 0o600)

	// Re-accept should produce different digest.
	aw2, _ := NewArtifactWorkspace(root, RunID("r1"))
	meta2, err := aw2.ValidateAndAccept(context.Background(), "data.json", AttemptID("a1"))
	if err != nil {
		t.Fatalf("re-accept: %v", err)
	}
	if meta1.Digest == meta2.Digest {
		t.Fatal("digest should change when file content changes")
	}
}

func TestArtifactWorkspace_RemoveUnreferenced(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	os.MkdirAll(root, 0o700)

	// Create an accepted file.
	os.WriteFile(filepath.Join(root, "accepted.json"), []byte("ok"), 0o600)
	aw.ValidateAndAccept(context.Background(), "accepted.json", AttemptID("a1"))

	// Create an unreferenced file.
	os.WriteFile(filepath.Join(root, "unreferenced.txt"), []byte("junk"), 0o600)

	// Run cleanup.
	if err := aw.RemoveUnreferenced(); err != nil {
		t.Fatalf("RemoveUnreferenced: %v", err)
	}

	// Accepted file should still exist.
	if _, err := os.Stat(filepath.Join(root, "accepted.json")); err != nil {
		t.Fatal("accepted file was removed")
	}
	// Unreferenced file should be removed.
	if _, err := os.Stat(filepath.Join(root, "unreferenced.txt")); err == nil {
		t.Fatal("unreferenced file was not removed")
	}
}

func TestArtifactWorkspace_ListMetadata(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	os.MkdirAll(filepath.Join(root, "sub"), 0o700)

	os.WriteFile(filepath.Join(root, "a.json"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(root, "sub", "b.json"), []byte("b"), 0o600)

	aw.ValidateAndAccept(context.Background(), "a.json", AttemptID("a1"))
	aw.ValidateAndAccept(context.Background(), "sub/b.json", AttemptID("a1"))

	list := aw.ListMetadata()
	if len(list) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(list))
	}
	if aw.TotalSize() != 2 {
		t.Fatalf("expected total size 2, got %d", aw.TotalSize())
	}
}

func TestArtifactWorkspace_Root(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "artifacts")
	aw, _ := NewArtifactWorkspace(root, RunID("r1"))
	if aw.Root() != root {
		t.Fatalf("expected root %s, got %s", root, aw.Root())
	}
}

func TestValidateArtifactRelPath_ValidCases(t *testing.T) {
	valid := []string{
		"file.json",
		"charts/themes.json",
		"a/b/c/d/e/f/g/h",
		"file-with-dashes.txt",
		"file_with_underscores.md",
		"file.with.dots.csv",
	}
	for _, p := range valid {
		if err := validateArtifactRelPath(p); err != nil {
			t.Errorf("expected valid: %s, got error: %v", p, err)
		}
	}
}

func TestValidateArtifactRelPath_InvalidCases(t *testing.T) {
	invalid := []string{
		"",           // empty
		"/abs/path",  // absolute
		"../escape",  // traversal
		"./dot",      // dot segment
		"foo\\bar",   // backslash
		"a//b",       // empty segment
		"a/b/c/d/e/f/g/i/j", // too many segments
		strings.Repeat("a", 513), // too long
	}
	for _, p := range invalid {
		if err := validateArtifactRelPath(p); err == nil {
			t.Errorf("expected invalid: %s, but got nil", p)
		}
	}
}

func TestDetectMediaType(t *testing.T) {
	cases := map[string]string{
		"file.json":  "application/json",
		"file.md":    "text/markdown",
		"file.txt":   "text/plain",
		"file.csv":   "text/csv",
		"file.png":   "image/png",
		"file.pdf":   "application/pdf",
		"file.unknown": "application/octet-stream",
	}
	for path, expected := range cases {
		got := detectMediaType(path)
		if got != expected {
			t.Errorf("detectMediaType(%s): expected %s, got %s", path, expected, got)
		}
	}
}
