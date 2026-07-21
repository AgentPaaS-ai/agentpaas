package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"github.com/spf13/cobra"
)

// registryListFactory is overridden in tests.
var registryListFactory = defaultRegistryList

// registryShowFactory is overridden in tests.
var registryShowFactory = defaultRegistryShow

func defaultRegistryList(cmd *cobra.Command) ([]registry.RegistryEntry, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("default registry list: %w", err)
	}
	paths := home.NewHomePaths(homeDir)
	return registry.ListEntries(paths.State, nil)
}

func defaultRegistryShow(cmd *cobra.Command, ref string) (*registry.RegistryEntry, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("default registry show: %w", err)
	}
	paths := home.NewHomePaths(homeDir)
	return registry.ShowEntry(paths.State, ref, nil)
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
B26 deployment store; it does not require a running daemon.`,
		Example: `  agentpaas registry list
  agentpaas registry list --json
  agentpaas registry show weather@a1b2c3d4
  agentpaas registry show weather --json`,
	}
	cmd.AddCommand(newRegistryListCmd())
	cmd.AddCommand(newRegistryShowCmd())
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
