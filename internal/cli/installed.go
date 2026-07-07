package cli

import (
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

// installStateFactory creates the install state store for CLI commands (overridden in tests).
var installStateFactory = newDefaultInstallState

func newDefaultInstallState(cmd *cobra.Command) (install.InstallStateStore, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, err
	}
	paths := home.NewHomePaths(homeDir)
	return &install.FileInstallState{StateRoot: paths.State}, nil
}

// newInstalledCmd creates the `agent installed` command group.
func newInstalledCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "installed",
		Short: "Manage installed shared agents",
		Long:  "Commands for installed agent state, including post-install credential mapping.",
	}
	cmd.AddCommand(newInstalledMapCredentialCmd())
	return cmd
}

func newInstalledMapCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "map-credential <ref> <declared>=<local>",
		Short: "Map a declared policy credential to a local secret name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := installStateFactory(cmd)
			if err != nil {
				return err
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return err
			}
			if err := install.ApplyMapCredential(install.MapCredentialOpts{
				State:   state,
				Store:   store,
				Ref:     args[0],
				Mapping: args[1],
			}); err != nil {
				return err
			}
			if jsonOutput(cmd) {
				fmt.Println(`{"status":"ok"}`)
			} else {
				cmd.Println("Credential mapping saved.")
			}
			return nil
		},
	}
	return cmd
}