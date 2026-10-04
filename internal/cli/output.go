package cli

import (
	"encoding/json"
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/openshellrt"
)

// VersionOutput holds the CLI version information for display.
type VersionOutput struct {
	CLIVersion       string `json:"cli_version"`
	ProtoVersion     string `json:"proto_version"`
	GitCommit        string `json:"git_commit"`
	OsArch           string `json:"os_arch"`
	OpenShellVersion string `json:"openshell_version"`
	OpenShellGateway string `json:"openshell_gateway"`
}

// VersionText returns a human-readable summary of VersionOutput.
func VersionText(v VersionOutput) string {
	return fmt.Sprintf(
		"CLI: %s | Proto: %s | Commit: %s | OS/Arch: %s | OpenShell: %s | OpenShell gateway: %s",
		v.CLIVersion, v.ProtoVersion, v.GitCommit, v.OsArch,
		v.OpenShellVersion, v.OpenShellGateway,
	)
}

// DaemonStatusOutput holds daemon version and readiness information.
type DaemonStatusOutput struct {
	DaemonVersion    string `json:"daemon_version"`
	ProtoVersion     string `json:"proto_version"`
	GitCommit        string `json:"git_commit"`
	OsArch           string `json:"os_arch"`
	OpenShellVersion string `json:"openshell_version"`
	OpenShellGateway string `json:"openshell_gateway"`
	Ready            bool   `json:"ready"`
}

// DaemonStatusText returns a human-readable summary of DaemonStatusOutput.
func DaemonStatusText(s DaemonStatusOutput) string {
	ready := "not ready"
	if s.Ready {
		ready = "ready"
	}
	return fmt.Sprintf(
		"Daemon: %s | Proto: %s | Commit: %s | OS/Arch: %s | OpenShell: %s | OpenShell gateway: %s | Status: %s",
		s.DaemonVersion, s.ProtoVersion, s.GitCommit, s.OsArch,
		s.OpenShellVersion, s.OpenShellGateway, ready,
	)
}

// JSONError is a structured error representation for JSON output.
type JSONError struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// printTextOrJSON prints val as JSON when jsonOut is true, or calls textFn for
// human-readable output.
func printTextOrJSON(jsonOut bool, val interface{}, textFn func(interface{}) string) error {
	if jsonOut {
		data, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return fmt.Errorf("json marshal error: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Println(textFn(val))
	return nil
}

// probeOpenShellStatus reports the pinned OpenShell CLI version and gateway.
func probeOpenShellStatus() (version, gateway string) {
	st := openshellrt.Probe()
	version = st.CLIVersion
	if version == "" {
		version = "not found"
	}
	if st.GatewayReady {
		gateway = st.GatewayDetail
		if gateway == "" {
			gateway = "ready"
		}
		return version, gateway
	}
	if st.GatewayDetail == "" {
		return version, "unavailable"
	}
	return version, st.GatewayDetail
}
