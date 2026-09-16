package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const policyValidateParseFailBody = "UNIQUE_POLICY_VALIDATE_RAW_BODY_xyz987"

const policyValidateParseFailYAML = "version: [this is: not: valid: yaml:\n" + policyValidateParseFailBody + "\n"

const policyValidateErrorYAML = `version: "1.0"
agent:
  name: validate-error
egress:
  - cidr: 10.0.0.0/8
    ports: [443]
`

const policyValidateWarnYAML = `version: "1.0"
agent:
  name: validate-warn
credentials:
  - id: unused-key
    type: header
    header: Authorization
    value: «redacted:sk-…»
`

func writePolicyValidateYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeCLITestFile(t, dir, "policy.yaml", content)
	return dir
}

func execPolicyValidate(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return executeCmd(append([]string{"policy", "validate"}, args...)...)
}

func mustPolicyValidateJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("json unmarshal: %v\nstdout:\n%s", err, stdout)
	}
	return got
}

func TestPolicyValidateCompiledJSONEqualsShow(t *testing.T) {
	dir := writePolicyValidateYAML(t, policyShowCompiledYAML)

	showOut, showErr, err := execPolicyShow(t, dir, "--json")
	if err != nil {
		t.Fatalf("policy show: %v\nstderr: %s\nstdout: %s", err, showErr, showOut)
	}
	validateOut, validateErr, _ := execPolicyValidate(t, dir, "--json")
	combined := validateOut + validateErr
	if strings.Contains(combined, policyShowCanary) {
		t.Fatal("canary credential value leaked in validate JSON output")
	}

	show := mustPolicyShowJSON(t, showOut)
	got := mustPolicyValidateJSON(t, validateOut)

	skip := map[string]bool{"valid": true, "errors": true, "warnings": true}
	for k, want := range show {
		if skip[k] {
			continue
		}
		have, ok := got[k]
		if !ok {
			t.Fatalf("validate JSON missing compiled field %q", k)
		}
		if !reflect.DeepEqual(have, want) {
			t.Fatalf("validate[%q] != show[%q]\ngot  %#v\nwant %#v", k, k, have, want)
		}
	}
}

func TestPolicyValidate_ParseFailure(t *testing.T) {
	dir := writePolicyValidateYAML(t, policyValidateParseFailYAML)
	stdout, stderr, err := execPolicyValidate(t, dir)
	if err == nil {
		t.Fatalf("Execute() error = nil, want parse failure\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	out := stdout + stderr
	if strings.Contains(out, policyValidateParseFailBody) {
		t.Fatalf("output dumped raw YAML body\n%s", out)
	}
	if strings.Contains(out, `"valid": true`) || strings.Contains(out, "valid: true") {
		t.Fatalf("parse failure claimed valid\n%s", out)
	}
	if strings.Contains(stdout, `"digest"`) {
		t.Fatalf("parse failure emitted compiled digest view\n%s", stdout)
	}
}

func TestPolicyValidate_ValidatePolicyError(t *testing.T) {
	dir := writePolicyValidateYAML(t, policyValidateErrorYAML)
	stdout, stderr, err := execPolicyValidate(t, dir, "--json")
	if err == nil {
		t.Fatalf("Execute() error = nil, want validation error\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	got := mustPolicyValidateJSON(t, stdout)
	if jsonBool(t, got, "valid") {
		t.Fatal(`JSON valid=true, want false`)
	}
	errs, ok := got["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("JSON errors missing or empty: %v", got["errors"])
	}
	e0 := jsonMap(t, errs[0], "errors[0]")
	if jsonString(t, e0, "severity") != "error" {
		t.Fatalf("errors[0].severity = %v, want error", e0["severity"])
	}
	if jsonString(t, e0, "field") == "" || jsonString(t, e0, "message") == "" {
		t.Fatalf("errors[0] missing field/message: %v", e0)
	}
	if jsonString(t, got, "digest") == "" {
		t.Fatal("compiled digest missing on validation error")
	}
	if _, ok := got["ingress"]; !ok {
		t.Fatal("compiled ingress missing on validation error")
	}
}

func TestPolicyValidate_WarningsOnly(t *testing.T) {
	dir := writePolicyValidateYAML(t, policyValidateWarnYAML)
	stdout, stderr, err := execPolicyValidate(t, dir, "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v, want 0 on warnings-only\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("canary credential value leaked")
	}
	got := mustPolicyValidateJSON(t, stdout)
	if !jsonBool(t, got, "valid") {
		t.Fatal(`JSON valid=false, want true on warnings-only`)
	}
	warns, ok := got["warnings"].([]any)
	if !ok || len(warns) == 0 {
		t.Fatalf("JSON warnings missing or empty: %v", got["warnings"])
	}
	if _, hasErrs := got["errors"]; hasErrs {
		t.Fatalf("warnings-only must not include errors, got %v", got["errors"])
	}
	w0 := jsonMap(t, warns[0], "warnings[0]")
	if jsonString(t, w0, "severity") != "warning" {
		t.Fatalf("warnings[0].severity = %v, want warning", w0["severity"])
	}
}

func TestPolicyValidate_MissingPolicy(t *testing.T) {
	dir := t.TempDir()
	_, _, err := execPolicyValidate(t, dir)
	if err == nil {
		t.Fatal("Execute() error = nil, want missing policy.yaml")
	}
	want := "policy.yaml not found in " + dir + "; run 'agentpaas policy init " + dir + "' to create one"
	if err.Error() != want {
		t.Fatalf("error = %q\nwant    %q", err.Error(), want)
	}
}

func TestPolicyValidate_TerminalNotRawYAML(t *testing.T) {
	dir := writePolicyValidateYAML(t, policyShowCompiledYAML)
	yamlBytes, err := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	stdout, stderr, _ := execPolicyValidate(t, dir)
	if strings.TrimSpace(stdout) == strings.TrimSpace(string(yamlBytes)) {
		t.Fatal("terminal output is identical to YAML file contents")
	}
	if !strings.Contains(stdout, "valid:") {
		t.Fatalf("terminal missing valid line\n%s", stdout)
	}
	if !strings.Contains(stdout, "Digest:") {
		t.Fatalf("terminal missing compiled digest\n%s", stdout)
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("canary leaked in terminal output")
	}
}
