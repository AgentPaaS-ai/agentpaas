package export

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func materializeLockedImage(ctx context.Context, home, agentName string, lock *pack.AgentLock) (string, error) {
	if lock == nil || lock.ImageDigest == "" {
		return "", fmt.Errorf("locked image digest missing")
	}
	deployed, err := pack.LoadDeployedAgent(home, agentName)
	if err != nil {
		return "", err
	}
	if deployed.ImageDigest != lock.ImageDigest {
		return "", fmt.Errorf("deployed image digest %s does not match lock %s", deployed.ImageDigest, lock.ImageDigest)
	}

	tmp, err := os.MkdirTemp("", "agentpaas-export-image-*")
	if err != nil {
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmp)
		}
	}()

	ref := pack.LocalImageRef(agentName, lock.ImageDigest)
	if err := verifyLocalImageDigest(ctx, ref, lock.ImageDigest); err != nil {
		return "", err
	}
	if err := copyImageToOCILayout(ctx, ref, tmp); err != nil {
		return "", err
	}
	cleanup = false
	return tmp, nil
}

func verifyLocalImageDigest(ctx context.Context, ref, wantDigest string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", ref, "--format", "{{.Id}}").Output()
	if err != nil {
		return fmt.Errorf("local image %q not found; run agentpaas pack first: %w", ref, err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, wantDigest) && got != "sha256:"+wantDigest {
		return fmt.Errorf("local image digest %q does not match lock %q", got, wantDigest)
	}
	return nil
}

func copyImageToOCILayout(ctx context.Context, ref, destDir string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ociDest := "oci:" + filepath.Clean(destDir)
	// Pack pushes the locked image to AgentPaaS's local HTTP registry. Use the
	// registry transport instead of skopeo's docker-daemon transport, which
	// hard-codes /var/run/docker.sock and cannot follow Colima contexts.
	cmd := exec.CommandContext(ctx, "skopeo", "copy", "--src-tls-verify=false", "docker://"+ref, ociDest)
	if out, err := cmd.CombinedOutput(); err != nil {
		if _, lookErr := exec.LookPath("skopeo"); lookErr != nil {
			return fmt.Errorf("skopeo not found in PATH (required for --with-image): %w", lookErr)
		}
		return fmt.Errorf("skopeo copy failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	indexPath := filepath.Join(destDir, "oci-layout")
	if _, err := os.Stat(indexPath); err != nil {
		if _, err2 := os.Stat(filepath.Join(destDir, "index.json")); err2 != nil {
			return fmt.Errorf("oci layout missing after skopeo copy")
		}
	}
	return nil
}
