package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/parvezsyed/agentpaas/internal/secrets"
	"github.com/spf13/cobra"
)

// stubRunE returns a RunE function that prints "not yet implemented".
func stubRunE(use string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		fmt.Printf("'agent %s' not yet implemented\n", use)
		return nil
	}
}

// newPackCmd creates the `agent pack` command (stub).
func newPackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pack [project-dir]",
		Short: "Build an agent image from a project directory (not yet implemented)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  stubRunE("pack"),
	}
}

// newRunCmd creates the `agent run` command (stub).
func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [image-or-project]",
		Short: "Start a new agent run (not yet implemented)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  stubRunE("run"),
	}
}

// newStopCmd creates the `agent stop` command (stub).
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <run-id>",
		Short: "Terminate a running agent (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("stop"),
	}
}

// newLogsCmd creates the `agent logs` command (stub).
func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <run-id>",
		Short: "Follow or query agent logs (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("logs"),
	}
}

// newPolicyCmd creates the `agent policy` command (stub).
func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage OPA/Rego policies (not yet implemented)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "apply <policy-file>",
		Short: "Apply or validate an OPA/Rego policy (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("policy apply"),
	})

	return cmd
}

var secretStoreFactory = newDefaultSecretStore

// newSecretCmd creates the `agent secret` command.
func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage local profile secrets",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "set <name>",
		Short: "Create or update a secret from stdin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return err
			}
			value, err := readSecretValue(cmd)
			if err != nil {
				return err
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return err
			}
			if err := store.Set(cmd.Context(), name, value); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "secret %q stored\n", name)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List secret metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return err
			}
			meta, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			return writeSecretList(cmd, meta)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := secrets.ValidateSecretName(name); err != nil {
				return err
			}
			store, err := secretStoreFactory(cmd)
			if err != nil {
				return err
			}
			if err := store.Delete(cmd.Context(), name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "secret %q removed\n", name)
			return nil
		},
	})

	return cmd
}

func newDefaultSecretStore(cmd *cobra.Command) (secrets.SecretStore, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, err
	}
	return secrets.NewKeychainStore(secretServiceName(homeDir))
}

func secretServiceName(homeDir string) string {
	sum := sha256.Sum256([]byte(homeDir))
	return "ai.agentpaas.secrets." + hex.EncodeToString(sum[:8])
}

func readSecretValue(cmd *cobra.Command) ([]byte, error) {
	in := cmd.InOrStdin()
	if isTerminal(in) {
		fmt.Fprint(cmd.ErrOrStderr(), "Secret value: ")
		reader := bufio.NewReader(io.LimitReader(in, secrets.MaxSecretValueSize+2))
		value, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read secret value: %w", err)
		}
		if len(value) > 0 && value[len(value)-1] == '\n' {
			value = value[:len(value)-1]
		}
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
		if len(value) > secrets.MaxSecretValueSize {
			return nil, fmt.Errorf("%w: exceeds %d byte limit", secrets.ErrSecretTooLarge, secrets.MaxSecretValueSize)
		}
		return value, nil
	}

	value, err := io.ReadAll(io.LimitReader(in, secrets.MaxSecretValueSize+1))
	if err != nil {
		return nil, fmt.Errorf("read secret value: %w", err)
	}
	if len(value) > secrets.MaxSecretValueSize {
		return nil, fmt.Errorf("%w: exceeds %d byte limit", secrets.ErrSecretTooLarge, secrets.MaxSecretValueSize)
	}
	return value, nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type secretListItem struct {
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
	ReferencedBy []string  `json:"referenced_by"`
}

func writeSecretList(cmd *cobra.Command, meta []secrets.SecretMeta) error {
	if jsonOutput(cmd) {
		items := make([]secretListItem, 0, len(meta))
		for _, m := range meta {
			items = append(items, secretListItem{
				Name:         m.Name,
				CreatedAt:    m.CreatedAt,
				UpdatedAt:    m.UpdatedAt,
				LastUsedAt:   m.LastUsedAt,
				ReferencedBy: []string{},
			})
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}

	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tCREATED_AT\tUPDATED_AT\tLAST_USED_AT\tREFERENCED_BY")
	for _, m := range meta {
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t-\n",
			m.Name,
			formatSecretTime(m.CreatedAt),
			formatSecretTime(m.UpdatedAt),
			formatSecretTime(m.LastUsedAt),
		)
	}
	return tw.Flush()
}

func formatSecretTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// newAuditCmd creates the `agent audit` command (stub).
func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query and export audit logs (not yet implemented)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "query [--since <time>] [--until <time>]",
		Short: "Query audit log entries (not yet implemented)",
		RunE:  stubRunE("audit query"),
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "export [--format <fmt>]",
		Short: "Export audit log entries (not yet implemented)",
		RunE:  stubRunE("audit export"),
	})

	return cmd
}

// newValidateCmd creates the `agent validate` command (stub).
func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <project-path>",
		Short: "Validate an agent project directory structure (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("validate"),
	}
}

// newSummarizeCmd creates the `agent summarize` command (stub).
func newSummarizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summarize <run-id>",
		Short: "Generate a natural-language summary of a completed run (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("summarize"),
	}
}

// newExplainFailureCmd creates the `agent explain-failure` command (stub).
func newExplainFailureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain-failure <run-id>",
		Short: "Analyze a failed run and return root cause (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("explain-failure"),
	}
}

// newExplainDenialCmd creates the `agent explain-denial` command (stub).
func newExplainDenialCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain-denial <destination>",
		Short: "Explain why a destination was denied by policy (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("explain-denial"),
	}
}

// newRecommendPatchCmd creates the `agent recommend-patch` command (stub).
func newRecommendPatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recommend-patch <desired-behavior>",
		Short: "Suggest a policy patch for a desired behavior (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("recommend-patch"),
	}
}

// newTimelineCmd creates the `agent timeline` command (stub).
func newTimelineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timeline <run-id>",
		Short: "Show chronological timeline of events for a run (not yet implemented)",
		Args:  cobra.ExactArgs(1),
		RunE:  stubRunE("timeline"),
	}
}

// newNextActionCmd creates the `agent next-action` command (stub).
func newNextActionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next-action",
		Short: "Recommend the next action based on current context (not yet implemented)",
		Args:  cobra.NoArgs,
		RunE:  stubRunE("next-action"),
	}
}
