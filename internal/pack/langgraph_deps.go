package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateLangGraphLibraryDeps rejects langgraph-server and checkpointer
// packages at pack time (M16.2 SC4 / D150). Library langgraph is allowed.
func ValidateLangGraphLibraryDeps(projectDir string) error {
	files := []string{"requirements.txt", "pyproject.toml", "uv.lock", "poetry.lock"}
	var parts []string
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(projectDir, name))
		if err != nil {
			continue
		}
		parts = append(parts, string(data))
	}
	if len(parts) == 0 {
		return nil
	}
	body := strings.ToLower(strings.Join(parts, "\n"))
	for _, pkg := range []string{"langgraph-server", "langgraph-api", "langgraph.server"} {
		if strings.Contains(body, pkg) {
			return fmt.Errorf("pack rejects %s (langgraph-server is not library mode)", pkg)
		}
	}
	for _, pkg := range []string{"langgraph-checkpoint", "langgraph-checkpoint-sqlite"} {
		if strings.Contains(body, pkg) {
			return fmt.Errorf("pack rejects checkpointer package %s", pkg)
		}
	}
	return nil
}
