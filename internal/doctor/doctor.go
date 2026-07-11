package doctor

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/daemon"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// Doctor runs all diagnostic checks and aggregates the results.
//
// Use New() to create a Doctor with optional configuration, then call
// Run() to execute all checks.
type Doctor struct {
	// homeDir is the agentpaas home directory path.
	homeDir string
	// socketPath is the daemon Unix socket path.
	socketPath string
	// cliVersion is the CLI version string.
	cliVersion string
	// cliProtoVersion is the CLI protocol version string.
	cliProtoVersion string
}

// Option configures the Doctor.
type Option func(*Doctor)

// WithHomeDir sets a custom home directory for diagnostics.
func WithHomeDir(dir string) Option {
	return func(d *Doctor) {
		d.homeDir = dir
	}
}

// WithSocketPath sets a custom socket path for diagnostics.
func WithSocketPath(path string) Option {
	return func(d *Doctor) {
		d.socketPath = path
	}
}

// WithCLIVersion sets the CLI version for proto compatibility checks.
func WithCLIVersion(version string) Option {
	return func(d *Doctor) {
		d.cliVersion = version
	}
}

// WithCLIProtoVersion sets the CLI protocol version for proto compatibility checks.
func WithCLIProtoVersion(version string) Option {
	return func(d *Doctor) {
		d.cliProtoVersion = version
	}
}

// New creates a Doctor with default settings.
//
// Defaults:
//   - homeDir:   home.DiscoverHome() or ~/.agentpaas
//   - socketPath: home.DiscoverSocketPath(homeDir)
//   - cliVersion: daemon.CLIVersion (settable via ldflags at build time)
//   - cliProtoVersion: "v1"
//
// Use With* options to override any default.
func New(opts ...Option) (*Doctor, error) {
	d := &Doctor{
		cliVersion:      daemon.CLIVersion,
		cliProtoVersion: "v1",
	}

	// Resolve home directory default.
	homeDir, err := home.DiscoverHome()
	if err != nil {
		return nil, fmt.Errorf("doctor: cannot discover home directory: %w", err)
	}
	d.homeDir = homeDir
	d.socketPath = home.DiscoverSocketPath(homeDir)

	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// Run executes all diagnostic checks and returns the results.
//
// Each check is independent; one failure does not prevent others from running.
// The overall status is derived from all check results.
func (d *Doctor) Run() ([]CheckResult, string) {
	checks := []CheckResult{
		CheckDockerReachable(),
		CheckDockerContext(),
		CheckDockerDesktop(),
		CheckLinuxDockerd(),
		CheckPortsFree(),
		CheckSocketPerms(d.homeDir),
		CheckHomeDirPerms(d.homeDir),
		CheckDaemonReady(d.socketPath),
		CheckProtoCompatible(d.socketPath, d.cliVersion, d.cliProtoVersion),
		CheckHarnessCopies(),
	}

	overall := OverallStatus(checks)
	return checks, overall
}

// FormatResults returns a human-readable summary of all check results.
func FormatResults(checks []CheckResult, overall string) string {
	var b strings.Builder

	b.WriteString("AgentPaaS Doctor Report\n")
	b.WriteString("=======================\n\n")

	for _, c := range checks {
		var statusIcon string
		switch c.Status {
		case "ok":
			statusIcon = "✓"
		case "warning":
			statusIcon = "⚠"
		case "error":
			statusIcon = "✗"
		default:
			statusIcon = "?"
		}
		fmt.Fprintf(&b, " [%s] %s: %s\n", statusIcon, c.Name, c.Message)
		if c.FixHint != "" && c.Status != "ok" {
			fmt.Fprintf(&b, "       Fix: %s\n", c.FixHint)
		}
	}

	fmt.Fprintf(&b, "\nOverall status: %s\n", overall)
	return b.String()
}

// IsHealthy returns true if the overall status is "ok".
func IsHealthy(overall string) bool {
	return overall == "ok"
}