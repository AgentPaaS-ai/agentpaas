package daemon

import (
	"context"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
)

// ADVERSARY BREAK: ExplainPolicyDenial must reject attacker-supplied rule IDs
// that do not identify a known policy rule format.
func TestAdversaryB11T04_PolicyDenialFakeRuleID(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("policy_denied", "run-fake", map[string]interface{}{
		"destination":   "evil.com",
		"rule_id":       "nonexistent-rule-999",
		"policy_digest": "sha256:fake",
		"reason":        "attacker controlled",
	}))

	resp, err := server.ExplainPolicyDenial(context.Background(), &controlv1.ExplainPolicyDenialRequest{
		RunId:             "run-fake",
		DeniedDestination: "evil.com",
	})
	if err != nil {
		t.Fatalf("ExplainPolicyDenial: %v", err)
	}
	if resp.GetBlockingRuleId() != "default_deny" {
		t.Fatalf("BlockingRuleId = %q, want default_deny", resp.GetBlockingRuleId())
	}
}

// ADVERSARY BREAK: ExplainFailure includes run_failed record even if payload run_id mismatches the event's intended run; mismatch allows confusion or wrong diagnosis for target run.
func TestAdversaryB11T04_MismatchedRunIDInPayload(t *testing.T) {
	// record claims to be for run "A" in eventType but payload run_id="B"
	rec := operatorTestRecord("run_failed", "run-A", map[string]interface{}{
		"category": "task_failed",
		"reason":   "mismatch secret",
	})
	rec.Payload["run_id"] = "run-B" // override to mismatch

	server := newOperatorTestServer(t, rec)

	// request for "run-A" should find nothing? but record's eventType run_failed but filter uses payload
	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-A"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	// if it returns the record anyway or leaks, break; here we expect no failure found but test to show potential confusion
	if resp.GetErrorCategory() != string(operator.ErrAgentRuntimeException) {
		t.Fatalf("mismatch allowed diagnosis of wrong run")
	}
}

// ADVERSARY BREAK: Secret in "reason" field with full Authorization: Bearer header may partially survive redaction or not trigger all patterns (e.g. charset, substring without \b).
func TestAdversaryB11T04_SecretInReasonBearer(t *testing.T) {
	const secret = "sk-abc12345678901234567890" // long enough
	server := newOperatorTestServer(t, operatorTestRecord("run_failed", "run-bearer", map[string]interface{}{
		"category": "task_failed",
		"reason":   "Authorization: Bearer " + secret,
	}))

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-bearer"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	combined := resp.GetRootCause()
	for _, ex := range resp.GetRedactedExcerpts() {
		combined += ex.GetContent()
	}
	if strings.Contains(combined, secret) {
		t.Fatalf("ADVERSARY BREAK: Bearer secret leaked in response: %s", combined)
	}
}

// ADVERSARY BREAK: Multiple run_failed events for same run with different categories; latest wins but attacker can append later fake category to control diagnosis.
func TestAdversaryB11T04_MultipleCategoriesLatestWins(t *testing.T) {
	r1 := operatorTestRecord("run_failed", "run-multi", map[string]interface{}{"category": "budget_exceeded"})
	r2 := operatorTestRecord("run_failed", "run-multi", map[string]interface{}{"category": "mcp_denied"})
	// r2 appended later so higher seq, should win
	server := newOperatorTestServer(t, r1, r2)

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-multi"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	// deterministic latest, but if non-deterministic or wrong action, break
	if resp.GetErrorCategory() != string(operator.ErrPolicyDenied) {
		t.Fatalf("latest category not selected deterministically: got %s", resp.GetErrorCategory())
	}
}

// ADVERSARY BREAK: Empty Payload in audit event causes missing category handling to default incorrectly or panic on nil map access (but code guards).
func TestAdversaryB11T04_EmptyPayload(t *testing.T) {
	rec := operatorTestRecord("run_failed", "run-empty", nil)
	rec.Payload = map[string]interface{}{} // empty

	server := newOperatorTestServer(t, rec)

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-empty"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if resp.GetErrorCategory() == "" {
		t.Fatalf("empty payload produced empty ErrorCategory")
	}
}
