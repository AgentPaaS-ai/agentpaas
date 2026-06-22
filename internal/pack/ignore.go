package pack

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// IgnoreMatcher implements .agentpaasignore pattern matching.
// It supports:
//   - Exact filename matches (e.g. ".git")
//   - Glob patterns (e.g. "*.pyc", "__pycache__")
//   - Directory patterns (e.g. "node_modules/")
//   - Comments (lines starting with #)
//   - Negation patterns (lines starting with !)
type IgnoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	value     string
	negated   bool
	directory bool
}

// LoadIgnore reads .agentpaasignore from projectDir and returns a matcher.
// If .agentpaasignore does not exist, returns a matcher with default excludes.
func LoadIgnore(projectDir string) (*IgnoreMatcher, error) {
	if err := validateProjectDir(projectDir); err != nil {
		return nil, err
	}

	data, err := readProjectFile(filepath.Join(projectDir, ".agentpaasignore"))
	if errors.Is(err, fs.ErrNotExist) {
		return NewIgnoreMatcher(strings.Join(DefaultIgnorePatterns(), "\n")), nil
	}
	if err != nil {
		return nil, err
	}

	return NewIgnoreMatcher(string(data)), nil
}

// NewIgnoreMatcher creates a matcher from the given .agentpaasignore content.
func NewIgnoreMatcher(content string) *IgnoreMatcher {
	matcher := &IgnoreMatcher{}
	for _, line := range strings.Split(content, "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}

		negated := false
		if strings.HasPrefix(pattern, "!") {
			negated = true
			pattern = strings.TrimSpace(strings.TrimPrefix(pattern, "!"))
			if pattern == "" {
				continue
			}
		}

		directory := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(pattern, "/")
		pattern = normalizeIgnorePath(pattern)
		if pattern == "" || pattern == "." {
			continue
		}

		matcher.patterns = append(matcher.patterns, ignorePattern{
			value:     pattern,
			negated:   negated,
			directory: directory,
		})
	}

	return matcher
}

// Match returns true if the given path should be ignored (excluded from
// build context).
func (m *IgnoreMatcher) Match(filePath string) bool {
	if m == nil || strings.TrimSpace(filePath) == "" {
		return false
	}

	normalized := normalizeIgnorePath(filePath)
	if normalized == "" || normalized == "." {
		return false
	}

	ignored := false
	for _, pattern := range m.patterns {
		if pattern.matches(normalized) {
			ignored = !pattern.negated
		}
	}

	return ignored
}

// DefaultIgnorePatterns returns the default exclude patterns used when
// .agentpaasignore is absent.
func DefaultIgnorePatterns() []string {
	return []string{
		".git",
		"__pycache__",
		"*.pyc",
		".venv",
		"venv",
		"node_modules",
		".pytest_cache",
		".mypy_cache",
		".ruff_cache",
		"dist",
		"build",
		"*.egg-info",
		".env",
		".DS_Store",
	}
}

func (p ignorePattern) matches(filePath string) bool {
	if p.directory {
		return matchesPathComponent(filePath, p.value)
	}
	if strings.ContainsAny(p.value, "*?[") {
		if ok, _ := path.Match(p.value, path.Base(filePath)); ok {
			return true
		}
		if ok, _ := path.Match(p.value, filePath); ok {
			return true
		}
	}
	if p.value == filePath || path.Base(filePath) == p.value {
		return true
	}

	return matchesPathComponent(filePath, p.value)
}

func matchesPathComponent(filePath string, pattern string) bool {
	if filePath == pattern || strings.HasPrefix(filePath, pattern+"/") {
		return true
	}

	return strings.Contains(filePath, "/"+pattern+"/")
}

func normalizeIgnorePath(filePath string) string {
	normalized := filepath.ToSlash(filepath.Clean(filePath))
	normalized = strings.TrimPrefix(normalized, "./")

	return strings.TrimPrefix(normalized, "/")
}
