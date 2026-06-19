package logging

import (
	"regexp"
	"strings"
)

// Patterns for sensitive values that must be redacted.
var (
	// apiKeyPattern matches common API key prefixes followed by base64/alphanumeric content.
	apiKeyPattern = regexp.MustCompile(`(?i)(sk-|key-|token-|bearer\s+)[a-zA-Z0-9+/=_-]{8,}`)

	// gitHubTokenPattern matches GitHub tokens with ghp_, gho_, ghs_, or gh prefix + underscore.
	gitHubTokenPattern = regexp.MustCompile(`gh[pso]?_[a-zA-Z0-9]{8,}`)

	// jwtPattern matches JWT tokens that start with the base64-encoded JSON header "eyJ".
	jwtPattern = regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`)

	// awsKeyPattern matches AWS access key IDs starting with AKIA or ASIA.
	awsKeyPattern = regexp.MustCompile(`(AKIA|ASIA)[A-Z0-9]{16}`)

	// privateKeyPattern matches PEM-encoded private key blocks.
	privateKeyPattern = regexp.MustCompile(`-----BEGIN[^-]+PRIVATE KEY-----`)

	// hexStringPattern matches hex strings of 32 or more characters.
	hexStringPattern = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)

	// base64HighEntropyPattern matches base64-encoded strings of 40 or more characters.
	// Uses a character class that captures base64 alphabet plus padding.
	base64HighEntropyPattern = regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`)

	// hexWithColonsPattern matches colon-separated hex strings (e.g., SHA256 fingerprints).
	hexWithColonsPattern = regexp.MustCompile(`[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){15,}`)

	// leetspeakPasswordPattern matches password-like strings even within words
	// (used for leetspeak-normalized strings, e.g., "Password" from "P@ssw0rd").
	leetspeakPasswordPattern = regexp.MustCompile(`(?i)(pass|pwd|secret|passphrase)`)

	// passwordValuePattern matches strings that look like password/passphrase values.
	// Matches whole words containing case-insensitive password indicators like "pass", "pwd", "secret".
	passwordValuePattern = regexp.MustCompile(`(?i)\b(pass|pwd|secret|passphrase)\b`)

	// passwordFieldNames are attribute keys that suggest the value is a password.
	passwordFieldNames = []string{
		"password",
		"passphrase",
		"passwd",
		"pwd",
		"secret",
		"api_key",
		"apikey",
		"auth",
		"credential",
		"token",
		"access_token",
		"refresh_token",
		"private_key",
		"client_secret",
	}
)

// isSensitiveValue checks whether the given string value should be redacted.
func isSensitiveValue(val string) bool {
	if len(val) == 0 {
		return false
	}

	// Check exact patterns
	if apiKeyPattern.MatchString(val) {
		return true
	}
	if gitHubTokenPattern.MatchString(val) {
		return true
	}
	if jwtPattern.MatchString(val) {
		return true
	}
	if awsKeyPattern.MatchString(val) {
		return true
	}
	if privateKeyPattern.MatchString(val) {
		return true
	}
	if hexStringPattern.MatchString(val) {
		return true
	}
	if base64HighEntropyPattern.MatchString(val) {
		return true
	}
	if hexWithColonsPattern.MatchString(val) {
		return true
	}

	// Check for password/passphrase values (contains "pass", "pwd", "secret", "passphrase").
	// Also checks with leetspeak normalization (e.g., "P@ssw0rd" -> "Password").
	if len(val) >= 8 && (passwordValuePattern.MatchString(val) || leetspeakPasswordPattern.MatchString(normalizeLeet(val))) {
		return true
	}

	// Check for long alphanumeric strings that look like API keys (20+ chars of mixed case/digits).
	if looksLikeAPIKey(val) {
		return true
	}

	return false
}

// looksLikeAPIKey checks if a string is a long alphanumeric/generic token 20+ characters.
func looksLikeAPIKey(s string) bool {
	if len(s) < 20 {
		return false
	}
	// Must be mostly alphanumeric (allow underscores and hyphens)
	alphaNumCount := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			alphaNumCount++
		}
	}
	// At least 80% alphanumeric (including underscores/hyphens for tokens)
	return alphaNumCount >= len(s)*8/10
}

// Redact replaces all sensitive values found in the input string with "[REDACTED]".
// It checks the entire string and replaces any substring matching a sensitive pattern.
//
// This is the core redaction function used by the logging handler.
func Redact(input string) string {
	if input == "" {
		return input
	}

	result := input

	// Replace full-string matches first (the string as a whole is sensitive)
	if isSensitiveValue(result) {
		result = replaceAllPatterns(result)
		// Fallback: if the value is sensitive but no pattern matched (e.g., password leetspeak),
		// replace the entire value with [REDACTED].
		if result == input {
			return "[REDACTED]"
		}
		return result
	}

	// For longer strings, find and replace sensitive substrings within the string.
	result = replaceAllPatterns(result)

	return result
}

// replaceAllPatterns applies all regex replacements to a string.
func replaceAllPatterns(s string) string {
	// Order matters: apply more specific patterns first, then generic ones.
	replacements := []*regexp.Regexp{
		privateKeyPattern,       // Whole block markers
		jwtPattern,              // JWT tokens
		gitHubTokenPattern,      // GitHub tokens
		awsKeyPattern,           // AWS keys
		apiKeyPattern,           // API key patterns
		hexWithColonsPattern,    // Colon-separated hex
		hexStringPattern,        // Long hex strings
		base64HighEntropyPattern, // Long base64 strings
	}

	for _, re := range replacements {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}

	return s
}

// hasSensitiveKey returns true if the attribute key suggests the value is sensitive.
func hasSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, name := range passwordFieldNames {
		if lower == name || strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

// normalizeLeet replaces common leetspeak characters with their standard alphabet equivalents
// to allow pattern matching against obfuscated password values.
func normalizeLeet(s string) string {
	s = strings.NewReplacer(
		"@", "a",
		"0", "o",
		"1", "l",
		"3", "e",
		"4", "a",
		"5", "s",
		"6", "g",
		"7", "t",
		"8", "b",
		"$", "s",
		"!", "i",
		"|", "i",
	).Replace(s)
	return s
}
