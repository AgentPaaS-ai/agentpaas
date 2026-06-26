//go:build integration

package pack

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSignImage_RealCosign(t *testing.T) {
	if os.Getenv("AGENTPAAS_PACK_REAL_TOOLS") != "1" {
		t.Skip("set AGENTPAAS_PACK_REAL_TOOLS=1 to run real cosign integration test")
	}
	if _, err := exec.LookPath("cosign"); err != nil {
		t.Skip("cosign not available")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	registryURL, err := EnsureLocalRegistry(ctx)
	if err != nil {
		t.Skipf("local registry unavailable: %v", err)
	}
	if err := waitForRegistryReady(ctx, registryURL); err != nil {
		t.Fatalf("wait for registry: %v", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pkcs8PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8DER})

	cosignKeyPath, cleanupKey, err := writeCosignSigningKey(pkcs8PEM)
	if err != nil {
		t.Fatalf("writeCosignSigningKey: %v", err)
	}
	defer cleanupKey()

	if _, err := os.Stat(cosignKeyPath); err != nil {
		t.Fatalf("cosign signing key missing: %v", err)
	}

	pubKeyOut, err := runCosign(ctx, "public-key", "--key", cosignKeyPath)
	if err != nil {
		t.Fatalf("cosign public-key --key %s: %v\n%s", cosignKeyPath, err, pubKeyOut)
	}
	if len(strings.TrimSpace(pubKeyOut)) == 0 {
		t.Fatal("cosign public-key returned empty output")
	}

	pubKeyPath := filepath.Join(t.TempDir(), "verify.pub")
	if err := os.WriteFile(pubKeyPath, []byte(pubKeyOut), 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	sourceTag := "agentpaas-sign-real-test:local"
	imageRef, err := buildAndPushTinyImage(ctx, t, sourceTag, registryURL, "test", "latest")
	if err != nil {
		t.Fatalf("build and push image: %v", err)
	}

	d3ImageRef, err := buildAndPushTinyImage(ctx, t, sourceTag+"-d3", registryURL, "test", "d3")
	if err != nil {
		t.Fatalf("build and push D3 probe image: %v", err)
	}
	signOutput, err := captureCosignSignOutput(ctx, d3ImageRef, cosignKeyPath)
	if err != nil {
		t.Fatalf("capture cosign sign output: %v\n%s", err, signOutput)
	}
	lower := strings.ToLower(signOutput)
	if strings.Contains(lower, "rekor") {
		t.Fatalf("cosign sign output mentions rekor (D3 tlog suppression failed):\n%s", signOutput)
	}
	if strings.Contains(lower, "tlog") {
		t.Fatalf("cosign sign output mentions tlog (D3 tlog suppression failed):\n%s", signOutput)
	}

	gotReferrer, err := SignImage(ctx, imageRef, cosignKeyPath)
	if err != nil {
		t.Fatalf("SignImage: %v", err)
	}
	wantReferrer := "cosign://" + imageRef
	if gotReferrer != wantReferrer {
		t.Fatalf("SignImage referrer = %q, want %q", gotReferrer, wantReferrer)
	}

	// noTlogSigningConfig suppresses Rekor upload, so verify must skip tlog checks.
	verifyOut, err := runCosign(ctx, "verify",
		"--insecure-ignore-tlog",
		"--key", pubKeyPath,
		"--allow-insecure-registry", imageRef)
	if err != nil {
		t.Fatalf("cosign verify: %v\n%s", err, verifyOut)
	}
}

func waitForRegistryReady(ctx context.Context, registryURL string) error {
	url := fmt.Sprintf("http://%s/v2/", registryURL)
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create registry probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("registry %s not ready before deadline", url)
}

func buildAndPushTinyImage(ctx context.Context, t *testing.T, sourceTag, registryURL, agentName, version string) (string, error) {
	t.Helper()

	buildDir := t.TempDir()
	dummyPath := filepath.Join(buildDir, "dummy")
	if err := os.WriteFile(dummyPath, []byte("agentpaas-sign-real-test\n"), 0o644); err != nil {
		return "", fmt.Errorf("write dummy file: %w", err)
	}
	dockerfile := strings.Join([]string{
		"FROM scratch",
		"ADD dummy /dummy",
	}, "\n")
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("write Dockerfile: %w", err)
	}

	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", sourceTag, buildDir)
	buildCmd.Env = append(os.Environ(), dockerHostEnv()...)
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker build: %w: %s", err, strings.TrimSpace(string(out)))
	}

	targetTag := fmt.Sprintf("%s/agentpaas/%s:%s", registryURL, agentName, version)
	tagCmd := exec.CommandContext(ctx, "docker", "tag", sourceTag, targetTag)
	tagCmd.Env = append(os.Environ(), dockerHostEnv()...)
	if out, err := tagCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker tag: %w: %s", err, strings.TrimSpace(string(out)))
	}

	pushCmd := exec.CommandContext(ctx, "docker", "push", targetTag)
	pushCmd.Env = append(os.Environ(), dockerHostEnv()...)
	if out, err := pushCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker push: %w: %s", err, strings.TrimSpace(string(out)))
	}

	inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", targetTag)
	inspectCmd.Env = append(os.Environ(), dockerHostEnv()...)
	digestOut, err := inspectCmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w", err)
	}
	digest := strings.TrimSpace(string(digestOut))
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	return fmt.Sprintf("%s/agentpaas/%s@%s", registryURL, agentName, digest), nil
}

func captureCosignSignOutput(ctx context.Context, imageRef, keyPath string) (string, error) {
	signingConfigPath, cleanupConfig, err := ensureNoTlogSigningConfig()
	if err != nil {
		return "", err
	}
	defer cleanupConfig()

	cmdCtx, cancel := context.WithTimeout(ctx, externalSignatureTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "cosign", "sign",
		"--key", keyPath,
		"--signing-config", signingConfigPath,
		"--allow-insecure-registry",
		"--yes", imageRef)
	cmd.Env = append(os.Environ(), dockerHostEnv()...)
	cmd.Env = append(cmd.Env, "COSIGN_PASSWORD=")
	output, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return string(output), cmdCtx.Err()
	}
	if err != nil {
		return string(output), fmt.Errorf("cosign sign: %w", err)
	}
	return string(output), nil
}

func runCosign(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "cosign", args...)
	cmd.Env = append(os.Environ(), dockerHostEnv()...)
	cmd.Env = append(cmd.Env, "COSIGN_PASSWORD=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("cosign %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}