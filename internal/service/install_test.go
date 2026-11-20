package service_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/service"
)

func TestInstallWritesPlist(t *testing.T) {
	dir := t.TempDir()

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	if err := service.InstallLaunchdPlist(cfg, dir, false); err != nil {
		t.Fatalf("InstallLaunchdPlist: %v", err)
	}

	dst := filepath.Join(dir, "com.agentpaas.daemon.plist")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("plist should exist at %s: %v", dst, err)
	}
}

func TestInstallCustomPath(t *testing.T) {
	dir := t.TempDir()
	customDir := filepath.Join(dir, "custom", "launchagents")

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/opt/agentpaas/bin/agentpaasd",
		HomeDir:    "/home/testuser/.agentpaas",
	}

	if err := service.InstallLaunchdPlist(cfg, customDir, false); err != nil {
		t.Fatalf("InstallLaunchdPlist: %v", err)
	}

	dst := filepath.Join(customDir, "com.agentpaas.daemon.plist")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("plist should exist at %s: %v", dst, err)
	}

	// Verify content matches expectation.
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	if !contains(string(data), "/opt/agentpaas/bin/agentpaasd") {
		t.Errorf("plist should contain custom daemon path")
	}
}

func TestUninstallRemovesPlist(t *testing.T) {
	dir := t.TempDir()

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	// Install first.
	if err := service.InstallLaunchdPlist(cfg, dir, false); err != nil {
		t.Fatalf("InstallLaunchdPlist: %v", err)
	}

	dst := filepath.Join(dir, "com.agentpaas.daemon.plist")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("plist should exist before uninstall: %v", err)
	}

	// Uninstall.
	if err := service.UninstallLaunchdPlist(dir); err != nil {
		t.Fatalf("UninstallLaunchdPlist: %v", err)
	}

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("plist should be removed after uninstall, stat: %v", err)
	}
}

func TestUninstallIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Uninstall when file doesn't exist should not error.
	if err := service.UninstallLaunchdPlist(dir); err != nil {
		t.Errorf("UninstallLaunchdPlist on non-existent file should not error: %v", err)
	}

	// Uninstall again should still not error.
	if err := service.UninstallLaunchdPlist(dir); err != nil {
		t.Errorf("second UninstallLaunchdPlist should still not error: %v", err)
	}
}

func TestInstallCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nestedDir := filepath.Join(dir, "a", "b", "c")

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	if err := service.InstallLaunchdPlist(cfg, nestedDir, false); err != nil {
		t.Fatalf("InstallLaunchdPlist should create nested directory: %v", err)
	}

	dst := filepath.Join(nestedDir, "com.agentpaas.daemon.plist")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("plist should exist after creating nested directory: %v", err)
	}
}

func TestInstallEmptyPathFails(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	if err := service.InstallLaunchdPlist(cfg, "", false); err == nil {
		t.Errorf("InstallLaunchdPlist with empty path should error")
	}
}

func TestInstallPlistContentsValid(t *testing.T) {
	dir := t.TempDir()

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	if err := service.InstallLaunchdPlist(cfg, dir, false); err != nil {
		t.Fatalf("InstallLaunchdPlist: %v", err)
	}

	dst := filepath.Join(dir, "com.agentpaas.daemon.plist")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}

	// Verify it's valid plist XML.
	content := string(data)
	if !contains(content, "<?xml") {
		t.Error("plist should have XML declaration")
	}
	if !contains(content, "<plist") {
		t.Error("plist should have plist element")
	}
	if !contains(content, "<dict>") {
		t.Error("plist should have dict element")
	}
	if !contains(content, "<true/>") {
		t.Error("plist should have self-closing true elements")
	}
}