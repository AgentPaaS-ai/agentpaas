package service

import (
	"fmt"
	"strings"
)

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