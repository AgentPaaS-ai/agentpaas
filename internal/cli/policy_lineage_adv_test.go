package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const (
	lineageAdvSecretID = "sk_live_CANARY_LINEAGE_ADV_DO_NOT_EMIT"
	lineageAdvAKIA     = "AKIAIOSFODNN7EXAMPLE"
)

func lineageAdvProject(t *testing.T) string {
	t.Helper()
	return writePolicyShowYAML(t, policyShowCompiledYAML)
}

func lineageAdvEnforcement(t *testing.T, stdout string) string {
	t.Helper()
	return lineageEnforcementSection(t, stdout)
}

func TestLineageAdv_LocalGatewayRowsUnderscoreDestination(t *testing.T) {
	// Real <home>/state/audit.jsonl after daemon ingest: event_type
	// egress_allowed / egress_denied, payload.destination, payload.run_id.
	// Consume that only matches dotted egress.allowed + payload.host stays at zeros.
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress_allowed",
			"payload": map[string]any{
				"run_id":        "run-local",
				"destination":   "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         4,
				"decision":      "allowed",
				"method":        "POST",
			},
		},
		{
			"event_type": "egress_denied",
			"payload": map[string]any{
				"run_id":      "run-local",
				"destination": "evil.example.com",
				"method":      "GET",
				"decision":    "denied",
				"reason":      "domain not in allowlist",
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-local")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if !strings.Contains(enforcement, "hooks.stripe.com") || !strings.Contains(enforcement, "×4") {
		t.Fatalf("ADVERSARY BREAK: local gateway allow rows (egress_allowed + destination) not consumed:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "evil.example.com") {
		t.Fatalf("ADVERSARY BREAK: local gateway deny rows (egress_denied + destination) not consumed:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "stripe-key") {
		t.Fatalf("ADVERSARY BREAK: credential_id from local allow row missing:\n%s", enforcement)
	}
}

func TestLineageAdv_RG3D1DumpStringPayload(t *testing.T) {
	// RG3 run_events dump: type=egress.allowed, run_id column, payload TEXT JSON.
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"type":    "egress.allowed",
			"run_id":  "run_rg3",
			"payload": `{"host":"api.x.ai","credential_id":"cred_llm_xai","count":5}`,
		},
		{
			"type":    "egress.denied",
			"run_id":  "run_rg3",
			"payload": `{"host":"evil.example.com","method":"POST"}`,
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run_rg3")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if !strings.Contains(enforcement, "api.x.ai") || !strings.Contains(enforcement, "×5") {
		t.Fatalf("ADVERSARY BREAK: RG3 aggregate row with string payload not consumed:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "evil.example.com") {
		t.Fatalf("ADVERSARY BREAK: RG3 deny row with string payload not consumed:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "cred_llm_xai") {
		t.Fatalf("ADVERSARY BREAK: RG3 credential_id from string payload missing:\n%s", enforcement)
	}
}

func TestLineageAdv_DenyMixedMethodsKeepTypedReasons(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.denied",
			"payload": map[string]any{
				"run_id": "run-deny-mix",
				"host":   "evil.example.com",
				"method": "POST",
				"reason": "domain not in allowlist",
			},
		},
		{
			"event_type": "egress.denied",
			"payload": map[string]any{
				"run_id": "run-deny-mix",
				"host":   "evil.example.com",
				"method": "GET",
				"reason": "domain not in allowlist",
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-deny-mix")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if !strings.Contains(enforcement, "evil.example.com") || !strings.Contains(enforcement, "×2") {
		t.Fatalf("ADVERSARY BREAK: mixed-method denials missing host ×M:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "POST") || !strings.Contains(enforcement, "GET") {
		t.Fatalf("ADVERSARY BREAK: mixed-method denials dropped typed reasons:\n%s", enforcement)
	}
}

func TestLineageAdv_PIIMasksFromGatewayDeniedReason(t *testing.T) {
	// Harness auditPIIDecision writes event_type=egress_denied, reason=pii_masked, no host.
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress_denied",
			"payload": map[string]any{
				"run_id":   "run-pii",
				"decision": "denied",
				"reason":   "pii_masked",
			},
		},
		{
			"event_type": "egress_denied",
			"payload": map[string]any{
				"run_id":   "run-pii",
				"decision": "denied",
				"reason":   "pii_masked",
			},
		},
		{
			"event_type": "egress_denied",
			"payload": map[string]any{
				"run_id":   "run-pii",
				"decision": "denied",
				"reason":   "pii_masked",
			},
		},
	})
	_, enf := lineageJSONEnforcement(t, dir, home, "run-pii")
	n, ok := enf["pii_masks"].(float64)
	if !ok {
		t.Fatalf("pii_masks type = %T (%v)", enf["pii_masks"], enf["pii_masks"])
	}
	if int(n) != 3 {
		t.Fatalf("ADVERSARY BREAK: pii_masks = %v, want 3 from gateway pii_masked reasons", enf["pii_masks"])
	}
}

func TestLineageAdv_BudgetFromGatewayExceeded(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "budget_exceeded",
			"payload": map[string]any{
				"run_id":   "run-budget",
				"category": "tokens",
				"limit":    1000,
				"observed": 1500,
			},
		},
	})
	_, enf := lineageJSONEnforcement(t, dir, home, "run-budget")
	got, _ := enf["budget_consumed"].(string)
	if strings.TrimSpace(got) == "" {
		t.Fatalf("ADVERSARY BREAK: budget_consumed empty; gateway budget_exceeded not consumed: %v", enf)
	}
}

func TestLineageAdv_SecretShapedCredentialIDNotEmitted(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-secret-id",
				"host":          "api.x.ai",
				"credential_id": lineageAdvSecretID,
				"count":         2,
			},
		},
		{
			"event_type": "credential.injected",
			"payload": map[string]any{
				"run_id":        "run-secret-id",
				"credential_id": lineageAdvAKIA,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-secret-id")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	blob := stdout + stderr
	if strings.Contains(blob, lineageAdvSecretID) {
		t.Fatal("ADVERSARY BREAK: secret-shaped credential_id leaked in text")
	}
	if strings.Contains(blob, lineageAdvAKIA) {
		t.Fatal("ADVERSARY BREAK: AKIA-shaped credential_id leaked in text")
	}
	jout, jerr, jerr2 := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-secret-id", "--json")
	if jerr2 != nil {
		t.Fatalf("json Execute() error = %v\nstderr: %s\nstdout: %s", jerr2, jerr, jout)
	}
	if strings.Contains(jout+jerr, lineageAdvSecretID) || strings.Contains(jout+jerr, lineageAdvAKIA) {
		t.Fatal("ADVERSARY BREAK: secret-shaped credential_id leaked in JSON")
	}
}

func TestLineageAdv_CountZeroDoesNotInventHost(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-zero",
				"host":          "never-called.example",
				"credential_id": "unused-key",
				"count":         0,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-zero")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if strings.Contains(enforcement, "never-called.example") {
		t.Fatalf("ADVERSARY BREAK: count=0 invented a host:\n%s", enforcement)
	}
}

func TestLineageAdv_OversizedLineMustNotZeroParsedRows(t *testing.T) {
	dir := lineageAdvProject(t)
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	good, err := json.Marshal(map[string]any{
		"event_type": "egress.allowed",
		"payload": map[string]any{
			"run_id":        "run-huge",
			"host":          "hooks.stripe.com",
			"credential_id": "stripe-key",
			"count":         3,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	huge := strings.Repeat("A", 70_000)
	body := string(good) + "\n" + huge + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, "audit.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatalf("write audit.jsonl: %v", err)
	}
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", homeDir, "--run-id", "run-huge")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if !strings.Contains(enforcement, "hooks.stripe.com") || !strings.Contains(enforcement, "×3") {
		t.Fatalf("ADVERSARY BREAK: oversized JSONL line wiped previously parsed aggregates:\n%s", enforcement)
	}
}

func TestLineageAdv_HarnessRunAuditPath(t *testing.T) {
	// Claim: consume local audit/run rows. Gateway writes harness-audit.jsonl
	// under state/runs/<id>/ before ingest into state/audit.jsonl.
	dir := lineageAdvProject(t)
	homeDir := t.TempDir()
	runDir := filepath.Join(homeDir, "state", "runs", "run-harness", "harness-audit")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("mkdir run audit: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "state"), 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	row, err := json.Marshal(map[string]any{
		"event_type": "egress.allowed",
		"payload": map[string]any{
			"run_id":        "run-harness",
			"host":          "api.x.ai",
			"credential_id": "cred_llm_xai",
			"count":         7,
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "harness-audit.jsonl"), append(row, '\n'), 0o600); err != nil {
		t.Fatalf("write harness-audit.jsonl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "state", "audit.jsonl"), []byte(""), 0o600); err != nil {
		t.Fatalf("write empty audit.jsonl: %v", err)
	}
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", homeDir, "--run-id", "run-harness")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if !strings.Contains(enforcement, "api.x.ai") || !strings.Contains(enforcement, "×7") {
		t.Fatalf("ADVERSARY BREAK: run-scoped harness-audit.jsonl not consumed:\n%s", enforcement)
	}
}

func TestLineageAdv_PolicyHostsStayOutOfEnforcement(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type": "egress.allowed",
			"payload": map[string]any{
				"run_id":        "run-sc7",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         1,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-sc7")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if !strings.Contains(stdout, "api.openai.com") {
		t.Fatal("POLICY compiled host api.openai.com missing from full output")
	}
	enforcement := lineageAdvEnforcement(t, stdout)
	if strings.Contains(enforcement, "api.openai.com") {
		t.Fatalf("ADVERSARY BREAK: ENFORCEMENT copied a policy.yaml host:\n%s", enforcement)
	}
	if !strings.Contains(enforcement, "hooks.stripe.com") {
		t.Fatalf("ENFORCEMENT missing RG3 host:\n%s", enforcement)
	}
}

func TestLineageAdv_DigestIgnoresLockJSON(t *testing.T) {
	dir := lineageAdvProject(t)
	lockCanary := "LOCK_DIGEST_CANARY_DO_NOT_READ"
	lock := `{
  "digest": "` + lockCanary + `",
  "policy": {"digest": "` + lockCanary + `"},
  "secret": "` + policyShowCanary + `"
}`
	if err := os.WriteFile(filepath.Join(dir, "agent.lock"), []byte(lock), 0o600); err != nil {
		t.Fatalf("write agent.lock: %v", err)
	}
	parsed, err := policy.ParsePolicy(strings.NewReader(policyShowCompiledYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	want, err := policy.Digest(parsed)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	stdout, stderr, err := execPolicyLineage(t, dir, "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, lockCanary) {
		t.Fatal("ADVERSARY BREAK: lineage parsed agent.lock digest")
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("ADVERSARY BREAK: agent.lock secret leaked")
	}
	got := mustPolicyLineageJSON(t, stdout)
	lin := jsonMap(t, got["lineage"], "lineage")
	if jsonString(t, lin, "digest") != want {
		t.Fatalf("lineage.digest = %q, want policy.Digest %q", lin["digest"], want)
	}
}

func TestLineageAdv_ProofUnchangedAfterConsume(t *testing.T) {
	dir := lineageAdvProject(t)
	home := writeLineageAuditRows(t, []map[string]any{
		{
			"event_type":  "egress.allowed",
			"seq":         1,
			"prev_hash":   "abc",
			"record_hash": "def",
			"payload": map[string]any{
				"run_id":        "run-proof",
				"host":          "hooks.stripe.com",
				"credential_id": "stripe-key",
				"count":         2,
			},
		},
	})
	stdout, stderr, err := execPolicyLineage(t, dir, "--home", home, "--run-id", "run-proof", "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	got := mustPolicyLineageJSON(t, stdout)
	prf := jsonMap(t, got["proof"], "proof")
	if v, ok := prf["hash_chain_verified"].(bool); !ok || v {
		t.Fatalf("proof.hash_chain_verified = %v, want false", prf["hash_chain_verified"])
	}
	if jsonString(t, prf, "export_pointer") != "agentpaas audit export" {
		t.Fatalf("proof.export_pointer = %q, want agentpaas audit export", prf["export_pointer"])
	}
}
