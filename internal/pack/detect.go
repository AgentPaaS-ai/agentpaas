package pack

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

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
	Provider   string `yaml:"provider" json:"provider,omitempty"`     // openai|anthropic|xai
	Model      string `yaml:"model" json:"model,omitempty"`           // e.g. "gpt-4o", "claude-sonnet-4", "grok-beta"
	Credential string `yaml:"credential" json:"credential,omitempty"` // Keychain secret name (e.g. "openai-key")
	Route      string `yaml:"route" json:"route,omitempty"`           // v0.3: logical model route (mutually exclusive with provider/model/credential)
}

// AgentYAML is a minimal subset of agent.yaml fields needed for detection
// and packaging. The runtime field overrides auto-detection.
// Both flat fields and the v1 metadata/spec schema are supported.
type AgentYAML struct {
	Name        string    `yaml:"name" json:"name,omitempty"`
	Version     string    `yaml:"version" json:"version,omitempty"`
	Runtime     string    `yaml:"runtime" json:"runtime,omitempty"`
	Entry       string    `yaml:"entry" json:"entry,omitempty"`
	Description string    `yaml:"description" json:"description,omitempty"`
	Kind        string    `yaml:"kind" json:"kind,omitempty"` // v0.3: "worker" or "mcp_service" (legacy absence means worker)
	LLM         LLMConfig `yaml:"llm" json:"llm,omitempty"`
	// MCPService is the mcp_service block for kind=mcp_service packages (v0.4).
	MCPService MCPServiceConfig `yaml:"mcp_service" json:"mcp_service,omitempty"`
	// Capabilities is additive optional metadata from the package manifest (B31-T01).
	// Stored verbatim; not schema-validated against other packages in v0.3.
	Capabilities []DeclaredCapability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	// Delegates is an optional pack-time list of capability strings
	// (e.g. "phone_call"). Copied verbatim onto lock.agent_yaml and
	// lock.component_index. Distinct from Capabilities.
	Delegates []string `yaml:"delegates,omitempty" json:"delegates,omitempty"`
	// Egress is a string-list of allowed egress hosts stamped from policy.yaml
	// domains (and optionally declared in agent.yaml). String list only so
	// `egress: [example.com]` still unmarshals.
	Egress []string `yaml:"egress,omitempty" json:"egress,omitempty"`
	// MCPServers are client MCP servers declared in agent.yaml. Stamped
	// onto lock agent_yaml so hosted bind can grant matching kind=mcp deploys.
	MCPServers []MCPServerDecl `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
	// ComponentIndex is an optional pack-time overlay (tools/schemas/_meta).
	// It is compiled into lock.component_index and is not copied into agent_yaml.
	ComponentIndex map[string]interface{} `yaml:"component_index,omitempty" json:"-"`
	Metadata       struct {
		Name        string `yaml:"name" json:"name,omitempty"`
		Version     string `yaml:"version" json:"version,omitempty"`
		Description string `yaml:"description" json:"description,omitempty"`
	} `yaml:"metadata" json:"metadata,omitempty"`
	Spec struct {
		Runtime    string `yaml:"runtime" json:"runtime,omitempty"`
		Entrypoint string `yaml:"entrypoint" json:"entrypoint,omitempty"`
		Entry      string `yaml:"entry" json:"entry,omitempty"`
	} `yaml:"spec" json:"spec,omitempty"`
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
	Transport      string   `yaml:"transport" json:"transport,omitempty"`             // Only "streamable_http" is supported in v0.4.
	Tools          []string `yaml:"tools" json:"tools,omitempty"`                     // Non-empty, unique tool names.
	MaxConcurrency int      `yaml:"max_concurrency" json:"max_concurrency,omitempty"` // 1..32, default 1 if omitted.
}

// MCPServerDecl is a client MCP server named in agent.yaml. Hosted bind
// matches name to a same-tenant kind=mcp deploy. URL is optional: bind
// fills the hosted /v1/deployments/:id/mcp URL when absent.
type MCPServerDecl struct {
	Name         string            `yaml:"name" json:"name,omitempty"`
	Transport    string            `yaml:"transport" json:"transport,omitempty"`
	URL          string            `yaml:"url" json:"url,omitempty"`
	AllowedTools []string          `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	Type         string            `yaml:"type" json:"type,omitempty"`
	Disabled     interface{}       `yaml:"disabled" json:"disabled,omitempty"`
	Enabled      interface{}       `yaml:"enabled" json:"enabled,omitempty"`
	OAuth        interface{}       `yaml:"oauth" json:"oauth,omitempty"`
	Cwd          string            `yaml:"cwd" json:"cwd,omitempty"`
	Notes        string            `yaml:"notes" json:"notes,omitempty"`
	Command      string            `yaml:"command" json:"command,omitempty"`
	Args         []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Extra holds unknown mcp_servers[] siblings (env / timeout / secrets /
	// any other key) so the stamp is the whole subtree (SC8). Not a named
	// allowlist. json:"-" so encoding/json does not nest Extra; UnmarshalJSON
	// rehydrates unknown siblings after WriteAgentLock so persist is not a hole.
	Extra map[string]interface{} `yaml:"-" json:"-"`
	// rejected is set when the declared name is injected (newline / NUL /
	// control / ".." / unicode-dot). Unexported so it cannot be stamped.
	// LoadAgentYAML fail-closes when any entry is rejected.
	rejected bool
}

// mcpDotEquivalents maps unicode-dot / fullwidth-dot / ellipsis runes to
// ASCII "." so ".." traversal homoglyphs are rejected before stamp.
func mcpDotEquivalents(r rune) string {
	switch r {
	case '\u2024', '\uff0e', '\ufe52', '\uff61', '\u00b7', '\u2219', '\u22c5',
		'\u3002', '\u2027', '\u30fb', '\uff65', '\u06d4', '\u0701', '\u0702',
		'\ufe12', '\u2022', '\u2e31', '\ua4f8', '\ua60e',
		'\ua78f', '\u2e33', '\u1362', '\u166e', '\u1803', '\u1809',
		'\u16eb', '\u0387', '\u1427', '\u1c3e', '\u2e3c', '\u02d9',
		'\u0700':
		return "."
	case '\u2025':
		return ".."
	case '\u2026', '\u22ef':
		return "..."
	default:
		return string(r)
	}
}

// mcpInvisibleRune reports format / invisible glyphs that must not appear
// inside a stamped MCP name token: ZWSP/ZWNJ/ZWJ/BOM, leftover Cf (WJ,
// soft hyphen, CGJ, MVS, U+2061–U+2064, U+206A–U+206F), bidi marks /
// embeds / isolates (U+200E/U+200F, U+202A–U+202E, U+2066–U+2069,
// U+061C), variation selectors (U+FE00–U+FE0F), TAG (U+E0020–U+E007F),
// and interlinear annotation (U+FFF9–U+FFFB).
func mcpInvisibleRune(r rune) bool {
	switch {
	case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff':
		return true
	case r == '\u200e' || r == '\u200f' || r == '\u061c':
		return true
	case r >= '\u202a' && r <= '\u202e':
		return true
	case r == '\u2060' || r == '\u00ad' || r == '\u034f' || r == '\u180e':
		return true
	case r >= '\u2061' && r <= '\u2064':
		return true
	case r >= '\u2066' && r <= '\u206f':
		return true
	case r >= '\ufe00' && r <= '\ufe0f':
		return true
	case r >= '\U000E0020' && r <= '\U000E007F':
		return true
	case r >= '\ufff9' && r <= '\ufffb':
		return true
	default:
		return false
	}
}

// mcpNameHasInvisible reports whether name contains a format / bidi /
// variation-selector rune. Load/unmarshal fail-closes; stamp strips.
func mcpNameHasInvisible(name string) bool {
	for _, r := range name {
		if mcpInvisibleRune(r) {
			return true
		}
	}
	return false
}

// stripMCPInvisibleName drops invisible format runes so the signed token
// is the visible hosted slug.
func stripMCPInvisibleName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	changed := false
	for _, r := range name {
		if mcpInvisibleRune(r) {
			changed = true
			continue
		}
		b.WriteRune(r)
	}
	if !changed {
		return name
	}
	return b.String()
}

func mcpNameDotFold(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		b.WriteString(mcpDotEquivalents(r))
	}
	return b.String()
}

// mcpUnicodeSpaceRune reports Zs/Zl/Zp that TrimSpace would fold onto a
// different hosted slug. ASCII space stays trim-able. NBSP (U+00A0) is
// left to the existing T16 TrimSpace fold so r2 stays green.
func mcpUnicodeSpaceRune(r rune) bool {
	if r == ' ' || r == '\u00a0' {
		return false
	}
	return unicode.Is(unicode.Zs, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// mcpLeftoverFormatRune reports leftover format / combining / blank glyphs
// that are not already on the mcpInvisibleRune strip path. General category
// reject (not per-plane instance patches): leftover Cf/Mn, spacing combining
// Mc, enclosing Me, remaining default-ignorables, and blank So. Must
// fail-close, not strip-then-stamp.
func mcpLeftoverFormatRune(r rune) bool {
	if mcpInvisibleRune(r) {
		return false
	}
	switch {
	case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Mn, r),
		unicode.Is(unicode.Mc, r), unicode.Is(unicode.Me, r):
		return true
	case unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r):
		return true
	case mcpBlankSoRune(r):
		return true
	default:
		return false
	}
}

// mcpBlankSoRune reports leftover Other symbols that can hide or replace
// a hosted slug. General category So — not Control Pictures / Specials
// range patches. U+1D159 / U+237D fail-close like U+FFFC / U+2423.
func mcpBlankSoRune(r rune) bool {
	return unicode.Is(unicode.So, r)
}

func mcpNameHasLeftoverFormat(name string) bool {
	for _, r := range name {
		if mcpLeftoverFormatRune(r) {
			return true
		}
	}
	return false
}

func replaceLeftoverFormatRunes(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if mcpLeftoverFormatRune(r) {
			b.WriteRune('\uFFFD')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// mcpVisibleBlankRune reports leftover lookalike / blank / format glyphs
// outside the r6 Cf/Zs window. These must fail-close (not strip-then-stamp):
// Hangul fillers (U+115F / U+1160 / U+3164 / U+FFA0), Braille blank
// (U+2800), leftover TAG (U+E0001), ideographic VS supplement
// (U+E0100–U+E01EF), and leftover Cf/Mn not on the invisible-strip path.
func mcpVisibleBlankRune(r rune) bool {
	switch {
	case r == '\u115f' || r == '\u1160' || r == '\u3164' || r == '\uffa0':
		return true
	case r == '\u2800':
		return true
	case r == '\U000E0001':
		return true
	case r >= '\U000E0100' && r <= '\U000E01EF':
		return true
	case mcpLeftoverFormatRune(r):
		return true
	default:
		return false
	}
}

// mcpServerNameRejected reports names that must not be stamped: newline, NUL,
// other C0/C1 controls, unicode Zs/Zl/Zp (other than ASCII space / NBSP),
// leftover visible-blank / format glyphs (Hangul filler, Braille blank,
// VS17+, LANGUAGE TAG), ASCII "..", or any unicode-dot / fullwidth-dot /
// "..." homoglyph (including a single folded '.'). Invisible format runes
// are stripped before stamp rather than fail-closing the whole list.
func mcpServerNameRejected(name string) bool {
	if strings.Contains(mcpNameDotFold(name), "..") {
		return true
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return true
		}
		if mcpUnicodeSpaceRune(r) {
			return true
		}
		if mcpVisibleBlankRune(r) {
			return true
		}
		folded := mcpDotEquivalents(r)
		if folded != string(r) && strings.Contains(folded, ".") {
			return true
		}
	}
	return false
}

// UnmarshalYAML rejects newline / NUL / control / ".." / unicode-dot names
// before stamp. The glyph is dropped; rejected is set so LoadAgentYAML and
// the stamp path fail the whole list instead of signing a sibling.
func (s *MCPServerDecl) UnmarshalYAML(value *yaml.Node) error {
	type rawMCPServerDecl struct {
		Name         string            `yaml:"name"`
		Transport    string            `yaml:"transport"`
		URL          string            `yaml:"url"`
		AllowedTools []string          `yaml:"allowed_tools"`
		Type         string            `yaml:"type"`
		Disabled     interface{}       `yaml:"disabled"`
		Enabled      interface{}       `yaml:"enabled"`
		OAuth        interface{}       `yaml:"oauth"`
		Cwd          string            `yaml:"cwd"`
		Notes        string            `yaml:"notes"`
		Command      string            `yaml:"command"`
		Args         []string          `yaml:"args"`
		Headers      map[string]string `yaml:"headers"`
	}
	var raw rawMCPServerDecl
	if err := value.Decode(&raw); err != nil {
		return err
	}
	s.Name = raw.Name
	s.Transport = raw.Transport
	s.URL = raw.URL
	s.AllowedTools = raw.AllowedTools
	s.Type = raw.Type
	s.Disabled = raw.Disabled
	s.Enabled = raw.Enabled
	s.OAuth = raw.OAuth
	s.Cwd = raw.Cwd
	s.Notes = raw.Notes
	s.Command = raw.Command
	s.Args = raw.Args
	s.Headers = raw.Headers
	s.Extra = extraMCPServerSiblings(value)
	s.rejected = false
	if mcpServerNameRejected(s.Name) || mcpNameHasInvisible(s.Name) {
		s.rejected = true
		s.Name = ""
	}
	return nil
}

// UnmarshalJSON rehydrates named fields plus unknown siblings into Extra.
// encoding/json drops unknown keys; json:"-" on Extra is the persist hole
// unless we capture them here after WriteAgentLock.
func (s *MCPServerDecl) UnmarshalJSON(data []byte) error {
	type rawMCPServerDecl struct {
		Name         string            `json:"name"`
		Transport    string            `json:"transport"`
		URL          string            `json:"url"`
		AllowedTools []string          `json:"allowed_tools"`
		Type         string            `json:"type"`
		Disabled     interface{}       `json:"disabled"`
		Enabled      interface{}       `json:"enabled"`
		OAuth        interface{}       `json:"oauth"`
		Cwd          string            `json:"cwd"`
		Notes        string            `json:"notes"`
		Command      string            `json:"command"`
		Args         []string          `json:"args"`
		Headers      map[string]string `json:"headers"`
	}
	var raw rawMCPServerDecl
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Name = raw.Name
	s.Transport = raw.Transport
	s.URL = raw.URL
	s.AllowedTools = raw.AllowedTools
	s.Type = raw.Type
	s.Disabled = raw.Disabled
	s.Enabled = raw.Enabled
	s.OAuth = raw.OAuth
	s.Cwd = raw.Cwd
	s.Notes = raw.Notes
	s.Command = raw.Command
	s.Args = raw.Args
	s.Headers = raw.Headers
	var all map[string]interface{}
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	s.Extra = extraMCPServerSiblingsFromMap(all)
	return nil
}

var mcpServerNamedFields = map[string]struct{}{
	"name": {}, "transport": {}, "url": {}, "allowed_tools": {},
	"type": {}, "disabled": {}, "enabled": {}, "oauth": {},
	"cwd": {}, "notes": {}, "command": {}, "args": {}, "headers": {},
}

// extraMCPServerSiblings captures unknown mcp_servers[] keys so the stamp
// is the whole subtree. Named-field allowlist is not SC8.
func extraMCPServerSiblings(value *yaml.Node) map[string]interface{} {
	if value == nil {
		return nil
	}
	var all map[string]interface{}
	if err := value.Decode(&all); err != nil || len(all) == 0 {
		return nil
	}
	return extraMCPServerSiblingsFromMap(all)
}

func extraMCPServerSiblingsFromMap(all map[string]interface{}) map[string]interface{} {
	if len(all) == 0 {
		return nil
	}
	extra := make(map[string]interface{})
	for k, v := range all {
		if _, known := mcpServerNamedFields[k]; known {
			continue
		}
		extra[k] = v
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
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
	for i := range agent.MCPServers {
		if agent.MCPServers[i].rejected || mcpServerNameRejected(agent.MCPServers[i].Name) || mcpNameHasInvisible(agent.MCPServers[i].Name) {
			agent.MCPServers[i].rejected = true
			agent.MCPServers[i].Name = ""
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
	if mcpServersRejected(&agent) {
		return nil, fmt.Errorf("parse agent.yaml: mcp_servers name rejected")
	}

	if err := ValidateMCPServiceConfig(&agent); err != nil {
		return nil, fmt.Errorf("validate agent.yaml: %w", err)
	}

	return &agent, nil
}

func mcpServersRejected(agent *AgentYAML) bool {
	if agent == nil {
		return false
	}
	for _, s := range agent.MCPServers {
		if s.rejected {
			return true
		}
	}
	return false
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
