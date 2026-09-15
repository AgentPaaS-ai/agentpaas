package policy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Unique refresh-token VALUE used only as a leak canary. Secret NAMES
// (oauth_llm_rt_*) are allowed in compiled artifacts; this VALUE is not.
const advCanaryRT = "CANARY_RT_m151t2_DO_NOT_EMIT"

func advOAuthLlmPolicy(t *testing.T, extra string) *Policy {
	t.Helper()
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
` + extra
	return parseYAML(t, yamlStr)
}

func advScanLeak(t *testing.T, claim, artifact, blob, canary string) {
	t.Helper()
	if canary == "" {
		return
	}
	if strings.Contains(blob, canary) {
		t.Errorf("ADVERSARY BREAK: %s: refresh-token canary leaked into %s", claim, artifact)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(canary))
	if enc != "" && strings.Contains(blob, enc) {
		t.Errorf("ADVERSARY BREAK: %s: base64(canary) leaked into %s", claim, artifact)
	}
	encRaw := base64.RawStdEncoding.EncodeToString([]byte(canary))
	if encRaw != "" && strings.Contains(blob, encRaw) {
		t.Errorf("ADVERSARY BREAK: %s: raw-base64(canary) leaked into %s", claim, artifact)
	}
}

func advWalkYAML(v any, fn func(key string, val any)) {
	switch n := v.(type) {
	case map[string]any:
		for k, val := range n {
			fn(k, val)
			advWalkYAML(val, fn)
		}
	case map[any]any:
		for k, val := range n {
			ks, _ := k.(string)
			fn(ks, val)
			advWalkYAML(val, fn)
		}
	case []any:
		for _, val := range n {
			advWalkYAML(val, fn)
		}
	}
}

func advHasValidationError(errs []ValidationError, substr string) bool {
	for _, e := range errs {
		if e.Severity == "error" && strings.Contains(e.Message, substr) {
			return true
		}
	}
	return false
}

func advValidationBlob(errs []ValidationError) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString(e.Error())
		b.WriteByte('\n')
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// SC1 — credential VALUE must never appear in compiled / derived artifacts
// ---------------------------------------------------------------------------

func TestAdvOAuthLlm_SC1_ValueCanaryAbsentFromCompiledArtifacts(t *testing.T) {
	p := &Policy{
		Version: "1.0",
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
				Value:                  advCanaryRT,
			},
		},
	}

	gw, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	advScanLeak(t, "SC1", "compiled gateway YAML", string(gw), advCanaryRT)

	rules, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	advScanLeak(t, "SC1", "CompileCredentialRules YAML", string(rules), advCanaryRT)

	cp, _ := Canonicalize(p)
	canon, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	advScanLeak(t, "SC1", "canonical policy JSON", string(canon), advCanaryRT)

	dig, err := Digest(p)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	advScanLeak(t, "SC1", "policy digest", dig, advCanaryRT)

	// Secret NAME is allowed; VALUE is not.
	if !strings.Contains(string(rules), "oauth_llm_rt_xai") {
		t.Errorf("SC2: expected refresh secret name in credential rules")
	}
}

func TestAdvOAuthLlm_SC1_TokenEndpointUserinfoLeaksCanary(t *testing.T) {
	// ADVERSARY BREAK: oauth_llm token_endpoint validation only checks
	// scheme==https (validation.go oauth_llm branch) and does not reject
	// URL userinfo. CompileCredentialRules copies TokenEndpoint verbatim,
	// so a refresh-token canary stuffed into userinfo lands in compiled
	// credential rules and canonical policy JSON (SC1).
	userinfoURL := "https://user:" + advCanaryRT + "@api.x.ai/oauth/token"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress: []EgressRule{
			{Domain: "api.x.ai", Ports: []int{443}, Credential: "xai-oauth-llm"},
		},
		Credentials: []Credential{
			{
				ID:                     "xai-oauth-llm",
				Type:                   "oauth_llm",
				TokenEndpoint:          userinfoURL,
				ClientID:               "client-1",
				RefreshTokenCredential: "oauth_llm_rt_xai",
			},
		},
	}

	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC1: ValidatePolicy accepted token_endpoint with userinfo canary")
	}
	advScanLeak(t, "SC1", "ValidatePolicy errors", advValidationBlob(errs), advCanaryRT)

	rules, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	if strings.Contains(string(rules), advCanaryRT) {
		t.Errorf("ADVERSARY BREAK: SC1: token_endpoint userinfo canary leaked into CompileCredentialRules")
	}

	gw, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	advScanLeak(t, "SC1", "compiled gateway YAML", string(gw), advCanaryRT)

	cp, _ := Canonicalize(p)
	canon, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	if strings.Contains(string(canon), advCanaryRT) {
		t.Errorf("ADVERSARY BREAK: SC1: token_endpoint userinfo canary leaked into canonical policy JSON")
	}
}

func TestAdvOAuthLlm_SC1_TokenEndpointQueryCanary(t *testing.T) {
	// ADVERSARY BREAK: query-string refresh_token= on token_endpoint is
	// not rejected for oauth_llm and is copied into CompileCredentialRules.
	qURL := "https://api.x.ai/oauth/token?refresh_token=" + advCanaryRT
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{
				ID:                     "xai-oauth-llm",
				Type:                   "oauth_llm",
				TokenEndpoint:          qURL,
				ClientID:               "client-1",
				RefreshTokenCredential: "oauth_llm_rt_xai",
			},
		},
	}
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC1: ValidatePolicy accepted token_endpoint query canary")
	}
	advScanLeak(t, "SC1", "ValidatePolicy errors", advValidationBlob(errs), advCanaryRT)

	rules, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	if strings.Contains(string(rules), advCanaryRT) {
		t.Errorf("ADVERSARY BREAK: SC1: token_endpoint query canary leaked into CompileCredentialRules")
	}
}

func TestAdvOAuthLlm_SC1_InlineValueInYAMLUnknownKeysRejected(t *testing.T) {
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
    refresh_token: ` + advCanaryRT + `
`
	_, err := ParsePolicy(strings.NewReader(yamlStr))
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: SC1: unknown refresh_token key accepted (KnownFields gap)")
	}
	if strings.Contains(err.Error(), advCanaryRT) {
		t.Errorf("ADVERSARY BREAK: SC1: parse error echoed refresh-token canary")
	}
}

// ---------------------------------------------------------------------------
// SC2 — prefix oauth_llm_rt_ is a secret NAME, not a nested credential id
// ---------------------------------------------------------------------------

func TestAdvOAuthLlm_SC2_EmptySuffixRejected(t *testing.T) {
	// ADVERSARY BREAK: strings.HasPrefix("oauth_llm_rt_", "oauth_llm_rt_")
	// is true, so a name that is ONLY the prefix (empty suffix) is accepted.
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
    refresh_token_credential: oauth_llm_rt_
`)
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC2: empty-suffix refresh name oauth_llm_rt_ accepted")
	}
}

func TestAdvOAuthLlm_SC2_NewlineInjectionInRefreshName(t *testing.T) {
	// ADVERSARY BREAK: ContainsInjectionPattern is applied to credential
	// id only, not refresh_token_credential. A newline after a valid
	// prefix passes HasPrefix and is copied into compiled YAML.
	injected := "oauth_llm_rt_xai\nbackendOAuth: injected"
	p := &Policy{
		Version: "1.0",
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
				RefreshTokenCredential: injected,
			},
		},
	}
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC2/INJECTION: newline in refresh_token_credential accepted")
	}

	rules, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	if strings.Contains(string(rules), "backendOAuth") {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: newline injection placed backendOAuth in credential rules:\n%s", string(rules))
	}
}

func TestAdvOAuthLlm_SC2_NullByteInRefreshName(t *testing.T) {
	injected := "oauth_llm_rt_xai\x00evil"
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Credentials: []Credential{
			{
				ID:                     "xai-oauth-llm",
				Type:                   "oauth_llm",
				TokenEndpoint:          "https://api.x.ai/oauth/token",
				ClientID:               "client-1",
				RefreshTokenCredential: injected,
			},
		},
	}
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC2/INJECTION: null byte in refresh_token_credential accepted")
	}
}

func TestAdvOAuthLlm_SC2_PathTraversalRefreshName(t *testing.T) {
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
    refresh_token_credential: oauth_llm_rt_../../etc/passwd
`)
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: SC2/PATH: path-traversal suffix in refresh_token_credential accepted")
	}
}

func TestAdvOAuthLlm_SC2_HomoglyphAndCasePrefixRejected(t *testing.T) {
	cases := []string{
		"OAUTH_LLM_RT_xai",
		"oauth-llm-rt-xai",
		"oauth_llm_rtxai",
		"foo_oauth_llm_rt_xai",
		"oauth_delegated_rt_xai",
		"oauth_rt_xai",
		"оauth_llm_rt_xai", // Cyrillic о
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p := &Policy{
				Version: "1.0",
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
						RefreshTokenCredential: name,
					},
				},
			}
			errs := ValidatePolicy(p)
			if !HasErrors(errs) {
				t.Errorf("SC2: refresh name %q must be rejected", name)
			}
		})
	}
}

func TestAdvOAuthLlm_SC2_RefreshNameIsNotNestedCredentialID(t *testing.T) {
	// oauth_llm refresh_token_credential is a Keychain secret NAME, not a
	// declared credential id (unlike type=oauth). Referencing a declared
	// header credential without the prefix must fail.
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
credentials:
  - id: nested-rt
    type: header
    header: Authorization
    value: placeholder
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: nested-rt
`)
	requireValidationError(t, ValidatePolicy(p), "error", "oauth_llm_rt_")
}

// ---------------------------------------------------------------------------
// STAY_1_3_0 — compiled gateway config omits backendOAuth and route credential
// ---------------------------------------------------------------------------

func TestAdvOAuthLlm_STAY130_NoBackendOAuthOrRouteCredential(t *testing.T) {
	p := advOAuthLlmPolicy(t, "")
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	outStr := string(out)
	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("gateway yaml: %v", err)
	}
	var badKeys []string
	advWalkYAML(decoded, func(key string, _ any) {
		if key == "backendOAuth" || key == "backendoauth" || key == "backend_oauth" {
			badKeys = append(badKeys, key)
		}
		if key == "credential" {
			badKeys = append(badKeys, key)
		}
	})
	if len(badKeys) > 0 {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: compiled gateway emitted keys %v\n%s", badKeys, outStr)
	}
	if strings.Contains(outStr, "backendOAuth") {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: backendOAuth substring in gateway yaml")
	}
	if strings.Contains(outStr, "credential:") {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: route credential: in gateway yaml")
	}
}

func TestAdvOAuthLlm_STAY130_MixedDelegatedDoesNotContaminate(t *testing.T) {
	p := parseYAML(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.x.ai
    ports: [443]
    credential: xai-oauth-llm
  - domain: www.googleapis.com
    ports: [443]
    credential: gmail-delegated
credentials:
  - id: cid
    type: header
    header: X-Client-Id
  - id: csec
    type: header
    header: X-Client-Secret
  - id: xai-oauth-llm
    type: oauth_llm
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
  - id: gmail-delegated
    type: oauth_delegated
    provider: google
    client_id_credential: cid
    client_secret_credential: csec
    scopes: [email]
    max_scopes: [email]
`)
	requireNoValidationErrors(t, ValidatePolicy(p), false)
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(string(out), "backendOAuth") {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: mixed policy emitted backendOAuth")
	}
	if strings.Contains(string(out), "credential:") {
		t.Errorf("ADVERSARY BREAK: STAY_1_3_0: mixed policy emitted route credential:")
	}
}

func TestAdvOAuthLlm_STAY130_CompileCredentialRulesIsOAuthLlmNotOAuth(t *testing.T) {
	p := advOAuthLlmPolicy(t, "")
	out, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	var rules []CredentialRule
	if err := yaml.Unmarshal(out, &rules); err != nil {
		t.Fatalf("rules yaml: %v\n%s", err, out)
	}
	if len(rules) != 1 {
		t.Fatalf("len(rules)=%d", len(rules))
	}
	if rules[0].OAuth != nil {
		t.Errorf("ADVERSARY BREAK: distinct-type: oauth_llm compiled as B19 oauth key")
	}
	if rules[0].OAuthLlm == nil {
		t.Errorf("expected oauth_llm rule, got:\n%s", out)
	}
	if rules[0].Value != "" {
		t.Errorf("ADVERSARY BREAK: SC1: credential rule Value=%q", rules[0].Value)
	}
}

// ---------------------------------------------------------------------------
// Distinct type — oauth_llm is not oauth_delegated; no consent /oauth/start
// ---------------------------------------------------------------------------

func TestAdvOAuthLlm_DistinctType_NoConsentOrStartRoute(t *testing.T) {
	p := advOAuthLlmPolicy(t, "")
	gw, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	blob := string(gw)
	for _, s := range []string{"/oauth/start", "consent", "<html", "oauth_delegated"} {
		if strings.Contains(strings.ToLower(blob), strings.ToLower(s)) {
			t.Errorf("ADVERSARY BREAK: distinct-type: compiled gateway contains %q", s)
		}
	}
	rules, err := CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	rblob := strings.ToLower(string(rules))
	if strings.Contains(rblob, "/oauth/start") || strings.Contains(rblob, "<html") {
		t.Errorf("ADVERSARY BREAK: distinct-type: credential rules contain consent artifacts")
	}
}

func TestAdvOAuthLlm_DistinctType_DelegatedOnlyFieldsRejected(t *testing.T) {
	fields := []string{
		"provider: google",
		"auth_endpoint: https://example.com/auth",
		"client_id_credential: cid",
		"client_secret_credential: csec",
		"max_scopes: [models.read]",
		"redirect_path: /oauth/callback",
	}
	for _, f := range fields {
		t.Run(f, func(t *testing.T) {
			p := advOAuthLlmPolicy(t, "    "+f+"\n")
			errs := ValidatePolicy(p)
			if !HasErrors(errs) {
				t.Errorf("distinct-type: oauth_llm with %s must be rejected", f)
			}
			if strings.Contains(advValidationBlob(errs), advCanaryRT) {
				t.Errorf("SC1: validation error echoed canary")
			}
		})
	}
}

func TestAdvOAuthLlm_DistinctType_ParserRejectsAliases(t *testing.T) {
	aliases := []string{"oauth-llm", "oauthLlm", "OAuth_LLM", "OAUTH_LLM", "oauth_llm ", "oauth_delegated"}
	for _, typ := range aliases {
		if typ == "oauth_delegated" {
			continue // valid other type; not an oauth_llm alias
		}
		t.Run(typ, func(t *testing.T) {
			yamlStr := fmt.Sprintf(`version: "1.0"
agent:
  name: test-agent
credentials:
  - id: x
    type: %q
    token_endpoint: https://api.x.ai/oauth/token
    client_id: client-1
    refresh_token_credential: oauth_llm_rt_xai
`, typ)
			_, err := ParsePolicy(strings.NewReader(yamlStr))
			if err == nil {
				t.Errorf("distinct-type: alias %q must not parse as oauth_llm", typ)
			}
		})
	}
}

func TestAdvOAuthLlm_HTTPSTokenEndpointRequiresHost(t *testing.T) {
	// ADVERSARY BREAK: oauth_llm checks parsed.Scheme == "https" only;
	// isHTTPSURL also requires a host. "https:" and "https:///token" pass.
	for _, ep := range []string{"https:", "https://", "https:///oauth/token"} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Credentials: []Credential{
				{
					ID:                     "xai-oauth-llm",
					Type:                   "oauth_llm",
					TokenEndpoint:          ep,
					ClientID:               "client-1",
					RefreshTokenCredential: "oauth_llm_rt_xai",
				},
			},
		}
		errs := ValidatePolicy(p)
		if !HasErrors(errs) {
			t.Errorf("ADVERSARY BREAK: token_endpoint %q accepted without host", ep)
		}
	}
}

func TestAdvOAuthLlm_HTTPTokenEndpointRejected(t *testing.T) {
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

func TestAdvOAuthLlm_WhitespaceClientIDRejected(t *testing.T) {
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
    client_id: " "
    refresh_token_credential: oauth_llm_rt_xai
`)
	errs := ValidatePolicy(p)
	if !HasErrors(errs) {
		t.Errorf("ADVERSARY BREAK: whitespace-only client_id accepted (no TrimSpace)")
	}
}
