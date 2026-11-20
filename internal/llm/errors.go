package llm

import (
	"encoding/json"
	"fmt"
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
		// OpenAI/Anthropic/Google/Z.AI: {"error": {"message": "...", ...}}
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok {
				providerMsg = msg
			}
			// Anthropic: {"error": {"type": "...", "message": "..."}}
			if etype, ok := errObj["type"].(string); ok && providerMsg == "" {
				providerMsg = etype
			}
			// Z.AI: {"error": {"code": "...", "message": "..."}}
			if c, ok := errObj["code"].(string); ok && providerMsg == "" {
				providerMsg = c
			}
		} else if errStr, ok := parsed["error"].(string); ok {
			// xAI: {"code": "unauthenticated:bad-credentials", "error": "..."}
			providerMsg = errStr
		}
		// xAI also has a top-level "code" field alongside the string error.
		if providerMsg == "" {
			if code, ok := parsed["code"].(string); ok {
				providerMsg = code
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
		// 403 can mean either insufficient credits/permissions OR an expired/
		// invalid credential (xAI returns 403 for bad OAuth tokens). The
		// provider message (surfaced above) disambiguates.
		guidance = fmt.Sprintf(
			"HTTP %d: credential for %q was rejected — if the provider message mentions authentication/credentials, the token is expired or invalid (refresh and re-store via 'agentpaas secret add <name>'); otherwise check the provider dashboard for billing/quota or model access permissions",
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

