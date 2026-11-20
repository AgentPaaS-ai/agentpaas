package mcpmanager

import (
	"encoding/json"
	"strings"
)

// sentinelSecretPatternsList returns the redaction patterns as a fresh slice.
// This prevents mutation of the shared pattern list.
func sentinelSecretPatternsList() []string {
	return []string{
		"sk-", "sk_live_", "AKIA", "ASIA",
		"ghp_", "gho_", "ghs_",
		"-----BEGIN", "PRIVATE KEY",
		"xoxb-", "xoxp-",
		"Bearer ", "bearer ",
	}
}

// maxToolOutputLen is the maximum length of tool output before truncation.
const maxToolOutputLen = 4096

// RedactToolOutput sanitizes MCP tool output for safe display/audit.
// It:
//   - Escapes control characters (prevents terminal escape injection)
//   - Redacts sentinel secret patterns
//   - Truncates excessively long output
//
// Returns the redacted string.
func RedactToolOutput(output any) string {
	raw, err := json.Marshal(sanitizeToolOutputValue(output))
	if err != nil {
		return "[redact: unserializable output]"
	}
	s := string(raw)

	if len(s) > maxToolOutputLen {
		s = s[:maxToolOutputLen] + "...[truncated]"
	}

	return s
}

func redactToolOutputValue(output any) any {
	return sanitizeToolOutputValue(output)
}

func sanitizeToolOutputValue(output any) any {
	raw, err := json.Marshal(output)
	if err != nil {
		return "[redact: unserializable output]"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "[redact: unserializable output]"
	}
	return sanitizeJSONValue(value)
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeToolOutputString(typed)
	case []any:
		sanitized := make([]any, len(typed))
		for i, item := range typed {
			sanitized[i] = sanitizeJSONValue(item)
		}
		return sanitized
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		for key, item := range typed {
			sanitizedKey := sanitizeToolOutputString(key)
			// Redact values stored under "api_key" or similar credential key names.
			if isCredentialKeyName(sanitizedKey) {
				sanitized[sanitizedKey] = "[REDACTED]"
			} else {
				sanitized[sanitizedKey] = sanitizeJSONValue(item)
			}
		}
		return sanitized
	default:
		return value
	}
}

// isCredentialKeyName returns true if the JSON key name indicates a
// credential field whose value should be redacted entirely.
func isCredentialKeyName(key string) bool {
	kl := strings.ToLower(key)
	return kl == "api_key" || kl == "apikey" || kl == "secret_key" ||
		kl == "access_key" || kl == "private_key" || kl == "token"
}

func sanitizeToolOutputString(s string) string {
	// Strip zero-width and homoglyph control characters before pattern matching.
	// This prevents bypass via \u200b (zero-width space), \uFEFF (BOM),
	// \u200c (ZWJ), \u200d (ZWNJ), and fullwidth confusable letters.
	s = stripZeroWidthAndFormatChars(s)

	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)

	for _, pattern := range sentinelSecretPatternsList() {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(pattern))
		for idx >= 0 {
			end := len(s)
			for i := idx + len(pattern); i < len(s); i++ {
				if s[i] == '"' || s[i] == '\'' || s[i] == ' ' || s[i] == '	' {
					end = i
					break
				}
			}
			s = s[:idx] + "[REDACTED]" + s[end:]
			idx = strings.Index(strings.ToLower(s), strings.ToLower(pattern))
		}
	}

	s = strings.ReplaceAll(s, "&", `\u0026`)
	s = strings.ReplaceAll(s, "<", `\u003c`)
	s = strings.ReplaceAll(s, ">", `\u003e`)

	if len(s) > maxToolOutputLen {
		return s[:maxToolOutputLen] + "...[truncated]"
	}
	return s
}

// sanitizeLastError sanitizes an error message for storage in LastError.
// It chains both tool output sanitization (control chars, sentinel patterns)
// and capability token redaction (hex tokens). It also attempts JSON-parsing
// to deeply redact credential fields like "api_key".
func sanitizeLastError(s string) string {
	// Try deep JSON sanitization first (handles "api_key" etc.)
	if jSanitized, err := trySanitizeJSONString(s); err == nil {
		s = jSanitized
	} else {
		s = sanitizeToolOutputString(s)
	}
	s = SanitizeErrorMessageForAgent(s)
	return s
}

// trySanitizeJSONString attempts to parse s as JSON, apply deep value
// sanitization, and return the re-serialized result. Returns an error
// if s is not valid JSON.
func trySanitizeJSONString(s string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", err
	}
	sanitized := sanitizeJSONValue(v)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// RedactToolOutputHash returns a hash of the redacted output (for audit).
// Uses the same hashJSONValue function from router.go.
func RedactToolOutputHash(output any) string {
	return hashRouterJSON(RedactToolOutput(output))
}

// stripZeroWidthAndFormatChars removes zero-width, BOM, format characters,
// and normalizes fullwidth confusable letters (U+FF01-U+FF5E) to ASCII.
// This prevents bypass via zero-width space, BOM, ZWJ/ZWNJ, and fullwidth
// confusables that look like ASCII but have different Unicode codepoints.
func stripZeroWidthAndFormatChars(s string) string {
	return strings.Map(func(r rune) rune {
		// Normalize fullwidth confusable letters to ASCII (U+FF01-U+FF5E → U+0021-U+007E).
		if r >= 0xFF01 && r <= 0xFF5E {
			return r - 0xFF01 + 0x0021
		}
		// Zero-width space, BOM, ZWJ, ZWNJ, word joiner, etc.
		if r == 0x200B || r == 0xFEFF || r == 0x200C || r == 0x200D ||
			r == 0x2060 || r == 0x200E || r == 0x200F || r == 0x00AD ||
			r == 0x2061 || r == 0x2062 || r == 0x2063 || r == 0x2064 {
			return -1 // drop the rune
		}
		return r
	}, s)
}
