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

// policyValidateResult wraps the compiled policy show contract with local
// ValidatePolicy findings. Embedding inlines the show JSON tags.
type policyValidateResult struct {
	Valid    bool                     `json:"valid"`
	Errors   []policy.ValidationError `json:"errors,omitempty"`
	Warnings []policy.ValidationError `json:"warnings,omitempty"`
	*policyShowResult
}

func newPolicyValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [project-dir]",
		Short: "Compile and validate a project policy.yaml locally",
		Long: `Parse and validate policy.yaml on this machine without the daemon.

Reads <project-dir>/policy.yaml (default: current directory), compiles the same
contract as 'policy show', and reports ValidatePolicy errors and warnings.`,
		Example: `  agentpaas policy validate
  agentpaas policy validate ./my-agent
  agentpaas policy validate ./my-agent --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			return validateProjectPolicy(cmd, projectDir)
		},
	}
}

func validateProjectPolicy(cmd *cobra.Command, projectDir string) error {
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

	var errs, warns []policy.ValidationError
	for _, finding := range policy.ValidatePolicy(parsed) {
		if finding.Severity == "error" {
			errs = append(errs, finding)
			continue
		}
		warns = append(warns, finding)
	}

	result := &policyValidateResult{
		Valid:            len(errs) == 0,
		Errors:           errs,
		Warnings:         warns,
		policyShowResult: compiled,
	}

	if err := printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
		r, ok := v.(*policyValidateResult)
		if !ok {
			return ""
		}
		return formatPolicyValidateText(r)
	}); err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf("policy validation failed")
	}
	return nil
}

func formatPolicyValidateText(r *policyValidateResult) string {
	var b strings.Builder
	if r.Valid {
		b.WriteString("valid: true\n")
	} else {
		b.WriteString("valid: false\n")
	}
	if r.policyShowResult != nil {
		b.WriteString(formatPolicyShowText(r.policyShowResult))
		b.WriteByte('\n')
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: %s\n", e.Error())
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w.Error())
	}
	return strings.TrimRight(b.String(), "\n")
}
