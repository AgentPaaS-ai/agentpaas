package harness

import (
	"os"
	"os/exec"
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
			relTarget, relErr := filepath.Rel(repoRoot, targetPath)
			if relErr == nil && pathGitignored(repoRoot, relTarget) {
				t.Errorf("B20 DOCS LINK CHECK: gitignored link in %s: [%s](%s) → %s is missing from worktrees", relPath, m[1], m[2], relTarget)
				broken++
				continue
			}

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

// pathGitignored reports whether git would omit rel from a fresh worktree.
// A file that exists only on this checkout is still broken for make test.
func pathGitignored(repoRoot, rel string) bool {
	cmd := exec.Command("git", "-C", repoRoot, "check-ignore", "-q", "--", rel)
	return cmd.Run() == nil
}

func TestDocsLinkChecker_ManualTestingStaysGitignored(t *testing.T) {
	root := findRepoRoot(t)
	if !pathGitignored(root, "docs/manual-testing.md") {
		t.Fatal("docs/manual-testing.md must stay gitignored; public docs must not link to it")
	}
}

