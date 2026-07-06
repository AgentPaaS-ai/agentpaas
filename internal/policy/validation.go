package policy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
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

	return errs
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