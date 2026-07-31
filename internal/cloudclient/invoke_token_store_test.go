package cloudclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileInvokeTokenStore_SetGetRotateDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invoke-tokens.json")
	store, err := NewFileInvokeTokenStore(path)
	if err != nil {
		t.Fatalf("NewFileInvokeTokenStore: %v", err)
	}
	ctx := context.Background()

	if err := store.Set(ctx, "dep-a", "inv_a_v1"); err != nil {
		t.Fatalf("Set dep-a: %v", err)
	}
	if err := store.Set(ctx, "dep-b", "inv_b_v1"); err != nil {
		t.Fatalf("Set dep-b: %v", err)
	}
	if err := store.Set(ctx, "dep-a", "inv_a_v2"); err != nil {
		t.Fatalf("rotate dep-a: %v", err)
	}

	got, err := store.Get(ctx, "dep-a")
	if err != nil {
		t.Fatalf("Get dep-a: %v", err)
	}
	if got != "inv_a_v2" {
		t.Errorf("Get dep-a = %q, want rotated token", got)
	}
	got, err = store.Get(ctx, "dep-b")
	if err != nil {
		t.Fatalf("Get dep-b: %v", err)
	}
	if got != "inv_b_v1" {
		t.Errorf("Get dep-b = %q, want inv_b_v1", got)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat token file: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("token file must not be a symlink")
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("token file permissions = %o, want 600", got)
	}

	if err := store.Delete(ctx, "dep-a"); err != nil {
		t.Fatalf("Delete dep-a: %v", err)
	}
	if _, err := store.Get(ctx, "dep-a"); !errors.Is(err, ErrInvokeTokenNotFound) {
		t.Fatalf("Get deleted dep-a error = %v, want ErrInvokeTokenNotFound", err)
	}
	if err := store.Delete(ctx, "dep-a"); err != nil {
		t.Fatalf("Delete dep-a idempotent: %v", err)
	}
}

func TestFileInvokeTokenStore_RejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside.json")
	link := filepath.Join(dir, "invoke-tokens.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	store, err := NewFileInvokeTokenStore(link)
	if err != nil {
		t.Fatalf("NewFileInvokeTokenStore: %v", err)
	}
	if err := store.Set(context.Background(), "dep-a", "inv_a"); err == nil {
		t.Fatal("Set should reject a symlink target")
	}
}

func TestFileInvokeTokenStore_RejectsSymlinkParent(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	linkDir := filepath.Join(root, "tokens")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	store, err := NewFileInvokeTokenStore(filepath.Join(linkDir, "invoke-tokens.json"))
	if err != nil {
		t.Fatalf("NewFileInvokeTokenStore: %v", err)
	}
	if err := store.Set(context.Background(), "dep-a", "inv_a"); err == nil {
		t.Fatal("Set should reject a symlink parent")
	}
}

func TestFakeInvokeTokenStore_SetGetDelete(t *testing.T) {
	store := NewFakeInvokeTokenStore()
	ctx := context.Background()

	if _, err := store.Get(ctx, "dep-a"); !errors.Is(err, ErrInvokeTokenNotFound) {
		t.Fatalf("Get empty store error = %v, want ErrInvokeTokenNotFound", err)
	}
	if err := store.Set(ctx, "dep-a", "inv_a"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx, "dep-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "inv_a" {
		t.Errorf("Get = %q, want inv_a", got)
	}
	if err := store.Delete(ctx, "dep-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
