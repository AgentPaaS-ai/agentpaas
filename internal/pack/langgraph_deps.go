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
	files := []string{
		"requirements.txt", "pyproject.toml", "uv.lock", "poetry.lock",
		"Pipfile", "setup.py", "setup.cfg", "main.py",
	}
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
	norm := strings.ReplaceAll(body, "_", "-")

	if strings.Contains(body, "langgraph[server]") || strings.Contains(norm, "langgraph[server]") {
		return fmt.Errorf("pack rejects langgraph-server extra")
	}
	if strings.Contains(body, "langgraph[checkpoint]") || strings.Contains(norm, "langgraph[checkpoint]") {
		return fmt.Errorf("pack rejects langgraph checkpointer extra")
	}
	for _, pkg := range []string{"langgraph-server", "langgraph-api", "langgraph.server"} {
		if strings.Contains(body, pkg) || strings.Contains(norm, pkg) {
			return fmt.Errorf("pack rejects %s (langgraph-server is not library mode)", pkg)
		}
	}
	if strings.Contains(body, "langgraph-cli") || strings.Contains(norm, "langgraph-cli") {
		return fmt.Errorf("pack rejects langgraph-cli (langgraph-server companion)")
	}
	if strings.Contains(body, "langgraph-checkpoint") || strings.Contains(norm, "langgraph-checkpoint") {
		return fmt.Errorf("pack rejects checkpointer package langgraph-checkpoint")
	}
	if strings.Contains(body, "langgraph.checkpoint") ||
		strings.Contains(body, "memorysaver") ||
		strings.Contains(body, "checkpointer=") {
		return fmt.Errorf("pack rejects checkpointer in entrypoint (langgraph.checkpoint / MemorySaver)")
	}
	return nil
}
