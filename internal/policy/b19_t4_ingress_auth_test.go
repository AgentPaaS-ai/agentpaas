package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T4: Ingress Auth (JWT & API Key) — Parser Tests
// ---------------------------------------------------------------------------

func TestParsePolicy_IngressAuth_JWT(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
ingress:
  - path: /
    port: 7718
ingress_auth:
  type: jwt
  jwt:
    issuer: https://auth.example.com
    audience: agentpaas
    jwks_url: https://auth.example.com/.well-known/jwks.json
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.IngressAuth == nil {
		t.Fatal("expected IngressAuth to be non-nil")
	}
	if p.IngressAuth.Type != "jwt" {
		t.Errorf("expected type=jwt, got %q", p.IngressAuth.Type)
	}
	if p.IngressAuth.JWT == nil {
		t.Fatal("expected JWT to be non-nil")
	}
	if p.IngressAuth.JWT.Issuer != "https://auth.example.com" {
		t.Errorf("expected issuer=https://auth.example.com, got %q", p.IngressAuth.JWT.Issuer)
	}
	if p.IngressAuth.JWT.Audience != "agentpaas" {
		t.Errorf("expected audience=agentpaas, got %q", p.IngressAuth.JWT.Audience)
	}
	if p.IngressAuth.JWT.JWKSURL != "https://auth.example.com/.well-known/jwks.json" {
		t.Errorf("expected jwks_url=https://auth.example.com/.well-known/jwks.json, got %q", p.IngressAuth.JWT.JWKSURL)
	}
	if p.IngressAuth.APIKey != nil {
		t.Error("expected APIKey to be nil for jwt type")
	}
}

func TestParsePolicy_IngressAuth_APIKey(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
ingress:
  - path: /
    port: 7718
credentials:
  - id: trigger-api-key
    type: header
    header: X-API-Key
ingress_auth:
  type: api_key
  api_key:
    header: X-API-Key
    credential: trigger-api-key
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.IngressAuth == nil {
		t.Fatal("expected IngressAuth to be non-nil")
	}
	if p.IngressAuth.Type != "api_key" {
		t.Errorf("expected type=api_key, got %q", p.IngressAuth.Type)
	}
	if p.IngressAuth.APIKey == nil {
		t.Fatal("expected APIKey to be non-nil")
	}
	if p.IngressAuth.APIKey.Header != "X-API-Key" {
		t.Errorf("expected header=X-API-Key, got %q", p.IngressAuth.APIKey.Header)
	}
	if p.IngressAuth.APIKey.Credential != "trigger-api-key" {
		t.Errorf("expected credential=trigger-api-key, got %q", p.IngressAuth.APIKey.Credential)
	}
	if p.IngressAuth.JWT != nil {
		t.Error("expected JWT to be nil for api_key type")
	}
}

func TestParsePolicy_WithoutIngressAuth(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.IngressAuth != nil {
		t.Error("expected IngressAuth to be nil when not configured")
	}
}

// ---------------------------------------------------------------------------
// B19-T4: Ingress Auth (JWT & API Key) — Validation Tests
// ---------------------------------------------------------------------------

func TestValidateIngressAuth_InvalidType(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "basic",
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "invalid ingress auth type")
}

func TestValidateIngressAuth_JWT_MissingFields(t *testing.T) {
	// Missing all JWT fields
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT:  &JWTAuth{},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "issuer is required for JWT ingress auth")
	requireValidationError(t, errs, "error", "audience is required for JWT ingress auth")
	requireValidationError(t, errs, "error", "jwks_url is required for JWT ingress auth")
}

func TestValidateIngressAuth_JWT_MissingJWTConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT:  nil,
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "jwt config is required")
}

func TestValidateIngressAuth_JWT_HTTPJWKSURL(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT: &JWTAuth{
				Issuer:   "https://auth.example.com",
				Audience: "agentpaas",
				JWKSURL:  "http://auth.example.com/.well-known/jwks.json",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "jwks_url must be a valid https URL")
}

func TestValidateIngressAuth_JWT_Valid(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT: &JWTAuth{
				Issuer:   "https://auth.example.com",
				Audience: "agentpaas",
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateIngressAuth_APIKey_MissingFields(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type:   "api_key",
			APIKey: &APIKeyAuth{},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "header is required for API key ingress auth")
	requireValidationError(t, errs, "error", "credential is required for API key ingress auth")
}

func TestValidateIngressAuth_APIKey_MissingAPIKeyConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type:   "api_key",
			APIKey: nil,
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "api_key config is required")
}

func TestValidateIngressAuth_APIKey_UndeclaredCredential(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "other-cred", Type: "header", Header: "Authorization"},
		},
		IngressAuth: &IngressAuth{
			Type: "api_key",
			APIKey: &APIKeyAuth{
				Header:     "X-API-Key",
				Credential: "trigger-api-key",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "references undeclared credential")
}

func TestValidateIngressAuth_APIKey_DeclaredCredential(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{ID: "trigger-api-key", Type: "header", Header: "X-API-Key"},
		},
		IngressAuth: &IngressAuth{
			Type: "api_key",
			APIKey: &APIKeyAuth{
				Header:     "X-API-Key",
				Credential: "trigger-api-key",
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestValidateIngressAuth_MissingType(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "",
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "type is required when ingress_auth is configured")
}

// ---------------------------------------------------------------------------
// B19-T4: Ingress Auth (JWT & API Key) — Compiler Tests
// ---------------------------------------------------------------------------

func TestCompileGatewayConfig_IngressAuth_JWT(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Ingress: []IngressRule{
			{Path: "/", Port: 7718},
		},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT: &JWTAuth{
				Issuer:   "https://auth.example.com",
				Audience: "agentpaas",
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
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

	// JWT policy should appear on the ingress route.
	if !strings.Contains(outStr, "jwt:") {
		t.Errorf("expected jwt policy in gateway config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "issuer: https://auth.example.com") {
		t.Errorf("expected issuer in jwt policy, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "audience: agentpaas") {
		t.Errorf("expected audience in jwt policy, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "jwksUrl: https://auth.example.com/.well-known/jwks.json") {
		t.Errorf("expected jwksUrl in jwt policy, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_IngressAuth_APIKey(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Ingress: []IngressRule{
			{Path: "/", Port: 7718},
		},
		Credentials: []Credential{
			{ID: "trigger-api-key", Type: "header", Header: "X-API-Key"},
		},
		IngressAuth: &IngressAuth{
			Type: "api_key",
			APIKey: &APIKeyAuth{
				Header:     "X-API-Key",
				Credential: "trigger-api-key",
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

	// API key policy should appear on the ingress route.
	if !strings.Contains(outStr, "apiKey:") {
		t.Errorf("expected apiKey policy in gateway config, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "header: X-API-Key") {
		t.Errorf("expected header in apiKey policy, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "credential: trigger-api-key") {
		t.Errorf("expected credential in apiKey policy, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_IngressAuth_NoAuthOnNonIngressRoutes(t *testing.T) {
	// Verify that ingress auth policies only appear on the ingress route,
	// not on egress routes.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.example.com", Ports: []int{443}},
		},
		Ingress: []IngressRule{
			{Path: "/", Port: 7718},
		},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT: &JWTAuth{
				Issuer:   "https://auth.example.com",
				Audience: "agentpaas",
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// JWT should appear exactly once (on the ingress route only).
	jwtCount := strings.Count(outStr, "jwt:")
	if jwtCount != 1 {
		t.Errorf("expected jwt to appear exactly once, got %d occurrences:\n%s", jwtCount, outStr)
	}
}

func TestCompileGatewayConfig_BackwardCompat_NoIngressAuth(t *testing.T) {
	// Existing policies without ingress_auth must still compile.
	p := samplePolicy()
	// samplePolicy has ingress but no ingress_auth.
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

	// Should not contain auth policies.
	if strings.Contains(string(out), "jwt:") {
		t.Error("backward compat policy should not have jwt auth")
	}
	if strings.Contains(string(out), "apiKey:") {
		t.Error("backward compat policy should not have apiKey auth")
	}
}

func TestCompileGatewayConfig_BackwardCompat_NoIngress(t *testing.T) {
	// Policy with ingress_auth but no ingress rules should not emit ingress bind at all.
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		IngressAuth: &IngressAuth{
			Type: "jwt",
			JWT: &JWTAuth{
				Issuer:   "https://auth.example.com",
				Audience: "agentpaas",
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	// No ingress rules → no ingress bind → no jwt should appear.
	if strings.Contains(outStr, "jwt:") {
		t.Errorf("jwt should NOT appear when no ingress rules exist, got:\n%s", outStr)
	}
}