package export

import (
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ExportIgnoreMatcher returns an IgnoreMatcher for export that matches
// exactly what pack uses (LoadIgnore). The export source digest MUST
// match the pack build input digest, so we must use the same ignore
// matcher — not a custom one with extra patterns.
func ExportIgnoreMatcher(projectDir string) (*pack.IgnoreMatcher, error) {
	return pack.LoadIgnore(projectDir)
}