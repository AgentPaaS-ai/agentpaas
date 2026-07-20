package daemon

import (
	"fmt"
	"os"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
)

func (s *controlServer) resolveDaemonAgentRef(input string) (daemonKey string, agentRefLabel string, err error) {
	if s.homePaths == nil {
		return input, "", nil
	}
	resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: s.homePaths.State,
		Input:     input,
		Infof: func(format string, args ...any) {
			_, _ = fmt.Fprintf(os.Stderr, format, args...) // best-effort write
		},
	})
	if err != nil {
		return "", "", err
	}
	label := ""
	if _, _, ok := install.ParseInstalledAgentDir(resolved.DaemonKey); ok { // intentionally ignored (reviewed)
		label = resolved.DaemonKey
	}
	return resolved.DaemonKey, label, nil
}