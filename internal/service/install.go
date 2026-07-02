package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LaunchdLabel is the standard launchd job label for agentpaasd.
const LaunchdLabel = "com.agentpaas.daemon"

// LaunchdPlistFilename returns the filename for the launchd plist.
func LaunchdPlistFilename() string {
	return LaunchdLabel + ".plist"
}

// InstallLaunchdPlist writes the generated launchd plist to the specified
// launchAgents directory (typically ~/Library/LaunchAgents).
//
// It creates the directory if it does not exist. The file is written with
// mode 0644. Install does NOT load the service via launchctl — it only
// writes the plist file.
//
// If force is false and the plist file already exists, an error is returned.
// Set force to true to overwrite the existing file.
func InstallLaunchdPlist(cfg LaunchdPlistConfig, launchAgentsDir string, force bool) error {
	if launchAgentsDir == "" {
		return fmt.Errorf("service: launch agents directory must not be empty")
	}

	// Path traversal validation.
	if !strings.HasPrefix(launchAgentsDir, "/") {
		return fmt.Errorf("service: launch agents directory %q must be an absolute path (start with /)", launchAgentsDir)
	}
	cleanedDir := filepath.Clean(launchAgentsDir)
	if cleanedDir != launchAgentsDir {
		return fmt.Errorf("service: launch agents directory %q contains \"..\" or \".\" components", launchAgentsDir)
	}
	if strings.Contains(launchAgentsDir, "..") {
		return fmt.Errorf("service: launch agents directory %q must not contain \"..\"", launchAgentsDir)
	}
	launchAgentsDir = cleanedDir

	plist, err := GenerateLaunchdPlist(cfg)
	if err != nil {
		return fmt.Errorf("service: generate plist: %w", err)
	}

	// Check for silent overwrite.
	dst := filepath.Join(launchAgentsDir, LaunchdPlistFilename())
	if !force {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("plist already installed at %s; uninstall first or use --force", dst)
		}
	}

	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("service: create directory %s: %w", launchAgentsDir, err)
	}

	if err := os.WriteFile(dst, plist, 0644); err != nil {
		return fmt.Errorf("service: write plist %s: %w", dst, err)
	}

	return nil
}

// UninstallLaunchdPlist removes the launchd plist from the specified
// launchAgents directory.
//
// It is idempotent: if the file does not exist, no error is returned.
func UninstallLaunchdPlist(launchAgentsDir string) error {
	dst := filepath.Join(launchAgentsDir, LaunchdPlistFilename())
	if err := os.Remove(dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("service: remove plist %s: %w", dst, err)
	}
	return nil
}