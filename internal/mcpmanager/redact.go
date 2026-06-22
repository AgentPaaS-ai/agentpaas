package mcpmanager

import (
	"encoding/json"
	"strings"
)

// sentinelSecretPatterns are redacted from all MCP tool output.
var sentinelSecretPatterns = []string{
	"sk-", "sk_live_", "AKIA", "ghp_", "gho_", "ghs_",
	"-----BEGIN", "PRIVATE KEY",
	"xoxb-", "xoxp-",
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
	raw, err := json.Marshal(output)
	if err != nil {
		return "[redact: unserializable output]"
	}
	s := string(raw)

	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)

	for _, pattern := range sentinelSecretPatterns {
		idx := strings.Index(strings.ToLower(s), strings.ToLower(pattern))
		for idx >= 0 {
			end := idx + len(pattern)
			if end > len(s) {
				end = len(s)
			}
			quoteIdx := strings.IndexByte(s[idx:], '"')
			if quoteIdx > 0 {
				end = idx + quoteIdx
			}
			s = s[:idx] + "[REDACTED]" + s[end:]
			idx = strings.Index(strings.ToLower(s), strings.ToLower(pattern))
		}
	}

	if len(s) > maxToolOutputLen {
		s = s[:maxToolOutputLen] + "...[truncated]"
	}

	return s
}

func redactToolOutputValue(output any) any {
	redacted := RedactToolOutput(output)
	var value any
	if err := json.Unmarshal([]byte(redacted), &value); err != nil {
		return redacted
	}
	return value
}

// RedactToolOutputHash returns a hash of the redacted output (for audit).
// Uses the same hashJSONValue function from router.go.
func RedactToolOutputHash(output any) string {
	return hashRouterJSON(RedactToolOutput(output))
}
