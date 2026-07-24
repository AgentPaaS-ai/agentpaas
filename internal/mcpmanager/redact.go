package mcpmanager

import (
	"encoding/json"
	"strings"
)

// sentinelSecretPatternsList returns the redaction patterns as a fresh slice.
// This prevents mutation of the shared pattern list.
func sentinelSecretPatternsList() []string {
	return []string{
		"sk-", "sk_live_", "AKIA", "ghp_", "gho_", "ghs_",
		"-----BEGIN", "PRIVATE KEY",
		"xoxb-", "xoxp-",
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
			sanitized[sanitizedKey] = sanitizeJSONValue(item)
		}
		return sanitized
	default:
		return value
	}
}

func sanitizeToolOutputString(s string) string {
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
// and capability token redaction (hex tokens).
func sanitizeLastError(s string) string {
	s = sanitizeToolOutputString(s)
	s = SanitizeErrorMessageForAgent(s)
	return s
}

// RedactToolOutputHash returns a hash of the redacted output (for audit).
// Uses the same hashJSONValue function from router.go.
func RedactToolOutputHash(output any) string {
	return hashRouterJSON(RedactToolOutput(output))
}
