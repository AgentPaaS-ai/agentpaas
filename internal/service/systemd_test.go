package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/service"
)

// goldenSystemdPath returns the path to a systemd golden file by name.
func goldenSystemdPath(name string) string {
	return filepath.Join("testdata", name)
}

func TestSystemdUnitMatchesGolden(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	want, err := os.ReadFile(goldenSystemdPath("systemd_golden.service"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("generated systemd unit does not match golden file")
		t.Logf("got (%d bytes):\n%s", len(got), string(got))
		t.Logf("want (%d bytes):\n%s", len(want), string(want))
	}
}

func TestSystemdUnitMatchesGoldenNoEnv(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/opt/agentpaas/bin/agentpaasd",
		HomeDir:    "/home/testuser/.agentpaas",
	}
	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	want, err := os.ReadFile(goldenSystemdPath("systemd_golden_noenv.service"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("generated systemd unit (no env) does not match golden file")
		t.Logf("got (%d bytes):\n%s", len(got), string(got))
		t.Logf("want (%d bytes):\n%s", len(want), string(want))
	}
}

func TestSystemdUnitDeterministic(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	first, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("systemd unit generation is not deterministic")
		t.Logf("first (%d bytes):\n%s", len(first), string(first))
		t.Logf("second (%d bytes):\n%s", len(second), string(second))
	}
}

func TestSystemdUnitSections(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	gotStr := string(got)

	if !contains(gotStr, "[Unit]") {
		t.Errorf("systemd unit should have [Unit] section")
	}
	if !contains(gotStr, "Description=AgentPaaS Control Daemon") {
		t.Errorf("systemd unit should have Description")
	}
	if !contains(gotStr, "[Service]") {
		t.Errorf("systemd unit should have [Service] section")
	}
	if !contains(gotStr, "ExecStart=/usr/local/bin/agentpaasd --home /Users/testuser/.agentpaas") {
		t.Errorf("systemd unit should have ExecStart with daemon path and home")
	}
	if !contains(gotStr, "Restart=on-failure") {
		t.Errorf("systemd unit should have Restart=on-failure")
	}
	if !contains(gotStr, "Environment=AGENTPAAS_HOME=/Users/testuser/.agentpaas") {
		t.Errorf("systemd unit should have Environment variable")
	}
	if !contains(gotStr, "[Install]") {
		t.Errorf("systemd unit should have [Install] section")
	}
	if !contains(gotStr, "WantedBy=default.target") {
		t.Errorf("systemd unit should have WantedBy=default.target")
	}
}

func TestSystemdUnitNoEnvHome(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}
	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	gotStr := string(got)
	if contains(gotStr, "Environment=") {
		t.Errorf("systemd unit should not contain Environment when EnvHome is empty, got:\n%s", gotStr)
	}
}