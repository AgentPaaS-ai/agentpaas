package policy

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Unique tenant-pattern sentinel. It is the detector, not a payload; it must
// not appear in compiled gateway YAML (audit/log-shaped artifacts). It may
// appear in CanonicalPII.Patterns.
const advSC6Canary = "CANARY_PII_078-05-1120"

func advSC6Base(piiBlock string) string {
	return `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
` + piiBlock
}

func advSC6Parse(t *testing.T, yml string) *Policy {
	t.Helper()
	p, err := ParsePolicy(strings.NewReader(yml))
	if err != nil {
		t.Fatalf("ParsePolicy: %v\n%s", err, yml)
	}
	return p
}

func advSC6HasErr(errs []ValidationError, substr string) bool {
	for _, e := range errs {
		if e.Severity == "error" && (strings.Contains(e.Message, substr) || strings.Contains(e.Field, substr)) {
			return true
		}
	}
	return false
}

func advSC6ErrBlob(errs []ValidationError) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString(e.Error())
		b.WriteByte('\n')
	}
	return b.String()
}

func advSC6ContainsAWSGitHubPEM(blob string) bool {
	l := strings.ToLower(blob)
	needles := []string{
		"akia", "github_pat", "-----begin", "private_key",
		"aws_access_key", "ghp_", "pem",
	}
	for _, n := range needles {
		if strings.Contains(l, n) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// D208 — fail-closed must persist when PII is on
// ---------------------------------------------------------------------------

func TestAdvSC6_D208_FailClosedMustPersistWhenPIIOn(t *testing.T) {
	// ADVERSARY BREAK: Canonicalize/compile of mask|reject PII does not persist
	// fail-closed (D208). Schema has no fail_closed / on_detector_error field
	// and Canonicalize does not inject one, so a down detector/audit/budget
	// store has no compiled closed default.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
`))
	if errs := ValidatePolicy(p); advSC6HasErr(errs, "") && len(errs) > 0 {
		for _, e := range errs {
			if e.Severity == "error" {
				t.Fatalf("fixture must validate: %s", advSC6ErrBlob(errs))
			}
		}
	}
	cp, _ := Canonicalize(p)
	if cp == nil || cp.PII == nil {
		t.Fatal("ADVERSARY BREAK: Canonicalize dropped PII (D205/D208)")
	}
	b, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	blob := string(b)
	closed := strings.Contains(blob, `"fail_closed":true`) ||
		strings.Contains(blob, `"fail_open":false`) ||
		strings.Contains(blob, `"on_detector_error":"reject"`) ||
		strings.Contains(blob, `"on_detector_error":"fail_closed"`) ||
		strings.Contains(blob, `"on_audit_error":"reject"`)
	if !closed {
		t.Errorf("ADVERSARY BREAK: D208 fail-closed not persisted in canonical JSON: %s", blob)
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	gw := string(out)
	gwClosed := strings.Contains(gw, "fail_closed") || strings.Contains(gw, "failClosed") ||
		strings.Contains(strings.ToLower(gw), "failclosed")
	if !closed && !gwClosed {
		t.Errorf("ADVERSARY BREAK: D208 fail-closed absent from compile output too")
	}
}

func TestAdvSC6_D208_FailOpenKeysMustNotCompile(t *testing.T) {
	// ADVERSARY BREAK: on_detector_error/fail_open/stream skip cannot be
	// expressed (KnownFields rejects), so there is also no way to pin
	// fail-closed. Complementary: compile of a valid mask policy must not
	// emit fail-open defaults.
	for _, yml := range []string{
		advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    fail_open: true
`),
		advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    on_detector_error: allow
`),
		advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    on_audit_error: allow
`),
		advSC6Base(`guardrails:
  pii:
    action: reject
    builtins: [Email]
    stream_inspect: skip
`),
		advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    stream_passthrough: true
`),
	} {
		_, err := ParsePolicy(strings.NewReader(yml))
		if err == nil {
			t.Errorf("ADVERSARY BREAK: D208 fail-open/skip key parsed instead of rejected:\n%s", yml)
		}
	}
}

func TestAdvSC6_D208_CompileMustNotSucceedWhenValidateFails(t *testing.T) {
	// ADVERSARY BREAK: CompileGatewayConfig does not consult ValidatePolicy.
	// Invalid PII (unknown action, empty detectors, bad regex) still emits a
	// usable-looking gateway config with live upstream backends (fail-open).
	cases := []struct {
		name string
		pii  *PIIGuardrail
	}{
		{"unknown_action", &PIIGuardrail{Action: "log_only", Builtins: []string{PIIBuiltinEmail}}},
		{"empty_action", &PIIGuardrail{Action: "", Builtins: []string{PIIBuiltinEmail}}},
		{"allow", &PIIGuardrail{Action: "allow", Builtins: []string{PIIBuiltinEmail}}},
		{"no_detectors", &PIIGuardrail{Action: PIIActionMask}},
		{"bad_regex", &PIIGuardrail{Action: PIIActionMask, Patterns: []string{"("}}},
		{"secret_pack_builtin", &PIIGuardrail{Action: PIIActionMask, Builtins: []string{"AWS"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Policy{
				Version: "1.0",
				Agent:   AgentConfig{Name: "test"},
				Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
				PII:     c.pii,
			}
			errs := ValidatePolicy(p)
			if !advSC6HasErr(errs, "") && len(errs) == 0 {
				t.Fatalf("fixture should fail ValidatePolicy")
			}
			hasError := false
			for _, e := range errs {
				if e.Severity == "error" {
					hasError = true
					break
				}
			}
			if !hasError {
				t.Fatalf("fixture should fail ValidatePolicy: %s", advSC6ErrBlob(errs))
			}
			out, err := CompileGatewayConfig(p)
			if err == nil && len(out) > 0 {
				t.Errorf("ADVERSARY BREAK: D208 CompileGatewayConfig succeeded on invalid PII %s (%d bytes)", c.name, len(out))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// D202 — stream inspect must buffer or fail closed when mask|reject on
// ---------------------------------------------------------------------------

func TestAdvSC6_D202_StreamBufferMustPersistWhenMaskReject(t *testing.T) {
	// ADVERSARY BREAK: PIIRequiresBufferedStream is true for mask|reject but
	// neither Canonicalize nor CompileGatewayConfig persist a buffer-full
	// / fail-closed stream mode. Runtime can skip inspection.
	for _, action := range []string{PIIActionMask, PIIActionReject} {
		p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: `+action+`
    builtins: [Email]
`))
		if !PIIRequiresBufferedStream(p) {
			t.Errorf("ADVERSARY BREAK: D202 PIIRequiresBufferedStream=false for action=%s", action)
		}
		cp, _ := Canonicalize(p)
		b, _ := json.Marshal(cp)
		blob := string(b)
		out, err := CompileGatewayConfig(p)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		gw := string(out)
		persisted := strings.Contains(blob, `"buffered_stream":true`) ||
			strings.Contains(blob, `"stream_inspect":"buffer"`) ||
			strings.Contains(blob, `"stream_mode":"buffer"`) ||
			strings.Contains(blob, `"fail_closed":true`) ||
			strings.Contains(strings.ToLower(gw), "buffer") && strings.Contains(strings.ToLower(gw), "stream")
		if !persisted {
			t.Errorf("ADVERSARY BREAK: D202 stream buffer/fail-closed not persisted for action=%s canon=%s", action, blob)
		}
	}
}

func TestAdvSC6_D202_PIIPresentUnknownActionMustNotSkipInspect(t *testing.T) {
	// ADVERSARY BREAK: PIIRequiresBufferedStream returns false when p.PII is
	// set but action is not mask|reject, so a present PII block with a typo'd
	// action skips inspection (fail-open) instead of fail-closed.
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: "log_only", Builtins: []string{PIIBuiltinEmail}},
	}
	if !PIIRequiresBufferedStream(p) {
		t.Errorf("ADVERSARY BREAK: D202 PII present with unknown action skipped inspect (buffered=false)")
	}
}

func TestAdvSC6_D202_SkipOnlyWhenPIIOff(t *testing.T) {
	if PIIRequiresBufferedStream(nil) || PIIRequiresBufferedStream(&Policy{}) {
		t.Fatal("skip inspect must be allowed only when PII is off")
	}
}

// ---------------------------------------------------------------------------
// SC6 — reject-on-request is policy-defined, no unconditional upstream
// ---------------------------------------------------------------------------

func TestAdvSC6_SC6_RejectRequiresPolicyDefinedError(t *testing.T) {
	// ADVERSARY BREAK: action=reject with omitempty reject_status/reject_body
	// validates and canonicalizes to RejectStatus=0, RejectBody="". SC6
	// requires a policy-defined error; empty default is not one.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: reject
    builtins: [Email]
`))
	errs := ValidatePolicy(p)
	if !advSC6HasErr(errs, "reject") {
		t.Errorf("ADVERSARY BREAK: SC6 reject without reject_status/reject_body accepted: %s", advSC6ErrBlob(errs))
	}
	if p.PII != nil && p.PII.RejectStatus == 0 && p.PII.RejectBody == "" {
		t.Errorf("ADVERSARY BREAK: SC6 reject has no policy-defined error (status=0 body empty)")
	}
}

func TestAdvSC6_SC6_RejectMustNotCompileUnconditionalUpstream(t *testing.T) {
	// Locked SC6: reject-on-request is a policy-defined error (reject_status
	// + reject_body persisted in parse/validate/canonical). "No upstream" is
	// runtime/harness — NOT "omit declared egress host from compile" (D205
	// same-policy). Compiled YAML must not leak a tenant canary.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: reject
    builtins: [Email]
    patterns:
      - "`+advSC6Canary+`"
    reject_status: 422
    reject_body: "policy-defined-reject"
`))
	errs := ValidatePolicy(p)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("valid reject policy must validate: %s", advSC6ErrBlob(errs))
		}
	}
	if p.PII == nil || p.PII.Action != PIIActionReject || p.PII.RejectStatus != 422 || p.PII.RejectBody != "policy-defined-reject" {
		t.Fatalf("SC6 parse did not keep policy-defined reject error: %+v", p.PII)
	}
	cp, _ := Canonicalize(p)
	if cp == nil || cp.PII == nil {
		t.Fatal("ADVERSARY BREAK: Canonicalize dropped PII")
	}
	if cp.PII.Action != PIIActionReject {
		t.Errorf("ADVERSARY BREAK: Canonicalize dropped Action=reject: %q", cp.PII.Action)
	}
	if cp.PII.RejectStatus != 422 || cp.PII.RejectBody != "policy-defined-reject" {
		t.Errorf("ADVERSARY BREAK: Canonicalize dropped policy-defined error: %+v", cp.PII)
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	gw := string(out)
	// D205 same-policy: compile still emits declared egress hosts.
	if !strings.Contains(gw, "api.openai.com") {
		t.Errorf("ADVERSARY BREAK: D205 compile omitted declared egress host api.openai.com:\n%s", gw)
	}
	if strings.Contains(gw, advSC6Canary) {
		t.Errorf("ADVERSARY BREAK: SC6 tenant canary leaked into compiled gateway YAML:\n%s", gw)
	}
}

func TestAdvSC6_SC6_CanaryAbsentFromCompiledGateway(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Ssn]
    patterns:
      - "`+advSC6Canary+`"
`))
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(string(out), advSC6Canary) {
		t.Errorf("ADVERSARY BREAK: SC6 canary leaked into compiled gateway YAML:\n%s", out)
	}
}

func TestAdvSC6_SC6_CanaryCommentNotInCompile(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`# `+advSC6Canary+`
guardrails:
  pii:
    action: mask
    builtins: [Email]
`))
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if strings.Contains(string(out), advSC6Canary) {
		t.Errorf("ADVERSARY BREAK: SC6 canary from YAML comment leaked into compile:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// D203 — builtins = gateway PII list only; extra = tenant patterns
// ---------------------------------------------------------------------------

func TestAdvSC6_D203_SecretPackBuiltinsRejected(t *testing.T) {
	for _, bad := range []string{"AWS", "GitHub", "PEM", "aws_key", "private_key", "github_token"} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
			PII:     &PIIGuardrail{Action: PIIActionMask, Builtins: []string{bad}},
		}
		errs := ValidatePolicy(p)
		if !advSC6HasErr(errs, "builtin") {
			t.Errorf("ADVERSARY BREAK: D203 builtin %q accepted: %s", bad, advSC6ErrBlob(errs))
		}
		if AllowedPIIBuiltin(bad) {
			t.Errorf("ADVERSARY BREAK: D203 AllowedPIIBuiltin(%q)=true", bad)
		}
	}
}

func TestAdvSC6_D203_NoAWSGitHubPEMInjectedWhenNoExtra(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
`))
	cp, _ := Canonicalize(p)
	b, _ := json.Marshal(cp)
	out, _ := CompileGatewayConfig(p)
	if advSC6ContainsAWSGitHubPEM(string(b)) {
		t.Errorf("ADVERSARY BREAK: D203 AWS/GitHub/PEM appeared in canonical with no tenant extra: %s", b)
	}
	if advSC6ContainsAWSGitHubPEM(string(out)) {
		t.Errorf("ADVERSARY BREAK: D203 AWS/GitHub/PEM appeared in compile with no tenant extra")
	}
	if cp.PII != nil {
		for _, x := range cp.PII.Builtins {
			if !AllowedPIIBuiltin(x) {
				t.Errorf("ADVERSARY BREAK: D203 canonical builtin %q not in gateway list", x)
			}
		}
	}
}

func TestAdvSC6_D203_TenantExtraNotMergedIntoBuiltinSlot(t *testing.T) {
	// Extra (patterns) must remain a separate namespace from builtins.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    patterns:
      - "`+advSC6Canary+`"
`))
	cp, _ := Canonicalize(p)
	if cp.PII == nil {
		t.Fatal("canonical PII dropped")
	}
	for _, b := range cp.PII.Builtins {
		if b == advSC6Canary {
			t.Errorf("ADVERSARY BREAK: D203 tenant extra merged into builtins slot")
		}
		if !AllowedPIIBuiltin(b) {
			t.Errorf("ADVERSARY BREAK: D203 builtin slot contains non-gateway %q", b)
		}
	}
	found := false
	for _, pat := range cp.PII.Patterns {
		if pat == advSC6Canary {
			found = true
		}
	}
	if !found {
		t.Errorf("ADVERSARY BREAK: D205 tenant extra dropped from canonical patterns")
	}
}

func TestAdvSC6_D203_CaseAndHomoglyphBuiltinsRejected(t *testing.T) {
	for _, bad := range []string{"email", "EMAIL", "Ssn ", " Email", "SSN", "Еmail"} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
			PII:     &PIIGuardrail{Action: PIIActionMask, Builtins: []string{bad}},
		}
		errs := ValidatePolicy(p)
		if !advSC6HasErr(errs, "builtin") {
			t.Errorf("ADVERSARY BREAK: D203 lookalike builtin %q accepted", bad)
		}
	}
}

func TestAdvSC6_D203_TenantAWSPatternIsExtraNotBuiltin(t *testing.T) {
	// Tenant extra MAY mention AWS as a pattern; it must not become a builtin.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    patterns:
      - "(?i)AKIA[0-9A-Z]{16}"
`))
	errs := ValidatePolicy(p)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("tenant extra AWS-shaped pattern must be allowed as extra: %s", advSC6ErrBlob(errs))
		}
	}
	cp, _ := Canonicalize(p)
	if AllowedPIIBuiltin("AWS") {
		t.Fatal("ADVERSARY BREAK: D203 AWS became a gateway builtin")
	}
	for _, b := range cp.PII.Builtins {
		if b == "AWS" || strings.Contains(strings.ToLower(b), "akia") {
			t.Errorf("ADVERSARY BREAK: D203 extra promoted to builtin %q", b)
		}
	}
}

// ---------------------------------------------------------------------------
// D205 — local compile then push the same policy.yaml
// ---------------------------------------------------------------------------

func TestAdvSC6_D205_CanonicalizeDoesNotDropPII(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: reject
    builtins: [Ssn, Email]
    patterns:
      - "emp-[0-9]+"
    reject_status: 422
    reject_body: "policy-defined-reject"
`))
	cp, _ := Canonicalize(p)
	if cp.PII == nil {
		t.Fatal("ADVERSARY BREAK: D205 Canonicalize dropped PII")
	}
	if cp.PII.Action != PIIActionReject {
		t.Errorf("ADVERSARY BREAK: D205 action dropped/changed: %q", cp.PII.Action)
	}
	if cp.PII.RejectStatus != 422 || cp.PII.RejectBody != "policy-defined-reject" {
		t.Errorf("ADVERSARY BREAK: D205 reject contract dropped: %+v", cp.PII)
	}
}

func TestAdvSC6_D205_CanonicalizeIdempotentAndDeterministic(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email, CreditCard, Ssn]
    patterns:
      - "b-pat"
      - "a-pat"
`))
	cp1, _ := Canonicalize(p)
	b1, err := json.Marshal(cp1)
	if err != nil {
		t.Fatal(err)
	}
	cp2, _ := Canonicalize(p)
	b2, _ := json.Marshal(cp2)
	if string(b1) != string(b2) {
		t.Errorf("ADVERSARY BREAK: D205 Canonicalize not deterministic\n%s\n%s", b1, b2)
	}
	d1, err := Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("ADVERSARY BREAK: D205 Digest not stable: %s vs %s", d1, d2)
	}
}

func TestAdvSC6_D205_CompileDoesNotMutateCallerPII(t *testing.T) {
	orig := []string{PIIBuiltinEmail, advSC6Canary}
	pats := []string{advSC6Canary}
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: PIIActionMask, Builtins: orig, Patterns: pats},
	}
	if _, err := CompileGatewayConfig(p); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(p.PII.Builtins) != 2 || p.PII.Builtins[0] != PIIBuiltinEmail || p.PII.Builtins[1] != advSC6Canary {
		t.Errorf("ADVERSARY BREAK: D205 Compile mutated builtins: %v", p.PII.Builtins)
	}
	if len(p.PII.Patterns) != 1 || p.PII.Patterns[0] != advSC6Canary {
		t.Errorf("ADVERSARY BREAK: D205 Compile mutated patterns: %v", p.PII.Patterns)
	}
	_, _ = Canonicalize(p)
	if p.PII.Patterns[0] != advSC6Canary {
		t.Errorf("ADVERSARY BREAK: Canonicalize mutated caller patterns: %v", p.PII.Patterns)
	}
}

func TestAdvSC6_D205_CrossTenantExtraMustNotLeak(t *testing.T) {
	a := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    patterns:
      - "TENANT_A_MARKER_m155"
`))
	b := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
`))
	c := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
    patterns:
      - "TENANT_C_MARKER_m155"
`))
	outA, err := CompileGatewayConfig(a)
	if err != nil {
		t.Fatal(err)
	}
	outB, err := CompileGatewayConfig(b)
	if err != nil {
		t.Fatal(err)
	}
	outC, err := CompileGatewayConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(outB), "TENANT_A_MARKER_m155") || strings.Contains(string(outB), "TENANT_C_MARKER_m155") {
		t.Errorf("ADVERSARY BREAK: cross-tenant extra leaked into tenant B compile")
	}
	cpB, _ := Canonicalize(b)
	jb, _ := json.Marshal(cpB)
	if strings.Contains(string(jb), "TENANT_A_MARKER_m155") || strings.Contains(string(jb), "TENANT_C_MARKER_m155") {
		t.Errorf("ADVERSARY BREAK: cross-tenant extra leaked into tenant B canonical")
	}
	_ = outA
	_ = outC
}

// ---------------------------------------------------------------------------
// SC8 — new fields optional
// ---------------------------------------------------------------------------

func TestAdvSC6_SC8_LegacyPolicyWithoutPIIStillCompiles(t *testing.T) {
	p := advSC6Parse(t, `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
`)
	if p.PII != nil {
		t.Fatalf("ADVERSARY BREAK: SC8 omitted PII materialized: %+v", p.PII)
	}
	if PIIRequiresBufferedStream(p) {
		t.Fatal("ADVERSARY BREAK: SC8 PII-off still requires buffered stream")
	}
	errs := ValidatePolicy(p)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("SC8 legacy policy validate error: %s", e.Error())
		}
	}
	if _, err := CompileGatewayConfig(p); err != nil {
		t.Fatalf("SC8 compile: %v", err)
	}
	cp, _ := Canonicalize(p)
	b, _ := json.Marshal(cp)
	if strings.Contains(string(b), `"pii"`) {
		t.Errorf("ADVERSARY BREAK: SC8 pii key present when omitted: %s", b)
	}
}

func TestAdvSC6_SC8_B19SequenceUnchanged(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  - type: regex
    pattern: "(?i)(password|secret)"
    action: block
`))
	if p.PII != nil {
		t.Fatal("ADVERSARY BREAK: sequence form set PII")
	}
	if len(p.Guardrails) != 1 || p.Guardrails[0].Type != "regex" {
		t.Fatalf("B19 sequence broken: %+v", p.Guardrails)
	}
}

// ---------------------------------------------------------------------------
// Independent: empty / whitespace / NUL / newline patterns
// ---------------------------------------------------------------------------

func TestAdvSC6_EmptyStringPatternRejected(t *testing.T) {
	// ADVERSARY BREAK: patterns: [""] is a detector (len==1) so it bypasses
	// "at least one of builtins or patterns is required"; regexp.Compile("")
	// succeeds (matches every position). Empty extra replaces builtins with
	// a match-everything detector.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    patterns:
      - ""
`))
	if p.PII == nil || len(p.PII.Patterns) != 1 || p.PII.Patterns[0] != "" {
		t.Fatalf("probe: empty pattern not preserved as [\"\"], got %+v", p.PII)
	}
	errs := ValidatePolicy(p)
	if len(errs) == 0 || !advSC6HasErr(errs, "pattern") && !advSC6HasErr(errs, "empty") && !advSC6HasErr(errs, "builtins") {
		t.Errorf("ADVERSARY BREAK: empty-string extra pattern accepted (match-all): %s", advSC6ErrBlob(errs))
	}
}

func TestAdvSC6_WhitespaceOnlyPatternRejected(t *testing.T) {
	// ADVERSARY BREAK: whitespace-only pattern compiles as a real regex and
	// is accepted as the sole detector.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    patterns:
      - "   "
`))
	errs := ValidatePolicy(p)
	if len(errs) == 0 {
		t.Errorf("ADVERSARY BREAK: whitespace-only extra pattern accepted")
	}
}

func TestAdvSC6_NullPatternDoesNotCountAsDetector(t *testing.T) {
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    patterns:
      - null
`))
	errs := ValidatePolicy(p)
	if p.PII != nil && len(p.PII.Patterns) == 0 && len(p.PII.Builtins) == 0 {
		if !advSC6HasErr(errs, "builtins") && !advSC6HasErr(errs, "required") {
			t.Errorf("ADVERSARY BREAK: null extra became empty detectors without error: %+v %s", p.PII, advSC6ErrBlob(errs))
		}
	}
}

func TestAdvSC6_NewlineAndNULInPatternRejected(t *testing.T) {
	// ADVERSARY BREAK: control chars in extra patterns are accepted and will
	// forge extra lines if the pattern is later persisted to audit/logs.
	for _, pat := range []string{"foo\nbar", "foo\r\nbar", "foo\x00bar"} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
			PII:     &PIIGuardrail{Action: PIIActionMask, Patterns: []string{pat}},
		}
		errs := ValidatePolicy(p)
		if len(errs) == 0 {
			t.Errorf("ADVERSARY BREAK: control/NUL pattern accepted: %q", pat)
		}
	}
}

func TestAdvSC6_RejectBodyCRLFRejected(t *testing.T) {
	// ADVERSARY BREAK: reject_body with embedded newlines is accepted; if
	// emitted as an HTTP body/header it splits the policy-defined error.
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: reject
    builtins: [Email]
    reject_status: 422
    reject_body: "nope\nstatus: 200"
`))
	errs := ValidatePolicy(p)
	if p.PII != nil && strings.ContainsAny(p.PII.RejectBody, "\r\n") && len(errs) == 0 {
		t.Errorf("ADVERSARY BREAK: reject_body CRLF injection accepted: %q", p.PII.RejectBody)
	}
}

func TestAdvSC6_NestedQuantifierReDoSRejected(t *testing.T) {
	// ADVERSARY BREAK: nested-quantifier extra is accepted at Validate.
	// Cheap pattern only — do not hang CI.
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: PIIActionMask, Patterns: []string{"(?i)(a+)+$"}},
	}
	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_ = ValidatePolicy(p)
	}()
	if panicked != nil {
		t.Errorf("ADVERSARY BREAK: ValidatePolicy panicked on nested quantifier: %v", panicked)
		return
	}
	errs := ValidatePolicy(p)
	if len(errs) == 0 {
		t.Errorf("ADVERSARY BREAK: nested-quantifier ReDoS extra accepted")
	}
}

func TestAdvSC6_DuplicateBuiltinsRejectedOrDeduped(t *testing.T) {
	// ADVERSARY BREAK: duplicate builtins are accepted and Canonicalize
	// preserves Email twice (no dedup).
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email, Email, Ssn]
`))
	errs := ValidatePolicy(p)
	cp, _ := Canonicalize(p)
	if len(errs) == 0 && cp.PII != nil {
		seen := map[string]int{}
		for _, b := range cp.PII.Builtins {
			seen[b]++
		}
		for name, n := range seen {
			if n > 1 {
				t.Errorf("ADVERSARY BREAK: duplicate builtin %q x%d in canonical form %v", name, n, cp.PII.Builtins)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Independent: YAML injection / type confusion / merge / multi-doc
// ---------------------------------------------------------------------------

func TestAdvSC6_YAML11BoolActionMustNotFailOpen(t *testing.T) {
	// action: no / yes / true must not silently disable PII.
	for _, act := range []string{"no", "yes", "true", "false", "on", "off"} {
		yml := advSC6Base(`guardrails:
  pii:
    action: ` + act + `
    builtins: [Email]
`)
		p, err := ParsePolicy(strings.NewReader(yml))
		if err != nil {
			continue
		}
		if p.PII != nil && (p.PII.Action == "" || p.PII.Action == "false" || p.PII.Action == "no" || p.PII.Action == "off") {
			if PIIRequiresBufferedStream(p) {
				continue
			}
			errs := ValidatePolicy(p)
			hasErr := false
			for _, e := range errs {
				if e.Severity == "error" {
					hasErr = true
				}
			}
			if !hasErr {
				t.Errorf("ADVERSARY BREAK: YAML 1.1 action %q disabled PII without validation error", act)
			}
			out, cerr := CompileGatewayConfig(p)
			if cerr == nil && len(out) > 0 && !hasErr {
				t.Errorf("ADVERSARY BREAK: YAML 1.1 action %q compiled after disabling PII", act)
			}
		}
		if p.PII != nil && p.PII.Action != PIIActionMask && p.PII.Action != PIIActionReject {
			out, cerr := CompileGatewayConfig(p)
			if cerr == nil {
				t.Errorf("ADVERSARY BREAK: non-enum action %q compiled (%d bytes)", p.PII.Action, len(out))
			}
		}
	}
}

func TestAdvSC6_UnknownActionMustNotNoopCompile(t *testing.T) {
	// ADVERSARY BREAK: action log_only/redact/allow/"" parse and Validate
	// error, but Compile still emits a live config (no-op PII).
	for _, act := range []string{"log_only", "redact", "allow", "", "Mask", "REJECT"} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
			PII:     &PIIGuardrail{Action: act, Builtins: []string{PIIBuiltinEmail}},
		}
		errs := ValidatePolicy(p)
		hasErr := false
		for _, e := range errs {
			if e.Severity == "error" {
				hasErr = true
			}
		}
		if !hasErr {
			t.Errorf("ADVERSARY BREAK: unknown action %q accepted by ValidatePolicy", act)
		}
		out, err := CompileGatewayConfig(p)
		if err == nil {
			t.Errorf("ADVERSARY BREAK: unknown action %q compiled to %d bytes (no-op fail-open)", act, len(out))
		}
	}
}

func TestAdvSC6_DuplicateYAMLActionKeyRejected(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii:
    action: mask
    action: allow
    builtins: [Email]
`)
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: duplicate YAML action key parsed (mask then allow)")
	}
}

func TestAdvSC6_MultiDocumentRejected(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
`) + "---\nagent:\n  name: other\n"
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: multi-document YAML accepted")
	}
}

func TestAdvSC6_TopLevelPIIRejected(t *testing.T) {
	yml := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
pii:
  action: mask
  builtins: [Email]
`
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: top-level pii: parsed (dual path vs guardrails.pii)")
	}
}

func TestAdvSC6_PIINullMappingRejected(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii: null
`)
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: guardrails.pii: null parsed as enabled or as silent off")
	}
}

func TestAdvSC6_UnknownMappingKeyRejected(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
  webhook: https://evil.example/check
`)
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: unknown guardrails mapping key accepted")
	}
}

func TestAdvSC6_ScalarBuiltinsRejected(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: Email
`)
	_, err := ParsePolicy(strings.NewReader(yml))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: scalar builtins coerced to list")
	}
}

func TestAdvSC6_MergeAnchorCannotSkipInspect(t *testing.T) {
	yml := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
skip: &skip
  stream_inspect: skip
guardrails:
  pii:
    <<: *skip
    action: mask
    builtins: [Email]
`
	p, err := ParsePolicy(strings.NewReader(yml))
	if err == nil && p != nil && p.PII != nil && !PIIRequiresBufferedStream(p) {
		t.Fatal("ADVERSARY BREAK: YAML merge smuggled stream skip into mask PII")
	}
}

func TestAdvSC6_JSONSubsetParseParity(t *testing.T) {
	yml := advSC6Base(`guardrails:
  pii:
    action: mask
    builtins: [Email]
`)
	py, err := ParsePolicy(strings.NewReader(yml))
	if err != nil {
		t.Fatalf("yaml parse: %v", err)
	}
	js := `{"version":"1.0","agent":{"name":"test-agent"},"egress":[{"domain":"api.openai.com","ports":[443]}],"guardrails":{"pii":{"action":"mask","builtins":["Email"]}}}`
	pj, jerr := ParsePolicy(strings.NewReader(js))
	if jerr != nil {
		// JSON-as-YAML may be unsupported; then D205 local yaml vs json push diverges.
		t.Logf("JSON subset ParsePolicy: %v", jerr)
		return
	}
	dy, _ := Digest(py)
	dj, _ := Digest(pj)
	if dy != dj {
		t.Errorf("ADVERSARY BREAK: D205 YAML vs JSON digest diverge: %s vs %s", dy, dj)
	}
}

func TestAdvSC6_RejectStatusBounds(t *testing.T) {
	for _, st := range []int{0, 200, 399, 500, 600, -1} {
		p := &Policy{
			Version: "1.0",
			Agent:   AgentConfig{Name: "test"},
			Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
			PII: &PIIGuardrail{
				Action:       PIIActionReject,
				Builtins:     []string{PIIBuiltinEmail},
				RejectStatus: st,
			},
		}
		errs := ValidatePolicy(p)
		if st == 0 {
			// 0 means omitted; SC6 still wants an explicit 4xx — covered elsewhere.
			continue
		}
		if st < 400 || st > 499 {
			if !advSC6HasErr(errs, "4xx") && !advSC6HasErr(errs, "reject_status") {
				t.Errorf("ADVERSARY BREAK: reject_status %d accepted", st)
			}
		}
	}
}

func TestAdvSC6_MaskMustNotCarryRejectFields(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII: &PIIGuardrail{
			Action:       PIIActionMask,
			Builtins:     []string{PIIBuiltinEmail},
			RejectStatus: 422,
			RejectBody:   "nope",
		},
	}
	errs := ValidatePolicy(p)
	if !advSC6HasErr(errs, "reject") {
		t.Errorf("ADVERSARY BREAK: mask accepted reject_status/body: %s", advSC6ErrBlob(errs))
	}
}

func TestAdvSC6_InvalidUTF8PatternRejected(t *testing.T) {
	p := &Policy{
		Version: "1.0",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		PII:     &PIIGuardrail{Action: PIIActionMask, Patterns: []string{string([]byte{0xff, 0xfe, 'a'})}},
	}
	if utf8.ValidString(p.PII.Patterns[0]) {
		t.Fatal("fixture not invalid utf8")
	}
	errs := ValidatePolicy(p)
	if len(errs) == 0 {
		t.Errorf("ADVERSARY BREAK: invalid UTF-8 extra pattern accepted")
	}
}

func TestAdvSC6_EmptyPIIMappingDoesNotCompileAsOn(t *testing.T) {
	// ADVERSARY BREAK: pii: {} parses with empty action, Validate errors,
	// Compile still succeeds; PIIRequiresBufferedStream is false (skip).
	p := advSC6Parse(t, advSC6Base(`guardrails:
  pii: {}
`))
	if p.PII == nil {
		t.Fatal("empty mapping dropped PII entirely")
	}
	if PIIRequiresBufferedStream(p) {
		t.Log("empty mapping requires buffer (closed)")
	} else {
		t.Errorf("ADVERSARY BREAK: pii:{} present but inspect skipped (buffered=false)")
	}
	errs := ValidatePolicy(p)
	hasErr := false
	for _, e := range errs {
		if e.Severity == "error" {
			hasErr = true
		}
	}
	if !hasErr {
		t.Errorf("ADVERSARY BREAK: pii:{} validated")
	}
	out, err := CompileGatewayConfig(p)
	if err == nil && hasErr {
		t.Errorf("ADVERSARY BREAK: pii:{} compiled despite validate errors (%d bytes)", len(out))
	}
}
