// Package service generates launchd and systemd service unit files for the
// AgentPaaS daemon (agentpaasd).
//
// It provides:
//   - Deterministic launchd plist generation for macOS
//   - Deterministic systemd user unit generation for Linux
//   - Install/uninstall functions that write or remove the plist file
//
// Generated files contain no timestamps, random values, or non-deterministic
// content. The same inputs always produce byte-identical output.
//
// The install path is configurable for testing. Install/uninstall only write
// or remove files — they do NOT load the service (no launchctl/systemctl).
package service