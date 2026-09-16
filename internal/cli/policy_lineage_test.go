package cli

import (
	"bytes"
	"encoding/json"
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
	stdout, stderr, err := execPolicyLineage(t, dir, "--run-id", "run-does-not-exist")
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
		t.Fatalf("ENFORCEMENT invented a host for unknown run:\n%s", enforcement)
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
