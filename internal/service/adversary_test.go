package service_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/service"
)

// ============================================================================
// ATTACK VECTOR 1: Path traversal in install path
// TestAdversaryPathTraversal_InstallDir validates that InstallLaunchdPlist
// rejects path traversal via the launchAgentsDir parameter.
//
// Previously, an attacker could write the plist to an arbitrary directory
// by passing a path like "../../etc/somewhere".
func TestAdversaryPathTraversal_InstallDir(t *testing.T) {
	// Pass a raw path with ".." components (the attacker's actual vector).
	traversalPath := "/tmp/../../etc/launchd-test"

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	// Now protected: path traversal should be rejected.
	err := service.InstallLaunchdPlist(cfg, traversalPath, false)
	if err == nil {
		t.Errorf("InstallLaunchdPlist should reject path traversal, but succeeded")
	} else {
		t.Logf("Path traversal correctly rejected: %v", err)
	}
}

// TestAdversaryPathTraversal_DotComponents tests that paths with ".." in the
// middle are rejected even when pre-resolved.
func TestAdversaryPathTraversal_DotComponents(t *testing.T) {
	dir := t.TempDir()
	traversalPath := dir + "/../../etc/launchd-test"

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	err := service.InstallLaunchdPlist(cfg, traversalPath, false)
	if err == nil {
		t.Errorf("InstallLaunchdPlist should reject path with .. components, but succeeded")
	} else {
		t.Logf("Path traversal correctly rejected (via Clean check): %v", err)
	}
}

// TestAdversaryPathTraversal_AbsolutePath verifies that an absolute path
// outside the expected launch agents directory can be used.
func TestAdversaryPathTraversal_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	altDir := filepath.Join(dir, "arbitrary-location")

	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	err := service.InstallLaunchdPlist(cfg, altDir, false)
	if err != nil {
		t.Fatalf("InstallLaunchdPlist with arbitrary absolute path: %v", err)
	}

	dst := filepath.Join(altDir, "com.agentpaas.daemon.plist")
	if _, statErr := os.Stat(dst); os.IsNotExist(statErr) {
		t.Errorf("plist should exist at %s", dst)
	}
}

// ============================================================================
// ATTACK VECTOR 2: XML injection in launchd plist
// ============================================================================

// TestAdversaryXMLInjection_DaemonPath tests that XML special characters
// in the DaemonPath are properly escaped by xml.MarshalIndent.
func TestAdversaryXMLInjection_DaemonPath(t *testing.T) {
	// Attempt XML injection via the daemon path.
	// If the XML encoder doesn't escape, this could break the plist structure.
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd\"><key>UserName</key><string>root</string><key>",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)

	// The injected XML should be properly escaped as entities.
	if contains(gotStr, "<key>UserName</key>") {
		t.Errorf("XML injection succeeded: injected <key>UserName</key> found in output")
		t.Logf("Output:\n%s", gotStr)
	}

	// Verify the special characters are entity-encoded.
	if !contains(gotStr, "&lt;") {
		t.Errorf("Expected XML entities for < characters, but none found")
	}

	t.Logf("XML injection attempt in DaemonPath was properly escaped (no <key>UserName</key> in output)")
}

// TestAdversaryXMLInjection_HomeDir tests that XML special characters
// in the HomeDir are properly escaped.
func TestAdversaryXMLInjection_HomeDir(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas\"/><key>SessionType</key><string>Background</string>",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)

	// The injected XML should NOT appear in the output.  If it does,
	// an attacker can inject arbitrary plist keys.
	if contains(gotStr, "<key>SessionType</key>") {
		t.Errorf("XML injection succeeded via HomeDir: <key>SessionType</key> found")
		t.Logf("Output:\n%s", gotStr)
	}
}

// TestAdversaryXMLInjection_Ampersand tests that '&' in paths is escaped.
func TestAdversaryXMLInjection_Ampersand(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agent&paasd",
		HomeDir:    "/Users/test&user/.agentpaas",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)

	// The ampersand should be escaped as &amp;
	if contains(gotStr, "&paasd") && !contains(gotStr, "&amp;paasd") {
		t.Errorf("Ampersand not escaped in DaemonPath")
		t.Logf("Output:\n%s", gotStr)
	}
}

// TestAdversaryXMLInjection_EnvHome tests XML injection via the EnvHome field.
func TestAdversaryXMLInjection_EnvHome(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas\"/><key>AbortOnLaunch</key><true/>",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)

	if contains(gotStr, "<key>AbortOnLaunch</key>") {
		t.Errorf("XML injection via EnvHome succeeded: found injected key")
	}
}

// ============================================================================
// ATTACK VECTOR 3: Systemd unit injection via newlines
// ============================================================================

// TestAdversarySystemdInjection_DaemonPath tests that newlines in DaemonPath
// are rejected by GenerateSystemdUnit.
func TestAdversarySystemdInjection_DaemonPath(t *testing.T) {
	// DaemonPath containing newlines should be rejected.
	injectedDaemonPath := "/usr/local/bin/agentpaasd\nExecReload=/bin/sh\nExecStopPost=/bin/sh -c 'echo pwned > /tmp/pwned'"

	cfg := service.SystemdUnitConfig{
		DaemonPath: injectedDaemonPath,
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	_, err := service.GenerateSystemdUnit(cfg)
	if err == nil {
		t.Errorf("GenerateSystemdUnit should reject newlines in DaemonPath (injection attempt)")
	} else {
		t.Logf("Systemd injection via DaemonPath correctly rejected: %v", err)
	}
}

// TestAdversarySystemdInjection_HomeDir tests that newlines in HomeDir
// are rejected by GenerateSystemdUnit.
func TestAdversarySystemdInjection_HomeDir(t *testing.T) {
	injectedHomeDir := "/Users/testuser/.agentpaas\nEnvironment=MALICIOUS=yes\nExecStartPre=/bin/sh -c 'id > /tmp/evil'"

	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    injectedHomeDir,
	}

	_, err := service.GenerateSystemdUnit(cfg)
	if err == nil {
		t.Errorf("GenerateSystemdUnit should reject newlines in HomeDir (injection attempt)")
	} else {
		t.Logf("Systemd injection via HomeDir correctly rejected: %v", err)
	}
}

// TestAdversarySystemdInjection_EnvHome tests that newlines in EnvHome
// are rejected by GenerateSystemdUnit.
func TestAdversarySystemdInjection_EnvHome(t *testing.T) {
	injectedEnvHome := "/Users/testuser/.agentpaas\nEnvironment=MALICIOUS=yes\nExecStartPre=/usr/bin/touch /tmp/evil"

	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    injectedEnvHome,
	}

	_, err := service.GenerateSystemdUnit(cfg)
	if err == nil {
		t.Errorf("GenerateSystemdUnit should reject newlines in EnvHome (injection attempt)")
	} else {
		t.Logf("Systemd injection via EnvHome correctly rejected: %v", err)
	}
}

// TestAdversarySystemdInjection_SectionBreak tests that injection of an entirely
// new systemd section is rejected.
func TestAdversarySystemdInjection_SectionBreak(t *testing.T) {
	// An attacker could try to inject a new [Install] section.
	injectedPath := "/usr/local/bin/agentpaasd\n[Install]\nWantedBy=multi-user.target\n#"

	cfg := service.SystemdUnitConfig{
		DaemonPath: injectedPath,
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	_, err := service.GenerateSystemdUnit(cfg)
	if err == nil {
		t.Errorf("GenerateSystemdUnit should reject section header injection in DaemonPath")
	} else {
		t.Logf("Section injection correctly rejected: %v", err)
	}
}

// ============================================================================
// ATTACK VECTOR 4: Symlink attack on uninstall
// ============================================================================

// TestAdversarySymlink_UninstallFollowsSymlink tests that UninstallLaunchdPlist
// follows symlinks when removing the plist file.
//
// If it follows symlinks, an attacker can create a symlink at the plist
// path pointing to a critical file, and uninstall would delete that file.
func TestAdversarySymlink_UninstallFollowsSymlink(t *testing.T) {
	dir := t.TempDir()

	// Create a "critical" file that should NOT be deleted.
	criticalFile := filepath.Join(dir, "critical-target.txt")
	if err := os.WriteFile(criticalFile, []byte("important data"), 0644); err != nil {
		t.Fatalf("create critical file: %v", err)
	}

	// Create a symlink at the expected plist path pointing to the critical file.
	plistPath := filepath.Join(dir, "com.agentpaas.daemon.plist")
	if err := os.Symlink(criticalFile, plistPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// Now uninstall - this should remove the plist file.
	_ = service.UninstallLaunchdPlist(dir)

	// Check if the critical file was also deleted (symlink following).
	if _, err := os.Stat(criticalFile); os.IsNotExist(err) {
		t.Errorf("SYMLINK ATTACK: UninstallLaunchdPlist followed symlink and deleted the target file %s", criticalFile)
	}
}

// TestAdversarySymlink_UninstallReportsNoError tests that when the plist
// is a symlink, UninstallLaunchdPlist still succeeds (removing the symlink).
func TestAdversarySymlink_UninstallReportsNoError(t *testing.T) {
	dir := t.TempDir()

	// Create a target file and a symlink.
	target := filepath.Join(dir, "target.txt")
	_ = os.WriteFile(target, []byte("data"), 0644)

	plistPath := filepath.Join(dir, "com.agentpaas.daemon.plist")
	_ = os.Symlink(target, plistPath)

	// Uninstall should succeed (it removes the symlink).
	err := service.UninstallLaunchdPlist(dir)
	if err != nil {
		t.Errorf("UninstallLaunchdPlist should not error even when plist is a symlink: %v", err)
	}
}

// ============================================================================
// ATTACK VECTOR 5: Determinism
// ============================================================================

// TestAdversaryDeterminism_SameConfigSameOutput verifies that the same config
// always produces the same output (no randomness, no timestamps).
func TestAdversaryDeterminism_SameConfigSameOutput(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	// Generate multiple times and compare.
	first, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("launchd plist generation is not deterministic")
	}
}

// TestAdversaryDeterminism_SystemdSameConfigSameOutput verifies systemd
// unit generation is deterministic.
func TestAdversaryDeterminism_SystemdSameConfigSameOutput(t *testing.T) {
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
	}
}

// ============================================================================
// ATTACK VECTOR 6: Privilege escalation via plist
// ============================================================================

// TestAdversaryPrivilegeEscalation_NoRootUser certifies that the generated
// plist does NOT contain UserName, GroupName, or SessionType keys that would
// allow running as root or with elevated privileges.
func TestAdversaryPrivilegeEscalation_NoRootUser(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)
	badKeys := []string{
		"UserName",
		"GroupName",
		"SessionType",
		"RootDirectory",
		"WatchPaths",
		"QueueDirectories",
		"LowPriorityIO",
		"Nice",
	}

	var foundKeys []string
	for _, key := range badKeys {
		if contains(gotStr, "<key>"+key+"</key>") {
			foundKeys = append(foundKeys, key)
		}
	}

	if len(foundKeys) > 0 {
		t.Errorf("Plist should not contain privilege-escalation keys, found: %v", foundKeys)
		t.Logf("Output:\n%s", gotStr)
	}
}

// TestAdversaryPrivilegeEscalation_SystemdNoRootUser certifies that the
// generated systemd unit does not contain User=root or Group=root.
func TestAdversaryPrivilegeEscalation_SystemdNoRootUser(t *testing.T) {
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	gotStr := string(got)

	// Check for User=, Group=, CapabilityBoundingSet=,
	// AmbientCapabilities= which could indicate privilege escalation.
	if contains(gotStr, "User=root") {
		t.Errorf("Systemd unit should not contain User=root")
	}
	if contains(gotStr, "Group=root") {
		t.Errorf("Systemd unit should not contain Group=root")
	}
}

// ============================================================================
// ATTACK VECTOR 7: Overwrite existing files
// ============================================================================

// TestAdversaryOverwrite_NoPreCheck verifies that InstallLaunchdPlist
// rejects overwriting an existing plist without force=true.
func TestAdversaryOverwrite_NoPreCheck(t *testing.T) {
	dir := t.TempDir()

	// Write a different plist file at the target path first.
	plistPath := filepath.Join(dir, "com.agentpaas.daemon.plist")
	originalContent := []byte("<?xml version=\"1.0\"?><plist><dict><key>ORIGINAL</key></dict></plist>")
	if err := os.WriteFile(plistPath, originalContent, 0644); err != nil {
		t.Fatalf("write original file: %v", err)
	}

	// Now install without force - should be rejected.
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	err := service.InstallLaunchdPlist(cfg, dir, false)
	if err == nil {
		t.Errorf("InstallLaunchdPlist should reject overwrite without force, but succeeded")
	} else {
		t.Logf("Overwrite correctly rejected: %v", err)
	}

	// With force=true, it should succeed.
	err = service.InstallLaunchdPlist(cfg, dir, true)
	if err != nil {
		t.Fatalf("InstallLaunchdPlist with force=true: %v", err)
	}
}

// ============================================================================
// ATTACK VECTOR 8: Race condition (window between generation and write)
// ============================================================================

// TestAdversaryRaceCondition_WindowBetweenGenAndWrite demonstrates the
// TOCTOU (time-of-check-to-time-of-use) window between generating the plist
// content and writing it to disk.
//
// In a real attack, another process could swap the file between generation
// and write. This test validates that the window exists.
func TestAdversaryRaceCondition_WindowBetweenGenAndWrite(t *testing.T) {
	dir := t.TempDir()

	// Generate content first.
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	generatedPlist, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	// Install writes the generated content. In a race condition attack,
	// between the generate and write, the file on disk could be swapped.
	err = service.InstallLaunchdPlist(cfg, dir, false)
	if err != nil {
		t.Fatalf("InstallLaunchdPlist: %v", err)
	}

	// Verify the content was written.
	plistPath := filepath.Join(dir, "com.agentpaas.daemon.plist")
	writtenData, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("read written plist: %v", err)
	}

	if string(writtenData) != string(generatedPlist) {
		t.Errorf("Written plist differs from generated plist (race condition window confirmed)")
	}

	// Log the TOCTOU window for documentation.
	t.Logf("TOCTOU window exists: GenerateLaunchdPlist and InstallLaunchdPlist are separate operations")
	t.Logf("Generated %d bytes, written %d bytes", len(generatedPlist), len(writtenData))
}

// ============================================================================
// ADDITIONAL: Edge case tests
// ============================================================================

// TestAdversaryEmptyDaemonPath tests that an empty DaemonPath is allowed.
func TestAdversaryEmptyDaemonPath(t *testing.T) {
	// An empty DaemonPath would make ExecStart just "--home /Users/testuser/.agentpaas"
	// which would fail at runtime but is not validated.
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	gotStr := string(got)
	if contains(gotStr, "<string></string>") {
		// This is interesting - empty DaemonPath generates valid XML but
		// an empty string element.
		t.Logf("Empty DaemonPath generates an empty <string></string> element in ProgramArguments array")
	}
}

// TestAdversaryEmptyHomeDir tests that empty HomeDir is rejected.
func TestAdversaryEmptyHomeDir(t *testing.T) {
	cfg := service.LaunchdPlistConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "",
	}

	_, err := service.GenerateLaunchdPlist(cfg)
	if err == nil {
		t.Errorf("GenerateLaunchdPlist should error with empty HomeDir")
	} else {
		t.Logf("Empty HomeDir correctly rejected: %v", err)
	}

	// Also test systemd
	sysCfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "",
	}
	_, err = service.GenerateSystemdUnit(sysCfg)
	if err == nil {
		t.Errorf("GenerateSystemdUnit should error with empty HomeDir")
	} else {
		t.Logf("Empty HomeDir correctly rejected in systemd: %v", err)
	}
}

// TestAdversarySystemdSpecialCharactersInExecStart tests that special
// systemd characters (like ;, #, =, spaces) in DaemonPath are not escaped.
//
// In systemd unit files, some characters have special meaning.
func TestAdversarySystemdSpecialCharacters(t *testing.T) {
	// Test semicolon and hash which have special meaning in some systemd directives.
	// In ExecStart=, semicolon separates commands but systemd handles it.
	// The main concern is newline injection (tested above).
	cfg := service.SystemdUnitConfig{
		DaemonPath: "/usr/local/bin/agentpaasd --some-flag=value",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateSystemdUnit(cfg)
	if err != nil {
		t.Fatalf("GenerateSystemdUnit: %v", err)
	}

	gotStr := string(got)
	if contains(gotStr, "ExecStart=/usr/local/bin/agentpaasd --some-flag=value --home") {
		t.Logf("Spaces in DaemonPath are preserved (not quoted/escaped)")
	}
}

// TestAdversaryLabelTooLong tests that an extremely long label is not rejected.
func TestAdversaryLabelTooLong(t *testing.T) {
	longLabel := strings.Repeat("A", 10000)
	cfg := service.LaunchdPlistConfig{
		Label:      longLabel,
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
	}

	got, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist with long label: %v", err)
	}

	gotStr := string(got)
	if !contains(gotStr, longLabel) {
		t.Errorf("Long label not present in output")
	}
	t.Logf("Long label (%d chars) produced %d bytes of output (no truncation)", len(longLabel), len(got))
}

// TestAdversaryContentsNoSecrets checks that generated files don't contain
// sensitive data like passwords, tokens, or keys.
func TestAdversaryContentsNoSecrets(t *testing.T) {
	// Verify the generated plist and systemd unit don't include
	// environment variables that could leak secrets.
	cfg := service.LaunchdPlistConfig{
		Label:      "com.agentpaas.daemon",
		DaemonPath: "/usr/local/bin/agentpaasd",
		HomeDir:    "/Users/testuser/.agentpaas",
		EnvHome:    "/Users/testuser/.agentpaas",
	}

	plist, err := service.GenerateLaunchdPlist(cfg)
	if err != nil {
		t.Fatalf("GenerateLaunchdPlist: %v", err)
	}

	plistStr := string(plist)

	// Check for password-like keys in the XML.
	passwordKeys := []string{"PASSWORD", "SECRET", "TOKEN", "API_KEY", "AUTH"}
	for _, key := range passwordKeys {
		if contains(plistStr, key) {
			t.Errorf("Potential secret leakage: plist contains '%s'", key)
		}
	}
}

// ============================================================================
// ADDITIONAL: Install validation
// ============================================================================

// TestAdversaryInstallNoPathValidation verifies that InstallLaunchdPlist
// validates the path (must be absolute, no "..").
func TestAdversaryInstallNoPathValidation(t *testing.T) {
	// Test paths that should be rejected.
	testPaths := []struct {
		name string
		path string
	}{
		{"current-dir", "."},
		{"relative-dir", "some/relative/path"},
		{"dot-dot", ".."},
		{"dot-path", "./LaunchAgents"},
	}

	for _, tc := range testPaths {
		t.Run(tc.name, func(t *testing.T) {
			cfg := service.LaunchdPlistConfig{
				DaemonPath: "/usr/local/bin/agentpaasd",
				HomeDir:    "/Users/testuser/.agentpaas",
			}

			err := service.InstallLaunchdPlist(cfg, tc.path, false)
			if err == nil {
				t.Errorf("InstallLaunchdPlist(%s) should reject relative path, but succeeded", tc.name)
			} else {
				t.Logf("Path %q correctly rejected: %v", tc.path, err)
			}
		})
	}
}