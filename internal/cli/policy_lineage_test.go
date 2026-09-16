package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func execPolicyLineage(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return executeCmd(append([]string{"policy", "lineage"}, args...)...)
}

func mustPolicyLineageJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("json unmarshal: %v\nstdout:\n%s", err, stdout)
	}
	return got
}

func sectionIndex(t *testing.T, stdout, name string) int {
	t.Helper()
	idx := strings.Index(stdout, name)
	if idx < 0 {
		t.Fatalf("missing section %q in:\n%s", name, stdout)
	}
	return idx
}

func TestPolicyLineageFourSectionsOrder(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	iL := sectionIndex(t, stdout, "LINEAGE")
	iP := sectionIndex(t, stdout, "POLICY")
	iE := sectionIndex(t, stdout, "ENFORCEMENT")
	iR := sectionIndex(t, stdout, "PROOF")
	if !(iL < iP && iP < iE && iE < iR) {
		t.Fatalf("section order want LINEAGE < POLICY < ENFORCEMENT < PROOF, got %d %d %d %d\n%s", iL, iP, iE, iR, stdout)
	}
}

func TestPolicyLineageCompiledPolicyMatchesShow(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	parsed, err := policy.ParsePolicy(bytes.NewReader([]byte(policyShowCompiledYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	compiled, err := compilePolicyShow(dir, parsed)
	if err != nil {
		t.Fatalf("compilePolicyShow: %v", err)
	}
	want := formatPolicyShowText(compiled)
	policyBody := stdout
	if i := strings.Index(stdout, "POLICY"); i >= 0 {
		policyBody = stdout[i:]
		if j := strings.Index(policyBody, "ENFORCEMENT"); j >= 0 {
			policyBody = policyBody[:j]
		}
	}
	if !strings.Contains(policyBody, want) {
		t.Fatalf("POLICY section must contain formatPolicyShowText output.\nwant snippet digest=%s host=api.openai.com\nPOLICY:\n%s", compiled.Digest, policyBody)
	}
	if !strings.Contains(stdout, compiled.Digest) {
		t.Fatalf("digest %q missing from output:\n%s", compiled.Digest, stdout)
	}
	if !strings.Contains(stdout, "api.openai.com") {
		t.Fatal("missing egress host api.openai.com")
	}
	if !strings.Contains(stdout, "openai-key") {
		t.Fatal("missing credential id openai-key")
	}
}

func TestPolicyLineageNoCanary(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("canary credential value leaked in text output")
	}
	jout, jerr, jerr2 := execPolicyLineage(t, dir, "--json")
	if jerr2 != nil {
		t.Fatalf("json Execute() error = %v\nstderr: %s\nstdout: %s", jerr2, jerr, jout)
	}
	if strings.Contains(jout+jerr, policyShowCanary) {
		t.Fatal("canary credential value leaked in JSON output")
	}

	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-canary",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         2,
				"value":         policyShowCanary,
				"secret":        lineageFixtureSecret,
			},
		},
	})
	cOut, cErr, cErr2 := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-canary")
	if cErr2 != nil {
		t.Fatalf("run-id Execute() error = %v\nstderr: %s\nstdout: %s", cErr2, cErr, cOut)
	}
	blob := cOut + cErr
	if strings.Contains(blob, policyShowCanary) {
		t.Fatal("canary credential value leaked in run-id text output")
	}
	if strings.Contains(blob, lineageFixtureSecret) {
		t.Fatal("fixture secret value leaked in run-id text output")
	}
	cjOut, cjErr, cjErr2 := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-canary", "--json")
	if cjErr2 != nil {
		t.Fatalf("run-id json Execute() error = %v\nstderr: %s\nstdout: %s", cjErr2, cjErr, cjOut)
	}
	if strings.Contains(cjOut+cjErr, policyShowCanary) {
		t.Fatal("canary credential value leaked in run-id JSON output")
	}
	if strings.Contains(cjOut+cjErr, lineageFixtureSecret) {
		t.Fatal("fixture secret value leaked in run-id JSON output")
	}
}

func TestPolicyLineageNoInternalMarkers(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	blob := stdout + stderr
	for _, bad := range []string{"M15", "WI-19", "SC7", "RG3", "D204"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("user-facing text contains internal marker %q:\n%s", bad, blob)
		}
	}
}

func TestPolicyLineageEnforcementZerosWithoutRunID(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := stdout
	if i := strings.Index(stdout, "ENFORCEMENT"); i >= 0 {
		enforcement = stdout[i:]
		if j := strings.Index(enforcement, "PROOF"); j >= 0 {
			enforcement = enforcement[:j]
		}
	}
	if strings.Contains(enforcement, "api.openai.com") {
		t.Fatalf("ENFORCEMENT invented a host without a run id:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "0") {
		t.Fatalf("ENFORCEMENT should show zero counts without a run id:\n%s", enforcement)
	}
}

func TestPolicyLineageEnforcementZerosUnknownRun(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-other",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         4,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-does-not-exist")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageEnforcementSection(t, stdout)
	if strings.Contains(enforcement, "api.openai.com") {
		t.Fatalf("ENFORCEMENT invented a host for unknown run:\n%s", enforcement)
	}
	if strings.Contains(enforcement, "hooks.stripe.com") {
		t.Fatalf("ENFORCEMENT used rows from a different run:\n%s", enforcement)
	}
}

func TestPolicyLineageJSONKeys(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyLineage(t, dir, "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	got := mustPolicyLineageJSON(t, stdout)
	for _, k := range []string{"lineage", "policy", "enforcement", "proof"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("JSON missing key %q in %v", k, got)
		}
	}
	lin := jsonMap(t, got["lineage"], "lineage")
	for _, k := range []string{"packer", "tool", "digest", "signature", "parent"} {
		if _, ok := lin[k]; !ok {
			t.Fatalf("lineage missing %q in %v", k, lin)
		}
	}
	parsed, err := policy.ParsePolicy(bytes.NewReader([]byte(policyShowCompiledYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	wantDigest, err := policy.Digest(parsed)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if jsonString(t, lin, "digest") != wantDigest {
		t.Fatalf("lineage.digest = %q, want %q", lin["digest"], wantDigest)
	}
	pol := jsonMap(t, got["policy"], "policy")
	if jsonString(t, pol, "digest") != wantDigest {
		t.Fatalf("policy.digest = %q, want %q", pol["digest"], wantDigest)
	}
	enf := jsonMap(t, got["enforcement"], "enforcement")
	if _, ok := enf["egress_allowed"]; !ok {
		t.Fatalf("enforcement missing egress_allowed: %v", enf)
	}
	if _, ok := enf["egress_denied"]; !ok {
		t.Fatalf("enforcement missing egress_denied: %v", enf)
	}
	prf := jsonMap(t, got["proof"], "proof")
	if _, ok := prf["hash_chain_verified"]; !ok {
		t.Fatalf("proof missing hash_chain_verified: %v", prf)
	}
	if jsonString(t, prf, "export_pointer") == "" {
		t.Fatal("proof.export_pointer empty")
	}
}

func TestPolicyLineageMissingPolicyYAML(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, err := execPolicyLineage(t, dir)
	if err == nil {
		t.Fatalf("expected error for missing policy.yaml, stdout=%s stderr=%s", stdout, stderr)
	}
	blob := strings.ToLower(stdout + stderr + err.Error())
	if !strings.Contains(blob, "policy.yaml") {
		t.Fatalf("missing-policy error should mention policy.yaml, got %v\n%s\n%s", err, stdout, stderr)
	}
}

const lineageFixtureSecret = "sk-fixture-lineage-canary"

func lineageEnforcementSection(t *testing.T, stdout string) string {
	t.Helper()
	enforcement := stdout
	if i := strings.Index(stdout, "ENFORCEMENT"); i >= 0 {
		enforcement = stdout[i:]
		if j := strings.Index(enforcement, "PROOF"); j >= 0 {
			enforcement = enforcement[:j]
		}
	}
	return enforcement
}

func writeLineageAuditRows(t *testing.T, rows []map[string]any) string {
	t.Helper()
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	var b strings.Builder
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal audit row: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	path := filepath.Join(stateDir, "audit.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write audit.jsonl: %v", err)
	}
	return homeDir
}

func lineageJSONEnforcement(t *testing.T, dir, home, runID string) (string, map[string]any) {
	t.Helper()
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", runID, "--json")
	if err != nil {
		t.Fatalf("json Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	got := mustPolicyLineageJSON(t, stdout)
	return stdout, jsonMap(t, got["enforcement"], "enforcement")
}

func jsonStringSlice(t *testing.T, m map[string]any, key string) []string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing %q in %v", key, m)
	}
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("%s: want array, got %T (%v)", key, v, v)
	}
	out := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("%s[%d]: want string, got %T (%v)", key, i, item, item)
		}
		out = append(out, s)
	}
	return out
}

func TestPolicyLineageEnforcementAllowAggregates(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-allow",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         3,
			},
		},
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-allow",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         2,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-allow")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageEnforcementSection(t, stdout)
	if !strings.Contains(enforcement, "hooks.stripe.com ×5") {
		t.Fatalf("ENFORCEMENT missing summed host ×N:\n%s", enforcement)
	}
	if strings.Contains(enforcement, "api.openai.com") {
		t.Fatalf("ENFORCEMENT copied a policy host with no rows:\n%s", enforcement)
	}

	_, enf := lineageJSONEnforcement(t, dir, home, "run-allow")
	allowed := jsonStringSlice(t, enf, "egress_allowed")
	if len(allowed) != 1 || allowed[0] != "hooks.stripe.com ×5" {
		t.Fatalf("egress_allowed = %v, want [hooks.stripe.com ×5]", allowed)
	}
}

func TestPolicyLineageEnforcementDenials(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.denied",
			"payload": map[string]any{
				"run_id": "run-deny",
				"host":   "evil.example.com",
				"method": "POST",
			},
		},
		{
			"event_type": "egress.denied",
			"payload": map[string]any{
				"run_id": "run-deny",
				"host":   "evil.example.com",
				"method": "POST",
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-deny")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageEnforcementSection(t, stdout)
	if !strings.Contains(enforcement, "evil.example.com") {
		t.Fatalf("ENFORCEMENT masked or omitted deny host:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "×2") {
		t.Fatalf("ENFORCEMENT missing denied ×M:\n%s", enforcement)
	}

	_, enf := lineageJSONEnforcement(t, dir, home, "run-deny")
	denied := jsonStringSlice(t, enf, "egress_denied")
	if len(denied) != 1 {
		t.Fatalf("egress_denied = %v, want one host line", denied)
	}
	if !strings.Contains(denied[0], "evil.example.com") || !strings.Contains(denied[0], "×2") {
		t.Fatalf("egress_denied line = %q, want host ×2", denied[0])
	}
}

func TestPolicyLineageEnforcementSumSameHost(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-sum",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         2,
			},
		},
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-sum",
				"host":          "hooks.stripe.com",
				"credential_id": "backup-key",
				"count":         3,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-sum")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageEnforcementSection(t, stdout)
	if !strings.Contains(enforcement, "hooks.stripe.com ×5") {
		t.Fatalf("same-host allows should sum to ×5:\n%s", enforcement)
	}
	if strings.Count(enforcement, "hooks.stripe.com") != 1 {
		t.Fatalf("want one host line for summed allows:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "stripe-key") || !strings.Contains(enforcement, "backup-key") {
		t.Fatalf("both credential ids missing:\n%s", enforcement)
	}

	_, enf := lineageJSONEnforcement(t, dir, home, "run-sum")
	allowed := jsonStringSlice(t, enf, "egress_allowed")
	if len(allowed) != 1 || allowed[0] != "hooks.stripe.com ×5" {
		t.Fatalf("egress_allowed = %v, want one summed host line", allowed)
	}
	ids := jsonStringSlice(t, enf, "credential_injections")
	got := strings.Join(ids, ",")
	if !strings.Contains(got, "stripe-key") || !strings.Contains(got, "backup-key") {
		t.Fatalf("credential_injections = %v, want both ids", ids)
	}
	if len(ids) != 2 {
		t.Fatalf("credential_injections = %v, want 2 ids", ids)
	}
}

func TestPolicyLineageEnforcementZerosEmptyFixture(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	home := writeLineageAuditRows(t, nil)
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-empty")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageEnforcementSection(t, stdout)
	if strings.Contains(enforcement, "api.openai.com") || strings.Contains(enforcement, "hooks.stripe.com") {
		t.Fatalf("empty fixture invented hosts:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "0") {
		t.Fatalf("empty fixture should stay at zero counts:\n%s", enforcement)
	}

	_, enf := lineageJSONEnforcement(t, dir, home, "run-empty")
	if got := jsonStringSlice(t, enf, "egress_allowed"); len(got) != 0 {
		t.Fatalf("egress_allowed = %v, want empty", got)
	}
	if got := jsonStringSlice(t, enf, "egress_denied"); len(got) != 0 {
		t.Fatalf("egress_denied = %v, want empty", got)
	}
	if got := jsonStringSlice(t, enf, "credential_injections"); len(got) != 0 {
		t.Fatalf("credential_injections = %v, want empty", got)
	}
}
