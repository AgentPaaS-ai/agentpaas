package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

// installStateFactory creates the install state store for CLI commands (overridden in tests).
var installStateFactory = newDefaultInstallState

// installedListFactory lists materialized installs (overridden in tests).
var installedListFactory = defaultListInstalled

// installedRemoveFactory removes materialized installs (overridden in tests).
var installedRemoveFactory = defaultRemoveInstalled

func newDefaultInstallState(cmd *cobra.Command) (install.InstallStateStore, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, err
	}
	paths := home.NewHomePaths(homeDir)
	return &install.FileInstallState{StateRoot: paths.State}, nil
}

func defaultListInstalled(cmd *cobra.Command) ([]install.InstalledAgentEntry, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, err
	}
	paths := home.NewHomePaths(homeDir)
	return install.ListInstalledAgents(paths.State)
}

func defaultRemoveInstalled(cmd *cobra.Command, ref string) error {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return err
	}
	paths := home.NewHomePaths(homeDir)
	return install.RemoveInstalledAgent(cmd.Context(), paths.State, ref, install.DockerContainerStopper{}, nil)
}

// newInstalledCmd creates the `agent installed` command group.
func newInstalledCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "installed",
		Short: "Manage installed shared agents",
		Long:  "Commands for installed agent state, including post-install credential mapping.",
	}
	cmd.AddCommand(newInstalledListCmd())
	cmd.AddCommand(newInstalledRemoveCmd())
	cmd.AddCommand(newInstalledAliasCmd())
	cmd.AddCommand(newInstalledMapCredentialCmd())
	return cmd
}

func newInstalledAliasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "alias <ref> <alias>",
		Short: "Set or change the display alias for an installed agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return err
			}
			paths := home.NewHomePaths(homeDir)
			emit := func(eventType string, payload map[string]string) {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "audit: %s %v\n", eventType, payload) // best-effort write
			}
			if err := install.SetInstalledAlias(paths.State, args[0], args[1], emit); err != nil {
				return err
			}
			ref, err := install.ResolveAgentRef(install.ResolveRefOpts{
				StateRoot: paths.State,
				Input:     args[0],
			})
			if err != nil {
				return err
			}
			display := install.FormatAgentDisplay(ref.Ref, strings.TrimSpace(args[1]))
			cmd.Printf("Alias updated: %s\n", display)
			return nil
		},
	}
}

func newInstalledListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed shared agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := installedListFactory(cmd)
			if err != nil {
				return err
			}
			if jsonOutput(cmd) {
				raw, err := json.Marshal(entries)
				if err != nil {
					return err
				}
				fmt.Println(string(raw))
				return nil
			}
			if len(entries) == 0 {
				cmd.Println("No installed shared agents.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "REF\tALIAS\tVERSION\tPUBLISHER\tINSTALLED\tMODE") // best-effort write
			for _, e := range entries {
				alias := e.Alias
				if alias == "" {
					alias = "-"
				}
				at := "-"
				if !e.InstalledAt.IsZero() {
					at = e.InstalledAt.UTC().Format("2006-01-02 15:04:05")
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Ref, alias, e.Version, e.Publisher, at, e.Mode) // best-effort write
			}
			return w.Flush()
		},
	}
	return cmd
}

func newInstalledRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <ref>",
		Short: "Remove installed agent state (trust pin retained)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := installedRemoveFactory(cmd, args[0]); err != nil {
				return err
			}
			if jsonOutput(cmd) {
				fmt.Println(`{"status":"ok"}`)
			} else {
				cmd.Println("Installed agent removed.")
			}
			return nil
		},
	}
	cmd.Flags().Bool("yes", false, "Skip confirmation prompts")
	return cmd
}

func newInstalledMapCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "map-credential <ref> <declared>=<local>",
		Short: "Map a declared policy credential to a local secret name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return err
			}
			state, err := installStateFactory(cmd)
			if err != nil {
				return err
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return err
			}
			paths := home.NewHomePaths(homeDir)
			if err := install.ApplyMapCredential(install.MapCredentialOpts{
				State:     state,
				Store:     store,
				Ref:       args[0],
				Mapping:   args[1],
				StateRoot: paths.State,
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
