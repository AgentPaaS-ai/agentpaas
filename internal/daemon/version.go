package daemon

import (
	"fmt"
	"runtime"
)

// Current CLI and daemon version. This is the single source of truth for the
// agentpaasd binary and the agent CLI alike.
const (
	// CLIVersion is the current version of the agentpaas CLI.
	CLIVersion = "0.1.0-dev"

	// DaemonVersion is the current version of the agentpaas daemon.
	DaemonVersion = "0.1.0-dev"

	// ProtoVersion is the version of the ControlService protocol that this
	// daemon implements. It corresponds to the API package import path.
	ProtoVersion = "v1"
)

// GitCommit is set at build time via -ldflags "-X github.com/parvezsyed/agentpaas/internal/daemon.GitCommit=<commit>".
// If not injected, it defaults to "unknown".
var GitCommit = "unknown"

// VersionInfo holds detailed version and compatibility information that the
// daemon reports to clients via the Doctor RPC and gRPC response trailers.
type VersionInfo struct {
	// CLIVersion is the version of the CLI that was compiled with this daemon.
	CLIVersion string

	// DaemonVersion is the version of the running daemon binary.
	DaemonVersion string

	// ProtoVersion is the protocol version string (e.g. "v1").
	ProtoVersion string

	// GitCommit is the full git SHA from which this binary was built.
	GitCommit string

	// GoVersion is the Go runtime version (e.g. "go1.26.4").
	GoVersion string

	// OsArch is the operating system and architecture (e.g. "linux/amd64").
	OsArch string

	// DockerContext is the name of the active Docker context (stub for now).
	DockerContext string

	// DockerAPIVersion is the Docker API version (stub for now).
	DockerAPIVersion string
}

// CurrentVersion returns a VersionInfo populated with build-time constants
// and runtime values. This is the canonical version snapshot for the daemon.
func CurrentVersion() VersionInfo {
	return VersionInfo{
		CLIVersion:       CLIVersion,
		DaemonVersion:    DaemonVersion,
		ProtoVersion:     ProtoVersion,
		GitCommit:        GitCommit,
		GoVersion:        runtime.Version(),
		OsArch:           fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		DockerContext:    "",
		DockerAPIVersion: "",
	}
}

// String returns a human-readable summary of the version info.
func (v VersionInfo) String() string {
	return fmt.Sprintf(
		"CLI: %s | Daemon: %s | Proto: %s | Commit: %s | Go: %s | OS/Arch: %s",
		v.CLIVersion, v.DaemonVersion, v.ProtoVersion, v.GitCommit,
		v.GoVersion, v.OsArch,
	)
}
