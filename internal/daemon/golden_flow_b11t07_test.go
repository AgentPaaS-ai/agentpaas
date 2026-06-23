package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGoldenFlow_B11T07(t *testing.T) {
	const (
		failedRunID = "golden-run-denied"
		retryRunID  = "golden-run-code-fixed"
		finalRunID  = "golden-run-policy-approved"
		secret      = "golden-flow-secret-token"
	)
	ctx := context.Background()

	// Steps 1-2: create an incomplete Python agent, then initialize its
	// manifests from code with the default-deny policy.
	projectDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(projectDir, "main.py"),
		[]byte("def app(input):\n    return {\"status\": \"ok\"}\n"),
		0o644,
	); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	detected, err := pack.DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject before init: %v", err)
	}
	if detected.Runtime != pack.RuntimePython {
		t.Fatalf("detected runtime = %q, want python", detected.Runtime)
	}
	if err := pack.InitFromCode(projectDir, detected.Runtime); err != nil {
		t.Fatalf("InitFromCode: %v", err)
	}
	if err := pack.InitPolicy(projectDir); err != nil {
		t.Fatalf("InitPolicy: %v", err)
	}

	agentConfig, err := pack.LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if agentConfig == nil || agentConfig.Runtime != string(pack.RuntimePython) {
		t.Fatalf("agent.yaml runtime = %#v, want python", agentConfig)
	}
	policyData, err := os.ReadFile(filepath.Join(projectDir, "policy.yaml"))
	if err != nil {
		t.Fatalf("read policy.yaml: %v", err)
	}
	if !strings.Contains(string(policyData), "egress: []") {
		t.Fatalf("policy.yaml is not default-deny: %s", policyData)
	}

	// Steps 5 and 10-12 are simulated by immutable audit records. Distinct run
	// IDs preserve the exact failed, code-fixed, and final successful histories.
	server := newOperatorTestServer(t,
		operatorTestRecord("run_start", failedRunID, nil),
		operatorTestRecord("invoke", failedRunID, map[string]interface{}{
			"destination": "evil.com",
		}),
		operatorTestRecord("policy_denied", failedRunID, map[string]interface{}{
			"destination":   "evil.com",
			"rule_id":       "default_deny",
			"policy_digest": "sha256:golden",
			"reason":        "egress to evil.com denied with Bearer " + secret,
		}),
		operatorTestRecord("run_failed", failedRunID, map[string]interface{}{
			"category":   "policy_denied",
			"reason":     "egress to evil.com denied with Bearer " + secret,
			"stderr_ref": "authorization=Bearer " + secret,
			"exit_code":  1,
		}),
		operatorTestRecord("run_start", retryRunID, nil),
		operatorTestRecord("invoke", retryRunID, map[string]interface{}{
			"detail": "invoke fixed agent",
		}),
		operatorTestRecord("run_complete", retryRunID, map[string]interface{}{"exit_code": 0}),
		operatorTestRecord("run_start", finalRunID, nil),
		operatorTestRecord("invoke", finalRunID, map[string]interface{}{
			"detail": "invoke approved agent",
		}),
		operatorTestRecord("run_complete", finalRunID, map[string]interface{}{"exit_code": 0}),
	)

	// Steps 3-4: validate the initialized project and use readiness as the
	// simulated pack check.
	validation, err := server.ValidateAgentProject(ctx, &controlv1.ValidateAgentProjectRequest{
		ProjectPath: projectDir,
	})
	if err != nil {
		t.Fatalf("ValidateAgentProject: %v", err)
	}
	if !validation.GetReady() || validation.GetRuntime() != string(pack.RuntimePython) {
		t.Fatalf("validation = %#v, want ready Python project", validation)
	}
	assertGoldenSchemaVersion(t, "ValidateAgentProject", validation.GetSchemaVersion())

	// Step 6: explain the default-deny decision.
	denial, err := server.ExplainPolicyDenial(ctx, &controlv1.ExplainPolicyDenialRequest{
		RunId:             failedRunID,
		DeniedDestination: "evil.com",
	})
	if err != nil {
		t.Fatalf("ExplainPolicyDenial: %v", err)
	}
	if denial.GetBlockingRuleId() != "default_deny" ||
		denial.GetNextAction() != string(operator.ActionReviewPolicyPatch) ||
		!strings.Contains(denial.GetDeniedAction(), "evil.com") {
		t.Fatalf("denial response = %#v", denial)
	}
	denialJSON, err := protojson.Marshal(denial)
	if err != nil {
		t.Fatalf("marshal denial response: %v", err)
	}
	if strings.Contains(string(denialJSON), secret) {
		t.Fatalf("denial response leaked secret: %s", denialJSON)
	}
	assertGoldenSchemaVersion(t, "ExplainPolicyDenial", denial.GetSchemaVersion())

	// Step 7: diagnose the failed run and verify secret redaction.
	explanation, err := server.ExplainFailure(ctx, &controlv1.ExplainFailureRequest{RunId: failedRunID})
	if err != nil {
		t.Fatalf("ExplainFailure: %v", err)
	}
	if explanation.GetErrorCategory() != string(operator.ErrPolicyDenied) ||
		explanation.GetNextAction() != string(operator.ActionReviewPolicyPatch) ||
		len(explanation.GetEvidenceRefs()) == 0 {
		t.Fatalf("explanation response = %#v", explanation)
	}
	explanationText := explanation.GetRootCause()
	for _, excerpt := range explanation.GetRedactedExcerpts() {
		explanationText += excerpt.GetContent()
	}
	if strings.Contains(explanationText, secret) || !strings.Contains(explanationText, "[REDACTED]") {
		t.Fatalf("failure detail was not safely redacted: %q", explanationText)
	}
	explanationJSON, err := protojson.Marshal(explanation)
	if err != nil {
		t.Fatalf("marshal explanation response: %v", err)
	}
	if strings.Contains(string(explanationJSON), secret) {
		t.Fatalf("explanation response leaked secret: %s", explanationJSON)
	}
	assertGoldenSchemaVersion(t, "ExplainFailure", explanation.GetSchemaVersion())

	// Steps 8-9: propose a patch, decline it, and verify the operator routes
	// back to a code fix instead of bypassing policy.
	proposal, err := server.RecommendPolicyPatch(ctx, &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	confirmationID := proposal.GetConfirmation().GetConfirmationId()
	if !strings.Contains(proposal.GetProposedPatch(), "api.example.com") ||
		(proposal.GetRiskLevel() != string(operator.RiskLow) &&
			proposal.GetRiskLevel() != string(operator.RiskMedium)) ||
		!proposal.GetConfirmation().GetRequiresConfirmation() ||
		confirmationID == "" ||
		proposal.GetNextAction() != string(operator.ActionReviewPolicyPatch) {
		t.Fatalf("patch proposal = %#v", proposal)
	}
	assertGoldenSchemaVersion(t, "RecommendPolicyPatch", proposal.GetSchemaVersion())
	if err := server.ConfirmChange(confirmationID, false); err != nil {
		t.Fatalf("decline ConfirmChange: %v", err)
	}
	next, err := server.NextAction(ctx, &controlv1.NextActionRequest{Context: confirmationID})
	if err != nil {
		t.Fatalf("NextAction after decline: %v", err)
	}
	if next.GetNextAction() != string(operator.ActionFixCode) {
		t.Fatalf("NextAction after decline = %q, want fix_code", next.GetNextAction())
	}
	assertGoldenSchemaVersion(t, "NextAction", next.GetSchemaVersion())

	retrySummary, err := server.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: retryRunID})
	if err != nil {
		t.Fatalf("SummarizeRun code-fixed retry: %v", err)
	}
	if retrySummary.GetStatus() != "completed" || retrySummary.GetPolicyDenials() != 0 {
		t.Fatalf("code-fixed retry summary = %#v", retrySummary)
	}
	assertGoldenSchemaVersion(t, "SummarizeRun retry", retrySummary.GetSchemaVersion())

	// Step 11: exercise the approval side of the confirmation protocol.
	approvedProposal, err := server.RecommendPolicyPatch(ctx, &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.github.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch approved flow: %v", err)
	}
	if !approvedProposal.GetConfirmation().GetRequiresConfirmation() {
		t.Fatal("approved policy proposal did not require confirmation")
	}
	approvedID := approvedProposal.GetConfirmation().GetConfirmationId()
	if approvedID == "" {
		t.Fatal("approved policy proposal confirmation ID is empty")
	}
	assertGoldenSchemaVersion(t, "RecommendPolicyPatch approved flow", approvedProposal.GetSchemaVersion())
	if err := server.ConfirmChange(approvedID, true); err != nil {
		t.Fatalf("approve ConfirmChange: %v", err)
	}
	approvedChange, err := server.confirmationStore().Get(approvedID)
	if err != nil {
		t.Fatalf("get approved confirmation: %v", err)
	}
	if approvedChange.Status != "approved" {
		t.Fatalf("confirmation status = %q, want approved", approvedChange.Status)
	}

	// Step 12: summarize the final successful run.
	finalSummary, err := server.SummarizeRun(ctx, &controlv1.SummarizeRunRequest{RunId: finalRunID})
	if err != nil {
		t.Fatalf("SummarizeRun final: %v", err)
	}
	if finalSummary.GetStatus() != "completed" ||
		finalSummary.GetInvocations() < 1 ||
		finalSummary.GetPolicyDenials() != 0 {
		t.Fatalf("final run summary = %#v", finalSummary)
	}
	assertGoldenSchemaVersion(t, "SummarizeRun final", finalSummary.GetSchemaVersion())

	// Step 13: export and verify an ordered, audit-referenced timeline.
	timeline, err := server.GetRunTimeline(ctx, &controlv1.GetRunTimelineRequest{RunId: finalRunID})
	if err != nil {
		t.Fatalf("GetRunTimeline: %v", err)
	}
	if len(timeline.GetEvents()) == 0 {
		t.Fatal("timeline has no events")
	}
	var previousSeq float64
	for i, event := range timeline.GetEvents() {
		var data map[string]interface{}
		if err := json.Unmarshal(event.GetData(), &data); err != nil {
			t.Fatalf("timeline event %d data is invalid JSON: %v", i, err)
		}
		seq, ok := data["audit_seq"].(float64)
		if !ok || seq <= 0 {
			t.Fatalf("timeline event %d audit_seq = %#v", i, data["audit_seq"])
		}
		if i > 0 && seq <= previousSeq {
			t.Fatalf("timeline audit sequence is not sorted: %v then %v", previousSeq, seq)
		}
		previousSeq = seq
	}
	assertGoldenSchemaVersion(t, "GetRunTimeline", timeline.GetSchemaVersion())

	// Step 14: ensure the operator response is JSON serializable, then print
	// the machine-readable golden-flow result.
	summaryJSON, err := protojson.Marshal(finalSummary)
	if err != nil {
		t.Fatalf("marshal final SummarizeRun response: %v", err)
	}
	if !json.Valid(summaryJSON) {
		t.Fatalf("final SummarizeRun response is not valid JSON: %s", summaryJSON)
	}
	if finalSummary.GetSummary() == "" || finalSummary.GetSchemaVersion() == "" ||
		len(finalSummary.GetEvidenceRefs()) == 0 {
		t.Fatalf("final SummarizeRun response is incomplete: %s", summaryJSON)
	}

	goldenSummary := struct {
		GoldenFlow     string                 `json:"golden_flow"`
		StepsCompleted int                    `json:"steps_completed"`
		Status         string                 `json:"status"`
		Init           map[string]interface{} `json:"init"`
		Validate       map[string]interface{} `json:"validate"`
		Denial         map[string]interface{} `json:"denial"`
		Explanation    map[string]interface{} `json:"explanation"`
		PatchProposal  map[string]interface{} `json:"patch_proposal"`
		Decline        map[string]interface{} `json:"decline"`
		Rerun          map[string]interface{} `json:"rerun"`
		AuditExport    map[string]interface{} `json:"audit_export"`
		SchemaVersion  string                 `json:"schema_version"`
	}{
		GoldenFlow:     "B11-T07",
		StepsCompleted: 14,
		Status:         "pass",
		Init: map[string]interface{}{
			"agent_yaml":  true,
			"policy_yaml": true,
			"runtime":     validation.GetRuntime(),
		},
		Validate: map[string]interface{}{"ready": validation.GetReady()},
		Denial: map[string]interface{}{
			"blocking_rule": denial.GetBlockingRuleId(),
			"next_action":   denial.GetNextAction(),
		},
		Explanation: map[string]interface{}{
			"error_category": explanation.GetErrorCategory(),
			"next_action":    explanation.GetNextAction(),
		},
		PatchProposal: map[string]interface{}{
			"risk_level":            proposal.GetRiskLevel(),
			"confirmation_required": proposal.GetConfirmation().GetRequiresConfirmation(),
		},
		Decline: map[string]interface{}{"next_action_after_decline": next.GetNextAction()},
		Rerun: map[string]interface{}{
			"status":         finalSummary.GetStatus(),
			"invocations":    finalSummary.GetInvocations(),
			"policy_denials": finalSummary.GetPolicyDenials(),
		},
		AuditExport: map[string]interface{}{
			"events": len(timeline.GetEvents()),
			"sorted": true,
		},
		SchemaVersion: finalSummary.GetSchemaVersion(),
	}
	output, err := json.MarshalIndent(goldenSummary, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden-flow summary: %v", err)
	}
	if !json.Valid(output) {
		t.Fatalf("golden-flow summary is not valid JSON: %s", output)
	}
	t.Logf("golden flow summary:\n%s", output)
}

func assertGoldenSchemaVersion(t *testing.T, responseName, schemaVersion string) {
	t.Helper()
	if schemaVersion != operator.SchemaVersion {
		t.Fatalf("%s SchemaVersion = %q, want %q", responseName, schemaVersion, operator.SchemaVersion)
	}
}
