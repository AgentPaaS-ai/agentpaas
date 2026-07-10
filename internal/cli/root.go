package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// rootCmd is the root cobra command for the agent CLI.
var rootCmd *cobra.Command

// jsonOutput returns whether the --json flag is set on the root command.
func jsonOutput(cmd *cobra.Command) bool {
	val, err := cmd.Root().PersistentFlags().GetBool("json")
	if err != nil {
		return false
	}
	return val
}

// socketPath returns the effective daemon socket path from the --socket flag,
// AGENTPAAS_SOCKET environment variable, or the default home directory path.
func socketPath(cmd *cobra.Command) (string, error) {
	// Check flag first.
	if cmd.Root().PersistentFlags().Changed("socket") {
		return cmd.Root().PersistentFlags().GetString("socket")
	}

	// Check environment variable.
	if s := os.Getenv("AGENTPAAS_SOCKET"); s != "" {
		return s, nil
	}

	// Fall back to home-based default.
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return "", err
	}
	paths := home.NewHomePaths(homeDir)
	return paths.Socket, nil
}

// homeDirPath returns the effective home directory from the --home flag,
// AGENTPAAS_HOME environment variable, or the default (~/.agentpaas).
func homeDirPath(cmd *cobra.Command) (string, error) {
	if cmd.Root().PersistentFlags().Changed("home") {
		val, err := cmd.Root().PersistentFlags().GetString("home")
		if err != nil {
			return "", err
		}
		return val, nil
	}
	return home.DiscoverHome()
}

// AgentCmd returns the root cobra command for the agent CLI. It is called once
// during CLI startup to build the full command tree.
func AgentCmd() *cobra.Command {
	if rootCmd != nil {
		return rootCmd
	}

	rootCmd = &cobra.Command{
		Use:   "agent",
		Short: "AgentPaaS CLI — control and manage AgentPaaS agents",
		Long: `AgentPaaS CLI provides operational control over the AgentPaaS daemon,
agent lifecycle, policy, secrets, audit, and diagnostics.

Start the daemon first with 'agent daemon start', then use subcommands
to pack, run, and manage agents.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate home path if --home was explicitly provided.
			if cmd.Root().PersistentFlags().Changed("home") {
				val, _ := cmd.Root().PersistentFlags().GetString("home")
				if err := home.ValidatePath(val); err != nil {
					return fmt.Errorf("invalid --home path: %w", err)
				}
			}
			// Validate socket path if --socket was explicitly provided.
			if cmd.Root().PersistentFlags().Changed("socket") {
				val, _ := cmd.Root().PersistentFlags().GetString("socket")
				if err := home.ValidatePath(val); err != nil {
					return fmt.Errorf("invalid --socket path: %w", err)
				}
			}
			return nil
		},
		// Silence usage on RunE errors so we don't print the usage string
		// for operational errors (daemon not running, etc.).
		SilenceUsage: true,
	}

	// Global persistent flags.
	rootCmd.PersistentFlags().Bool("json", false, "Output JSON instead of human-readable text")
	rootCmd.PersistentFlags().String("socket", "", "Daemon Unix socket path (default: AGENTPAAS_SOCKET or <home>/daemon.sock)")
	rootCmd.PersistentFlags().String("home", "", "AgentPaaS home directory (default: AGENTPAAS_HOME or ~/.agentpaas)")

	// Register subcommands.
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newDoctorCmd())
	rootCmd.AddCommand(newPackCmd())
	rootCmd.AddCommand(newExportCmd())
	rootCmd.AddCommand(newBundleCmd())
	rootCmd.AddCommand(newInstallBundleCmd())
	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newConfirmCmd())
	rootCmd.AddCommand(newConfirmationsCmd())
	rootCmd.AddCommand(newLogsCmd())
	rootCmd.AddCommand(newPolicyCmd())
	rootCmd.AddCommand(newSecretCmd())
	rootCmd.AddCommand(newAuditCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newSummarizeCmd())
	rootCmd.AddCommand(newExplainFailureCmd())
	rootCmd.AddCommand(newExplainDenialCmd())
	rootCmd.AddCommand(newRecommendPatchCmd())
	rootCmd.AddCommand(newTimelineCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newNextActionCmd())
	rootCmd.AddCommand(newTriggerCmd())
	rootCmd.AddCommand(newCronCmd())
	rootCmd.AddCommand(newTrustCmd())
	rootCmd.AddCommand(newIdentityCmd())
	rootCmd.AddCommand(newInstalledCmd())
	rootCmd.AddCommand(newForkCmd())
	rootCmd.AddCommand(newProvenanceCmd())

	return rootCmd
}
