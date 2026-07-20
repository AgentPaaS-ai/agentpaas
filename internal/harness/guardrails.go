package harness

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// StatusGuardrailBlocked is returned when a guardrail blocks an LLM prompt or response.
const StatusGuardrailBlocked = "guardrail_blocked"

// GuardrailRule is the invoke-payload form of a policy.Guardrail entry.
// Only fields required by harness enforcement are kept.
type GuardrailRule struct {
	Type       string
	Pattern    string
	Action     string
	Provider   string
	Credential string
	URL        string
}

// compiledGuardrails holds precompiled regex rules for the current invoke.
type compiledGuardrails struct {
	regexes []compiledRegexGuardrail
	// Webhook unreachable = open-fail for now? Spec is multi-layered content
	// filtering. We fail closed for regex block/mask and for moderation basic checks.
	webhooks []string
	// moderationProvider is best-effort; without a dedicated moderation call path
	// beyond LLM providers, we record rules and only enforce regex here + optional webhook.
	moderation []GuardrailRule
}

type compiledRegexGuardrail struct {
	re     *regexp.Regexp
	action string // "block" (default) or "mask"
}

func guardrailsFromPayload(payload map[string]any) *compiledGuardrails {
	raw, ok := payload["guardrails"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		// Also accept []map[string]any after json decode variability.
		if maps, ok2 := raw.([]map[string]any); ok2 {
			list = make([]any, len(maps))
			for i, m := range maps {
				list[i] = m
			}
		} else {
			return nil
		}
	}
	if len(list) == 0 {
		return nil
	}
	cg := &compiledGuardrails{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(asString(m["type"])))
		switch typ {
		case "regex":
			pat := asString(m["pattern"])
			if pat == "" {
				continue
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				// Invalid patterns should have been rejected at policy validation;
				// fail-closed at runtime if one slipped through.
				cg.regexes = append(cg.regexes, compiledRegexGuardrail{
					re:     regexp.MustCompile(`(?s).*`),
					action: "block",
				})
				continue
			}
			action := strings.ToLower(strings.TrimSpace(asString(m["action"])))
			if action == "" {
				action = "block"
			}
			cg.regexes = append(cg.regexes, compiledRegexGuardrail{re: re, action: action})
		case "webhook":
			if u := asString(m["url"]); u != "" {
				cg.webhooks = append(cg.webhooks, u)
			}
		case "moderation":
			cg.moderation = append(cg.moderation, GuardrailRule{
				Type:       "moderation",
				Provider:   asString(m["provider"]),
				Credential: asString(m["credential"]),
			})
		}
	}
	if len(cg.regexes) == 0 && len(cg.webhooks) == 0 && len(cg.moderation) == 0 {
		return nil
	}
	return cg
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return ""
	}
}

// applyGuardrailsToText enforces regex (+ optional webhook) on LLM prompt or response text.
// Returns (resultText, blockedError).
func applyGuardrailsToText(cg *compiledGuardrails, text string, direction string, credentials map[string]rpcCredential) (string, error) {
	if cg == nil {
		return text, nil
	}
	out := text
	for _, g := range cg.regexes {
		if g.re == nil {
			continue
		}
		if !g.re.MatchString(out) {
			continue
		}
		switch g.action {
		case "mask":
			out = g.re.ReplaceAllString(out, "[REDACTED]")
		default: // block
			return "", fmt.Errorf("guardrail blocked %s: pattern matched", direction)
		}
	}
	// Webhooks: fail-closed if configured and request cannot be completed, or response denies.
	for _, target := range cg.webhooks {
		if err := checkGuardrailWebhook(target, out, direction); err != nil {
			return "", fmt.Errorf("apply guardrails to text: %w", err)
		}
	}
	// Moderation: when configured, require a credential value and call the provider
	// moderation endpoint. Fail closed if credential missing or the call fails.
	for _, m := range cg.moderation {
		provider := strings.ToLower(strings.TrimSpace(m.Provider))
		if provider == "" {
			provider = "openai"
		}
		if provider != "openai" {
			return "", fmt.Errorf("guardrail moderation provider %q not supported", provider)
		}
		cred, ok := credentials[m.Credential]
		if !ok || strings.TrimSpace(cred.Value) == "" {
			return "", fmt.Errorf("guardrail moderation credential %q unavailable", m.Credential)
		}
		if err := checkOpenAIModeration(out, cred.Value); err != nil {
			return "", fmt.Errorf("apply guardrails to text: %w", err)
		}
	}
	return out, nil
}

// checkOpenAIModeration calls OpenAI/moderation (or gateway-rewritten URL).
func checkOpenAIModeration(text, apiKey string) error {
	endpoint := "https://api.openai.com/v1/moderations"
	requestURL := endpoint
	var host string
	if gw := os.Getenv("AGENTPAAS_GATEWAY_URL"); gw != "" {
		if rewritten, err := rewriteURLForGateway(endpoint, gw); err == nil {
			requestURL = rewritten
			if u, err := url.Parse(endpoint); err == nil {
				host = u.Host
			}
		}
	}
	body := fmt.Sprintf(`{"input":%q}`, text)
	req, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("moderation request build: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	resp, err := webhookClient.Do(req)
	if err != nil {
		return fmt.Errorf("moderation unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("moderation status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("moderation read: %w", err)
	}
	// Minimal parse: look for "flagged":true without full schema dependency.
	if strings.Contains(string(raw), `"flagged":true`) || strings.Contains(string(raw), `"flagged": true`) {
		return fmt.Errorf("guardrail blocked content: moderation flagged")
	}
	return nil
}

var webhookClient = &http.Client{Timeout: 5 * time.Second}

// checkGuardrailWebhook POSTs {direction,text} JSON to the webhook. Expects HTTP 200
// and optional body containing "block":true / "allow":false to deny. Network errors fail closed.
func checkGuardrailWebhook(rawURL, text, direction string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("guardrail webhook invalid url")
	}
	// Always rewrite through gateway when present so egress policy still applies.
	requestURL := rawURL
	var host string
	if gw := os.Getenv("AGENTPAAS_GATEWAY_URL"); gw != "" {
		if rewritten, rerr := rewriteURLForGateway(rawURL, gw); rerr == nil {
			requestURL = rewritten
			host = u.Host
		}
	}
	body := fmt.Sprintf(`{"direction":%q,"text":%q}`, direction, text)
	req, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("guardrail webhook build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if host != "" {
		req.Host = host
	}
	resp, err := webhookClient.Do(req)
	if err != nil {
		return fmt.Errorf("guardrail webhook unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("guardrail webhook blocked %s", direction)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("guardrail webhook status %d for %s", resp.StatusCode, direction)
	}
	return nil
}

// injectSystemPromptFromPayload returns the system prompt string from payload, if any.
func injectSystemPromptFromPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["inject_system_prompt"].(string); ok {
		return v
	}
	return ""
}

// combineSystemPrompt prepends a system prompt to the user prompt in a stable format
// adapters that accept a freeform prompt can still send as a single user message body.
// Prefer structured multi-message once adapters support it.
func combineSystemPrompt(systemPrompt, userPrompt string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		return userPrompt
	}
	// Use a delimiter that is easy to audit and unlikely to confuse providers.
	return "System: " + systemPrompt + "\n\nUser: " + userPrompt
}
