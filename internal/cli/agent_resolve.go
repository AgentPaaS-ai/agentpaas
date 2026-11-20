package cli

import (
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

func resolveCLIAgentRef(cmd *cobra.Command, input string) (*install.ResolvedAgent, error) {
	homeDir, err := getAgentpaasHome(cmd)
	if err != nil {
		return nil, fmt.Errorf("resolve cliagent ref: %w", err)
	}
	paths := home.NewHomePaths(homeDir)
	return install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: paths.State,
		Input:     input,
		Infof: func(format string, args ...any) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), format, args...) // best-effort write
		},
	})
}
