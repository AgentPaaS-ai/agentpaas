package policy

import (
	"strings"
	"testing"
)

func TestParsePolicy_OAuthDelegatedGoogleDefaults(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: gmail.googleapis.com
    ports: [443]
    credential: gmail_oauth
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: google_client_id
    scopes: [gmail.readonly]
    max_scopes: [gmail.readonly]
  - id: google_client_id
    type: header
    header: X-Client-Id
    value: cid
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if len(p.Credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(p.Credentials))
	}
	c := p.Credentials[0]
	if c.Type != "oauth_delegated" {
		t.Errorf("type=%s", c.Type)
	}
	if c.Provider != "google" {
		t.Errorf("provider=%s", c.Provider)
	}
	if c.ClientIDCredential != "google_client_id" {
		t.Errorf("client_id_credential=%s", c.ClientIDCredential)
	}
	if len(c.Scopes) != 1 || c.Scopes[0] != "gmail.readonly" {
		t.Errorf("scopes=%v", c.Scopes)
	}
	if len(c.MaxScopes) != 1 || c.MaxScopes[0] != "gmail.readonly" {
		t.Errorf("max_scopes=%v", c.MaxScopes)
	}
	// auth/token endpoints optional for known provider
	if c.AuthEndpoint != "" || c.TokenEndpoint != "" {
		t.Errorf("expected empty endpoints for google defaults, got auth=%q token=%q", c.AuthEndpoint, c.TokenEndpoint)
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestParsePolicy_OAuthDelegatedGeneric(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.example.com
    ports: [443]
    credential: custom_oauth
credentials:
  - id: custom_oauth
    type: oauth_delegated
    provider: generic
    auth_endpoint: https://auth.example.com/authorize
    token_endpoint: https://auth.example.com/token
    client_id_credential: cid
    client_secret_credential: csec
    scopes: [read]
    max_scopes: [read, write]
    redirect_path: /oauth/callback
  - id: cid
    type: header
    header: X-Client-Id
    value: x
  - id: csec
    type: header
    header: X-Client-Secret
    value: y
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	c := p.Credentials[0]
	if c.Provider != "generic" {
		t.Errorf("provider=%s", c.Provider)
	}
	if c.AuthEndpoint != "https://auth.example.com/authorize" {
		t.Errorf("auth_endpoint=%s", c.AuthEndpoint)
	}
	if c.RedirectPath != "/oauth/callback" {
		t.Errorf("redirect_path=%s", c.RedirectPath)
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateOAuthDelegated_MissingScopes(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    max_scopes: [gmail.readonly]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "scopes")
}

func TestValidateOAuthDelegated_MissingMaxScopes(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    scopes: [gmail.readonly]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "max_scopes")
}

func TestValidateOAuthDelegated_MissingClientIDCredential(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    scopes: [gmail.readonly]
    max_scopes: [gmail.readonly]
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "client_id_credential")
}

func TestValidateOAuthDelegated_MaxNotSuperset(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    scopes: [gmail.readonly, gmail.send]
    max_scopes: [gmail.readonly]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "superset")
}

func TestValidateOAuthDelegated_WildcardWithoutReason(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    scopes: [gmail.readonly]
    max_scopes: ["https://mail.google.com/", gmail.readonly]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "wildcard")
}

func TestValidateOAuthDelegated_WildcardWithReason(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    scopes: [gmail.readonly]
    max_scopes: ["https://mail.google.com/", gmail.readonly]
    reason: "full mailbox access required for demo agent"
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateOAuthDelegated_GenericMissingEndpoints(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: custom_oauth
    type: oauth_delegated
    provider: generic
    client_id_credential: cid
    scopes: [read]
    max_scopes: [read]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "auth_endpoint")
	requireValidationError(t, errs, "error", "token_endpoint")
}

func TestValidateOAuthDelegated_NonHTTPS(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: custom_oauth
    type: oauth_delegated
    provider: generic
    auth_endpoint: http://auth.example.com/authorize
    token_endpoint: https://auth.example.com/token
    client_id_credential: cid
    scopes: [read]
    max_scopes: [read]
  - id: cid
    type: header
    header: X
    value: v
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "https")
}

func TestValidateOAuthDelegated_MissingClientIDRef(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: missing_cid
    scopes: [gmail.readonly]
    max_scopes: [gmail.readonly]
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "not a declared credential ID")
}

func TestValidateOAuthDelegated_ProviderOnHeaderRejected(t *testing.T) {
	// provider is a known YAML key but invalid on header credentials.
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: k
    type: header
    header: Authorization
    value: x
    provider: aws
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "oauth_delegated")
}

func TestNormalizeScopeAndSetMath(t *testing.T) {
	if NormalizeScope("  a  ") != "a" {
		t.Fatalf("normalize")
	}
	cases := []struct {
		name      string
		max       []string
		granted   []string
		want      bool
	}{
		{"empty granted", []string{"a"}, nil, true},
		{"subset", []string{"a", "b"}, []string{"a"}, true},
		{"equal", []string{"a"}, []string{"a"}, true},
		{"not subset", []string{"a"}, []string{"a", "b"}, false},
		{"trim", []string{" a "}, []string{"a"}, true},
		{"order irrelevant", []string{"b", "a"}, []string{"a", "b"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeSetContains(tc.max, tc.granted); got != tc.want {
				t.Errorf("ScopeSetContains=%v want %v", got, tc.want)
			}
			if got := ScopesWithinMax(tc.granted, tc.max); got != tc.want {
				t.Errorf("ScopesWithinMax=%v want %v", got, tc.want)
			}
		})
	}
}

func TestIsWildcardScope(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"*", true},
		{"repo/*", true},
		{"https://mail.google.com/", true},
		{"http://example.com/", true},
		{"https://mail.google.com/foo", false},
		{"gmail.readonly", false},
		{"", false},
		{"  *  ", true},
	}
	for _, tc := range cases {
		if got := IsWildcardScope(tc.s); got != tc.want {
			t.Errorf("IsWildcardScope(%q)=%v want %v", tc.s, got, tc.want)
		}
	}
}

func TestCanonicalCredential_OAuthDelegatedSortedScopes(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
credentials:
  - id: gmail_oauth
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    scopes: [z.scope, a.scope]
    max_scopes: [z.scope, a.scope, m.scope]
  - id: cid
    type: header
    header: X
    value: v
`)
	cp, _ := Canonicalize(p)
	if len(cp.Credentials) != 2 {
		t.Fatalf("creds=%d", len(cp.Credentials))
	}
	// sorted by id: cid then gmail_oauth
	var od *CanonicalCredential
	for i := range cp.Credentials {
		if cp.Credentials[i].ID == "gmail_oauth" {
			od = &cp.Credentials[i]
			break
		}
	}
	if od == nil {
		t.Fatal("gmail_oauth missing from canonical")
	}
	if len(od.Scopes) != 2 || od.Scopes[0] != "a.scope" || od.Scopes[1] != "z.scope" {
		t.Errorf("scopes not sorted: %v", od.Scopes)
	}
	if len(od.MaxScopes) != 3 || od.MaxScopes[0] != "a.scope" {
		t.Errorf("max_scopes not sorted: %v", od.MaxScopes)
	}
	if od.Provider != "google" {
		t.Errorf("provider=%s", od.Provider)
	}
}
