package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocsLinkChecker_NoBrokenLocalLinks scans all tracked markdown files
// for local links and verifies the target files exist.
func TestDocsLinkChecker_NoBrokenLocalLinks(t *testing.T) {
	// Find the project root by walking up from the test directory.
	repoRoot := findRepoRoot(t)

	// Markdown files to check.
	mdFiles := []string{
		"README.md",
		"docs/how-enforcement-works.md",
		"docs/threat-model.md",
		"docs/audit-export.md",
		"docs/known-limitations.md",
		"docs/quickstart.md",
		"docs/sharing.md",
		"docs/policy-reference.md",
		"docs/manual-testing.md",
		"docs/secrets.md",
		"docs/privacy.md",
		"integrations/hermes-plugin/SKILL.md",
	}

	// Regex to match markdown links: [text](path)
	// Ignores external URLs (http://, https://) and anchor-only links (#...).
	linkRe := regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)

	broken := 0
	for _, relPath := range mdFiles {
		absPath := filepath.Join(repoRoot, relPath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			// Skip files that don't exist (may be gitignored or removed).
			continue
		}

		matches := linkRe.FindAllStringSubmatch(string(data), -1)
		for _, m := range matches {
			linkTarget := m[2]

			// Skip external URLs.
			if strings.HasPrefix(linkTarget, "http://") || strings.HasPrefix(linkTarget, "https://") {
				continue
			}

			// Skip anchor-only links.
			if strings.HasPrefix(linkTarget, "#") {
				continue
			}

			// Strip anchor from path.
			if idx := strings.Index(linkTarget, "#"); idx >= 0 {
				linkTarget = linkTarget[:idx]
			}

			// Skip empty paths after anchor stripping.
			if linkTarget == "" {
				continue
			}

			// Resolve relative to the markdown file's directory.
			mdDir := filepath.Dir(absPath)
			targetPath := filepath.Join(mdDir, linkTarget)

			if _, err := os.Stat(targetPath); os.IsNotExist(err) {
				t.Errorf("B20 DOCS LINK CHECK: broken link in %s: [%s](%s) → %s does not exist", relPath, m[1], m[2], targetPath)
				broken++
			}
		}
	}

	if broken > 0 {
		t.Fatalf("B20 DOCS LINK CHECK: %d broken local link(s) found", broken)
	}
}

