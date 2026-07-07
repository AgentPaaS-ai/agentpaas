package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ErrImageDigestMismatch is returned when a loaded/built image digest does not match expectation.
var ErrImageDigestMismatch = errors.New("image digest mismatch")

// ErrPrebuiltPlatformMismatch is returned when bundle image platform does not match the host.
var ErrPrebuiltPlatformMismatch = errors.New("prebuilt image platform mismatch")

// ImageBuilder builds an OCI image from an extracted source tree.
type ImageBuilder interface {
	Build(ctx context.Context, sourceDir, agentName string) (digest string, err error)
}

// ImageLoader loads an OCI layout directory into the local Docker engine.
type ImageLoader interface {
	Load(ctx context.Context, ociLayoutDir, expectedDigest string) (digest string, err error)
}

// PackImageBuilder wraps pack.BuildImage.
type PackImageBuilder struct {
	HarnessPath string
	SDKDir      string
	ImageTag    string
}

// Build implements ImageBuilder using pack.BuildImage.
func (b *PackImageBuilder) Build(ctx context.Context, sourceDir, agentName string) (string, error) {
	cfg := pack.BuildConfig{
		ProjectDir: sourceDir,
		ImageTag:   b.ImageTag,
		HarnessPath: b.HarnessPath,
		SDKDir:      b.SDKDir,
	}
	if cfg.ImageTag == "" {
		cfg.ImageTag = "agentpaas/" + agentName + ":install"
	}
	res, err := pack.BuildImage(ctx, cfg)
	if err != nil {
		return "", err
	}
	return normalizeImageDigest(res.ImageDigest), nil
}

// SkopeoImageLoader loads OCI layouts via skopeo copy into docker-daemon.
type SkopeoImageLoader struct{}

// Load implements ImageLoader.
func (SkopeoImageLoader) Load(ctx context.Context, ociLayoutDir, expectedDigest string) (string, error) {
	if err := pack.ValidateOCILayout(ociLayoutDir); err != nil {
		return "", err
	}
	want := normalizeImageDigest(expectedDigest)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ref := "agentpaas-prebuilt-load:" + strings.TrimPrefix(want, "sha256:")
	ociSrc := "oci:" + filepath.Clean(ociLayoutDir)
	cmd := exec.CommandContext(ctx, "skopeo", "copy", ociSrc, "docker-daemon:"+ref)
	if out, err := cmd.CombinedOutput(); err != nil {
		if _, lookErr := exec.LookPath("skopeo"); lookErr != nil {
			return "", fmt.Errorf("skopeo not found in PATH (required for --prefer-image): %w", lookErr)
		}
		return "", fmt.Errorf("skopeo copy failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	got, err := dockerImageID(ctx, ref)
	if err != nil {
		return "", err
	}
	got = normalizeImageDigest(got)
	if got != want && !digestMatches(got, want) {
		return "", fmt.Errorf("%w: loaded %s expected %s", ErrImageDigestMismatch, got, want)
	}
	return got, nil
}

func dockerImageID(ctx context.Context, ref string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", ref, "--format", "{{.Id}}").Output()
	if err != nil {
		return "", fmt.Errorf("docker image inspect %q: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func normalizeImageDigest(d string) string {
	d = strings.TrimSpace(d)
	d = strings.TrimPrefix(d, "sha256:")
	if d == "" {
		return ""
	}
	return "sha256:" + d
}

func digestMatches(got, want string) bool {
	got = normalizeImageDigest(got)
	want = normalizeImageDigest(want)
	return got == want || strings.TrimPrefix(got, "sha256:") == strings.TrimPrefix(want, "sha256:")
}

// CheckPrebuiltPlatform refuses amd64 bundles on arm64 hosts (and vice versa) per spec.
func CheckPrebuiltPlatform(bundlePlatform string) error {
	bp := strings.TrimSpace(strings.ToLower(bundlePlatform))
	if bp == "" {
		return nil
	}
	host := runtime.GOOS + "/" + runtime.GOARCH
	if bp == host {
		return nil
	}
	// Normalize common docker platform strings.
	if strings.Contains(bp, "arm64") && runtime.GOARCH == "arm64" {
		return nil
	}
	if strings.Contains(bp, "amd64") && runtime.GOARCH == "amd64" {
		return nil
	}
	return fmt.Errorf("%w: bundle platform %q on host %q — reinstall without --prefer-image to rebuild", ErrPrebuiltPlatformMismatch, bundlePlatform, host)
}

// SourceHasUVLock reports whether uv.lock exists under sourceDir.
func SourceHasUVLock(sourceDir string) bool {
	p := filepath.Join(sourceDir, "uv.lock")
	info, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}