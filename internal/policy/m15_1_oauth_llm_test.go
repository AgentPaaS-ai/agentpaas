package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const canaryRefreshOSS = "canary-refresh-token-m151-oss"

func TestOAuthLlmRefreshPrefix(t *testing.T) {
	if OAuthLlmRefreshPrefix != "oauth_llm_rt_" {
		t.Fatalf("OAuthLlmRefreshPrefix=%q", OAuthLlmRefreshPrefix)
	}
}

func TestParsePolicy_OAuthLlmType(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
    scopes: [models.read]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if len(p.Credentials) != 1 {
		t.Fatalf("len=%d", len(p.Credentials))
	}
	c := p.Credentials[0]
	if c.Type != "oauth_llm" {
		t.Errorf("type=%s", c.Type)
	}
	if c.TokenEndpoint != "https://api.x.ai/oauth/token" {
		t.Errorf("token_endpoint=%s", c.TokenEndpoint)
	}
	if c.ClientID != "client-1" {
		t.Errorf("client_id=%s", c.ClientID)
	}
	if c.RefreshTokenCredential != "oauth_llm_rt_xai" {
		t.Errorf("refresh=%s", c.RefreshTokenCredential)
	}
	if len(c.Scopes) != 1 || c.Scopes[0] != "models.read" {
		t.Errorf("scopes=%v", c.Scopes)
	}
	requireNoValidationErrors(t, ValidatePolicy(p), false)
}

func TestParsePolicy_OAuthLlmDistinctFromDelegated(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
    provider: google
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	if p.Credentials[0].Type != "oauth_llm" {
		t.Fatalf("type=%s", p.Credentials[0].Type)
	}
	requireValidationError(t, ValidatePolicy(p), "error", "oauth_delegated")
}

func TestValidateOAuthLlm_MissingTokenEndpoint(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
`)
	requireValidationError(t, ValidatePolicy(p), "error", "token_endpoint")
}

func TestValidateOAuthLlm_HTTPTokenEndpoint(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: http://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
`)
	requireValidationError(t, ValidatePolicy(p), "error", "https")
}

func TestValidateOAuthLlm_MissingClientID(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    refresh_token_credential: oauth_llm_rt_xai
`)
	requireValidationError(t, ValidatePolicy(p), "error", "client_id")
}

func TestValidateOAuthLlm_MissingRefresh(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
`)
	requireValidationError(t, ValidatePolicy(p), "error", "refresh_token_credential")
}

func TestValidateOAuthLlm_UnprefixedRefresh(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: xai-refresh-token
`)
	requireValidationError(t, ValidatePolicy(p), "error", "oauth_llm_rt_")
}

func TestCompileGatewayConfig_OAuthLlmOmitsBackendOAuth(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.x.ai", Ports: []int{443}, Credential: "xai-oauth-llm"},
		},
		Credentials: []Credential{
			{
				ID:                     "xai-oauth-llm",
				Type:                   "oauth_llm",
				TokenEndpoint:          "https://api.x.ai/oauth/token",
				ClientID:               "client-1",
				RefreshTokenCredential: "oauth_llm_rt_xai",
				Value:                  canaryRefreshOSS,
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	outStr := string(out)
	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("not yaml: %v\n%s", err, outStr)
	}
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("backendOAuth must be omitted, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "credential:") {
		t.Errorf("route credential must be omitted, got:\n%s", outStr)
	}
	if strings.Contains(outStr, canaryRefreshOSS) {
		t.Errorf("refresh canary leaked into gateway yaml")
	}
}

func TestCompileCredentialRules_OAuthLlmNoSecretValue(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{
				ID:                     "xai-oauth-llm",
				Type:                   "oauth_llm",
				TokenEndpoint:          "https://api.x.ai/oauth/token",
				ClientID:               "client-1",
				RefreshTokenCredential: "oauth_llm_rt_xai",
				Scopes:                 []string{"models.read"},
				Value:                  canaryRefreshOSS,
			},
		},
	}
	out, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "oauth_llm:") {
		t.Errorf("expected oauth_llm field, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "oauth:") && !strings.Contains(outStr, "oauth_llm:") {
		t.Errorf("must not emit B19 oauth key instead of oauth_llm")
	}
	if strings.Contains(outStr, canaryRefreshOSS) {
		t.Errorf("refresh canary leaked into credential rules")
	}
	if !strings.Contains(outStr, "oauth_llm_rt_xai") {
		t.Errorf("expected refresh secret name, got:\n%s", outStr)
	}
}

func oauthLlmPolicyYAML(extra string) string {
	return `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
` + extra
}

func TestValidateOAuthLlm_MaxAccessTTLAbsentValid(t *testing.T) {
	p := parseYAML(t, oauthLlmPolicyYAML(""))
	if p.Credentials[0].MaxAccessTTLSeconds != nil {
		t.Fatalf("absent field must stay nil, got %v", p.Credentials[0].MaxAccessTTLSeconds)
	}
	requireNoValidationErrors(t, ValidatePolicy(p), false)
}

func TestValidateOAuthLlm_MaxAccessTTLPresentValid(t *testing.T) {
	for _, raw := range []string{"    max_access_ttl_seconds: 60\n", "    max_access_ttl_seconds: 120\n"} {
		p := parseYAML(t, oauthLlmPolicyYAML(raw))
		if p.Credentials[0].MaxAccessTTLSeconds == nil {
			t.Fatalf("present field missing for %q", raw)
		}
		requireNoValidationErrors(t, ValidatePolicy(p), false)
	}
}

func TestValidateOAuthLlm_MaxAccessTTLBelow60(t *testing.T) {
	for _, raw := range []string{"    max_access_ttl_seconds: 0\n", "    max_access_ttl_seconds: 59\n"} {
		p := parseYAML(t, oauthLlmPolicyYAML(raw))
		if p.Credentials[0].MaxAccessTTLSeconds == nil {
			t.Fatalf("explicit value must be non-nil for %q", raw)
		}
		requireValidationError(t, ValidatePolicy(p), "error", "max_access_ttl_seconds")
	}
}

func TestCanonicalAndLock_MaxAccessTTLKeptWhenPresent(t *testing.T) {
	ttl := 120
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{{
			ID:                     "xai-oauth-llm",
			Type:                   "oauth_llm",
			TokenEndpoint:          "https://api.x.ai/oauth/token",
			ClientID:               "client-1",
			RefreshTokenCredential: "oauth_llm_rt_xai",
			MaxAccessTTLSeconds:    &ttl,
		}},
	}
	cp, _ := Canonicalize(p)
	if len(cp.Credentials) != 1 || cp.Credentials[0].MaxAccessTTLSeconds == nil || *cp.Credentials[0].MaxAccessTTLSeconds != 120 {
		t.Fatalf("stamp dropped max_access_ttl_seconds: %+v", cp.Credentials)
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"max_access_ttl_seconds":120`) {
		t.Fatalf("signed stamp JSON missing cap: %s", raw)
	}
	locked, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(locked), "maxAccessTTLSeconds: 120") && !strings.Contains(string(locked), "maxAccessTTLSeconds:120") {
		t.Fatalf("credential lock dropped cap:\n%s", locked)
	}

	absent := *p
	absent.Credentials = []Credential{{
		ID:                     "xai-oauth-llm",
		Type:                   "oauth_llm",
		TokenEndpoint:          "https://api.x.ai/oauth/token",
		ClientID:               "client-1",
		RefreshTokenCredential: "oauth_llm_rt_xai",
	}}
	cpAbsent, _ := Canonicalize(&absent)
	if cpAbsent.Credentials[0].MaxAccessTTLSeconds != nil {
		t.Fatalf("absent cap leaked onto stamp: %v", *cpAbsent.Credentials[0].MaxAccessTTLSeconds)
	}
	rawAbsent, err := json.Marshal(cpAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawAbsent), "max_access_ttl_seconds") {
		t.Fatalf("absent cap present on stamp: %s", rawAbsent)
	}
}
