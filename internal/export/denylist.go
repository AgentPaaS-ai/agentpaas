package export

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// Denied export filenames (basename patterns). Matched case-sensitively on the
// final path component before inclusion in the bundle.
var deniedBasenames = []string{
	".env",
	".env.local",
	".env.production",
	".env.development",
	"credentials.json",
	"secrets.json",
	"service-account.json",
	"id_rsa",
	"id_ed25519",
}

// CheckDeniedProjectFiles rejects sensitive filenames even when a project
// ignore rule would otherwise omit them from the bundle. Ignoring a secret
// file is not proof that it is safe to export the project.
func CheckDeniedProjectFiles(projectDir string) error {
	return filepath.WalkDir(projectDir, func(pathname string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".venv" || entry.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(projectDir, pathname)
		if err != nil {
			return fmt.Errorf("check denied project files: %w", err)
		}
		if denied, reason := IsDeniedExportPath(rel); denied {
			return fmt.Errorf("export blocked: %s (%s)", filepath.ToSlash(rel), reason)
		}
		return nil
	})
}

var deniedSuffixes = []string{
	".pem",
	".key",
	".p12",
	".pfx",
	".kdbx",
}

// IsDeniedExportPath reports whether relPath (project-relative, slash-separated)
// must be rejected before export.
func IsDeniedExportPath(relPath string) (bool, string) {
	relPath = filepath.ToSlash(relPath)
	base := path.Base(relPath)
	if strings.HasPrefix(base, ".env") {
		return true, "denied filename pattern: .env*"
	}
	for _, name := range deniedBasenames {
		if base == name {
			return true, "denied filename: " + name
		}
	}
	lower := strings.ToLower(base)
	for _, suf := range deniedSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true, "denied extension: " + suf
		}
	}
	return false, ""
}
