package export

import (
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