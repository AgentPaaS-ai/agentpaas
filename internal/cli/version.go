package cli

import (
	"runtime"

	"github.com/parvezsyed/agentpaas/internal/daemon"
	"github.com/spf13/cobra"
)

// newVersionCmd creates the `agent version` command.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version information",
		Long: `Print the CLI version, protocol version, git commit, OS/architecture,
Docker context, and Docker API version.

Use --json for structured JSON output.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			v := daemon.CurrentVersion()
			output := VersionOutput{
				CLIVersion:       v.CLIVersion,
				ProtoVersion:     v.ProtoVersion,
				GitCommit:        v.GitCommit,
				OsArch:           v.OsArch,
				DockerContext:    "unknown",
				DockerAPIVersion: "unknown",
			}
			return printTextOrJSON(jsonOutput(cmd), output, func(v interface{}) string {
				return VersionText(v.(VersionOutput))
			})
		},
	}
}

// init registers version-specific ldflags vars. This is a no-op placeholder
// for the ldflags injection pattern; the actual values are set at build time
// in internal/daemon.GitCommit.
func init() {
	_ = runtime.Version
}