package bundle

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

const LintChainAddsEgress = "chain_adds_egress"

// ComputeChainLints returns warnings when any provenance hop adds egress vs the original.
func ComputeChainLints(report *pack.ProvenanceReport) []PolicyLint {
	if report == nil || len(report.Entries) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var added []string
	for _, e := range report.Entries {
		if e.PolicyDelta == nil {
			continue
		}
		for _, dest := range e.PolicyDelta.EgressAdded {
			dest = strings.TrimSpace(dest)
			if dest == "" {
				continue
			}
			if _, ok := seen[dest]; ok {
				continue
			}
			seen[dest] = struct{}{}
			added = append(added, dest)
		}
	}
	if len(added) == 0 {
		return nil
	}
	sort.Strings(added)
	return []PolicyLint{{
		Code:    LintChainAddsEgress,
		Message: fmt.Sprintf("⚠ chain adds egress vs original: %s", strings.Join(added, ", ")),
	}}
}