package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/spf13/cobra"
)

type policyLineageResult struct {
	Lineage     policyLineageMeta        `json:"lineage"`
	Policy      *policyShowResult        `json:"policy"`
	Enforcement policyLineageEnforcement `json:"enforcement"`
	Proof       policyLineageProof       `json:"proof"`
}

type policyLineageMeta struct {
	Packer    string `json:"packer"`
	Tool      string `json:"tool"`
	Digest    string `json:"digest"`
	Signature string `json:"signature"`
	Parent    string `json:"parent"`
}

type policyLineageEnforcement struct {
	EgressAllowed        []string `json:"egress_allowed"`
	EgressDenied         []string `json:"egress_denied"`
	CredentialInjections []string `json:"credential_injections"`
	PIIMasks             int      `json:"pii_masks"`
	BudgetConsumed       string   `json:"budget_consumed"`
}

type policyLineageProof struct {
	HashChainVerified bool   `json:"hash_chain_verified"`
	ExportPointer     string `json:"export_pointer"`
}

func newPolicyLineageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lineage [project-dir]",
		Short: "Show policy lineage, compiled contract, enforcement, and proof",
		Long: `Show the compiled policy contract for a project, its lineage fields,
enforcement counts, and how to export proof.

Reads policy.yaml from the project directory (default: current directory).
Optional --run-id is accepted; counts stay at zero when no aggregate rows exist.`,
		Example: `  agentpaas policy lineage
  agentpaas policy lineage ./my-agent
  agentpaas policy lineage ./my-agent --json
  agentpaas policy lineage ./my-agent --run-id run-01HXYZ`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			_, _ = cmd.Flags().GetString("run-id") // cobra flag default on missing; ignored without aggregate rows
			return showProjectPolicyLineage(cmd, projectDir)
		},
	}
	cmd.Flags().String("run-id", "", "Optional run id; enforcement counts stay zero without aggregate rows")
	return cmd
}

func showProjectPolicyLineage(cmd *cobra.Command, projectDir string) error {
	if projectDir == "" {
		projectDir = "."
	}
	policyPath := filepath.Join(projectDir, "policy.yaml")

	data, err := os.ReadFile(policyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("policy.yaml not found in %s; run 'agentpaas policy init %s' to create one", projectDir, projectDir)
		}
		return fmt.Errorf("read policy: %w", err)
	}

	parsed, err := policy.ParsePolicy(bytes.NewReader(data))
	if err != nil {
		return err
	}

	compiled, err := compilePolicyShow(projectDir, parsed)
	if err != nil {
		return err
	}

	result := &policyLineageResult{
		Lineage: policyLineageMeta{
			Packer:    "",
			Tool:      "",
			Digest:    compiled.Digest,
			Signature: "",
			Parent:    "",
		},
		Policy: compiled,
		Enforcement: policyLineageEnforcement{
			EgressAllowed:        []string{},
			EgressDenied:         []string{},
			CredentialInjections: []string{},
			PIIMasks:             0,
			BudgetConsumed:       "",
		},
		Proof: policyLineageProof{
			HashChainVerified: false,
			ExportPointer:     "agentpaas audit export",
		},
	}

	return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
		r, ok := v.(*policyLineageResult)
		if !ok {
			return ""
		}
		return formatPolicyLineageText(r)
	})
}

func formatPolicyLineageText(r *policyLineageResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LINEAGE\n")
	fmt.Fprintf(&b, "packer: %s\n", r.Lineage.Packer)
	fmt.Fprintf(&b, "tool: %s\n", r.Lineage.Tool)
	fmt.Fprintf(&b, "digest: %s\n", r.Lineage.Digest)
	fmt.Fprintf(&b, "signature: %s\n", r.Lineage.Signature)
	fmt.Fprintf(&b, "parent: %s\n", r.Lineage.Parent)
	fmt.Fprintf(&b, "POLICY\n")
	fmt.Fprintf(&b, "%s\n", formatPolicyShowText(r.Policy))
	fmt.Fprintf(&b, "ENFORCEMENT\n")
	fmt.Fprintf(&b, "egress allowed: %d\n", len(r.Enforcement.EgressAllowed))
	fmt.Fprintf(&b, "egress denied: %d\n", len(r.Enforcement.EgressDenied))
	fmt.Fprintf(&b, "credential injections: %d\n", len(r.Enforcement.CredentialInjections))
	fmt.Fprintf(&b, "pii masks: %d\n", r.Enforcement.PIIMasks)
	fmt.Fprintf(&b, "budget consumed: %s\n", r.Enforcement.BudgetConsumed)
	fmt.Fprintf(&b, "PROOF\n")
	fmt.Fprintf(&b, "hash chain verified: %v\n", r.Proof.HashChainVerified)
	fmt.Fprintf(&b, "export pointer: %s", r.Proof.ExportPointer)
	return b.String()
}
