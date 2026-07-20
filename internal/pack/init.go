package pack

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// InitScaffold creates a new agent project in the given directory.
// If the directory does not exist, it is created.
// If agent.yaml already exists, return an error (don't overwrite).
// Files created:
//   - agent.yaml (minimal template with name, version, runtime)
//   - main.py (entry point stub using @agent.on_invoke SDK pattern)
//   - requirements.txt (empty, with a comment)
//   - .agentpaasignore (default excludes)
//
// Policy is created separately by InitPolicy or `policy init`.
func InitScaffold(projectDir string, runtime RuntimeType) error {
	if err := validateProjectDir(projectDir); err != nil {
		return fmt.Errorf("init scaffold: %w", err)
	}
	if runtime == "" || runtime == RuntimeUnknown {
		runtime = RuntimePython
	}
	if runtime != RuntimePython && runtime != RuntimeLangGraph && runtime != RuntimeCrewAI {
		return fmt.Errorf("unsupported runtime: %s", runtime)
	}
	if err := rejectSymlinkPath(projectDir, true); err != nil {
		return fmt.Errorf("init scaffold: %w", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return fmt.Errorf("init scaffold: %w", err)
	}

	agentPath := filepath.Join(projectDir, "agent.yaml")
	if err := rejectSymlinkPath(agentPath, true); err != nil {
		return fmt.Errorf("init scaffold: %w", err)
	}
	if _, err := os.Lstat(agentPath); err == nil {
		return fmt.Errorf("agent.yaml already exists in %s", projectDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect agent.yaml: %w", err)
	}

	// Derive agent name from the project directory basename.
	projectName := filepath.Base(projectDir)
	if projectName == "." || projectName == "/" || projectName == "" {
		projectName = "agent"
	}

	files := map[string]string{
		"agent.yaml":       DefaultAgentYAML(runtime, projectName),
		"main.py":          DefaultMainPy(),
		"requirements.txt": "# Python dependencies (pip-installed at pack time).\n# Do NOT list agentpaas-sdk here — it is bundled automatically.\n",
		".agentpaasignore": DefaultAgentPaasIgnore(),
	}
	for name, content := range files {
		if err := writeNewProjectFile(filepath.Join(projectDir, name), content); err != nil {
			return fmt.Errorf("init scaffold: %w", err)
		}
	}

	return nil
}

// InitFromCode reconciles an agent.yaml from existing source files.
// If agent.yaml exists, it is left untouched. If not, a minimal one is
// created with the detected runtime and agent name derived from the dir.
func InitFromCode(projectDir string, runtime RuntimeType) error {
	if err := validateProjectDir(projectDir); err != nil {
		return fmt.Errorf("init from code: %w", err)
	}
	if runtime == "" || runtime == RuntimeUnknown {
		runtime = RuntimePython
	}
	if runtime != RuntimePython && runtime != RuntimeLangGraph && runtime != RuntimeCrewAI {
		return fmt.Errorf("unsupported runtime: %s", runtime)
	}
	if err := rejectSymlinkPath(projectDir, true); err != nil {
		return fmt.Errorf("init from code: %w", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return fmt.Errorf("init from code: %w", err)
	}

	agentPath := filepath.Join(projectDir, "agent.yaml")
	if err := rejectSymlinkPath(agentPath, true); err != nil {
		return fmt.Errorf("init from code: %w", err)
	}
	if _, err := os.Lstat(agentPath); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect agent.yaml: %w", err)
	}

	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	name := sanitizeAgentName(filepath.Base(absPath))
	content := fmt.Sprintf(
		"version: \"1.0\"\nruntime: %s\nname: %s\ndescription: \"\"\n",
		runtime,
		name,
	)

	return writeNewProjectFile(agentPath, content)
}

// InitPolicy writes a minimal default-deny policy.yaml if one does not exist.
// If policy.yaml already exists, it is left untouched (never overwrite policy).
func InitPolicy(projectDir string) error {
	if err := validateProjectDir(projectDir); err != nil {
		return fmt.Errorf("init policy: %w", err)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return fmt.Errorf("init policy: %w", err)
	}

	policyPath := filepath.Join(projectDir, "policy.yaml")
	if err := rejectSymlinkPath(policyPath, true); err != nil {
		return fmt.Errorf("init policy: %w", err)
	}
	if info, err := os.Lstat(policyPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("policy.yaml exists but is not a regular file (mode: %s)", info.Mode())
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect policy.yaml: %w", err)
	}

	const content = `version: "1.0"
agent:
  name: ""
  description: ""
egress: []
credentials: []
mcp_servers: []
hooks: []
ingress: []
`

	return writeNewProjectFile(policyPath, content)
}

func sanitizeAgentName(name string) string {
	var b strings.Builder
	lastHyphen := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if b.Len() > 0 && !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}

	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return "agent"
	}

	return sanitized
}

// DefaultAgentYAML returns the minimal agent.yaml content for scaffolding.
// The agent name is derived from the project directory basename.
func DefaultAgentYAML(runtime RuntimeType, projectName string) string {
	runtimeValue := "python3.12"
	switch runtime {
	case RuntimeLangGraph:
		runtimeValue = "langgraph"
	case RuntimeCrewAI:
		runtimeValue = "crewai"
	}

	if projectName == "" {
		projectName = "agent"
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "name: %s\n", projectName)
	fmt.Fprintf(&b, "version: 0.1.0\n")
	fmt.Fprintf(&b, "runtime: %s\n", runtimeValue)
	fmt.Fprintf(&b, "description: \"\"\n")
	fmt.Fprintf(&b, "# llm:\n")
	fmt.Fprintf(&b, "#   provider: openrouter  # openrouter|openai|anthropic|xai|nous\n")
	fmt.Fprintf(&b, "#   model: deepseek/deepseek-v4-flash\n")
	fmt.Fprintf(&b, "#   credential: openrouter-key  # Keychain secret name (agentpaas secret add openrouter-key)\n")

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
// Uses the AgentPaaS SDK @agent.on_invoke pattern — plain app()/main()
// functions will NOT work with the harness.
func DefaultMainPy() string {
	return `from agentpaas_sdk import agent


@agent.on_invoke
def handle_invoke(payload):
    """Called when the agent is invoked. payload is a dict from the trigger."""
    return {"status": "OK"}
`
}

func writeNewProjectFile(path string, content string) error {
	if err := rejectSymlinkPath(path, true); err != nil {
		return fmt.Errorf("write new project file: %w", err)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = file.Close() }() // best-effort close

	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
