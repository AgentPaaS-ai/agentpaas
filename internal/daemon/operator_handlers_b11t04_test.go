package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/operator"
)

func TestExplainFailure_PolicyDeniedRun(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("policy_denied", "run-policy", map[string]interface{}{
		"category": "mcp_denied",
		"reason":   "egress denied",
	}))

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-policy"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if resp.GetErrorCategory() != string(operator.ErrPolicyDenied) {
		t.Fatalf("ErrorCategory = %q, want %q", resp.GetErrorCategory(), operator.ErrPolicyDenied)
	}
	if resp.GetNextAction() != string(operator.ActionReviewPolicyPatch) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionReviewPolicyPatch)
	}
	if len(resp.GetEvidenceRefs()) == 0 {
		t.Fatal("EvidenceRefs is empty")
	}
}

func TestExplainFailure_BudgetExceededRun(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("run_failed", "run-budget", map[string]interface{}{
		"category": "budget_exceeded",
		"reason":   "token budget exhausted",
	}))

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-budget"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if resp.GetErrorCategory() != string(operator.ErrBudgetExceeded) {
		t.Fatalf("ErrorCategory = %q, want %q", resp.GetErrorCategory(), operator.ErrBudgetExceeded)
	}
	if resp.GetNextAction() != string(operator.ActionIncreaseBudget) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionIncreaseBudget)
	}
}

func TestExplainFailure_NoEvents(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "unknown"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if resp.GetErrorCategory() != string(operator.ErrAgentRuntimeException) {
		t.Fatalf("ErrorCategory = %q, want %q", resp.GetErrorCategory(), operator.ErrAgentRuntimeException)
	}
	if resp.GetNextAction() != string(operator.ActionAskUser) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionAskUser)
	}
}

func TestExplainPolicyDenial_FoundEvent(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("policy_denied", "run-denial", map[string]interface{}{
		"destination":   "api.example.com",
		"rule_id":       "egress[2]",
		"policy_digest": "sha256:abc",
		"reason":        "destination denied",
	}))

	resp, err := server.ExplainPolicyDenial(context.Background(), &controlv1.ExplainPolicyDenialRequest{
		RunId:             "run-denial",
		DeniedDestination: "api.example.com",
	})
	if err != nil {
		t.Fatalf("ExplainPolicyDenial: %v", err)
	}
	if resp.GetBlockingRuleId() != "egress[2]" {
		t.Fatalf("BlockingRuleId = %q, want egress[2]", resp.GetBlockingRuleId())
	}
	if resp.GetDeniedAction() != "api.example.com" {
		t.Fatalf("DeniedAction = %q, want api.example.com", resp.GetDeniedAction())
	}
}

func TestExplainPolicyDenial_NoEvent(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.ExplainPolicyDenial(context.Background(), &controlv1.ExplainPolicyDenialRequest{
		DeniedDestination: "blocked.example.com",
	})
	if err != nil {
		t.Fatalf("ExplainPolicyDenial: %v", err)
	}
	if resp.GetBlockingRuleId() != "default_deny" {
		t.Fatalf("BlockingRuleId = %q, want default_deny", resp.GetBlockingRuleId())
	}
	if resp.GetDeniedAction() != "blocked.example.com" {
		t.Fatalf("DeniedAction = %q, want blocked.example.com", resp.GetDeniedAction())
	}
}

func TestSummarizeRun_CompletedRun(t *testing.T) {
	start := operatorTestRecord("run_start", "run-complete", nil)
	start.Timestamp = "2026-01-02T03:04:05Z"
	invoke := operatorTestRecord("invoke", "run-complete", nil)
	invoke.Timestamp = "2026-01-02T03:04:06Z"
	complete := operatorTestRecord("run_complete", "run-complete", nil)
	complete.Timestamp = "2026-01-02T03:04:07Z"
	server := newOperatorTestServer(t, start, invoke, complete)

	resp, err := server.SummarizeRun(context.Background(), &controlv1.SummarizeRunRequest{RunId: "run-complete"})
	if err != nil {
		t.Fatalf("SummarizeRun: %v", err)
	}
	if resp.GetStatus() != "completed" {
		t.Fatalf("Status = %q, want completed", resp.GetStatus())
	}
	if resp.GetInvocations() != 1 {
		t.Fatalf("Invocations = %d, want 1", resp.GetInvocations())
	}
	if resp.GetDurationMs() != 2000 {
		t.Fatalf("DurationMs = %d, want 2000", resp.GetDurationMs())
	}
}

func TestSummarizeRun_NoEvents(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.SummarizeRun(context.Background(), &controlv1.SummarizeRunRequest{RunId: "unknown"})
	if err != nil {
		t.Fatalf("SummarizeRun: %v", err)
	}
	if resp.GetStatus() != "unknown" {
		t.Fatalf("Status = %q, want unknown", resp.GetStatus())
	}
}

func TestGetRunTimeline_Events(t *testing.T) {
	server := newOperatorTestServer(t,
		operatorTestRecord("run_start", "run-timeline", map[string]interface{}{"detail": "started"}),
		operatorTestRecord("invoke", "run-timeline", map[string]interface{}{"description": "called tool"}),
		operatorTestRecord("run_complete", "run-timeline", map[string]interface{}{"detail": "finished"}),
	)

	resp, err := server.GetRunTimeline(context.Background(), &controlv1.GetRunTimelineRequest{RunId: "run-timeline"})
	if err != nil {
		t.Fatalf("GetRunTimeline: %v", err)
	}
	if len(resp.GetEvents()) != 3 {
		t.Fatalf("len(Events) = %d, want 3", len(resp.GetEvents()))
	}
	if resp.GetEvents()[0].GetType() != "run_start" || resp.GetEvents()[2].GetType() != "run_complete" {
		t.Fatalf("events not sorted by sequence: %#v", resp.GetEvents())
	}
}

func TestNextAction_FailedRun(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("run_failed", "run-failed", map[string]interface{}{
		"category": "import_failed",
		"reason":   "module missing",
	}))

	resp, err := server.NextAction(context.Background(), &controlv1.NextActionRequest{Context: "run-failed"})
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if resp.GetNextAction() != string(operator.ActionInstallDependency) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionInstallDependency)
	}
}

func TestNextAction_NoContext(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.NextAction(context.Background(), &controlv1.NextActionRequest{})
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if resp.GetNextAction() != string(operator.ActionAskUser) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionAskUser)
	}
}

func TestExplainFailure_SecretRedaction(t *testing.T) {
	const token = "secret-token-value"
	server := newOperatorTestServer(t, operatorTestRecord("run_failed", "run-secret", map[string]interface{}{
		"category":   "task_failed",
		"detail":     "request failed with Bearer " + token,
		"stderr_ref": "api_key=" + token,
	}))

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-secret"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	combined := resp.GetRootCause()
	for _, excerpt := range resp.GetRedactedExcerpts() {
		combined += excerpt.GetContent()
	}
	if strings.Contains(combined, token) {
		t.Fatalf("response leaked token: %q", combined)
	}
	if !strings.Contains(combined, "[REDACTED]") {
		t.Fatalf("response did not contain redaction marker: %q", combined)
	}
}

func TestExplainFailure_MissingSecretBinding(t *testing.T) {
	server := newOperatorTestServer(t, operatorTestRecord("run_failed", "run-missing-secret", map[string]interface{}{
		"category": "missing_secret_binding",
		"reason":   "credential binding is not configured",
	}))

	resp, err := server.ExplainFailure(context.Background(), &controlv1.ExplainFailureRequest{RunId: "run-missing-secret"})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if resp.GetErrorCategory() != string(operator.ErrMissingSecretBinding) {
		t.Fatalf("ErrorCategory = %q, want %q", resp.GetErrorCategory(), operator.ErrMissingSecretBinding)
	}
	if resp.GetNextAction() != string(operator.ActionSetSecret) {
		t.Fatalf("NextAction = %q, want %q", resp.GetNextAction(), operator.ActionSetSecret)
	}
}

func newOperatorTestServer(t *testing.T, records ...audit.AuditRecord) *controlServer {
	t.Helper()
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for _, record := range records {
		if err := writer.Append(record); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	indexer, err := audit.NewSQLiteIndexer(filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = indexer.Close() })
	if err := indexer.Rebuild(auditPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	return &controlServer{auditIndex: indexer}
}

func operatorTestRecord(eventType, runID string, payload map[string]interface{}) audit.AuditRecord {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["run_id"] = runID
	return audit.AuditRecord{
		Timestamp:      time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano),
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          "test",
		Payload:        payload,
	}
}
