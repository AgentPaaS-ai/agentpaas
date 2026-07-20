package cli

import (
	"fmt"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/spf13/cobra"
)

func newBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Offline bundle operations (no daemon)",
		Long: `Commands for inspecting .agentpaas bundles without the daemon or trust store.
These operations are read-only and fully offline.`,
	}
	cmd.AddCommand(newBundleInspectCmd())
	return cmd
}

func newBundleInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <file>",
		Short: "Offline security review of a .agentpaas bundle",
		Long: `Inspect a bundle file without installing, trusting, or contacting the daemon.
Shows integrity checks, and when verification passes: publisher, provenance,
full policy summary, lints, requirements, and SBOM.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			b, err := bundle.Open(path)
			if err != nil {
				return fmt.Errorf("open bundle: %w", err)
			}
			defer func() { _ = b.Close() }() // best-effort close

			verifyReport, err := bundle.Verify(b)
			if err != nil {
				return fmt.Errorf("verify bundle: %w", err)
			}

			report, err := bundle.Inspect(path, b, verifyReport)
			if err != nil {
				return fmt.Errorf("inspect bundle: %w", err)
			}

			jsonOut, _ := cmd.Flags().GetBool("json") // cobra flag default on missing
			if !jsonOut {
				jsonOut = jsonOutput(cmd)
			}
			if jsonOut {
				if err := printTextOrJSON(true, report, nil); err != nil {
					return fmt.Errorf("new bundle inspect cmd: %w", err)
				}
			} else {
				_, _ = fmt.Fprint(os.Stdout, bundle.FormatInspectText(report)) // best-effort write
			}

			if !report.Verified {
				return fmt.Errorf("bundle integrity verification failed")
			}
			return nil
		},
	}
	return cmd
}
