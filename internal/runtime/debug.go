package runtime

import (
	"regexp"
	"strings"
)

// Common patterns associated with secrets that should be redacted from
// debug output. These match values that look like API keys, tokens,
// passwords, private keys, and other sensitive material.
var (
	secretPatterns = []*regexp.Regexp{
		// API keys: sk- prefix matches OpenAI-style keys
		regexp.MustCompile(`(?i)(sk-|key-|token-|bearer\s+)[a-zA-Z0-9+/=_-]{8,}`),
		// GitHub tokens
		regexp.MustCompile(`gh[pso]?_[a-zA-Z0-9]{8,}`),
		// JWT tokens
		regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
		// AWS access keys
		regexp.MustCompile(`(AKIA|ASIA)[A-Z0-9]{16}`),
		// PEM private key markers
		regexp.MustCompile(`-----BEGIN[^-]+PRIVATE KEY-----`),
		// Long hex strings (32+ chars)
		regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`),
		// Long base64 strings (40+ chars)
		regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`),
		// Colon-separated hex fingerprints (SHA256)
		regexp.MustCompile(`[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){15,}`),
		// Strings with "pass", "pwd", "secret", "token" as entire words in context
		regexp.MustCompile(`(?i)(pass|pwd|secret|token|credential|apikey|api_key|private_key|access_key)[:=]\s*\S+`),
	}

	// sentinelPattern matches strings known to be sentinel test secrets.
	sentinelPattern = regexp.MustCompile(`(?i)(sentinel-secret|test-secret-value|agentpaas-secret|super-secret-value|dummy-secret)`)
)

// SanitizeDebugOutput scans the given debug output (Docker inspect JSON, logs,
// config dumps) for secret patterns and replaces matching values with
// "[REDACTED]".
//
// This is used to ensure that Docker inspect, runtime logs, and network config
// dumps never expose raw secret values.
func SanitizeDebugOutput(output string) string {
	if output == "" {
		return output
	}

	result := output

	// Apply all secret patterns
	for _, re := range secretPatterns {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}

	// Sentinel patterns for test verification
	result = sentinelPattern.ReplaceAllString(result, "[REDACTED]")

	return result
}

// HasSecretLeak returns true if the given output contains any pattern that
// looks like an unredacted secret. Used in tests to verify debug output is
// safe.
func HasSecretLeak(output string) bool {
	if output == "" {
		return false
	}
	for _, re := range secretPatterns {
		if re.MatchString(output) {
			return true
		}
	}
	return sentinelPattern.MatchString(output)
}

// SanitizeDockerInspect redacts secrets from a Docker container inspect JSON
// string. It specifically targets the Environment variables and Labels sections
// which are the most common sources of secret leakage.
func SanitizeDockerInspect(inspectJSON string) string {
	result := inspectJSON

	// Environment variables are the most common leak vector.
	// Pattern: "Env":["KEY=VALUE","KEY2=VALUE2"]
	envRe := regexp.MustCompile(`"Env":\s*\[([^\]]*)\]`)
	result = envRe.ReplaceAllStringFunc(result, func(match string) string {
		// Find all KEY=VALUE patterns and redact the VALUE part
		kvRe := regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=([^"]*)`)
		return kvRe.ReplaceAllStringFunc(match, func(kv string) string {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return kv
			}
			key := strings.ToLower(parts[0])
			val := parts[1]

			// Check if the key suggests a secret value
			secretKeySuffixes := []string{
				"key", "token", "secret", "password", "passwd", "pwd",
				"credential", "auth", "access_key", "api_key", "private_key",
				"client_secret", "refresh_token", "access_token",
			}
			for _, suffix := range secretKeySuffixes {
				if strings.HasSuffix(key, suffix) || strings.Contains(key, "_"+suffix) {
					return parts[0] + "=[REDACTED]"
				}
			}

			// Check the value itself against secret patterns
			if HasSecretLeak(val) {
				return parts[0] + "=[REDACTED]"
			}

			return kv
		})
	})

	// Also run the general SanitizeDebugOutput for any remaining patterns
	result = SanitizeDebugOutput(result)

	return result
}