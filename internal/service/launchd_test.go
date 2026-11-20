package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/service"
)

// goldenLaunchdPath returns the path to a launchd golden file by name.
func goldenLaunchdPath(name string) string {
	return filepath.Join("testdata", name)
}

func TestLaunchdPlistMatchesGolden(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	want, err := os.ReadFile(goldenLaunchdPath("launchd_golden.plist"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("generated plist does not match golden file")
		t.Logf("got (%d bytes):\n%s", len(got), string(got))
		t.Logf("want (%d bytes):\n%s", len(want), string(want))
	}
}

func TestLaunchdPlistMatchesGoldenNoEnv(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/opt/agentpaas/bin/agentpaasd",
		HomeDir:    "/home/testuser/.agentpaas",
	}
	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	want, err := os.ReadFile(goldenLaunchdPath("launchd_golden_noenv.plist"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("generated plist (no env) does not match golden file")
		t.Logf("got (%d bytes):\n%s", len(got), string(got))
		t.Logf("want (%d bytes):\n%s", len(want), string(want))
	}
}

func TestLaunchdPlistDeterministic(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	first, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("plist generation is not deterministic")
		t.Logf("first (%d bytes):\n%s", len(first), string(first))
		t.Logf("second (%d bytes):\n%s", len(second), string(second))
	}
}

func TestLaunchdPlistHasCorrectLabel(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)
	if !contains(gotStr, "com.agentpaas.daemon") {
		t.Errorf("plist should contain label com.agentpaas.daemon")
	}

	if !contains(gotStr, "/usr/local/bin/agentpaasd") {
		t.Errorf("plist should contain daemon path")
	}

	if !contains(gotStr, "--home") {
		t.Errorf("plist should contain --home flag")
	}

	if !contains(gotStr, "/Users/testuser/.agentpaas") {
		t.Errorf("plist should contain home directory")
	}

	if !contains(gotStr, "daemon.out.log") {
		t.Errorf("plist should contain stdout log path")
	}

	if !contains(gotStr, "daemon.err.log") {
		t.Errorf("plist should contain stderr log path")
	}
}

func TestLaunchdPlistDefaultLabel(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	if !contains(string(got), "com.agentpaas.daemon") {
		t.Errorf("default label should be com.agentpaas.daemon, got:\n%s", string(got))
	}
}

func TestLaunchdPlistNoEnvHome(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)
	if contains(gotStr, "EnvironmentVariables") {
		t.Errorf("plist should not contain EnvironmentVariables when EnvHome is empty")
	}
}

// contains reports whether substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
