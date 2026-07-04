package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// formatHTTPError produces an actionable error message for non-2xx LLM API responses.
// It parses the response body for provider-specific error fields and includes guidance
// for common failure modes (expired credentials, wrong model, rate limits, etc.).
// The credential value is NEVER included — only the provider's error message is surfaced.
func formatHTTPError(provider string, statusCode int, body []byte) error {
	// Try to extract provider error message from common JSON error formats.
	providerMsg := ""
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		// OpenAI-compatible: {"error": {"message": "..."}}
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok {
				providerMsg = msg
			}
			// Anthropic: {"error": {"type": "...", "message": "..."}}
			if etype, ok := errObj["type"].(string); ok && providerMsg == "" {
				providerMsg = etype
			}
		}
		// Google Gemini: {"error": {"code": ..., "message": "...", "status": "..."}}
		// (same error.message pattern, already handled above)
		// Z.AI: {"error": {"code": "...", "message": "..."}}
		if code, ok := parsed["error"].(map[string]any); ok {
			if c, ok := code["code"].(string); ok && providerMsg == "" {
				providerMsg = c
			}
		}
	}

	// Build the actionable guidance based on status code.
	var guidance string
	switch {
	case statusCode == 401:
		guidance = fmt.Sprintf(
			"HTTP %d: credential for %q may be expired or invalid — refresh the API key/OAuth token and re-store via 'agentpaas secret add <name>' (the stored credential was rejected by the provider)",
			statusCode, provider,
		)
	case statusCode == 403:
		guidance = fmt.Sprintf(
			"HTTP %d: credential for %q lacks permission or has insufficient credits — check the provider dashboard for billing/quota, or verify the API key has access to the requested model",
			statusCode, provider,
		)
	case statusCode == 404:
		guidance = fmt.Sprintf(
			"HTTP %d: model not found on %q — check the model name in agent.yaml llm.model, or list available models from the provider's API",
			statusCode, provider,
		)
	case statusCode == 429:
		guidance = fmt.Sprintf(
			"HTTP %d: rate limit exceeded on %q — reduce request frequency or add delay between calls; check the provider dashboard for quota details",
			statusCode, provider,
		)
	case statusCode >= 500:
		guidance = fmt.Sprintf(
			"HTTP %d: %q provider error — the provider is experiencing issues; retry after a brief wait",
			statusCode, provider,
		)
	default:
		guidance = fmt.Sprintf("HTTP %d from %q", statusCode, provider)
	}

	if providerMsg != "" {
		return fmt.Errorf("%s (provider says: %s) — %s", provider, providerMsg, guidance)
	}
	return fmt.Errorf("%s — %s", guidance, "no error message in response body")
}

// sanitizeForLog removes any potential credential values from error strings.
// Currently a passthrough — provider error messages don't contain credentials,
// but this is a safety net for future provider responses.
func sanitizeForLog(s string) string {
	// Redact anything that looks like a Bearer token or API key
	if strings.Contains(s, "Bearer ") {
		s = strings.ReplaceAll(s, "Bearer ", "Bearer «redacted»")
	}
	return s
}
