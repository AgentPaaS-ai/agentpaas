package pack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/fsutil"
	"gopkg.in/yaml.v3"
)

// RuntimeType represents the detected agent runtime/framework.
type RuntimeType string

const (
	RuntimePython    RuntimeType = "python"
	RuntimeLangGraph RuntimeType = "langgraph"
	RuntimeCrewAI    RuntimeType = "crewai"
	RuntimeUnknown   RuntimeType = "unknown"
)

// LLMConfig defines the LLM provider and credential binding for the agent.
// This is used by the harness to route agent.llm() calls through the gateway
// as credentialed HTTP egress (Option B unified egress).
// In v0.3, Route is mutually exclusive with Provider, Model, and Credential.
type LLMConfig struct {
	Provider   string `yaml:"provider"`   // openai|anthropic|xai
	Model      string `yaml:"model"`      // e.g. "gpt-4o", "claude-sonnet-4", "grok-beta"
	Credential string `yaml:"credential"` // Keychain secret name (e.g. "openai-key")
	Route      string `yaml:"route"`      // v0.3: logical model route (mutually exclusive with provider/model/credential)
}

// AgentYAML is a minimal subset of agent.yaml fields needed for detection
// and packaging. The runtime field overrides auto-detection.
// Both flat fields and the v1 metadata/spec schema are supported.
type AgentYAML struct {
	Name        string    `yaml:"name"`
	Version     string    `yaml:"version"`
	Runtime     string    `yaml:"runtime"`
	Entry       string    `yaml:"entry"`
	Description string    `yaml:"description"`
	Kind        string    `yaml:"kind"` // v0.3: "worker" or "mcp_service" (legacy absence means worker)
	LLM         LLMConfig `yaml:"llm"`
	// MCPService is the mcp_service block for kind=mcp_service packages (v0.4).
	MCPService MCPServiceConfig `yaml:"mcp_service"`
	// Capabilities is additive optional metadata from the package manifest (B31-T01).
	// Stored verbatim; not schema-validated against other packages in v0.3.
	Capabilities []DeclaredCapability `yaml:"capabilities,omitempty"`
	Metadata     struct {
		Name        string `yaml:"name"`
		Version     string `yaml:"version"`
		Description string `yaml:"description"`
	} `yaml:"metadata"`
	Spec struct {
		Runtime    string `yaml:"runtime"`
		Entrypoint string `yaml:"entrypoint"`
		Entry      string `yaml:"entry"`
	} `yaml:"spec"`
}

// DeclaredCapability is a single capability entry from the agent.yaml manifest.
// Stored verbatim in the lockfile; not schema-matched in v0.3.
type DeclaredCapability struct {
	ID          string `json:"id" yaml:"id"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// MCPServiceConfig represents the mcp_service block in agent.yaml for
// kind: mcp_service packages (v0.4).
type MCPServiceConfig struct {
	Transport      string   `yaml:"transport"`       // Only "streamable_http" is supported in v0.4.
	Tools          []string `yaml:"tools"`           // Non-empty, unique tool names.
	MaxConcurrency int      `yaml:"max_concurrency"` // 1..32, default 1 if omitted.
}

func (agent *AgentYAML) normalize() {
	if agent == nil {
		return
	}
	if strings.TrimSpace(agent.Name) == "" {
		agent.Name = agent.Metadata.Name
	}
	if strings.TrimSpace(agent.Version) == "" {
		agent.Version = agent.Metadata.Version
	}
	if strings.TrimSpace(agent.Description) == "" {
		agent.Description = agent.Metadata.Description
	}
	if strings.TrimSpace(agent.Runtime) == "" {
		agent.Runtime = agent.Spec.Runtime
	}
	if strings.TrimSpace(agent.Entry) == "" {
		switch {
		case strings.TrimSpace(agent.Spec.Entrypoint) != "":
			agent.Entry = agent.Spec.Entrypoint
		case strings.TrimSpace(agent.Spec.Entry) != "":
			agent.Entry = agent.Spec.Entry
		}
	}
}

// DetectionResult holds the outcome of project type detection.
type DetectionResult struct {
	Runtime         RuntimeType `json:"runtime"`
	HasAgentYAML    bool        `json:"has_agent_yaml"`
	ProjectDir      string      `json:"project_dir"`
	ExplicitRuntime bool        `json:"explicit_runtime"`
}

// DetectProject examines a project directory and returns the runtime type.
// If agent.yaml exists and has a runtime: field, that overrides detection.
// Otherwise, scan requirements.txt, pyproject.toml, and .py files for
// langgraph or crewai imports.
func DetectProject(projectDir string) (*DetectionResult, error) {
	if err := validateProjectDir(projectDir); err != nil {
		return nil, fmt.Errorf("detect project: %w", err)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return nil, fmt.Errorf("detect project: %w", err)
	}

	result := &DetectionResult{
		Runtime:    RuntimeUnknown,
		ProjectDir: projectDir,
	}

	agentYAML, err := LoadAgentYAML(projectDir)
	if err != nil {
		return nil, fmt.Errorf("detect project: %w", err)
	}
	if agentYAML != nil {
		result.HasAgentYAML = true
		if strings.TrimSpace(agentYAML.Runtime) != "" {
			result.Runtime = resolveRuntime(agentYAML.Runtime)
			result.ExplicitRuntime = true
			return result, nil
		}
	}

	if runtime := scanDependencies(projectDir); runtime != RuntimeUnknown {
		result.Runtime = runtime
		return result, nil
	}
	if runtime := scanSourceFiles(projectDir); runtime != RuntimeUnknown {
		result.Runtime = runtime
		return result, nil
	}
	if hasPlainPythonMarker(projectDir) {
		result.Runtime = RuntimePython
	}

	return result, nil
}

// LoadAgentYAML reads and parses agent.yaml from the project directory.
// Returns nil, nil if agent.yaml does not exist (not an error).
func LoadAgentYAML(projectDir string) (*AgentYAML, error) {
	if err := validateProjectDir(projectDir); err != nil {
		return nil, fmt.Errorf("load agent yaml: %w", err)
	}

	path := filepath.Join(projectDir, "agent.yaml")
	data, err := readProjectFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load agent yaml: %w", err)
	}

	var agent AgentYAML
	if err := yaml.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("parse agent.yaml: %w", err)
	}
	agent.normalize()

	return &agent, nil
}

// resolveRuntime maps the agent.yaml runtime: string to a RuntimeType.
// "python3.12", "python3.11", "python" -> RuntimePython
// "langgraph" -> RuntimeLangGraph
// "crewai" -> RuntimeCrewAI
func resolveRuntime(s string) RuntimeType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "python", "python3.11", "python3.12":
		return RuntimePython
	case "langgraph":
		return RuntimeLangGraph
	case "crewai":
		return RuntimeCrewAI
	default:
		return RuntimeUnknown
	}
}

// scanDependencies checks requirements.txt and pyproject.toml for
// langgraph or crewai package dependencies.
func scanDependencies(projectDir string) RuntimeType {
	for _, name := range []string{"requirements.txt", "pyproject.toml"} {
		data, err := readProjectFile(filepath.Join(projectDir, name))
		if err != nil {
			continue
		}
		if runtime := markerRuntime(string(data)); runtime != RuntimeUnknown {
			return runtime
		}
	}

	return RuntimeUnknown
}

// scanSourceFiles scans .py files for "import langgraph" or "import crewai"
// or "from langgraph" or "from crewai" patterns. Reads at most the first
// 50 .py files to bound work.
func scanSourceFiles(projectDir string) RuntimeType {
	const maxFiles = 50

	filesRead := 0
	runtime := RuntimeUnknown
	err := filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || runtime != RuntimeUnknown || filesRead >= maxFiles {
			return err
		}
		if d.IsDir() {
			if path == projectDir {
				return nil
			}
			if err := rejectSymlinkPath(path, false); err != nil {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".py" {
			return nil
		}

		data, err := readProjectFile(path)
		if err != nil {
			return nil
		}
		filesRead++
		runtime = markerRuntime(string(data))

		return nil
	})
	if err != nil {
		return RuntimeUnknown
	}

	return runtime
}

func validateProjectDir(projectDir string) error {
	if strings.TrimSpace(projectDir) == "" {
		return errors.New("project directory is required")
	}
	if strings.ContainsRune(projectDir, 0) {
		return errors.New("project directory contains null byte")
	}

	normalized := strings.ToValidUTF8(projectDir, "")
	if normalized != projectDir {
		return errors.New("project directory contains invalid UTF-8")
	}
	for _, r := range normalized {
		if r < 0x20 || r > 0x7e {
			return fmt.Errorf("invalid project directory %q: non-ASCII or non-printable characters are not allowed", projectDir)
		}
	}

	for _, component := range strings.Split(normalized, string(filepath.Separator)) {
		if component == ".." {
			return fmt.Errorf("invalid project directory %q: path traversal is not allowed", projectDir)
		}
	}

	absProjectDir, err := filepath.Abs(normalized)
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}
	if !filepath.IsAbs(normalized) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get current directory: %w", err)
		}
		rel, err := filepath.Rel(cwd, absProjectDir)
		if err != nil {
			return fmt.Errorf("resolve project path relative to current directory: %w", err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid project directory %q: path traversal is not allowed", projectDir)
		}
	}
	if err := rejectSymlinkPath(absProjectDir, true); err != nil {
		return fmt.Errorf("validate project dir: %w", err)
	}

	return nil
}

func hasPlainPythonMarker(projectDir string) bool {
	for _, name := range []string{"main.py", "app.py", "requirements.txt"} {
		if err := rejectSymlinkPath(filepath.Join(projectDir, name), false); err == nil {
			return true
		}
	}

	return false
}

func markerRuntime(content string) RuntimeType {
	lowered := strings.ToLower(content)
	if strings.Contains(lowered, "langgraph") {
		return RuntimeLangGraph
	}
	if strings.Contains(lowered, "crewai") {
		return RuntimeCrewAI
	}

	return RuntimeUnknown
}

func readProjectFile(path string) ([]byte, error) {
	if err := rejectSymlinkPath(path, false); err != nil {
		return nil, fmt.Errorf("read project file: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return data, nil
}

func rejectSymlinkPath(path string, allowMissingLeaf bool) error {
	missing := fsutil.MissingFail
	if allowMissingLeaf {
		// Historical pack behavior: when allowMissingLeaf is set, any missing
		// component along the upward walk is tolerated (not only the leaf).
		missing = fsutil.MissingAllowAll
	}
	err := fsutil.RejectSymlinkWalk(path, fsutil.WalkOptions{
		ResolveAbs:             true,
		Missing:                missing,
		SkipVolumeRootSymlinks: true,
	})
	if err == nil {
		return nil
	}
	var se *fsutil.SymlinkError
	if errors.As(err, &se) {
		return fmt.Errorf("path component %s is a symlink (potential escape)", se.Path)
	}
	return err
}

// validMCPToolNameRegex matches the tool ID rules: [a-zA-Z][a-zA-Z0-9_.-]*
var validMCPToolNameRegex = mkToolNameRegex()

func mkToolNameRegex() *regexp.Regexp {
	return regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*$`)
}

// ValidateMCPServiceConfig validates the mcp_service block of an agent.yaml.
// Returns nil if the block is absent or valid; returns an error describing the
// first violation otherwise.
func ValidateMCPServiceConfig(agent *AgentYAML) error {
	if agent == nil {
		return nil
	}
	// If no mcp_service block and no kind=mcp_service, nothing to validate.
	if agent.Kind != "mcp_service" && agent.MCPService.Transport == "" && len(agent.MCPService.Tools) == 0 {
		return nil
	}
	// If kind=mcp_service, the mcp_service block is required.
	if agent.Kind == "mcp_service" {
		if agent.MCPService.Transport == "" {
			return fmt.Errorf("mcp_service.transport is required for kind=mcp_service")
		}
	}
	// If mcp_service block is present, kind must match (or be absent = legacy worker — then block is invalid).
	if agent.MCPService.Transport != "" || len(agent.MCPService.Tools) > 0 {
		if agent.Kind != "mcp_service" {
			return fmt.Errorf("mcp_service block requires kind: mcp_service")
		}
	}

	cfg := agent.MCPService

	// Transport must be "streamable_http" in v0.4.
	if cfg.Transport != "" && cfg.Transport != "streamable_http" {
		return fmt.Errorf("mcp_service.transport must be \"streamable_http\" in v0.4, got %q", cfg.Transport)
	}

	// Tools must be non-empty and unique.
	if len(cfg.Tools) == 0 {
		return fmt.Errorf("mcp_service.tools must be non-empty")
	}
	seen := make(map[string]bool, len(cfg.Tools))
	for _, t := range cfg.Tools {
		if t == "" {
			return fmt.Errorf("mcp_service.tools contains empty tool name")
		}
		if !validMCPToolNameRegex.MatchString(t) {
			return fmt.Errorf("mcp_service.tools contains invalid tool name %q (must match [a-zA-Z][a-zA-Z0-9_.-]*)", t)
		}
		if seen[t] {
			return fmt.Errorf("mcp_service.tools contains duplicate tool name %q", t)
		}
		seen[t] = true
	}

	// MaxConcurrency: 0 means default (1), validate range if set.
	if cfg.MaxConcurrency < 0 {
		return fmt.Errorf("mcp_service.max_concurrency must be >= 0, got %d", cfg.MaxConcurrency)
	}
	if cfg.MaxConcurrency > 32 {
		return fmt.Errorf("mcp_service.max_concurrency must be <= 32, got %d", cfg.MaxConcurrency)
	}

	return nil
}
