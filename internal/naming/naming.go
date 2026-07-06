// Package naming provides primitives for parsing and formatting agent
// references in the form "name@pub8", where pub8 is the first 8 hex
// characters of the publisher's public-key fingerprint.
//
// This package ships only the parser + formatter; threading through
// daemon state/trigger/cron is deferred to later tasks to keep the
// blast radius minimal.
package naming

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors returned by ParseAgentRef and the validation functions.
var (
	// ErrInvalidAgentRef is returned when the agent reference string
	// cannot be parsed at all (multiple @ signs, empty string, etc.).
	ErrInvalidAgentRef = errors.New("invalid agent reference")

	// ErrInvalidName is returned when the agent name part fails validation.
	ErrInvalidName = errors.New("invalid agent name")

	// ErrInvalidPub8 is returned when the pub8 suffix is not exactly 8
	// lowercase hex characters.
	ErrInvalidPub8 = errors.New("invalid pub8: must be exactly 8 hex characters")
)

// agentNamePattern matches valid agent names:
//
//	[a-z0-9]([a-z0-9-]*[a-z0-9])?   (1-63 characters, must be lowercase)
var agentNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateName checks whether the given string is a valid agent name.
//
// Agent names must be 1–63 characters long and must match the pattern
// [a-z0-9]([a-z0-9-]*[a-z0-9])? — no uppercase, no leading/trailing
// hyphens, no empty string.
func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 63 {
		return fmt.Errorf("%w: must be 1-63 characters, got %d", ErrInvalidName, len(name))
	}
	if !agentNamePattern.MatchString(name) {
		return fmt.Errorf("%w: must match [a-z0-9]([a-z0-9-]*[a-z0-9])?", ErrInvalidName)
	}
	return nil
}

// ValidatePub8 checks whether the given string is a valid pub8 suffix.
// A valid pub8 is exactly 8 lowercase hex characters ([0-9a-f]).
func ValidatePub8(pub8 string) error {
	if len(pub8) != 8 {
		return fmt.Errorf("%w: got %d characters", ErrInvalidPub8, len(pub8))
	}
	for _, c := range pub8 {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return fmt.Errorf("%w: contains non-hex character %q", ErrInvalidPub8, c)
		}
	}
	return nil
}

// ParseAgentRef parses an agent reference string of the form "name" or
// "name@pub8".
//
// Accepted forms:
//   - "name"           → bare agent name
//   - "name@<8-hex>"   → agent name with pub8 suffix
//
// The pub8 suffix is normalized to lowercase before being returned.
// The name part is validated via ValidateName.
//
// Errors (wrapping ErrInvalidAgentRef):
//   - Empty string
//   - Multiple @ separators
//   - @ at start (empty name)
//   - Empty pub8 after @
//   - pub8 not exactly 8 hex characters
//   - Name fails ValidateName
func ParseAgentRef(s string) (name string, pub8 string, err error) {
	if s == "" {
		return "", "", fmt.Errorf("%w: empty string", ErrInvalidAgentRef)
	}

	parts := strings.Split(s, "@")

	switch len(parts) {
	case 1:
		// Bare name, no @
		name = parts[0]
	case 2:
		name = parts[0]
		pub8 = strings.ToLower(parts[1])

		if name == "" {
			return "", "", fmt.Errorf("%w: empty name before @", ErrInvalidAgentRef)
		}
		if pub8 == "" {
			return "", "", fmt.Errorf("%w: empty pub8 after @", ErrInvalidAgentRef)
		}
		if err := ValidatePub8(pub8); err != nil {
			return "", "", fmt.Errorf("%w: %v", ErrInvalidAgentRef, err)
		}
	default:
		return "", "", fmt.Errorf("%w: multiple @ separators", ErrInvalidAgentRef)
	}

	if err := ValidateName(name); err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrInvalidAgentRef, err)
	}

	return name, pub8, nil
}

// FormatAgentRef formats an agent reference string from a name and a
// full fingerprint (64 hex chars).
//
// If fingerprint is non-empty, the first 8 hex characters are used as
// the pub8 suffix: "name@<first-8>". The pub8 is always lowercased.
//
// If fingerprint is empty, the result is the bare agent name.
//
// Examples:
//
//	FormatAgentRef("weather", "a1b2c3d4e5f6...")  → "weather@a1b2c3d4"
//	FormatAgentRef("weather", "")                  → "weather"
func FormatAgentRef(name string, fingerprint string) string {
	if fingerprint == "" {
		return name
	}
	prefix := strings.ToLower(fingerprint)
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return name + "@" + prefix
}