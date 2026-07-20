package cli

import (
	"encoding/json"
	"fmt"
)

// VersionOutput holds the CLI version information for display.
type VersionOutput struct {
	CLIVersion       string `json:"cli_version"`
	ProtoVersion     string `json:"proto_version"`
	GitCommit        string `json:"git_commit"`
	OsArch           string `json:"os_arch"`
	DockerContext    string `json:"docker_context"`
	DockerAPIVersion string `json:"docker_api_version"`
}

// VersionText returns a human-readable summary of VersionOutput.
func VersionText(v VersionOutput) string {
	return fmt.Sprintf(
		"CLI: %s | Proto: %s | Commit: %s | OS/Arch: %s | Docker: %s | Docker API: %s",
		v.CLIVersion, v.ProtoVersion, v.GitCommit, v.OsArch,
		v.DockerContext, v.DockerAPIVersion,
	)
}

// DaemonStatusOutput holds daemon version and readiness information.
type DaemonStatusOutput struct {
	DaemonVersion    string `json:"daemon_version"`
	ProtoVersion     string `json:"proto_version"`
	GitCommit        string `json:"git_commit"`
	OsArch           string `json:"os_arch"`
	DockerContext    string `json:"docker_context"`
	DockerAPIVersion string `json:"docker_api_version"`
	Ready            bool   `json:"ready"`
}

// DaemonStatusText returns a human-readable summary of DaemonStatusOutput.
func DaemonStatusText(s DaemonStatusOutput) string {
	ready := "not ready"
	if s.Ready {
		ready = "ready"
	}
	return fmt.Sprintf(
		"Daemon: %s | Proto: %s | Commit: %s | OS/Arch: %s | Docker: %s | Docker API: %s | Status: %s",
		s.DaemonVersion, s.ProtoVersion, s.GitCommit, s.OsArch,
		s.DockerContext, s.DockerAPIVersion, ready,
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
