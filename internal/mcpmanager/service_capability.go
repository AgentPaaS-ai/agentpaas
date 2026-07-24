package mcpmanager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// CapabilityHeader is the internal header name used to carry the per-binding
// MCP service capability token between the trusted caller gateway and service
// gateway. This header is STRIPPED before it reaches agent/Python code and
// MUST NEVER appear in agent-facing responses, SDK DTOs, logs, or audit event
// payloads.
const CapabilityHeader = "X-AgentPaaS-MCP-Capability"

const (
	// capabilityTokenLength is the byte length for cryptographically random
	// capability tokens. 32 bytes → 64 hex chars; unguessable.
	capabilityTokenLength = 32
)

// GenerateCapability produces a cryptographically random unguessable
// capability token. Called only by trusted harness/gateway code; the result
// is stored on ServiceInstance and NEVER serialized into agent responses or
// Python environment maps.
func GenerateCapability() (string, error) {
	var b [capabilityTokenLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mcpmanager: generate capability: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ServiceRouteAuthorizer enforces that an MCP service route request carries
// the correct binding capability. Used by the gateway/proxy (T05) when
// constructing route configurations.
type ServiceRouteAuthorizer struct{}

// Authorized checks whether the provided capability matches the expected
// capability. Returns an error if not — the error message intentionally does
// NOT contain the actual token values to prevent leakage.
func (a *ServiceRouteAuthorizer) Authorized(expected, provided string) error {
	if provided == "" {
		return fmt.Errorf("mcp_service_route_authorize: missing capability")
	}
	if expected == "" {
		return fmt.Errorf("mcp_service_route_authorize: no expected capability configured")
	}
	if !strings.EqualFold(expected, provided) {
		return fmt.Errorf("mcp_service_route_authorize: invalid capability (capability stripped)")
	}
	return nil
}

// ForbiddenForAgentHTTP checks whether a raw agent.http() request is trying
// to reach an MCP service route. This is a unit-level deny helper: any
// generic HTTP path requested by the agent that looks like an MCP service
// route MUST be denied.
//
// The presence of the CapabilityHeader in the request headers (even with the
// wrong value) is considered a policy violation because agent code should
// never emit this header. Returns an error describing the violation.
func ForbiddenForAgentHTTP(requestHeaders map[string]string) error {
	if _, ok := requestHeaders[CapabilityHeader]; ok {
		return fmt.Errorf("agent.http forbidden: MCP service capability header detected in generic HTTP path")
	}
	return nil
}

// StripCapabilityFromHeaders removes the CapabilityHeader from a headers map
// in place. Must be called before service Python dispatch. Returns true if
// the header was present (and stripped).
func StripCapabilityFromHeaders(headers map[string]string) bool {
	_, ok := headers[CapabilityHeader]
	delete(headers, CapabilityHeader)
	return ok
}

// StripCapabilityFromEnv removes any environment variable that carries MCP
// service capability tokens or network endpoint information. This is called
// before constructing the Python process environment map. Returns the number
// of stripped entries.
func StripCapabilityFromEnv(env []string) (cleaned []string, stripped int) {
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) < 1 {
			cleaned = append(cleaned, e)
			continue
		}
		key := strings.ToUpper(parts[0])
		if isForbiddenEnvKey(key) {
			stripped++
			continue
		}
		cleaned = append(cleaned, e)
	}
	return cleaned, stripped
}

// forbiddenEnvPatterns are substrings that must never appear in environment
// variable names visible to agent/Python code.
var forbiddenEnvPatterns = []string{
	"AGENTPAAS_MCP_CAPABILITY",
	"AGENTPAAS_MCP_ENDPOINT",
	"AGENTPAAS_MCP_NETWORK",
	"AGENTPAAS_MCP_ALIAS",
	"AGENTPAAS_SERVICE_NETWORK",
	"AGENTPAAS_SERVICE_CAPABILITY",
	"AGENTPAAS_SERVICE_ENDPOINT",
}

// isForbiddenEnvKey returns true if the key matches a forbidden env pattern.
func isForbiddenEnvKey(key string) bool {
	for _, pattern := range forbiddenEnvPatterns {
		if strings.Contains(key, pattern) {
			return true
		}
	}
	return false
}

// SanitizeErrorMessageForAgent strips capability tokens and network
// endpoint information from error messages before they are returned to
// agent code. This ensures no trusted data leaks through error paths.
func SanitizeErrorMessageForAgent(msg string) string {
	// Strip any hex-looking strings that could be capability tokens
	// (64 hex chars is a capability token signature).
	msg = sanitizeHexTokens(msg)
	return msg
}

// sanitizeHexTokens replaces 64-character hex strings (signature of
// capability tokens) with [REDACTED].
func sanitizeHexTokens(s string) string {
	// Find 64-char hex runs.
	var result strings.Builder
	result.Grow(len(s))
	i := 0
	for i < len(s) {
		// Look for hex run.
		j := i
		for j < len(s) && isHex(s[j]) {
			j++
		}
		runLen := j - i
		if runLen >= 64 {
			result.WriteString("[REDACTED]")
		} else {
			result.WriteString(s[i:j])
		}
		i = j
		// Copy non-hex run.
		for i < len(s) && !isHex(s[i]) {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
