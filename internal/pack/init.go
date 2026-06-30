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

// InitFromCode reconciles an agent.yaml from existing source files.
// If agent.yaml exists, it is left untouched. If not, a minimal one is
// created with the detected runtime and agent name derived from the dir.
func InitFromCode(projectDir string, runtime RuntimeType) error {
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
		"version: \"1\"\nruntime: %s\nname: %s\ndescription: \"\"\n",
		runtime,
		name,
	)

	return writeNewProjectFile(agentPath, content)
}

// InitPolicy writes a minimal default-deny policy.yaml if one does not exist.
// If policy.yaml already exists, it is left untouched (never overwrite policy).
func InitPolicy(projectDir string) error {
	if err := validateProjectDir(projectDir); err != nil {
		return err
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return err
	}

	policyPath := filepath.Join(projectDir, "policy.yaml")
	if err := rejectSymlinkPath(policyPath, true); err != nil {
		return err
	}
	if info, err := os.Lstat(policyPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("policy.yaml exists but is not a regular file (mode: %s)", info.Mode())
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect policy.yaml: %w", err)
	}

	const content = `version: "1"
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
	fmt.Fprintf(&b, "# llm:\n")
	fmt.Fprintf(&b, "#   provider: openai  # openai|anthropic|xai\n")
	fmt.Fprintf(&b, "#   model: gpt-4o\n")
	fmt.Fprintf(&b, "#   credential: openai-key  # Keychain secret name (agentpaas secret add openai-key)\n")

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
