package service

import (
	"fmt"
	"os"
	"path/filepath"
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
func InstallLaunchdPlist(cfg LaunchdPlistConfig, launchAgentsDir string) error {
	if launchAgentsDir == "" {
		return fmt.Errorf("service: launch agents directory must not be empty")
	}

	plist, err := GenerateLaunchdPlist(cfg)
	if err != nil {
		return fmt.Errorf("service: generate plist: %w", err)
	}

	if err := os.MkdirAll(launchAgentsDir, 0755); err != nil {
		return fmt.Errorf("service: create directory %s: %w", launchAgentsDir, err)
	}

	dst := filepath.Join(launchAgentsDir, LaunchdPlistFilename())
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
