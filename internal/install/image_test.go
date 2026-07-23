package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeImageBuilder struct {
	digest string
	err    error
	calls  int
}

func (f *fakeImageBuilder) Build(ctx context.Context, sourceDir, agentName string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if f.digest != "" {
		return f.digest, nil
	}
	return "sha256:deadbeef000000000000000000000000000000000000000000000000000001", nil
}

type fakeImageLoader struct {
	digest string
	err    error
}

func (f *fakeImageLoader) Load(ctx context.Context, ociLayoutDir, expectedDigest string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	got := f.digest
	if got == "" {
		got = expectedDigest
	}
	want := normalizeImageDigest(expectedDigest)
	got = normalizeImageDigest(got)
	if got != want {
		return "", fmt.Errorf("%w: loaded %s expected %s", ErrImageDigestMismatch, got, want)
	}
	return got, nil
}

func TestCheckPrebuiltPlatformMismatch(t *testing.T) {
	other := "linux/amd64"
	if runtime.GOARCH == "amd64" {
		other = "linux/arm64"
	}
	err := CheckPrebuiltPlatform(other)
	if !errors.Is(err, ErrPrebuiltPlatformMismatch) {
		t.Fatalf("err = %v want platform mismatch", err)
	}
	if !strings.Contains(err.Error(), "reinstall without --prefer-image") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestFakeImageLoaderDigestMismatch(t *testing.T) {
	loader := &fakeImageLoader{digest: "sha256:wrong0000000000000000000000000000000000000000000000000000000001"}
	_, err := loader.Load(context.Background(), t.TempDir(), "sha256:expected0000000000000000000000000000000000000000000000000000000001")
	if !errors.Is(err, ErrImageDigestMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestSourceHasUVLock(t *testing.T) {
	dir := t.TempDir()
	if SourceHasUVLock(dir) {
		t.Fatal("want false without uv.lock")
	}
	p := filepath.Join(dir, "uv.lock")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SourceHasUVLock(dir) {
		t.Fatal("want true with uv.lock")
	}
}

func TestSourceHasRequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	if SourceHasRequirementsTxt(dir) {
		t.Fatal("want false without requirements.txt")
	}
	p := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(p, []byte("requests>=2.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SourceHasRequirementsTxt(dir) {
		t.Fatal("want true with requirements.txt")
	}
}