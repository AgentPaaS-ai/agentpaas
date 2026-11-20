package cli

import (
	"fmt"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/spf13/cobra"
)

func newProvenanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provenance",
		Short: "Show provenance chains for installed agents or bundles",
		Long: `Inspect provenance chains that record how an agent was built and forked.

Works offline for .agentpaas files and against installed agent state.
No daemon required.`,
		Example: `  agentpaas provenance show weather@a1b2c3d4
  agentpaas provenance show ./weather-agent.agentpaas
  agentpaas provenance show weather --json`,
	}
	cmd.AddCommand(newProvenanceShowCmd())
	return cmd
}

func newProvenanceShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <installed-ref-or-bundle-path>",
		Short: "Render provenance report from an installed lock or bundle file",
		Long: `Show the provenance report for a materialized install (name@pub8, alias,
or Phase-1 bare name) or for a .agentpaas bundle file.

For bundles, integrity is verified first. Exits non-zero if the provenance
chain is missing or invalid.`,
		Example: `  agentpaas provenance show weather@a1b2c3d4
  agentpaas provenance show ./weather-agent.agentpaas
  agentpaas provenance show weather --json`,
		Args: cobra.ExactArgs(1),
		RunE: runProvenanceShow,
	}
	return cmd
}

func runProvenanceShow(cmd *cobra.Command, args []string) error {
	arg := args[0]
	jsonOut := jsonOutput(cmd)

	if install.IsBundleFileArg(arg) {
		return provenanceShowBundle(cmd, arg, jsonOut)
	}
	return provenanceShowInstalled(cmd, arg, jsonOut)
}

func provenanceShowBundle(cmd *cobra.Command, path string, jsonOut bool) error {
	b, err := bundle.Open(path)
	if err != nil {
		return fmt.Errorf("open bundle: %w", err)
	}
	defer func() { _ = b.Close() }() // best-effort close

	verifyReport, err := bundle.Verify(b)
	if err != nil {
		return fmt.Errorf("verify bundle: %w", err)
	}
	if !verifyReport.Verified {
		return fmt.Errorf("bundle integrity verification failed")
	}

	report, err := bundle.Inspect(path, b, verifyReport)
	if err != nil {
		return fmt.Errorf("inspect bundle: %w", err)
	}
	if report.Provenance == nil {
		return fmt.Errorf("bundle has no provenance section")
	}
	if !report.Provenance.Verified {
		return fmt.Errorf("provenance chain invalid")
	}

	if jsonOut {
		return printTextOrJSON(true, report.Provenance, nil)
	}
	_, err = fmt.Fprint(os.Stdout, report.ProvenanceText)
	return err
}

func provenanceShowInstalled(cmd *cobra.Command, ref string, jsonOut bool) error {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return fmt.Errorf("provenance show installed: %w", err)
	}
	stateRoot := home.NewHomePaths(homeDir).State

	provReport, err := install.ReadInstalledProvenanceReport(stateRoot, ref)
	if err != nil {
		return fmt.Errorf("provenance show installed: %w", err)
	}

	if jsonOut {
		return printTextOrJSON(true, provReport, nil)
	}
	_, err = fmt.Fprint(os.Stdout, pack.FormatProvenance(provReport))
	return err
}
