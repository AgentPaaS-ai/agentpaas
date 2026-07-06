package export

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// exportAlwaysIgnore are paths never included in an export bundle (D9).
var exportAlwaysIgnore = []string{
	".git/",
	".hermes/",
	"__pycache__/",
	".venv/",
}

// ExportIgnoreMatcher returns an IgnoreMatcher for export: project .agentpaasignore
// plus mandatory export exclusions.
func ExportIgnoreMatcher(projectDir string) (*pack.IgnoreMatcher, error) {
	ignorePath := filepath.Join(projectDir, ".agentpaasignore")
	var patterns []string
	patterns = append(patterns, exportAlwaysIgnore...)
	if data, err := os.ReadFile(ignorePath); err == nil && len(data) > 0 {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			patterns = append(patterns, line)
		}
	}
	return pack.NewIgnoreMatcher(strings.Join(patterns, "\n")), nil
}