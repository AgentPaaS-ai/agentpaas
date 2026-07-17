package policy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/money"
)

// ValidationError represents a policy semantic validation finding.
type ValidationError struct {
	Field    string `json:"field"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" or "warning"
}

func (ve ValidationError) Error() string {
	return fmt.Sprintf("%s: [%s] %s", ve.Field, ve.Severity, ve.Message)
}

// validateHostname checks whether a hostname is well-formed and returns
// whether it is a wildcard pattern.  Any `*` in the hostname is treated as a
// wildcard, not just the `*.` prefix.
func validateHostname(hostname string) (hasWildcard bool, clean string, err error) {
	if hostname == "" {
		return false, "", fmt.Errorf("empty hostname")
	}
	// Reject embedded newlines, null bytes, control chars.
	for _, r := range hostname {
		if r < 0x20 || r == 0x7f {
			return false, "", fmt.Errorf("hostname contains control character")
		}
	}
	// Detect `*` anywhere in the hostname — not just the `*.` prefix.
	if strings.Contains(hostname, "*") {
		if hostname == "*" {
			// Bare wildcard "*" means match all domains.  This is only
			// valid when allow_wildcard is true (checked by the caller).
			return true, "*", nil
		}
		// Wildcard at the start: "*.example.com"
		if strings.HasPrefix(hostname, "*.") {
			rest := hostname[2:]
			if rest == "" {
				return false, "", fmt.Errorf("wildcard domain missing base name")
			}
			return true, rest, nil
		}
		// Wildcard in middle or end: "sub.*.example.com" or "example.*"
		return true, hostname, nil
	}
	return false, hostname, nil
}

// isPrivateCIDR checks if the given CIDR is in RFC1918, RFC6598, or loopback ranges.
func isPrivateCIDR(cidr string) bool {
	_, cidrNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false // invalid CIDR; we let other validators catch it
	}
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",   // RFC6598 (CGNAT)
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local
	}
	for _, pr := range privateRanges {
		_, privNet, err := net.ParseCIDR(pr)
		if err != nil {
			continue
		}
		if containsCIDR(privNet, cidrNet) {
			return true
		}
	}
	return false
}

// containsCIDR checks if subnet a contains subnet b.
func containsCIDR(a, b *net.IPNet) bool {
	onesA, bitsA := a.Mask.Size()
	onesB, bitsB := b.Mask.Size()
	if bitsA != bitsB {
		return false
	}
	if onesB < onesA {
		return false // b is wider than a
	}
	return a.Contains(b.IP)
}

// ValidatePolicy checks semantic validation rules on a parsed Policy and
// returns all errors and warnings found.  Validation is idempotent and
// does not mutate the Policy.
func ValidatePolicy(p *Policy) []ValidationError {
	var errs []ValidationError

	if p == nil {
		errs = append(errs, ValidationError{
			Field:    "<root>",
			Message:  "policy is nil",
			Severity: "error",
		})
		return errs
	}

	// ----- Egress rule validation -----
	credIDs := make(map[string]bool)
	for _, c := range p.Credentials {
		credIDs[c.ID] = true
	}

	// Collect declared MCP server names.
	mcpNames := make(map[string]int) // name -> index
	for i, m := range p.MCPServers {
		mcpNames[m.Name] = i
	}

	// Track which credential IDs are referenced by egress/MCP rules.
	credRefd := make(map[string]bool)

	for i, e := range p.Egress {
		prefix := fmt.Sprintf("egress[%d]", i)

		// An egress rule must have either domain or CIDR.
		if e.Domain == "" && e.CIDR == "" {
			errs = append(errs, ValidationError{
				Field:    prefix,
				Message:  "egress rule must specify domain or cidr",
				Severity: "error",
			})
			continue
		}

		// CIDR-only rules (no Domain) are not yet supported in P1.
		// The gateway enforces egress by hostname routing; CIDR-based
		// enforcement requires IP-level routing which is not available.
		// Reject explicitly instead of silently ignoring.
		if e.CIDR != "" && e.Domain == "" {
			errs = append(errs, ValidationError{
				Field:    prefix,
				Message:  "CIDR egress rules are not yet supported in P1; use domain-based egress instead",
				Severity: "error",
			})
			continue
		}
		// Domain+CIDR combos: CIDR is silently ignored by the compiler.
		// Reject explicitly so users know CIDR has no effect in P1.
		if e.CIDR != "" && e.Domain != "" {
			errs = append(errs, ValidationError{
				Field:    prefix,
				Message:  "CIDR is not enforced when domain is present in P1; remove cidr or use domain-only egress",
				Severity: "error",
			})
			continue
		}

		// Hostname validation and wildcard check.
		if e.Domain != "" {
			hasWild, _, hostErr := validateHostname(e.Domain)
			if hostErr != nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".domain",
					Message:  fmt.Sprintf("invalid hostname: %v", hostErr),
					Severity: "error",
				})
			}
			if hasWild && (e.AllowWildcard == nil || !*e.AllowWildcard) {
				errs = append(errs, ValidationError{
					Field:    prefix + ".domain",
					Message:  fmt.Sprintf("wildcard domain %q requires allow_wildcard: true", e.Domain),
					Severity: "error",
				})
			}
		}

		// CIDR validation.
		if e.CIDR != "" {
			if _, _, cidrErr := net.ParseCIDR(e.CIDR); cidrErr != nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".cidr",
					Message:  fmt.Sprintf("invalid CIDR: %v", cidrErr),
					Severity: "error",
				})
			} else if isPrivateCIDR(e.CIDR) && (e.AllowPrivate == nil || !*e.AllowPrivate) {
				errs = append(errs, ValidationError{
					Field:    prefix + ".cidr",
					Message:  fmt.Sprintf("private CIDR %q requires allow_private: true", e.CIDR),
					Severity: "error",
				})
			}
		}

		// Port validation: explicit ports only (no ranges), valid range.
		if len(e.Ports) == 0 {
			errs = append(errs, ValidationError{
				Field:    prefix + ".ports",
				Message:  "at least one port is required",
				Severity: "error",
			})
		}
		for j, port := range e.Ports {
			if port < 1 || port > 65535 {
				errs = append(errs, ValidationError{
					Field:    fmt.Sprintf("%s.ports[%d]", prefix, j),
					Message:  fmt.Sprintf("port %d out of valid range 1-65535", port),
					Severity: "error",
				})
			}
		}

		// Credential reference validation.
		if e.Credential != "" {
			credRefd[e.Credential] = true
			if !credIDs[e.Credential] {
				errs = append(errs, ValidationError{
					Field:    prefix + ".credential",
					Message:  fmt.Sprintf("references undeclared credential %q", e.Credential),
					Severity: "error",
				})
			}
		}

		// Timeout validation.
		if e.Timeout != "" {
			if _, err := time.ParseDuration(e.Timeout); err != nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".timeout",
					Message:  fmt.Sprintf("invalid duration %q: %v", e.Timeout, err),
					Severity: "error",
				})
			}
		}

		// Retry validation.
		if e.Retry != nil {
			retryPrefix := prefix + ".retry"
			if e.Retry.MaxAttempts < 1 {
				errs = append(errs, ValidationError{
					Field:    retryPrefix + ".max_attempts",
					Message:  "max_attempts must be >= 1",
					Severity: "error",
				})
			}
			switch e.Retry.Backoff {
			case "exponential", "linear", "fixed", "":
				// valid
			default:
				errs = append(errs, ValidationError{
					Field:    retryPrefix + ".backoff",
					Message:  fmt.Sprintf("invalid backoff strategy %q; must be 'exponential', 'linear', or 'fixed'", e.Retry.Backoff),
					Severity: "error",
				})
			}
			if e.Retry.MaxBackoff != "" {
				if _, err := time.ParseDuration(e.Retry.MaxBackoff); err != nil {
					errs = append(errs, ValidationError{
						Field:    retryPrefix + ".max_backoff",
						Message:  fmt.Sprintf("invalid duration %q: %v", e.Retry.MaxBackoff, err),
						Severity: "error",
					})
				}
			}
		}
	}

	// ----- Credential validation -----
	for i, c := range p.Credentials {
		prefix := fmt.Sprintf("credentials[%d]", i)

		if c.ID == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".id",
				Message:  "credential id is required",
				Severity: "error",
			})
		} else if ContainsInjectionPattern(c.ID) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".id",
				Message:  "credential id contains control characters or injection patterns",
				Severity: "error",
			})
		}

		// Credential type is required. A credential with no type field (empty string)
		// passes parser validation but must be caught here.
		if c.Type == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".type",
				Message:  "credential type is required",
				Severity: "error",
			})
			continue
		}

		switch c.Type {
		case "header":
			if c.Header == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".header",
					Message:  "header-type credential requires a header field",
					Severity: "error",
				})
			}
		case "brokered":
			if c.Service == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".service",
					Message:  "brokered credential requires a service field",
					Severity: "error",
				})
			}
		case "oauth":
			if c.TokenEndpoint == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".token_endpoint",
					Message:  "oauth credential requires token_endpoint",
					Severity: "error",
				})
			} else {
				parsed, urlErr := url.Parse(c.TokenEndpoint)
				if urlErr != nil || (parsed.Scheme != "https") {
					errs = append(errs, ValidationError{
						Field:    prefix + ".token_endpoint",
						Message:  fmt.Sprintf("token_endpoint must be a valid https URL, got %q", c.TokenEndpoint),
						Severity: "error",
					})
				}
			}
			if c.ClientID == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".client_id",
					Message:  "oauth credential requires client_id",
					Severity: "error",
				})
			}
			if c.RefreshTokenCredential == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".refresh_token_credential",
					Message:  "oauth credential requires refresh_token_credential",
					Severity: "error",
				})
			} else if !credIDs[c.RefreshTokenCredential] {
				errs = append(errs, ValidationError{
					Field:    prefix + ".refresh_token_credential",
					Message:  fmt.Sprintf("refresh_token_credential %q is not a declared credential ID", c.RefreshTokenCredential),
					Severity: "error",
				})
			}
		case "direct_lease":
			if c.Mode == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".mode",
					Message:  "direct-lease credential requires a mode field",
					Severity: "error",
				})
			}
			if c.Mode != "file" && c.Mode != "env" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".mode",
					Message:  fmt.Sprintf("direct-lease mode must be 'file' or 'env', got %q", c.Mode),
					Severity: "error",
				})
			}
			if c.Reason == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".reason",
					Message:  "direct-lease credential requires a reason field",
					Severity: "error",
				})
			}
		default:
			// Unknown credential types should have been caught by the strict YAML
			// parser from B4-T01, but we handle non-empty unknown gracefully.
			if c.Type != "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".type",
					Message:  fmt.Sprintf("unknown credential type %q", c.Type),
					Severity: "error",
				})
			}
		}

		// Reject query-string or body injection markers in credential value.
		if strings.Contains(c.Value, "?") || strings.Contains(c.Value, "&") {
			errs = append(errs, ValidationError{
				Field:    prefix + ".value",
				Message:  "credential value contains query-string injection marker (?, &)",
				Severity: "error",
			})
		}
		// Reject body injection attempts (content-type-like patterns).
		lowerVal := strings.ToLower(c.Value)
		if strings.Contains(lowerVal, "content-type") || strings.Contains(lowerVal, "content-disposition") {
			errs = append(errs, ValidationError{
				Field:    prefix + ".value",
				Message:  "credential value contains body injection patterns",
				Severity: "error",
			})
		}
	}

	// Unused credential warnings.
	for _, c := range p.Credentials {
		if c.Type == "brokered" && !credRefd[c.ID] {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("credentials[%q]", c.ID),
				Message:  fmt.Sprintf("brokered credential %q is declared but not referenced by any egress or MCP rule", c.ID),
				Severity: "warning",
			})
		}
		if c.Type == "header" && !credRefd[c.ID] {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("credentials[%q]", c.ID),
				Message:  fmt.Sprintf("credential %q is declared but not referenced by any egress or MCP rule", c.ID),
				Severity: "warning",
			})
		}
	}

	// ----- MCP server validation -----
	for i, m := range p.MCPServers {
		prefix := fmt.Sprintf("mcp_servers[%d]", i)

		if m.Name == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".name",
				Message:  "MCP server name is required",
				Severity: "error",
			})
		}

		// Validate transport.
		switch m.Transport {
		case "":
			// Default: http if URL is set, stdio if command is set.
			if m.URL != "" {
				m.Transport = "http"
			} else if m.Command != "" {
				m.Transport = "stdio"
			} else {
				errs = append(errs, ValidationError{
					Field:    prefix,
					Message:  "MCP server must specify url (http transport) or command (stdio transport)",
					Severity: "error",
				})
			}
		case "http":
			if m.URL == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".url",
					Message:  "http MCP server requires a url",
					Severity: "error",
				})
			}
		case "stdio":
			if m.Command == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".command",
					Message:  "stdio MCP server requires a command",
					Severity: "error",
				})
			}
		default:
			errs = append(errs, ValidationError{
				Field:    prefix + ".transport",
				Message:  fmt.Sprintf("unknown MCP transport %q; must be 'http' or 'stdio'", m.Transport),
				Severity: "error",
			})
		}

		// Remote MCP server (http/https) must have a matching egress rule.
		// Loopback addresses are exempt (local MCP servers are always reachable).
		if m.URL != "" {
			parsedURL, urlErr := url.Parse(m.URL)
			if urlErr == nil && (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") {
				host := stripPort(parsedURL.Host)
				if !IsLoopbackAddress(host) && !hasMatchingEgress(p.Egress, host) {
					redactedURL := redactURL(m.URL)
					errs = append(errs, ValidationError{
						Field:    prefix + ".url",
						Message:  fmt.Sprintf("remote MCP server %q has no matching egress allow rule", redactedURL),
						Severity: "error",
					})
				}
			}
		}
	}

	// ----- Hook validation -----
	for i, h := range p.Hooks {
		prefix := fmt.Sprintf("hooks[%d]", i)

		if h.Name == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".name",
				Message:  "hook name is required",
				Severity: "error",
			})
		}

		if h.URL == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".url",
				Message:  "hook url is required",
				Severity: "error",
			})
			continue
		}

		parsedURL, urlErr := url.Parse(h.URL)
		if urlErr != nil {
			errs = append(errs, ValidationError{
				Field:    prefix + ".url",
				Message:  fmt.Sprintf("invalid hook URL: %v", urlErr),
				Severity: "error",
			})
			continue
		}

		// Loopback hook exposure refuse.
		host := parsedURL.Hostname()
		if IsLoopbackAddress(host) {
			redactedURL := redactURL(h.URL)
			errs = append(errs, ValidationError{
				Field:    prefix + ".url",
				Message:  fmt.Sprintf("loopback hook URL %q must be explicitly local and cannot be exposed to the agent container", redactedURL),
				Severity: "error",
			})
		}

		// Remote hook must have matching egress rule.
		if (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") && !IsLoopbackAddress(host) {
			if !hasMatchingEgress(p.Egress, host) {
				redactedURL := redactURL(h.URL)
				errs = append(errs, ValidationError{
					Field:    prefix + ".url",
					Message:  fmt.Sprintf("remote hook %q has no matching egress allow rule", redactedURL),
					Severity: "error",
				})
			}
		}
	}

	// ----- LLM budget validation -----
	if p.LLMBudget != nil {
		if p.LLMBudget.MaxTokens < 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_budget.max_tokens",
				Message:  "max_tokens must be non-negative",
				Severity: "error",
			})
		}
		if p.LLMBudget.MaxTokensPerRequest < 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_budget.max_tokens_per_request",
				Message:  "max_tokens_per_request must be non-negative",
				Severity: "error",
			})
		}
		if p.LLMBudget.MaxTokens > 0 && p.LLMBudget.MaxTokensPerRequest > 0 && p.LLMBudget.MaxTokensPerRequest > p.LLMBudget.MaxTokens {
			errs = append(errs, ValidationError{
				Field:    "llm_budget.max_tokens_per_request",
				Message:  "max_tokens_per_request cannot exceed max_tokens",
				Severity: "error",
			})
		}
	}

	// ----- LLM rate limit validation -----
	if p.LLMRateLimit != nil {
		if p.LLMRateLimit.RequestsPerMinute < 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_rate_limit.requests_per_minute",
				Message:  "requests_per_minute must be non-negative",
				Severity: "error",
			})
		}
		if p.LLMRateLimit.TokensPerMinute < 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_rate_limit.tokens_per_minute",
				Message:  "tokens_per_minute must be non-negative",
				Severity: "error",
			})
		}
		if p.LLMRateLimit.RequestsPerMinute == 0 && p.LLMRateLimit.TokensPerMinute == 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_rate_limit",
				Message:  "at least one of requests_per_minute or tokens_per_minute must be > 0",
				Severity: "error",
			})
		}
	}

	// ----- Ingress auth validation -----
	if p.IngressAuth != nil {
		prefix := "ingress_auth"

		// Type must be "jwt" or "api_key".
		switch p.IngressAuth.Type {
		case "jwt":
			if p.IngressAuth.JWT == nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".jwt",
					Message:  "jwt config is required when ingress_auth type is 'jwt'",
					Severity: "error",
				})
			} else {
				if p.IngressAuth.JWT.Issuer == "" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".jwt.issuer",
						Message:  "issuer is required for JWT ingress auth",
						Severity: "error",
					})
				}
				if p.IngressAuth.JWT.Audience == "" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".jwt.audience",
						Message:  "audience is required for JWT ingress auth",
						Severity: "error",
					})
				}
				if p.IngressAuth.JWT.JWKSURL == "" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".jwt.jwks_url",
						Message:  "jwks_url is required for JWT ingress auth",
						Severity: "error",
					})
				} else {
					parsedJWKS, err := url.Parse(p.IngressAuth.JWT.JWKSURL)
					if err != nil || parsedJWKS.Scheme != "https" {
						errs = append(errs, ValidationError{
							Field:    prefix + ".jwt.jwks_url",
							Message:  "jwks_url must be a valid https URL",
							Severity: "error",
						})
					}
				}
			}
		case "api_key":
			if p.IngressAuth.APIKey == nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".api_key",
					Message:  "api_key config is required when ingress_auth type is 'api_key'",
					Severity: "error",
				})
			} else {
				if p.IngressAuth.APIKey.Header == "" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".api_key.header",
						Message:  "header is required for API key ingress auth",
						Severity: "error",
					})
				}
				if p.IngressAuth.APIKey.Credential == "" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".api_key.credential",
						Message:  "credential is required for API key ingress auth",
						Severity: "error",
					})
				} else if !credIDs[p.IngressAuth.APIKey.Credential] {
					errs = append(errs, ValidationError{
						Field:    prefix + ".api_key.credential",
						Message:  fmt.Sprintf("references undeclared credential %q", p.IngressAuth.APIKey.Credential),
						Severity: "error",
					})
				}
			}
		case "":
			errs = append(errs, ValidationError{
				Field:    prefix + ".type",
				Message:  "type is required when ingress_auth is configured (must be 'jwt' or 'api_key')",
				Severity: "error",
			})
		default:
			errs = append(errs, ValidationError{
				Field:    prefix + ".type",
				Message:  fmt.Sprintf("invalid ingress auth type %q; must be 'jwt' or 'api_key'", p.IngressAuth.Type),
				Severity: "error",
			})
		}
	}

	// ----- LLM provider lock validation -----
	if p.LLMProviderLock != nil {
		if len(p.LLMProviderLock.AllowedEndpoints) == 0 {
			errs = append(errs, ValidationError{
				Field:    "llm_provider_lock.allowed_endpoints",
				Message:  "at least one endpoint is required when llm_provider_lock is configured",
				Severity: "error",
			})
		}
		for i, endpoint := range p.LLMProviderLock.AllowedEndpoints {
			prefix := fmt.Sprintf("llm_provider_lock.allowed_endpoints[%d]", i)
			u, parseErr := url.Parse(endpoint)
			if parseErr != nil {
				errs = append(errs, ValidationError{
					Field:    prefix,
					Message:  fmt.Sprintf("invalid URL: %v", parseErr),
					Severity: "error",
				})
				continue
			}
			if u.Scheme != "https" {
				errs = append(errs, ValidationError{
					Field:    prefix,
					Message:  fmt.Sprintf("endpoint must use https scheme, got %q", u.Scheme),
					Severity: "error",
				})
			}
			if u.Host == "" {
				errs = append(errs, ValidationError{
					Field:    prefix,
					Message:  "endpoint URL must include a hostname",
					Severity: "error",
				})
			}
			if u.Path == "" {
				errs = append(errs, ValidationError{
					Field:    prefix,
					Message:  "endpoint URL must include a path",
					Severity: "error",
				})
			}
		}
	}

	// ----- Guardrails validation -----
	for i, g := range p.Guardrails {
		prefix := fmt.Sprintf("guardrails[%d]", i)

		// type is required.
		if g.Type == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".type",
				Message:  "guardrail type is required",
				Severity: "error",
			})
			continue
		}

		switch g.Type {
		case "regex":
			if g.Pattern == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".pattern",
					Message:  "pattern is required for regex guardrail",
					Severity: "error",
				})
			}
			if g.Action != "block" && g.Action != "mask" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".action",
					Message:  fmt.Sprintf("action must be 'block' or 'mask', got %q", g.Action),
					Severity: "error",
				})
			}
		case "moderation":
			if g.Provider == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".provider",
					Message:  "provider is required for moderation guardrail",
					Severity: "error",
				})
			}
			if g.Credential == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".credential",
					Message:  "credential is required for moderation guardrail",
					Severity: "error",
				})
			} else if !credIDs[g.Credential] {
				errs = append(errs, ValidationError{
					Field:    prefix + ".credential",
					Message:  fmt.Sprintf("references undeclared credential %q", g.Credential),
					Severity: "error",
				})
			}
		case "webhook":
			if g.URL == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".url",
					Message:  "url is required for webhook guardrail",
					Severity: "error",
				})
			} else {
				parsedURL, urlErr := url.Parse(g.URL)
				if urlErr != nil || parsedURL.Scheme != "https" {
					errs = append(errs, ValidationError{
						Field:    prefix + ".url",
						Message:  fmt.Sprintf("url must be a valid https URL, got %q", g.URL),
						Severity: "error",
					})
				} else if !hasMatchingEgress(p.Egress, parsedURL.Hostname()) {
					errs = append(errs, ValidationError{
						Field:    prefix + ".url",
						Message:  fmt.Sprintf("webhook guardrail %q has no matching egress allow rule", redactURL(g.URL)),
						Severity: "error",
					})
				}
			}
		default:
			errs = append(errs, ValidationError{
				Field:    prefix + ".type",
				Message:  fmt.Sprintf("invalid guardrail type %q; must be 'regex', 'moderation', or 'webhook'", g.Type),
				Severity: "error",
			})
		}
	}

	// ----- Transformations validation -----
	if p.Transformations != nil {
		if p.Transformations.Request == nil && p.Transformations.Response == nil {
			errs = append(errs, ValidationError{
				Field:    "transformations",
				Message:  "transformations is set but neither request nor response is configured",
				Severity: "error",
			})
		}
		if p.Transformations.Request != nil {
			if len(p.Transformations.Request.InjectSystemPrompt) > 4096 {
				errs = append(errs, ValidationError{
					Field:    "transformations.request.inject_system_prompt",
					Message:  "inject_system_prompt must not exceed 4096 characters",
					Severity: "error",
				})
			}
		}
		if p.Transformations.Response != nil {
			for i, h := range p.Transformations.Response.RemoveHeaders {
				if !isValidHeaderName(h) {
					errs = append(errs, ValidationError{
						Field:    fmt.Sprintf("transformations.response.remove_headers[%d]", i),
						Message:  fmt.Sprintf("invalid HTTP header name %q: contains control characters or is empty", h),
						Severity: "error",
					})
				}
			}
		}
	}

	// ----- Egress timeout & retry validation -----
	for i, e := range p.Egress {
		prefix := fmt.Sprintf("egress[%d]", i)
		if e.Timeout != "" {
			if _, derr := time.ParseDuration(e.Timeout); derr != nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".timeout",
					Message:  fmt.Sprintf("invalid duration %q: %v", e.Timeout, derr),
					Severity: "error",
				})
			}
		}
		if e.Retry != nil {
			if e.Retry.MaxAttempts < 1 {
				errs = append(errs, ValidationError{
					Field:    prefix + ".retry.max_attempts",
					Message:  "max_attempts must be >= 1",
					Severity: "error",
				})
			}
			switch e.Retry.Backoff {
			case "", "exponential", "linear", "fixed":
			default:
				errs = append(errs, ValidationError{
					Field:    prefix + ".retry.backoff",
					Message:  fmt.Sprintf("backoff must be 'exponential', 'linear', or 'fixed', got %q", e.Retry.Backoff),
					Severity: "error",
				})
			}
			if e.Retry.MaxBackoff != "" {
				if _, derr := time.ParseDuration(e.Retry.MaxBackoff); derr != nil {
					errs = append(errs, ValidationError{
						Field:    prefix + ".retry.max_backoff",
						Message:  fmt.Sprintf("invalid duration %q: %v", e.Retry.MaxBackoff, derr),
						Severity: "error",
					})
				}
			}
		}
	}

	// ----- Observability validation -----
	if p.Observability != nil {
		if p.Observability.OTelEndpoint != "" {
			u, err := url.Parse(p.Observability.OTelEndpoint)
			if err != nil || u.Scheme == "" || u.Host == "" {
				errs = append(errs, ValidationError{
					Field:    "observability.otel_endpoint",
					Message:  fmt.Sprintf("otel_endpoint must be a valid URL, got %q", p.Observability.OTelEndpoint),
					Severity: "error",
				})
			}
		}
	}

	// ----- MCP tool access validation -----
	for i, m := range p.MCPServers {
		prefix := fmt.Sprintf("mcp_servers[%d]", i)
		// Check that a tool is not in both allowed and denied lists.
		allowed := make(map[string]bool)
		for _, t := range m.AllowedTools {
			allowed[t] = true
		}
		for _, t := range m.DeniedTools {
			if allowed[t] {
				errs = append(errs, ValidationError{
					Field:    prefix + ".denied_tools",
					Message:  fmt.Sprintf("tool %q is in both allowed_tools and denied_tools", t),
					Severity: "error",
				})
			}
		}
	}

	// ----- Version-specific validation -----
	errs = append(errs, validateVersionSpecificRules(p)...)

	// ----- Routed run validation (v1.1+) -----
	if p.Version == SchemaVersion11 && p.RoutedRun != nil {
		errs = append(errs, validateRoutedRun(p.RoutedRun)...)
	}

	return errs
}

// ValidatePolicyWithRoute performs the same validation as ValidatePolicy,
// plus route/candidate validation that requires the agent.yaml route name.
// This is called during pack when both policy.yaml and agent.yaml are available.
func ValidatePolicyWithRoute(p *Policy, routeName string) []ValidationError {
	errs := ValidatePolicy(p)
	if p.Version == SchemaVersion11 {
		errs = append(errs, validateRouteAndCandidateRules(p, routeName)...)
	}
	return errs
}

// isValidHeaderName checks whether a string is a valid HTTP header name
// (no control characters, not empty).
func isValidHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// redactURL removes userinfo (credentials/tokens) from a URL string
// for safe inclusion in error messages. If the URL cannot be parsed,
// the original string is returned unchanged.
func redactURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "***redacted***")
	}
	return u.String()
}

// stripPort removes the port suffix from a host if present.
func stripPort(host string) string {
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		// IPv6 bracketed hosts like [::1]:8080
		if strings.HasSuffix(host[:idx], "]") {
			// Could be IPv6 with port — find the last colon after the closing bracket
			return host[:idx]
		}
		// Simple host:port or IPv4:port
		if strings.Count(host, ":") == 1 {
			return host[:idx]
		}
		// IPv6 without brackets — no port
	}
	return host
}

// IsLoopbackAddress checks if a hostname is a loopback address.
func IsLoopbackAddress(host string) bool {
	if host == "" {
		return false
	}
	// Case-insensitive comparison for named loopbacks.
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "localhost.localdomain" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	// Subdomain of localhost (e.g., "x.localhost", "x.Localhost").
	if strings.HasSuffix(lower, ".localhost") {
		return true
	}
	// Check IP CIDR
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// hasMatchingEgress checks if any egress rule's domain matches the given host.
// For exact hostname matching (default), the domain must equal the host.
// For wildcard domains, the host must match the wildcard pattern.
func hasMatchingEgress(rules []EgressRule, host string) bool {
	for _, e := range rules {
		if e.Domain == "" {
			continue
		}
		hasWild, cleanHost, _ := validateHostname(e.Domain)
		if hasWild {
			// Wildcard: "*.example.com" matches "api.example.com" but not "example.com"
			if strings.HasSuffix(host, "."+cleanHost) {
				return true
			}
		} else if host == e.Domain {
			return true
		}
	}

	// Also check CIDR-based egress rules for completeness.
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, e := range rules {
		if e.CIDR == "" {
			continue
		}
		_, cidrNet, err := net.ParseCIDR(e.CIDR)
		if err != nil {
			continue
		}
		if cidrNet.Contains(ip) {
			return true
		}
	}
	return false
}

// GetErrorCount returns the number of error-severity findings.
func GetErrorCount(errs []ValidationError) int {
	count := 0
	for _, e := range errs {
		if e.Severity == "error" {
			count++
		}
	}
	return count
}

// HasErrors returns true if any validation errors exist (excluding warnings).
func HasErrors(errs []ValidationError) bool {
	return GetErrorCount(errs) > 0
}

// ContainsInjectionPattern checks for common HTTP injection characters
// in a string.  Exported for use by higher-level validation layers.
func ContainsInjectionPattern(s string) bool {
	return strings.ContainsAny(s, "\r\n\x00")
}

// validateRouteIDChars validates the route ID character grammar.
// Must match: ^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$
func validateRouteIDChars(id string) error {
	if id == "" {
		return fmt.Errorf("route ID must not be empty")
	}
	if len(id) > 128 {
		return fmt.Errorf("route ID %q exceeds 128 characters", id)
	}
	if id[0] < 'a' || id[0] > 'z' {
		return fmt.Errorf("route ID %q must start with a lowercase letter", id)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' || c == '_' || c == '-' {
			if i+1 >= len(id) {
				return fmt.Errorf("route ID %q: trailing separator not allowed", id)
			}
			next := id[i+1]
			if next == '.' || next == '_' || next == '-' {
				return fmt.Errorf("route ID %q: consecutive separators not allowed", id)
			}
			if next < '0' || (next > '9' && next < 'a') || next > 'z' {
				return fmt.Errorf("route ID %q: separator must be followed by alphanumeric", id)
			}
			continue
		}
		return fmt.Errorf("route ID %q: invalid character %q", id, c)
	}
	return nil
}

// validateUpstreamProviderChars validates safe ASCII characters for upstream providers.
func validateUpstreamProviderChars(s string) error {
	if s == "" {
		return fmt.Errorf("upstream provider must not be empty")
	}
	if len(s) > 128 {
		return fmt.Errorf("upstream provider %q exceeds 128 characters", s)
	}
	for _, r := range s {
		if r <= 0x20 || r > 0x7e {
			return fmt.Errorf("upstream provider %q contains invalid character", s)
		}
	}
	if strings.Contains(s, "/") || strings.Contains(s, "\\") || strings.Contains(s, ".") {
		return fmt.Errorf("upstream provider %q contains URL syntax, backslashes, or dot segments", s)
	}
	return nil
}

// validateVersionSpecificRules checks version-specific validation rules.
// v1.0 policies must not have routed fields.
// v1.1 policies must have llm_budget.max_cost_usd.
func validateVersionSpecificRules(p *Policy) []ValidationError {
	var errs []ValidationError

	if p.Version == SchemaVersion10 && p.HasRoutedFields() {
		errs = append(errs, ValidationError{
			Field:    "version",
			Message:  "v1.0 policy must not have v1.1 routed fields (routed_run, model_routes, max_cost_usd)",
			Severity: "error",
		})
	}

	if p.Version == SchemaVersion11 {
		if p.LLMBudget == nil || p.LLMBudget.MaxCostUSD == "" {
			errs = append(errs, ValidationError{
				Field:    "llm_budget.max_cost_usd",
				Message:  "v1.1 policy requires llm_budget.max_cost_usd to be set explicitly",
				Severity: "error",
			})
		}
		if p.LLMBudget != nil && p.LLMBudget.MaxCostUSD != "" {
			if _, err := money.Parse(p.LLMBudget.MaxCostUSD); err != nil {
				errs = append(errs, ValidationError{
					Field:    "llm_budget.max_cost_usd",
					Message:  fmt.Sprintf("invalid max_cost_usd: %v", err),
					Severity: "error",
				})
			}
		}
	}

	return errs
}

// validateRouteAndCandidateRules validates the model_routes, candidates, and
// routed_run fields for v1.1+ policies.
func validateRouteAndCandidateRules(p *Policy, routeName string) []ValidationError {
	var errs []ValidationError

	if p.Version != SchemaVersion11 {
		return nil
	}

	if len(p.ModelRoutes) == 0 {
		errs = append(errs, ValidationError{
			Field:    "model_routes",
			Message:  "v1.1 policy requires at least one model route",
			Severity: "error",
		})
		return errs
	}

	// Validate the named route exists
	if routeName != "" {
		if _, ok := p.ModelRoutes[routeName]; !ok {
			errs = append(errs, ValidationError{
				Field:    "model_routes",
				Message:  fmt.Sprintf("route %q named in agent.yaml does not exist in model_routes", routeName),
				Severity: "error",
			})
		}
	}

	// Exactly one executable route
	if routeName != "" {
		for name := range p.ModelRoutes {
			if name == routeName {
				// Executable route: validate it
				route := p.ModelRoutes[name]
				errs = append(errs, validateSingleRoute(name, route, p)...)
			} else {
				// Extra routes: reject
				errs = append(errs, ValidationError{
					Field:    "model_routes",
					Message:  fmt.Sprintf("extra route %q found; only the named route %q is allowed in v0.3", name, routeName),
					Severity: "error",
				})
			}
		}
	}

	// Validate route IDs
	for name := range p.ModelRoutes {
		if err := validateRouteIDChars(name); err != nil {
			errs = append(errs, ValidationError{
				Field:    "model_routes",
				Message:  fmt.Sprintf("invalid route ID %q: %v", name, err),
				Severity: "error",
			})
		}
	}

	// Validate RoutedRun fields
	if p.RoutedRun != nil {
		errs = append(errs, validateRoutedRun(p.RoutedRun)...)
	}

	return errs
}

func validateSingleRoute(name string, route ModelRoute, p *Policy) []ValidationError {
	var errs []ValidationError

	// Validate pattern
	if route.Pattern != PatternLocalFirst && route.Pattern != PatternCloudCostFirst {
		errs = append(errs, ValidationError{
			Field:    fmt.Sprintf("model_routes[%s].pattern", name),
			Message:  fmt.Sprintf("unknown pattern %q; must be %q or %q", route.Pattern, PatternLocalFirst, PatternCloudCostFirst),
			Severity: "error",
		})
	}

	// Validate cloud_transfer
	if route.CloudTransfer != CloudTransferAllowed && route.CloudTransfer != CloudTransferDenied {
		errs = append(errs, ValidationError{
			Field:    fmt.Sprintf("model_routes[%s].cloud_transfer", name),
			Message:  fmt.Sprintf("unknown cloud_transfer %q; must be %q or %q", route.CloudTransfer, CloudTransferAllowed, CloudTransferDenied),
			Severity: "error",
		})
	}

	// Candidate count: 2-64
	if len(route.Candidates) < 2 {
		errs = append(errs, ValidationError{
			Field:    fmt.Sprintf("model_routes[%s].candidates", name),
			Message:  "at least 2 candidates required (minimum 2-64)",
			Severity: "error",
		})
	}
	if len(route.Candidates) > 64 {
		errs = append(errs, ValidationError{
			Field:    fmt.Sprintf("model_routes[%s].candidates", name),
			Message:  "at most 64 candidates allowed",
			Severity: "error",
		})
	}

	// Candidate IDs must be unique and safe
	seenIDs := make(map[string]bool)
	for _, c := range route.Candidates {
		if c.ID == "" {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].candidates", name),
				Message:  "candidate ID must not be empty",
				Severity: "error",
			})
			continue
		}
		if len(c.ID) > 64 {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].candidates[%s]", name, c.ID),
				Message:  "candidate ID exceeds 64 characters",
				Severity: "error",
			})
		}
		if err := validateRouteIDChars(c.ID); err != nil {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].candidates[%s]", name, c.ID),
				Message:  fmt.Sprintf("invalid candidate ID: %v", err),
				Severity: "error",
			})
		}
		if seenIDs[c.ID] {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].candidates", name),
				Message:  fmt.Sprintf("duplicate candidate ID %q", c.ID),
				Severity: "error",
			})
		}
		seenIDs[c.ID] = true
	}

	// Validate roles and locations
	hasPrimary := false
	hasRecovery := false

	for _, c := range route.Candidates {
		if c.Role == RolePrimary {
			hasPrimary = true
		}
		if c.Role == RoleRecovery {
			hasRecovery = true
		}
	}

	// Check recovery enabled
	maxRecoveries := 0
	if p.RoutedRun != nil {
		maxRecoveries = p.RoutedRun.MaxModelRecoveriesPerAttempt
	}

	if maxRecoveries > 0 && (!hasPrimary || !hasRecovery) {
		errs = append(errs, ValidationError{
			Field:    fmt.Sprintf("model_routes[%s].candidates", name),
			Message:  "at least one primary and one recovery candidate required when automatic recovery is enabled",
			Severity: "error",
		})
	}

	// Validate each candidate
	for _, c := range route.Candidates {
		prefix := fmt.Sprintf("model_routes[%s].candidates[%s]", name, c.ID)
		errs = append(errs, validateCandidate(c, prefix, route)...)
	}

	// Validate minimum requirements
	if route.Minimum != nil {
		if route.Minimum.CapabilityTier != "" &&
			route.Minimum.CapabilityTier != CapabilityBasic &&
			route.Minimum.CapabilityTier != CapabilityStandard &&
			route.Minimum.CapabilityTier != CapabilityAdvanced {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].minimum.capability_tier", name),
				Message:  fmt.Sprintf("unknown capability_tier %q", route.Minimum.CapabilityTier),
				Severity: "error",
			})
		}
		for _, f := range route.Minimum.Features {
			if f != FeatureChat && f != FeatureStructuredJSON && f != FeatureReasoningEffort {
				errs = append(errs, ValidationError{
					Field:    fmt.Sprintf("model_routes[%s].minimum.features", name),
					Message:  fmt.Sprintf("unknown feature %q", f),
					Severity: "error",
				})
			}
		}
		if route.Minimum.Effort != "" && route.Minimum.Effort != EffortStandard && route.Minimum.Effort != EffortHigh {
			errs = append(errs, ValidationError{
				Field:    fmt.Sprintf("model_routes[%s].minimum.effort", name),
				Message:  fmt.Sprintf("unknown effort %q", route.Minimum.Effort),
				Severity: "error",
			})
		}
	}

	return errs
}

func validateCandidate(c Candidate, prefix string, route ModelRoute) []ValidationError {
	var errs []ValidationError

	// Validate role
	if c.Role != RolePrimary && c.Role != RoleRecovery {
		errs = append(errs, ValidationError{
			Field:    prefix + ".role",
			Message:  fmt.Sprintf("unknown role %q; must be %q or %q", c.Role, RolePrimary, RoleRecovery),
			Severity: "error",
		})
	}

	// Validate location
	if c.Location != LocationLocal && c.Location != LocationCloud {
		errs = append(errs, ValidationError{
			Field:    prefix + ".location",
			Message:  fmt.Sprintf("unknown location %q; must be %q or %q", c.Location, LocationLocal, LocationCloud),
			Severity: "error",
		})
	}

	// local-first pattern rules
	if route.Pattern == PatternLocalFirst {
		// Primary candidates must be local
		if c.Role == RolePrimary && c.Location != LocationLocal {
			errs = append(errs, ValidationError{
				Field:    prefix + ".location",
				Message:  "local-first primary candidates must be local",
				Severity: "error",
			})
		}
		// Recovery candidates must be cloud
		if c.Role == RoleRecovery && c.Location != LocationCloud {
			errs = append(errs, ValidationError{
				Field:    prefix + ".location",
				Message:  "local-first recovery candidates must be cloud",
				Severity: "error",
			})
		}
	}

	// cloud-cost-first pattern rules
	if route.Pattern == PatternCloudCostFirst {
		// All candidates must be cloud
		if c.Location != LocationCloud {
			errs = append(errs, ValidationError{
				Field:    prefix + ".location",
				Message:  "cloud-cost-first candidates must be cloud",
				Severity: "error",
			})
		}
	}

	// Cloud transfer denial
	if route.CloudTransfer == CloudTransferDenied && c.Location == LocationCloud {
		errs = append(errs, ValidationError{
			Field:    prefix + ".location",
			Message:  "cloud candidate rejected when cloud_transfer is denied",
			Severity: "error",
		})
	}

	// Valid provider
	if c.Provider == "" {
		errs = append(errs, ValidationError{
			Field:    prefix + ".provider",
			Message:  "provider is required",
			Severity: "error",
		})
	}

	// Credential reference
	if c.Provider == "openrouter" && c.Credential == "" {
		errs = append(errs, ValidationError{
			Field:    prefix + ".credential",
			Message:  "credential is required for OpenRouter provider",
			Severity: "error",
		})
	}

	// OpenRouter: upstream_providers is required and validated
	if c.Provider == "openrouter" {
		if len(c.UpstreamProviders) == 0 {
			errs = append(errs, ValidationError{
				Field:    prefix + ".upstream_providers",
				Message:  "upstream_providers is required for OpenRouter candidates",
				Severity: "error",
			})
		}
		if len(c.UpstreamProviders) > 8 {
			errs = append(errs, ValidationError{
				Field:    prefix + ".upstream_providers",
				Message:  "at most 8 upstream providers allowed",
				Severity: "error",
			})
		}
		// Check for duplicates and validate each
		seen := make(map[string]bool)
		for _, up := range c.UpstreamProviders {
			if seen[up] {
				errs = append(errs, ValidationError{
					Field:    prefix + ".upstream_providers",
					Message:  fmt.Sprintf("duplicate upstream_provider %q", up),
					Severity: "error",
				})
			}
			seen[up] = true
			if err := validateUpstreamProviderChars(up); err != nil {
				errs = append(errs, ValidationError{
					Field:    prefix + ".upstream_providers",
					Message:  fmt.Sprintf("invalid upstream_provider: %v", err),
					Severity: "error",
				})
			}
		}
	}

	// Direct/local: no upstream_providers
	if c.Provider != "openrouter" && len(c.UpstreamProviders) > 0 {
		errs = append(errs, ValidationError{
			Field:    prefix + ".upstream_providers",
			Message:  "upstream_providers only allowed for OpenRouter candidates",
			Severity: "error",
		})
	}

	// auth: none only for local custom endpoints
	if c.AuthMode == AuthModeNone && c.Location != LocationLocal {
		errs = append(errs, ValidationError{
			Field:    prefix + ".auth_mode",
			Message:  "auth: none only accepted for local custom endpoints",
			Severity: "error",
		})
	}

	// Custom endpoint with local location requires allow_private in egress.
	// Full egress integration is deferred to B32/B35; validation of
	// allow_private in egress rules will be added when the compiler
	// integration is implemented.

	return errs
}

func validateRoutedRun(rr *RoutedRunPolicy) []ValidationError {
	var errs []ValidationError

	// Validate durations can be parsed
	durations := map[string]string{
		"model_call_timeout":  rr.ModelCallTimeout,
		"stall_timeout":       rr.StallTimeout,
		"attempt_lease":       rr.AttemptLease,
		"max_active_duration": rr.MaxActiveDuration,
		"recovery_margin":     rr.RecoveryMargin,
	}
	durValues := make(map[string]time.Duration)
	for field, val := range durations {
		if val == "" {
			errs = append(errs, ValidationError{
				Field:    "routed_run." + field,
				Message:  "duration is required",
				Severity: "error",
			})
			continue
		}
		d, err := time.ParseDuration(val)
		if err != nil {
			errs = append(errs, ValidationError{
				Field:    "routed_run." + field,
				Message:  fmt.Sprintf("invalid duration %q: %v", val, err),
				Severity: "error",
			})
			continue
		}
		if d <= 0 {
			errs = append(errs, ValidationError{
				Field:    "routed_run." + field,
				Message:  "duration must be positive",
				Severity: "error",
			})
		}
		durValues[field] = d
	}

	// Validate limit relationships (only if all durations parsed)
	if len(durValues) == 5 {
		ct := durValues["model_call_timeout"]
		st := durValues["stall_timeout"]
		al := durValues["attempt_lease"]
		ma := durValues["max_active_duration"]
		rm := durValues["recovery_margin"]

		if ct > al {
			errs = append(errs, ValidationError{
				Field:    "routed_run",
				Message:  "model_call_timeout must be <= attempt_lease",
				Severity: "error",
			})
		}
		if st > al {
			errs = append(errs, ValidationError{
				Field:    "routed_run",
				Message:  "stall_timeout must be <= attempt_lease",
				Severity: "error",
			})
		}
		if al >= ma {
			errs = append(errs, ValidationError{
				Field:    "routed_run",
				Message:  "attempt_lease must be < max_active_duration",
				Severity: "error",
			})
		}
		if rm <= 0 || rm >= ma {
			errs = append(errs, ValidationError{
				Field:    "routed_run",
				Message:  "recovery_margin must be > 0 and < max_active_duration",
				Severity: "error",
			})
		}
		if al+rm > ma {
			errs = append(errs, ValidationError{
				Field:    "routed_run",
				Message:  "attempt_lease + recovery_margin must be <= max_active_duration",
				Severity: "error",
			})
		}
	}

	// Validate integer limits
	if rr.MaxModelRecoveriesPerAttempt != 0 && rr.MaxModelRecoveriesPerAttempt != 1 {
		errs = append(errs, ValidationError{
			Field:    "routed_run.max_model_recoveries_per_attempt",
			Message:  "must be 0 or 1",
			Severity: "error",
		})
	}
	if rr.MaxWorkerRetries != 0 && rr.MaxWorkerRetries != 1 {
		errs = append(errs, ValidationError{
			Field:    "routed_run.max_worker_retries",
			Message:  "must be 0 or 1",
			Severity: "error",
		})
	}
	if rr.MaxLLMCalls < 0 {
		errs = append(errs, ValidationError{
			Field:    "routed_run.max_llm_calls",
			Message:  "must be non-negative",
			Severity: "error",
		})
	}
	if rr.MaxIdenticalToolActions < 0 {
		errs = append(errs, ValidationError{
			Field:    "routed_run.max_identical_tool_actions",
			Message:  "must be non-negative",
			Severity: "error",
		})
	}
	if rr.MaxActionsWithoutProgress < 0 {
		errs = append(errs, ValidationError{
			Field:    "routed_run.max_actions_without_progress",
			Message:  "must be non-negative",
			Severity: "error",
		})
	}

	return errs
}