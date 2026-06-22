package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// InitScaffold creates a new agent project in the given directory.
// If the directory does not exist, it is created.
// If agent.yaml already exists, return an error (don't overwrite).
// Files created:
//   - agent.yaml (minimal template with name, version, runtime, entry)
//   - main.py (entry point stub: def app(input): return {"status":"ok"})
//   - requirements.txt (empty, with a comment)
//   - .agentpaasignore (default excludes)
func InitScaffold(projectDir string, runtime RuntimeType) error {
	if err := validateProjectDir(projectDir); err != nil {
		return err
	}
	if runtime == "" || runtime == RuntimeUnknown {
		runtime = RuntimePython
	}
	if runtime != RuntimePython && runtime != RuntimeLangGraph && runtime != RuntimeCrewAI {
		return fmt.Errorf("unsupported runtime: %s", runtime)
	}
	if err := rejectSymlinkPath(projectDir, true); err != nil {
		return err
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return err
	}

	agentPath := filepath.Join(projectDir, "agent.yaml")
	if err := rejectSymlinkPath(agentPath, true); err != nil {
		return err
	}
	if _, err := os.Lstat(agentPath); err == nil {
		return fmt.Errorf("agent.yaml already exists in %s", projectDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect agent.yaml: %w", err)
	}

	files := map[string]string{
		"agent.yaml":       DefaultAgentYAML(runtime),
		"main.py":          DefaultMainPy(),
		"requirements.txt": "# Add Python dependencies here.\n",
		".agentpaasignore": DefaultAgentPaasIgnore(),
	}
	for name, content := range files {
		if err := writeNewProjectFile(filepath.Join(projectDir, name), content); err != nil {
			return err
		}
	}

	return nil
}

// DefaultAgentYAML returns the minimal agent.yaml content for scaffolding.
func DefaultAgentYAML(runtime RuntimeType) string {
	runtimeValue := "python3.12"
	switch runtime {
	case RuntimeLangGraph:
		runtimeValue = "langgraph"
	case RuntimeCrewAI:
		runtimeValue = "crewai"
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "name: agent\n")
	fmt.Fprintf(&b, "version: 0.1.0\n")
	fmt.Fprintf(&b, "runtime: %s\n", runtimeValue)
	fmt.Fprintf(&b, "entry: main:app\n")

	return b.String()
}

// DefaultAgentPaasIgnore returns the default .agentpaasignore content.
func DefaultAgentPaasIgnore() string {
	var b bytes.Buffer
	for _, pattern := range DefaultIgnorePatterns() {
		fmt.Fprintf(&b, "%s\n", pattern)
	}

	return b.String()
}

// DefaultMainPy returns the default main.py entry point stub.
func DefaultMainPy() string {
	return "def app(input):\n    return {\"status\": \"ok\"}\n"
}

func writeNewProjectFile(path string, content string) error {
	if err := rejectSymlinkPath(path, true); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
