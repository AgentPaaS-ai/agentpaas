package service

import (
	"fmt"
	"strings"
)

// validateSystemdField rejects strings that could be used for systemd
// unit injection. It checks for newlines, null bytes, and section headers.
func validateSystemdField(fieldName, value string) error {
	if strings.Contains(value, "\n") || strings.Contains(value, "\r") {
		return fmt.Errorf("service: %s contains newline characters (injection attempt)", fieldName)
	}
	if strings.Contains(value, "\x00") {
		return fmt.Errorf("service: %s contains null byte (injection attempt)", fieldName)
	}
	if strings.Contains(value, "[Install]") {
		return fmt.Errorf("service: %s contains section header [Install] (injection attempt)", fieldName)
	}
	// Check for "[" at the start of a line (section header injection).
	lines := strings.Split(value, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return fmt.Errorf("service: %s contains section header injection %q", fieldName, trimmed)
		}
	}
	return nil
}

// SystemdUnitConfig holds the inputs for generating a systemd user service unit.
type SystemdUnitConfig struct {
	// DaemonPath is the absolute path to the agentpaasd binary.
	DaemonPath string

	// HomeDir is the agentpaas home directory, passed via --home.
	HomeDir string

	// EnvHome, if non-empty, is the AGENTPAAS_HOME environment variable value.
	// When set, it is included as an Environment= line in the Service section.
	EnvHome string
}

// GenerateSystemdUnit generates a systemd user service unit file for agentpaasd.
//
// The output is deterministic: the same config always produces byte-identical output.
// No timestamps or random values are included.
func GenerateSystemdUnit(cfg SystemdUnitConfig) ([]byte, error) {
	if cfg.HomeDir == "" {
		return nil, fmt.Errorf("service: HomeDir must not be empty")
	}

	if err := validateSystemdField("DaemonPath", cfg.DaemonPath); err != nil {
		return nil, fmt.Errorf("generate systemd unit: %w", err)
	}
	if err := validateSystemdField("HomeDir", cfg.HomeDir); err != nil {
		return nil, fmt.Errorf("generate systemd unit: %w", err)
	}
	if cfg.EnvHome != "" {
		if err := validateSystemdField("EnvHome", cfg.EnvHome); err != nil {
			return nil, fmt.Errorf("generate systemd unit: %w", err)
		}
	}

	var b strings.Builder

	// [Unit] section
	b.WriteString("[Unit]\n")
	b.WriteString("Description=AgentPaaS Control Daemon\n")
	b.WriteString("\n")

	// [Service] section
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "ExecStart=%s --home %s\n", cfg.DaemonPath, cfg.HomeDir)
	b.WriteString("Restart=on-failure\n")
	if cfg.EnvHome != "" {
		fmt.Fprintf(&b, "Environment=AGENTPAAS_HOME=%s\n", cfg.EnvHome)
	}
	b.WriteString("\n")

	// [Install] section
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return []byte(b.String()), nil
}
