package trust

import (
	"path/filepath"
	"strings"
	"unicode"
)

// NormalizeFingerprint strips all whitespace, colons, and dashes from a
// fingerprint string and lowercases it. This accepts both compact form
// (64 hex chars) and display form (groups of 4 hex chars separated by
// spaces, e.g. "a1b2 c3d4 ...").
func NormalizeFingerprint(s string) string {
	var b strings.Builder
	b.Grow(64)
	for _, r := range s {
		if unicode.IsSpace(r) || r == ':' || r == '-' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// DisplayFingerprint formats a 64-character hex fingerprint into groups of 4
// characters separated by spaces (e.g. "a1b2 c3d4 e5f6 ...").
// If the input is not exactly 64 hex characters, it is returned as-is.
func DisplayFingerprint(s string) string {
	fp := NormalizeFingerprint(s)
	if len(fp) != 64 {
		return s
	}
	var b strings.Builder
	b.Grow(79) // 64 chars + 15 spaces
	for i, r := range fp {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// DefaultStorePath returns the default path to the trust store file for the
// given home directory.
func DefaultStorePath(homeDir string) string {
	return filepath.Join(homeDir, "trust", "publishers.json")
}

// IsValidAlias returns true if the alias is a valid slug: lowercase
// alphanumeric with hyphens, 1-64 characters.
func IsValidAlias(alias string) bool {
	if len(alias) == 0 || len(alias) > 64 {
		return false
	}
	for _, r := range alias {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}