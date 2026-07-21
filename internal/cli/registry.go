package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/spf13/cobra"
)

// registryListFactory is overridden in tests.
var registryListFactory = defaultRegistryList

// registryShowFactory is overridden in tests.
var registryShowFactory = defaultRegistryShow

// registryPromoteFactory is overridden in tests.
var registryPromoteFactory = defaultRegistryPromote

// registryDemoteFactory is overridden in tests.
var registryDemoteFactory = defaultRegistryDemote

func defaultRegistryList(cmd *cobra.Command) ([]registry.RegistryEntry, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("default registry list: %w", err)
	}
	paths := home.NewHomePaths(homeDir)
	store := openRegistryDeploymentStore(paths)
	return registry.ListEntries(paths.State, store)
}

func defaultRegistryShow(cmd *cobra.Command, ref string) (*registry.RegistryEntry, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("default registry show: %w", err)
	}
	paths := home.NewHomePaths(homeDir)
	store := openRegistryDeploymentStore(paths)
	return registry.ShowEntry(paths.State, ref, store)
}

func defaultRegistryPromote(cmd *cobra.Command, stateRootDir, ref, actor string) error {
	return registry.Promote(stateRootDir, ref, actor)
}

func defaultRegistryDemote(cmd *cobra.Command, stateRootDir, ref string) error {
	return registry.Demote(stateRootDir, ref)
}

// openRegistryDeploymentStore opens the B26 routed LocalStore if the
// state/routed directory exists. Returns nil if the directory is missing
// or OpenLocalStore fails, allowing the registry read to proceed without
// deployment join (graceful degradation).
func openRegistryDeploymentStore(paths *home.HomePaths) registry.DeploymentStoreReader {
	root := filepath.Join(paths.State, "routed")
	// Do not create the routed directory if the daemon has never run.
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	store, err := routedrun.OpenLocalStore(root)
	if err != nil {
		return nil
	}
	return store
}

// newRegistryCmd creates the `agentpaas registry` command group.
func newRegistryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Query the local package registry",
		Long: `Read-only queries over the local package registry.

List every installed package with deployment status, digests,
aliases, and the promoted flag. Show a single entry by name@pub8
or alias, including declared capability metadata.

This is a local read API over the B23 installed-agent store and
B26 deployment store; it does not require a running daemon.
Deployment status and deployment-level aliases are sourced from
the local routed store (state/routed). When that store is absent
(e.g., no daemon has ever deployed), the columns appear as "-" or
"none" rather than failing.`,
		Example: `  agentpaas registry list
  agentpaas registry list --json
  agentpaas registry show weather@a1b2c3d4
  agentpaas registry show weather --json`,
	}
	cmd.AddCommand(newRegistryListCmd())
	cmd.AddCommand(newRegistryShowCmd())
	cmd.AddCommand(newRegistryPromoteCmd())
	cmd.AddCommand(newRegistryDemoteCmd())
	return cmd
}

func newRegistryListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed packages with deployment status and promoted flag",
		Long: `List every installed package with deployment status, aliases, digests,
and the promoted flag.

Output is ordered deterministically (name, then version) and bounded.
Use the global --json flag for structured output.`,
		Example: `  agentpaas registry list
  agentpaas registry list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := registryListFactory(cmd)
			if err != nil {
				return fmt.Errorf("registry list: %w", err)
			}
			if jsonOutput(cmd) {
				raw, merr := json.Marshal(entries)
				if merr != nil {
					return fmt.Errorf("registry list: %w", merr)
				}
				fmt.Println(string(raw))
				return nil
			}
			if len(entries) == 0 {
				cmd.Println("No installed packages.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "REF\tVERSION\tPUBLISHER\tMODE\tSTATUS\tPROMOTED\tALIAS") // best-effort write
			for _, e := range entries {
				status := "-"
				if e.DeploymentStatus != "" {
					status = e.DeploymentStatus
				}
				promoted := "no"
				if e.Promoted {
					promoted = "yes"
				}
				alias := e.Alias
				if alias == "" {
					alias = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", // best-effort write
					e.Ref, e.Version, e.PublisherName, e.InstallMode, status, promoted, alias)
			}
			return w.Flush()
		},
	}
	return cmd
}

func newRegistryShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name|alias>",
		Short: "Show full registry entry including capability metadata",
		Long: `Show the full registry entry for a single installed package by name@pub8
or alias.

Includes declared capability metadata from the signed package manifest
(stored verbatim; not schema-matched in v0.3).

Use the global --json flag for structured output.`,
		Example: `  agentpaas registry show weather@a1b2c3d4
  agentpaas registry show weather
  agentpaas registry show weather --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entry, err := registryShowFactory(cmd, args[0])
			if err != nil {
				return fmt.Errorf("registry show: %w", err)
			}
			if jsonOutput(cmd) {
				raw, merr := json.Marshal(entry)
				if merr != nil {
					return fmt.Errorf("registry show: %w", merr)
				}
				fmt.Println(string(raw))
				return nil
			}
			return printRegistryEntry(cmd, entry)
		},
	}
	return cmd
}

func printRegistryEntry(cmd *cobra.Command, e *registry.RegistryEntry) error {
	var b strings.Builder

	fmt.Fprintf(&b, "Package:     %s\n", e.Ref)
	fmt.Fprintf(&b, "Version:     %s\n", e.Version)
	fmt.Fprintf(&b, "Publisher:   %s (%s)\n", e.PublisherName, truncateFingerprint(e.PublisherFingerprint))
	fmt.Fprintf(&b, "Package digest:  %s\n", e.PackageDigest)
	fmt.Fprintf(&b, "Policy digest:   %s\n", e.PolicyDigest)
	fmt.Fprintf(&b, "Install mode:    %s\n", e.InstallMode)
	fmt.Fprintf(&b, "Local image:     %s\n", truncateDigest(e.LocalImageDigest))
	fmt.Fprintf(&b, "Installed at:    %s\n", e.InstalledAt.UTC().Format("2006-01-02 15:04:05 MST"))
	if e.Alias != "" {
		fmt.Fprintf(&b, "Alias:       %s\n", e.Alias)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "Promoted:    %v\n", e.Promoted)
	if e.PromotedAt != nil {
		fmt.Fprintf(&b, "Promoted at: %s\n", e.PromotedAt.UTC().Format("2006-01-02 15:04:05 MST"))
	}
	if e.PromotedBy != "" {
		fmt.Fprintf(&b, "Promoted by: %s\n", e.PromotedBy)
	}
	fmt.Fprintf(&b, "\n")

	if e.DeploymentID != nil {
		fmt.Fprintf(&b, "Deployment ID:   %s\n", *e.DeploymentID)
		fmt.Fprintf(&b, "Status:          %s\n", e.DeploymentStatus)
		fmt.Fprintf(&b, "Generation:      %d\n", e.Generation)
		if e.BundleDigest != "" {
			fmt.Fprintf(&b, "Bundle digest:   %s\n", e.BundleDigest)
		}
		if len(e.Aliases) > 0 {
			fmt.Fprintf(&b, "Deployment aliases: %s\n", strings.Join(e.Aliases, ", "))
		}
	} else {
		fmt.Fprintf(&b, "Deployment:  none\n")
	}
	fmt.Fprintf(&b, "\n")

	if len(e.CredentialIDs) > 0 {
		sort.Strings(e.CredentialIDs)
		fmt.Fprintf(&b, "Credentials: %s\n", strings.Join(e.CredentialIDs, ", "))
	}
	if len(e.Capabilities) > 0 {
		fmt.Fprintf(&b, "Capabilities:\n")
		for _, c := range e.Capabilities {
			fmt.Fprintf(&b, "  - %s", c.ID)
			if c.Description != "" {
				fmt.Fprintf(&b, ": %s", c.Description)
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	cmd.Println(b.String())
	return nil
}

func truncateFingerprint(fp string) string {
	if len(fp) > 16 {
		return fp[:16] + "..."
	}
	return fp
}

func truncateDigest(d string) string {
	if len(d) > 18 {
		return d[:18] + "..."
	}
	return d
}

// newRegistryPromoteCmd creates the `agentpaas registry promote` subcommand.
func newRegistryPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote <ref>",
		Short: "Promote a package for use in workflows",
		Long: `Mark an installed package as promoted so it can be referenced
in workflow.yaml service bindings, pipeline stages, and child
allowlists.

Promotion is idempotent: promoting an already-promoted package is
a no-op. An audit event is recorded.`,
		Example: `  agentpaas registry promote weather@a1b2c3d4
  agentpaas registry promote my-alias`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return fmt.Errorf("registry promote: %w", err)
			}
			paths := home.NewHomePaths(homeDir)
			actor := os.Getenv("USER")
			if actor == "" {
				actor = "local"
			}
			if err := registryPromoteFactory(cmd, paths.State, args[0], actor); err != nil {
				return fmt.Errorf("registry promote: %w", err)
			}
			cmd.Printf("Promoted %s\n", args[0])
			return nil
		},
	}
	return cmd
}

// newRegistryDemoteCmd creates the `agentpaas registry demote` subcommand.
func newRegistryDemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demote <ref>",
		Short: "Demote a package, preventing future workflow references",
		Long: `Clear the promoted flag on a package. After demotion, the package
can no longer be referenced in new workflow.yaml service bindings,
pipeline stages, or child allowlists.

Demotion does NOT invalidate already-signed workflows — those remain
immutable. An audit event is recorded.`,
		Example: `  agentpaas registry demote weather@a1b2c3d4
  agentpaas registry demote my-alias`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return fmt.Errorf("registry demote: %w", err)
			}
			paths := home.NewHomePaths(homeDir)
			if err := registryDemoteFactory(cmd, paths.State, args[0]); err != nil {
				return fmt.Errorf("registry demote: %w", err)
			}
			cmd.Printf("Demoted %s\n", args[0])
			return nil
		},
	}
	return cmd
}
