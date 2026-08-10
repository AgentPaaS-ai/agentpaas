package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareInputFilePath_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := []byte("m138-local-input")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	prep, err := prepareInputFilePath(path)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	if prep.SHA256 != want {
		t.Fatalf("sha = %s want %s", prep.SHA256, want)
	}
	if prep.Size != int64(len(data)) {
		t.Fatalf("size = %d", prep.Size)
	}
	if !strings.HasSuffix(prep.Bind, ":/agentpaas/input:ro") {
		t.Fatalf("bind = %q, want :ro mount", prep.Bind)
	}
	if !strings.HasPrefix(prep.Bind, path+":") {
		t.Fatalf("bind host = %q", prep.Bind)
	}
}

func TestPrepareInputFilePath_RejectRelative(t *testing.T) {
	if _, err := prepareInputFilePath("relative.bin"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPrepareInputFilePath_RejectDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := prepareInputFilePath(dir); err == nil {
		t.Fatal("expected error for directory")
	}
}

func TestPrepareInputFilePath_RejectMissing(t *testing.T) {
	if _, err := prepareInputFilePath(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error")
	}
}
