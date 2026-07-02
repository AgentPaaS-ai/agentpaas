package redteam

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	docker "github.com/parvezsyed/agentpaas/internal/runtime"
)

func readRedteamFixtureSource(t *testing.T, filename string) string {
	t.Helper()
	_, thisFile, _, ok := goruntime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return string(data)
}

// TestAdversary_B12_T02_WgetAvailability checks if the T02 egress probes
// could false-pass because wget is missing or fails for non-network reasons
// in alpine:latest (busybox wget may behave differently or be absent in minimal images).
func TestAdversary_B12_T02_WgetAvailability(t *testing.T) {
	requireDocker(t)
	skipOnPlatform(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dr, err := docker.NewDockerRuntime()
	if err != nil {
		t.Skipf("DockerRuntime unavailable: %v", err)
	}

	// Create a temp alpine container and check if wget exists and works
	runID := uniqueRunID("adv-t02")
	_, _, _, agentID := createTopology(ctx, &fixtureT{result: &FixtureResult{}}, dr, runID)
	defer cleanupContainers(ctx, dr, agentID)
	defer cleanupNetworks(ctx, dr) // partial, but ok for test

	time.Sleep(1 * time.Second)

	// Check wget presence
	output, err := dockerExec(ctx, string(agentID), "sh", "-c", "which wget || echo 'NO_WGET'; wget --version 2>&1 | head -1 || echo 'WGET_FAIL'")
	if err != nil && !strings.Contains(output, "NO_WGET") {
		t.Logf("wget probe output: %s (err=%v)", output, err)
	}
	if strings.Contains(output, "NO_WGET") {
		t.Errorf("ADVERSARY BREAK T02: alpine container has no wget — T02 'BLOCKED' may be command-not-found, not egress block. False pass possible.")
	}
}

// TestAdversary_B12_T05a_ColimaBridgeIP checks if T05a hardcodes 172.17.0.1
// which is incorrect for Colima on macOS (uses different Docker network, often vmnet or 192.168.x).
// This would make the bridge probe always BLOCKED regardless of actual containment.
func TestAdversary_B12_T05a_ColimaBridgeIP(t *testing.T) {
	requireDocker(t)
	skipOnPlatform(t)

	// On macOS + Colima, inspect docker network to find actual bridge IP
	cmd := exec.Command("docker", "network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}}{{end}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("cannot inspect bridge: %v", err)
	}
	gateway := strings.TrimSpace(string(out))
	t.Logf("Actual Docker bridge gateway: %s", gateway)

	if gateway == "" || gateway == "172.17.0.1" {
		t.Logf("Gateway is 172.17.0.1 or empty — T05a may be correct by luck")
	} else {
		t.Errorf("ADVERSARY BREAK T05a: Colima/Docker bridge gateway is %s, not 172.17.0.1. T05a probe to hardcoded IP always fails (false BLOCKED). False pass.", gateway)
	}
}

// TestAdversary_B12_T05b_NoDaemon verifies the strengthened T05b fixture
// enforces a memory limit and asserts containment under pressure.
func TestAdversary_B12_T05b_NoDaemon(t *testing.T) {
	src := readRedteamFixtureSource(t, "fixture_t05_host_resource_test.go")

	required := []string{
		"MemoryLimitBytes",
		"memoryContained",
		"ContainerStatusStopped",
		"Docker runtime client still functional",
	}
	for _, needle := range required {
		if !strings.Contains(src, needle) {
			t.Errorf("T05b fixture missing strengthened containment check %q", needle)
		}
	}
	if strings.Contains(src, "daemon survives") {
		t.Error("T05b fixture still overstates daemon survival claim")
	}
}

// TestAdversary_B12_T04_UpstreamInjectionReal verifies T04 documents the
// RequestCredential injection path and scans for encoded/truncated sentinel leaks.
func TestAdversary_B12_T04_UpstreamInjectionReal(t *testing.T) {
	src := readRedteamFixtureSource(t, "fixture_t04_secret_test.go")

	required := []string{
		"broker.RequestCredential",
		"Gateway.Do calls broker.RequestCredential",
		"sentinelLeakScan",
		"encoding/base64",
	}
	for _, needle := range required {
		if !strings.Contains(src, needle) {
			t.Errorf("T04 fixture missing strengthened injection/leak check %q", needle)
		}
	}
}

// TestAdversary_B12_T03_DenialBeforeInjection verifies T03 proves denial
// occurred before credential fetch/injection.
func TestAdversary_B12_T03_DenialBeforeInjection(t *testing.T) {
	src := readRedteamFixtureSource(t, "fixture_t03_credential_test.go")

	required := []string{
		"recordingSecretStore",
		"store.getCalls",
		"auditRecordsDenyCredential",
		"CREDENTIAL LEAKED TO EVIL",
	}
	for _, needle := range required {
		if !strings.Contains(src, needle) {
			t.Errorf("T03 fixture missing denial-before-injection proof %q", needle)
		}
	}
}

// TestAdversary_B12_T06_RedactionVsTruncation verifies T06 distinguishes
// real redaction from mere truncation using length-based assertions.
func TestAdversary_B12_T06_RedactionVsTruncation(t *testing.T) {
	src := readRedteamFixtureSource(t, "fixture_t06_operator_test.go")

	required := []string{
		"rootCause2",
		"[REDACTED]",
		"len(injectedSecret)",
		"len(rootCause2)",
		"truncation rather than redaction",
	}
	for _, needle := range required {
		if !strings.Contains(src, needle) {
			t.Errorf("T06 fixture missing redaction-vs-truncation proof %q", needle)
		}
	}
}