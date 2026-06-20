package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// agentgatewayBinaryURL and checksum for v1.3.0 (darwin-arm64).
const (
	agentgatewayVersion   = "v1.3.0"
	agentgatewayDarwinSHA = "664e8ee98658ffe07fbe35fa52a3818ca1aa6f27361c189694c468c74ef10eea"
)

// TestBinarySmoke compiles a sample policy and validates it against the
// real agentgateway binary. Downloads the binary on demand if not cached.
// This test is opt-in via AGENTPAAS_SMOKE_TESTS=1 because:
//   - It downloads a ~69 MB binary
//   - It requires internet access
//   - It's macOS-arm64 specific
func TestBinarySmoke(t *testing.T) {
	if os.Getenv("AGENTPAAS_SMOKE_TESTS") == "" {
		t.Skip("Skipping binary smoke test; set AGENTPAAS_SMOKE_TESTS=1 to run")
	}

	binaryPath := downloadAgentgateway(t)
	t.Logf("Using agentgateway binary at %s", binaryPath)

	// Compile a sample policy to a temp config file.
	p := samplePolicy()
	configYAML, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig failed: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, configYAML, 0644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	// Validate the config with the real agentgateway binary.
	cmd := exec.Command(binaryPath, "-f", configPath, "--validate-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agentgateway --validate-only failed: %v\nOutput:\n%s", err, string(output))
	}
	t.Logf("agentgateway --validate-only output:\n%s", string(output))
}

// TestBinarySmoke_EmptyPolicy validates that an empty (deny-all) config
// is a valid agentgateway config.
func TestBinarySmoke_EmptyPolicy(t *testing.T) {
	if os.Getenv("AGENTPAAS_SMOKE_TESTS") == "" {
		t.Skip("Skipping binary smoke test; set AGENTPAAS_SMOKE_TESTS=1 to run")
	}

	binaryPath := downloadAgentgateway(t)

	p := &Policy{}
	configYAML, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig failed: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, configYAML, 0644); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cmd := exec.Command(binaryPath, "-f", configPath, "--validate-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agentgateway --validate-only failed: %v\nOutput:\n%s", err, string(output))
	}
	t.Logf("agentgateway --validate-only (empty policy) output:\n%s", string(output))
}

// downloadAgentgateway downloads the agentgateway binary and caches it.
// Returns the path to the cached binary.
func downloadAgentgateway(t *testing.T) string {
	t.Helper()

	// Only support darwin-arm64 for now.
	if runtime.GOARCH != "arm64" || runtime.GOOS != "darwin" {
		t.Skipf("agentgateway binary smoke test requires darwin/arm64, got %s/%s",
			runtime.GOOS, runtime.GOARCH)
	}

	cacheDir := filepath.Join(os.TempDir(), "agentpaas-agentgateway-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("creating cache dir: %v", err)
	}

	binaryPath := filepath.Join(cacheDir, "agentgateway-darwin-arm64")

	// Check if binary already exists and is valid.
	if _, err := os.Stat(binaryPath); err == nil {
		// Quick check: binary runs and reports version.
		if checkVersion(binaryPath) {
			return binaryPath
		}
		// Binary is stale; remove and re-download.
		_ = os.Remove(binaryPath)
	}

	t.Logf("Downloading agentgateway %s (darwin-arm64)...", agentgatewayVersion)
	url := fmt.Sprintf("https://github.com/agentgateway/agentgateway/releases/download/%s/agentgateway-darwin-arm64",
		agentgatewayVersion)

	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("downloading agentgateway: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download returned status %d", resp.StatusCode)
	}

	// Read the body and compute sha256.
	data := make([]byte, 0, 70*1024*1024) // ~69 MB
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			data = append(data, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	// Verify checksum.
	hash := sha256.Sum256(data)
	gotHash := hex.EncodeToString(hash[:])
	if gotHash != agentgatewayDarwinSHA {
		t.Fatalf("checksum mismatch: got %s, expected %s", gotHash, agentgatewayDarwinSHA)
	}
	t.Log("Checksum verified")

	// Write binary.
	if err := os.WriteFile(binaryPath, data, 0755); err != nil {
		t.Fatalf("writing binary: %v", err)
	}

	return binaryPath
}

// checkVersion runs `agentgateway --version` and returns true if it succeeds.
func checkVersion(path string) bool {
	cmd := exec.Command(path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), agentgatewayVersion)
}
