package cli

import (
	"fmt"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

var forkInstalledFunc = defaultForkInstalled

func defaultForkInstalled(cmd *cobra.Command, stateRoot, ref, targetDir string) error {
	var auditAppender audit.AuditAppender
	homeDir, err := homeDirPath(cmd)
	if err == nil {
		auditPath := filepath.Join(homeDir, "state", "audit.jsonl")
		if w, werr := audit.NewAuditWriter(auditPath); werr == nil {
			auditAppender = w
			defer func() { _ = w.Close() }() // best-effort close
		}
	}
	return install.ForkInstalled(stateRoot, ref, targetDir, auditAppender)
}

func newForkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork <installed-ref> <target-dir>",
		Short: "Fork an installed agent into an editable project",
		Long: `Fork copies the verified installed agent source tree and policy into a new
project directory and writes lineage.json for fork-aware pack/export.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, err := homeDirPath(cmd)
			if err != nil {
				return fmt.Errorf("new fork cmd: %w", err)
			}
			paths := home.NewHomePaths(homeDir)

			resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
				StateRoot: paths.State,
				Input:     args[0],
			})
			if err != nil {
				return fmt.Errorf("new fork cmd: %w", err)
			}
			if resolved == nil || !resolved.Installed {
				return fmt.Errorf("no installed agent at %q", args[0])
			}
			if warn := install.ForkPublisherWarning(paths.State, args[0]); warn != "" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), warn) // best-effort write
			}

			if err := forkInstalledFunc(cmd, paths.State, args[0], args[1]); err != nil {
				return fmt.Errorf("new fork cmd: %w", err)
			}

			agentName, err := install.ForkAgentNameFromRef(resolved.Ref)
			if err != nil {
				agentName = resolved.Ref
			}
			targetAbs, err := filepath.Abs(args[1])
			if err != nil {
				targetAbs = args[1]
			}
			cmd.Printf("Forked %s to %s\n", agentName, targetAbs)
			cmd.Printf("Next steps: edit the project, then run:\n")
			cmd.Printf("  agentpaas pack   (in %s)\n", targetAbs)
			cmd.Printf("  agentpaas export (in %s)\n", targetAbs)
			cmd.Println("Hint: consider bumping agent.version or renaming in agent.yaml to distinguish your fork.")
			return nil
		},
	}
	return cmd
}
