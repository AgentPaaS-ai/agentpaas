package policy

import (
	"fmt"
	"net/url"
	"strings"
)

// KnownProviderDefaults holds default OAuth endpoints for well-known providers.
// For these providers, auth_endpoint and token_endpoint are optional in policy;
// empty values are filled at validate/compile time. For provider "generic"
// (or any unknown provider), both endpoints are required.
var knownProviderDefaults = map[string]struct {
	AuthEndpoint  string
	TokenEndpoint string
}{
	"google": {
		AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint: "https://oauth2.googleapis.com/token",
	},
	"github": {
		AuthEndpoint:  "https://github.com/login/oauth/authorize",
		TokenEndpoint: "https://github.com/login/oauth/access_token",
	},
	"slack": {
		AuthEndpoint:  "https://slack.com/oauth/v2/authorize",
		TokenEndpoint: "https://slack.com/api/oauth.v2.access",
	},
}

// isKnownOAuthProvider reports whether provider has default endpoints.
// "generic" and any unrecognised value require explicit endpoints.
func isKnownOAuthProvider(provider string) bool {
	_, ok := knownProviderDefaults[provider]
	return ok
}

// NormalizeScope returns a canonical, comparable form of an OAuth scope string.
// Normalisation is intentionally minimal and deterministic: surrounding
// whitespace is trimmed. Scope strings are compared by exact match after
// normalisation. This is the single point that consent/inject math must route
// through so granted-vs-max comparisons are stable.
func NormalizeScope(s string) string {
	return strings.TrimSpace(s)
}

// ScopeSetContains reports whether every scope in granted is present in max
// (set membership), after normalising each scope via NormalizeScope. Returns
// true if granted is empty. Order and duplicates do not affect the result.
// This is the core of the SC3 scope-escalation check: granted ⊆ max.
func ScopeSetContains(max, granted []string) bool {
	maxSet := make(map[string]struct{}, len(max))
	for _, s := range max {
		maxSet[NormalizeScope(s)] = struct{}{}
	}
	for _, g := range granted {
		if _, ok := maxSet[NormalizeScope(g)]; !ok {
			return false
		}
	}
	return true
}

// ScopesWithinMax reports whether every scope in requested is present in max
// (requested ⊆ max), after normalisation. This validates the policy rule that
// max_scopes must be a superset of scopes. Equivalent to
// ScopeSetContains(max, requested).
func ScopesWithinMax(requested, max []string) bool {
	return ScopeSetContains(max, requested)
}

// IsWildcardScope reports whether a scope string grants broad access that
// should require an explicit reason in policy. A scope is considered wildcard
// if, after normalisation, it is exactly "*" OR ends with "/*" OR matches the
// Google full-mailbox style "https://mail.google.com/" (a bare host URL with a
// trailing slash and no path beyond it). Such scopes are permitted in
// max_scopes only when the credential carries a non-empty reason field.
func IsWildcardScope(s string) bool {
	n := NormalizeScope(s)
	if n == "" {
		return false
	}
	if n == "*" {
		return true
	}
	if strings.HasSuffix(n, "/*") {
		return true
	}
	// Google full-mailbox style: https://mail.google.com/
	// i.e. an https/http URL with host, empty path beyond a single trailing slash.
	if strings.HasPrefix(n, "https://") || strings.HasPrefix(n, "http://") {
		// Strip scheme.
		rest := n[strings.Index(n, "://")+3:]
		// Must have a host and exactly a trailing slash with nothing after.
		slash := strings.IndexByte(rest, '/')
		if slash > 0 && slash == len(rest)-1 {
			return true
		}
	}
	return false
}

// validateOAuthDelegated applies all M13.9-T1 policy rules for a credential of
// type oauth_delegated. It does not mutate the credential. Rules enforced:
//   - provider is required (google|github|slack|generic); unknown => generic
//   - client_id_credential required and must reference a declared credential id
//   - client_secret_credential optional; if set must reference a declared id
//   - scopes non-empty required; max_scopes non-empty required
//   - max_scopes must be a superset of scopes (ScopesWithinMax)
//   - wildcard scope in max_scopes requires non-empty reason
//   - generic provider: auth_endpoint + token_endpoint required and must be https
//   - known providers: endpoints optional; if provided must be https
func validateOAuthDelegated(c Credential, credIDs map[string]bool, prefix string) []ValidationError {
	var errs []ValidationError

	// Provider: required. Empty is treated as a missing field error.
	provider := strings.TrimSpace(c.Provider)
	if provider == "" {
		errs = append(errs, ValidationError{
			Field:    prefix + ".provider",
			Message:  "oauth_delegated credential requires a provider (google, github, slack, or generic)",
			Severity: "error",
		})
	}

	// client_id_credential required + must reference declared id.
	if c.ClientIDCredential == "" {
		errs = append(errs, ValidationError{
			Field:    prefix + ".client_id_credential",
			Message:  "oauth_delegated credential requires client_id_credential",
			Severity: "error",
		})
	} else if !credIDs[c.ClientIDCredential] {
		errs = append(errs, ValidationError{
			Field:    prefix + ".client_id_credential",
			Message:  fmt.Sprintf("client_id_credential %q is not a declared credential ID", c.ClientIDCredential),
			Severity: "error",
		})
	}

	// client_secret_credential optional; if set must reference declared id.
	if c.ClientSecretCredential != "" && !credIDs[c.ClientSecretCredential] {
		errs = append(errs, ValidationError{
			Field:    prefix + ".client_secret_credential",
			Message:  fmt.Sprintf("client_secret_credential %q is not a declared credential ID", c.ClientSecretCredential),
			Severity: "error",
		})
	}

	// scopes non-empty required.
	if len(c.Scopes) == 0 {
		errs = append(errs, ValidationError{
			Field:    prefix + ".scopes",
			Message:  "oauth_delegated credential requires a non-empty scopes list",
			Severity: "error",
		})
	}

	// max_scopes non-empty required.
	if len(c.MaxScopes) == 0 {
		errs = append(errs, ValidationError{
			Field:    prefix + ".max_scopes",
			Message:  "oauth_delegated credential requires a non-empty max_scopes list",
			Severity: "error",
		})
	}

	// max_scopes must be a superset of scopes.
	if len(c.Scopes) > 0 && len(c.MaxScopes) > 0 {
		if !ScopesWithinMax(c.Scopes, c.MaxScopes) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".max_scopes",
				Message:  "max_scopes must be a superset of scopes (every requested scope must appear in max_scopes)",
				Severity: "error",
			})
		}
	}

	// Wildcard scope in max_scopes requires non-empty reason.
	if len(c.MaxScopes) > 0 {
		for _, sc := range c.MaxScopes {
			if IsWildcardScope(sc) && strings.TrimSpace(c.Reason) == "" {
				errs = append(errs, ValidationError{
					Field:    prefix + ".reason",
					Message:  fmt.Sprintf("max_scopes contains wildcard scope %q; a non-empty reason is required for wildcard scopes", sc),
					Severity: "error",
				})
				break // one error is enough; the user sees the offending scope
			}
		}
	}

	// Endpoint rules depend on provider knowledge.
	// For known providers endpoints are optional (defaults exist). For
	// generic / unknown providers both endpoints are REQUIRED and must be https.
	authEP := strings.TrimSpace(c.AuthEndpoint)
	tokenEP := strings.TrimSpace(c.TokenEndpoint)
	if isKnownOAuthProvider(provider) {
		// Optional, but if provided must be https.
		if authEP != "" && !isHTTPSURL(authEP) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".auth_endpoint",
				Message:  fmt.Sprintf("auth_endpoint must be a valid https URL, got %q", authEP),
				Severity: "error",
			})
		}
		if tokenEP != "" && !isHTTPSURL(tokenEP) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".token_endpoint",
				Message:  fmt.Sprintf("token_endpoint must be a valid https URL, got %q", tokenEP),
				Severity: "error",
			})
		}
	} else {
		// generic or unknown provider: both required and must be https.
		if authEP == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".auth_endpoint",
				Message:  "oauth_delegated credential with generic/unknown provider requires auth_endpoint",
				Severity: "error",
			})
		} else if !isHTTPSURL(authEP) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".auth_endpoint",
				Message:  fmt.Sprintf("auth_endpoint must be a valid https URL, got %q", authEP),
				Severity: "error",
			})
		}
		if tokenEP == "" {
			errs = append(errs, ValidationError{
				Field:    prefix + ".token_endpoint",
				Message:  "oauth_delegated credential with generic/unknown provider requires token_endpoint",
				Severity: "error",
			})
		} else if !isHTTPSURL(tokenEP) {
			errs = append(errs, ValidationError{
				Field:    prefix + ".token_endpoint",
				Message:  fmt.Sprintf("token_endpoint must be a valid https URL, got %q", tokenEP),
				Severity: "error",
			})
		}
	}

	return errs
}

// isHTTPSURL reports whether s parses as a URL with scheme https and a host.
func isHTTPSURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "https" && u.Host != ""
}

// rejectOAuthDelegatedOnlyFields fails closed when oauth_delegated-only fields
// appear on a credential that is not type=oauth_delegated. YAML KnownFields
// alone cannot enforce type-conditional keys; validation closes that gap.
func rejectOAuthDelegatedOnlyFields(c Credential, prefix string) []ValidationError {
	var errs []ValidationError
	check := func(field, label string, set bool) {
		if !set {
			return
		}
		errs = append(errs, ValidationError{
			Field:    prefix + "." + field,
			Message:  fmt.Sprintf("%s is only valid on type oauth_delegated credentials, not type %q", label, c.Type),
			Severity: "error",
		})
	}
	check("provider", "provider", strings.TrimSpace(c.Provider) != "")
	check("auth_endpoint", "auth_endpoint", strings.TrimSpace(c.AuthEndpoint) != "")
	// token_endpoint is shared with type=oauth (B19). Only reject when type is
	// neither oauth nor oauth_delegated — callers invoke this helper only on
	// non-delegated types; for type=oauth, skip token_endpoint/client_id style
	// fields that legitimately belong to backend refresh.
	if c.Type != "oauth" {
		check("token_endpoint", "token_endpoint", strings.TrimSpace(c.TokenEndpoint) != "")
		check("client_id", "client_id", strings.TrimSpace(c.ClientID) != "")
		check("refresh_token_credential", "refresh_token_credential", strings.TrimSpace(c.RefreshTokenCredential) != "")
	}
	check("client_id_credential", "client_id_credential", strings.TrimSpace(c.ClientIDCredential) != "")
	check("client_secret_credential", "client_secret_credential", strings.TrimSpace(c.ClientSecretCredential) != "")
	check("scopes", "scopes", len(c.Scopes) > 0)
	check("max_scopes", "max_scopes", len(c.MaxScopes) > 0)
	check("redirect_path", "redirect_path", strings.TrimSpace(c.RedirectPath) != "")
	return errs
}
