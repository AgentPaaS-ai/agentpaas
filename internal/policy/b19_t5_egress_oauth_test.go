package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T5: Egress OAuth — Backend Token Refresh
// ---------------------------------------------------------------------------

// ---- Parser tests ----

func TestParsePolicy_OAuthCredentialType(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: my-client
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Refresh-Token
    value: secret-refresh-token
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.Credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(p.Credentials))
	}
	oauth := p.Credentials[0]
	if oauth.Type != "oauth" {
		t.Errorf("expected type=oauth, got %s", oauth.Type)
	}
	if oauth.TokenEndpoint != "https://api.x.ai/oauth/token" {
		t.Errorf("expected TokenEndpoint, got %s", oauth.TokenEndpoint)
	}
	if oauth.ClientID != "my-client" {
		t.Errorf("expected ClientID=my-client, got %s", oauth.ClientID)
	}
	if oauth.RefreshTokenCredential != "xai-refresh-token" {
		t.Errorf("expected RefreshTokenCredential=xai-refresh-token, got %s", oauth.RefreshTokenCredential)
	}
}

func TestParsePolicy_OAuthCredentialWithHeader(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: my-client
    refresh_token_credential: xai-refresh-token
    header: X-Custom-Auth
  - id: xai-refresh-token
    type: header
    header: X-Refresh-Token
    value: secret-refresh-token
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	oauth := p.Credentials[0]
	if oauth.Header != "X-Custom-Auth" {
		t.Errorf("expected Header=X-Custom-Auth, got %s", oauth.Header)
	}
}

// ---- Validation: rejects missing fields ----

func TestValidateOAuth_MissingTokenEndpoint(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    client_id: my-client
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Token
    value: secret
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "token_endpoint")
}

func TestValidateOAuth_MissingClientID(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Token
    value: secret
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "client_id")
}

func TestValidateOAuth_MissingRefreshTokenCredential(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: my-client
  - id: xai-refresh-token
    type: header
    header: X-Token
    value: secret
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "refresh_token_credential")
}

// ---- Validation: rejects non-https token_endpoint ----

func TestValidateOAuth_NonHTTPSTokenEndpoint(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: http://api.x.ai/oauth/token
    client_id: my-client
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Token
    value: secret
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "https")
}

func TestValidateOAuth_InvalidTokenEndpointURL(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: "not-a-valid-url"
    client_id: my-client
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Token
    value: secret
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "https")
}

// ---- Validation: rejects refresh_token_credential not in credentials list ----

func TestValidateOAuth_RefreshTokenCredentialNotDeclared(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: my-client
    refresh_token_credential: nonexistent-token
`)
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "not a declared credential")
}

// ---- Validation: accepts valid oauth credential ----

func TestValidateOAuth_ValidConfig(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth
credentials:
  - id: xai-oauth
    type: oauth
    token_endpoint: https://api.x.ai/oauth/token
    client_id: my-client
    refresh_token_credential: xai-refresh-token
  - id: xai-refresh-token
    type: header
    header: X-Refresh-Token
    value: secret-refresh-token
`)
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

// ---- Compiler: OAuth credential produces backend OAuth config ----

func TestCompileGatewayConfig_OAuthBackendConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.x.ai", Ports: []int{443}, Credential: "xai-oauth"},
		},
		Credentials: []Credential{
			{
				ID:                    "xai-oauth",
				Type:                  "oauth",
				TokenEndpoint:         "https://api.x.ai/oauth/token",
				ClientID:              "my-client-id",
				RefreshTokenCredential: "xai-refresh-token",
			},
			{
				ID:   "xai-refresh-token",
				Type: "header",
				Header: "X-Refresh-Token",
				Value: "secret-refresh",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Must be valid YAML.
	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, outStr)
	}

	// Should contain backendOAuth configuration.
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth is not a valid agentgateway v1.3.0 route policy field and must be omitted, got:\n%s", outStr)
	}

	if !strings.Contains(outStr, "credential: xai-oauth") {
		t.Errorf("expected route credential binding xai-oauth, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_OAuthBackendConfig_CustomHeader(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.x.ai", Ports: []int{443}, Credential: "xai-oauth"},
		},
		Credentials: []Credential{
			{
				ID:                    "xai-oauth",
				Type:                  "oauth",
				TokenEndpoint:         "https://api.x.ai/oauth/token",
				ClientID:              "my-client-id",
				RefreshTokenCredential: "xai-refresh-token",
				Header:                "X-Custom-Auth",
			},
			{
				ID:   "xai-refresh-token",
				Type: "header",
				Header: "X-Refresh-Token",
				Value: "secret-refresh",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth must be omitted, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "credential: xai-oauth") {
		t.Errorf("expected route credential binding xai-oauth, got:\n%s", outStr)
	}
}

// ---- Compiler: OAuth credential rules produce OAuth metadata ----

func TestCompileCredentialRules_OAuthRule(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{
				ID:                    "xai-oauth",
				Type:                  "oauth",
				TokenEndpoint:         "https://api.x.ai/oauth/token",
				ClientID:              "my-client-id",
				RefreshTokenCredential: "xai-refresh-token",
			},
		},
	}
	out, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "oauth:") {
		t.Errorf("expected oauth field in credential rule, got:\n%s", outStr)
	}
}

// ---- Compiler: OAuth + LLM rate limits combine on same route ----

func TestCompileGatewayConfig_OAuthWithLLMRateLimit(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.x.ai", Ports: []int{443}, Credential: "xai-oauth"},
		},
		Credentials: []Credential{
			{
				ID:                    "xai-oauth",
				Type:                  "oauth",
				TokenEndpoint:         "https://api.x.ai/oauth/token",
				ClientID:              "my-client",
				RefreshTokenCredential: "xai-refresh-token",
			},
			{
				ID:   "xai-refresh-token",
				Type: "header",
				Header: "X-Refresh-Token",
				Value: "secret",
			},
		},
		LLMRateLimit: &LLMRateLimit{
			RequestsPerMinute: 30,
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Should have both backendOAuth and localRateLimit
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth is not a valid agentgateway v1.3.0 route policy field and must be omitted, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "localRateLimit") {
		t.Errorf("expected localRateLimit alongside oauth credential binding, got:\n%s", outStr)
	}
}

// ---- Backward compatibility ----

func TestCompileGatewayConfig_BackwardCompat_NoOAuth(t *testing.T) {
	// Existing policies without oauth credentials must still compile.
	p := samplePolicy()
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}

	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, string(out))
	}

	// Should not contain OAuth config when no oauth credentials exist.
	if strings.Contains(string(out), "backendOAuth") {
		t.Error("samplePolicy should not have backendOAuth")
	}
}

func TestCompileCredentialRules_BackwardCompat_HeaderStillWorks(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "api-key", Type: "header", Header: "Authorization", Value: "Bearer test"},
			{ID: "legacy-tool-token", Type: "direct_lease", Mode: "file", Reason: "legacy SDK"},
		},
	}
	out, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "id: api-key") {
		t.Error("expected header credential in output")
	}
	if !strings.Contains(outStr, "id: legacy-tool-token") {
		t.Error("expected direct_lease credential in output")
	}
	// Should NOT contain oauth field when no oauth credentials exist.
	if strings.Contains(outStr, "oauth:") {
		t.Error("should not contain oauth field for non-oauth credentials")
	}
}

func TestParsePolicy_BackwardCompat_InvalidTypeStillRejected(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.example.com
    ports: [443]
credentials:
  - id: bad-cred
    type: invalid-type
    value: test
`
	_, err := ParsePolicy(strings.NewReader(yamlStr))
	if err == nil {
		t.Fatal("expected ParsePolicy to fail for invalid credential type")
	}
	if !strings.Contains(err.Error(), "invalid credential type") {
		t.Errorf("expected 'invalid credential type' error, got: %v", err)
	}
}

func TestParsePolicy_BackwardCompat_ValidTypeOAuthInList(t *testing.T) {
	// Verify that "oauth" is listed in accepted types in the error message.
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.example.com
    ports: [443]
credentials:
  - id: bad-cred
    type: made-up-type
    value: test
`
	_, err := ParsePolicy(strings.NewReader(yamlStr))
	if err == nil {
		t.Fatal("expected ParsePolicy to fail")
	}
	if !strings.Contains(err.Error(), "oauth") {
		t.Errorf("expected error to mention 'oauth' as valid type, got: %v", err)
	}
}

// ---- Edge case: OAuth route without LLM domain still gets OAuth config ----

func TestCompileGatewayConfig_OAuthOnNonLLMDomain(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.corp.example.com", Ports: []int{443}, Credential: "corp-oauth"},
		},
		Credentials: []Credential{
			{
				ID:                    "corp-oauth",
				Type:                  "oauth",
				TokenEndpoint:         "https://auth.corp.example.com/oauth/token",
				ClientID:              "corp-client",
				RefreshTokenCredential: "corp-refresh",
			},
			{
				ID:   "corp-refresh",
				Type: "header",
				Header: "X-Refresh",
				Value: "secret",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// Non-LLM route should still get backendOAuth.
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth must be omitted on non-LLM route, got:\n%s", outStr)
	}
	// Non-LLM route should NOT get localRateLimit.
	if strings.Contains(outStr, "localRateLimit") {
		t.Errorf("should not have localRateLimit on non-LLM route, got:\n%s", outStr)
	}
}