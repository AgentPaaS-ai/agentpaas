package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/spf13/cobra"
)

// newPolicyInitCmd creates the `agent policy init` subcommand.
func newPolicyInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [project-dir]",
		Short: "Scaffold a policy.yaml from a named template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}

			templateName, _ := cmd.Flags().GetString("template") // cobra flag default on missing
			providerName, _ := cmd.Flags().GetString("provider") // cobra flag default on missing
			noninteractive, _ := cmd.Flags().GetBool("noninteractive") // cobra flag default on missing
			force, _ := cmd.Flags().GetBool("force") // cobra flag default on missing

			// Resolve the project directory to an absolute path.
			resolvedDir, err := filepath.Abs(projectDir)
			if err != nil {
				return fmt.Errorf("resolve project directory: %w", err)
			}
			projectDir = resolvedDir

			// Validate --provider is only used with allow-llm.
			if providerName != "" && templateName != "" && templateName != "allow-llm" {
				return fmt.Errorf("--provider can only be used with --template allow-llm")
			}

			// Determine the template.
			if templateName != "" {
				if _, ok := policyTemplate(templateName); !ok {
					return fmt.Errorf("unknown policy template %q (valid: %s)",
						templateName, strings.Join(policyTemplateNames(), ", "))
				}
			} else if noninteractive {
				templateName = "deny-all"
			} else {
				// Interactive selection.
				templateName, err = promptPolicyTemplate(cmd)
				if err != nil {
					return err
				}
			}

			// If provider is specified with allow-llm, generate the template dynamically.
			var content string
			if providerName != "" && templateName == "allow-llm" {
				domain := llm.ProviderDomain(providerName)
				if domain == "" {
					return fmt.Errorf("unknown provider %q (valid: %s)",
						providerName, strings.Join(llm.SupportedProviders(), ", "))
				}
				content = buildAllowLLMTemplate(domain)
			} else {
				content, _ = policyTemplate(templateName) // optional value; zero on miss
			}

			// Write the policy file.
			policyPath := filepath.Join(projectDir, "policy.yaml")
			if err := writePolicyFile(policyPath, content, force); err != nil {
				return err
			}

			absPath, err := filepath.Abs(policyPath)
			if err != nil {
				absPath = policyPath
			}

			result := struct {
				Template string `json:"template"`
				Path     string `json:"path"`
			}{
				Template: templateName,
				Path:     absPath,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					Template string `json:"template"`
					Path     string `json:"path"`
				})
				return fmt.Sprintf("Wrote policy.yaml (%s template) to %s", r.Template, r.Path)
			})
		},
	}
	cmd.Flags().String("template", "", "Policy template name: deny-all, allow-http, allow-llm, allow-mcp")
	cmd.Flags().String("provider", "", "LLM provider for allow-llm template: openrouter, openai, anthropic, xai, nous")
	cmd.Flags().Bool("noninteractive", false, "Skip prompt and use deny-all template (default-deny)")
	cmd.Flags().Bool("force", false, "Overwrite existing policy.yaml")
	return cmd
}

// buildAllowLLMTemplate generates an allow-llm policy template YAML
// with the specified provider domain as the egress destination.
func buildAllowLLMTemplate(domain string) string {
	return fmt.Sprintf(`version: "1.0"
agent:
  name: ""
  description: ""
egress:
  - domain: %s
    ports:
      - 443
credentials: []
mcp_servers: []
hooks: []
ingress: []
`, domain)
}

// promptPolicyTemplate shows a numbered list of templates on stdout and reads
// the user's selection from stdin.
func promptPolicyTemplate(cmd *cobra.Command) (string, error) {
	names := policyTemplateNames()

	out := cmd.OutOrStdout()
	for i, name := range names {
		if _, err := fmt.Fprintf(out, "[%d] %s\n", i+1, name); err != nil {
			return "", fmt.Errorf("write prompt: %w", err)
		}
	}
	if _, err := fmt.Fprint(out, "Select template (1-", len(names), "): "); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}

	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read selection: %w", err)
	}
	line = strings.TrimSpace(line)

	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(names) {
		return "", fmt.Errorf("invalid selection: %q (enter 1-%d)", line, len(names))
	}

	return names[n-1], nil
}

// writePolicyFile writes the policy content to the given path.
// If force is false, it refuses to overwrite an existing file.
func writePolicyFile(path string, content string, force bool) error {
	// Ensure the directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("policy.yaml exists but is not a regular file (mode: %s)", info.Mode())
		}
		if !force {
			return fmt.Errorf("policy.yaml already exists in %s (use --force to overwrite)", dir)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect policy.yaml: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write policy.yaml: %w", err)
	}

	return nil
}