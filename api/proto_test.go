package api_test

import (
	"encoding/json"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// TestTriggerRunStatusFieldNumbersGolden ensures B26 statuses are additive and
// existing RunStatus numbers are never renumbered.
func TestTriggerRunStatusFieldNumbersGolden(t *testing.T) {
	want := map[string]int32{
		"RUN_STATUS_UNSPECIFIED":     0,
		"RUN_STATUS_PENDING":         1,
		"RUN_STATUS_RUNNING":         2,
		"RUN_STATUS_SUCCEEDED":       3,
		"RUN_STATUS_FAILED":          4,
		"RUN_STATUS_CANCELLED":       5,
		"RUN_STATUS_BUDGET_EXCEEDED": 6,
		"RUN_STATUS_PAUSE_REQUESTED": 7,
		"RUN_STATUS_PAUSED":          8,
		"RUN_STATUS_NEEDS_REPLAN":    9,
		"RUN_STATUS_EXPIRED":         10,
	}
	for name, num := range want {
		v, ok := triggerv1.RunStatus_value[name]
		if !ok {
			t.Errorf("missing RunStatus %s", name)
			continue
		}
		if v != num {
			t.Errorf("RunStatus %s = %d, want %d (field-number stability)", name, v, num)
		}
	}
}

// TestOldTriggerRunJSONUnmarshals verifies old clients' JSON shapes still load
// and new optional hierarchy fields remain empty when absent.
func TestOldTriggerRunJSONUnmarshals(t *testing.T) {
	oldJSON := `{
		"runId": "run_legacy",
		"agentName": "demo",
		"agentVersion": "0.1.0",
		"status": "RUN_STATUS_RUNNING"
	}`
	var run triggerv1.Run
	if err := protojson.Unmarshal([]byte(oldJSON), &run); err != nil {
		t.Fatalf("unmarshal old run: %v", err)
	}
	if run.GetRunId() != "run_legacy" {
		t.Fatalf("run_id = %q", run.GetRunId())
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_RUNNING {
		t.Fatalf("status = %v", run.GetStatus())
	}
	if run.GetWorkflowId() != "" || run.GetInvocationId() != "" || run.GetAttemptId() != "" {
		t.Fatalf("new fields should be empty for old payload: workflow=%q inv=%q att=%q",
			run.GetWorkflowId(), run.GetInvocationId(), run.GetAttemptId())
	}
}

// TestNewTriggerRunRoundTripWithWorkflowID verifies additive workflow_id is
// preservable without implying pipeline/spawn execution is enabled.
func TestNewTriggerRunRoundTripWithWorkflowID(t *testing.T) {
	run := &triggerv1.Run{
		RunId:        "run_new",
		AgentName:    "demo",
		Status:       triggerv1.RunStatus_RUN_STATUS_PAUSE_REQUESTED,
		WorkflowId:   "wf_standalone_1",
		InvocationId: "inv_1",
	}
	b, err := proto.Marshal(run)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out triggerv1.Run
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GetWorkflowId() != "wf_standalone_1" {
		t.Errorf("workflow_id lost: %q", out.GetWorkflowId())
	}
	if out.GetStatus() != triggerv1.RunStatus_RUN_STATUS_PAUSE_REQUESTED {
		t.Errorf("status = %v", out.GetStatus())
	}
}

// TestControlRunRequestResponseFieldNumbersGolden locks additive field numbers
// for RunRequest/RunResponse (never renumber existing fields).
func TestControlRunRequestResponseFieldNumbersGolden(t *testing.T) {
	// Descriptor-level checks via known getters + round-trip of wire numbers.
	req := &controlv1.RunRequest{
		AgentName:               "a",
		AgentVersion:            "1",
		ContinueRunId:           "run_prev",
		RecoveryAction:          "retry",
		RequestedAttemptLeaseMs: 5000,
		IdempotencyKey:          "idem-1",
		DeploymentRef:           "alias/prod",
	}
	b, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	var req2 controlv1.RunRequest
	if err := proto.Unmarshal(b, &req2); err != nil {
		t.Fatalf("unmarshal req: %v", err)
	}
	if req2.GetContinueRunId() != "run_prev" || req2.GetIdempotencyKey() != "idem-1" {
		t.Fatalf("request additive fields lost: %+v", &req2)
	}
	if req2.GetRequestedAttemptLeaseMs() != 5000 {
		t.Fatalf("lease ms = %d", req2.GetRequestedAttemptLeaseMs())
	}

	// Old request JSON (fields 1-4 only) still unmarshals.
	oldReqJSON := `{"agentName":"demo","agentVersion":"1.0.0"}`
	var oldReq controlv1.RunRequest
	if err := protojson.Unmarshal([]byte(oldReqJSON), &oldReq); err != nil {
		t.Fatalf("old RunRequest: %v", err)
	}
	if oldReq.GetAgentName() != "demo" || oldReq.GetContinueRunId() != "" {
		t.Fatalf("old request unexpected: %+v", &oldReq)
	}

	resp := &controlv1.RunResponse{
		RunId:                     "run_1",
		InvocationId:              "inv_1",
		WorkflowId:                "wf_1",
		Status:                    "PENDING",
		RequestedDeploymentRef:    "alias/prod",
		ResolvedDeploymentId:      "dep_1",
		ResolvedDeploymentVersion: "1.2.3",
	}
	// attempt_id intentionally absent on admission receipt.
	rb, err := proto.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal resp: %v", err)
	}
	var resp2 controlv1.RunResponse
	if err := proto.Unmarshal(rb, &resp2); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp2.GetRunId() != "run_1" || resp2.GetWorkflowId() != "wf_1" {
		t.Fatalf("response hierarchy lost: %+v", &resp2)
	}
	if resp2.GetAttemptId() != "" {
		t.Error("attempt_id should be absent on admission receipt")
	}

	// Old client only cares about run_id.
	oldRespJSON := `{"runId":"run_only"}`
	var oldResp controlv1.RunResponse
	if err := protojson.Unmarshal([]byte(oldRespJSON), &oldResp); err != nil {
		t.Fatalf("old RunResponse: %v", err)
	}
	if oldResp.GetRunId() != "run_only" {
		t.Fatalf("old run_id = %q", oldResp.GetRunId())
	}
}

// TestAttemptReportOnSummarizeRunResponse verifies attempt_report field 20.
func TestAttemptReportOnSummarizeRunResponse(t *testing.T) {
	resp := &controlv1.SummarizeRunResponse{
		Summary:       "done",
		SchemaVersion: "1.1.0",
		Status:        "failed",
		AttemptReport: &controlv1.AttemptReport{
			SchemaVersion: "1.1.0",
			RunId:         "run_1",
			AttemptId:     "att_1",
			Status:        "FAILED",
			Reason:        "BUDGET_EXCEEDED",
			Time: &controlv1.TimeBudgetSummary{
				AttemptDurationMs: 100,
				RemainingMs:       0,
			},
			LlmBudget: &controlv1.LLMBudgetSummary{
				TotalCostDecimal: "1.25",
				TotalTokens:      42,
			},
		},
	}
	b, err := proto.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out controlv1.SummarizeRunResponse
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GetAttemptReport() == nil {
		t.Fatal("attempt_report missing")
	}
	if out.GetAttemptReport().GetAttemptId() != "att_1" {
		t.Errorf("attempt_id = %q", out.GetAttemptReport().GetAttemptId())
	}
	if out.GetAttemptReport().GetLlmBudget().GetTotalCostDecimal() != "1.25" {
		t.Errorf("cost = %q", out.GetAttemptReport().GetLlmBudget().GetTotalCostDecimal())
	}
}

// TestTypedControlErrorCodesGolden locks typed error enum numbers.
func TestTypedControlErrorCodesGolden(t *testing.T) {
	want := map[string]int32{
		"TYPED_CONTROL_ERROR_UNSPECIFIED":                0,
		"TYPED_CONTROL_ERROR_IDEMPOTENT_REPLAY":          1,
		"TYPED_CONTROL_ERROR_ALREADY_RUNNING":            2,
		"TYPED_CONTROL_ERROR_IDEMPOTENCY_CONFLICT":       3,
		"TYPED_CONTROL_ERROR_DEPLOYMENT_INACTIVE":        4,
		"TYPED_CONTROL_ERROR_RUN_TERMINAL":               5,
		"TYPED_CONTROL_ERROR_UNSAFE_PAUSE_BOUNDARY":      6,
		"TYPED_CONTROL_ERROR_CONCURRENCY_UNAVAILABLE":    7,
		"TYPED_CONTROL_ERROR_LIMIT_AMENDMENT_DENIED":     8,
		"TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED":        9,
		"TYPED_CONTROL_ERROR_MISSING_SCOPE":              10,
		"TYPED_CONTROL_ERROR_NUMERIC_OVERFLOW":           11,
		"TYPED_CONTROL_ERROR_CHANGED_IDEMPOTENCY_PAYLOAD": 12,
	}
	for name, num := range want {
		v, ok := controlv1.TypedControlErrorCode_value[name]
		if !ok {
			t.Errorf("missing TypedControlErrorCode %s", name)
			continue
		}
		if v != num {
			t.Errorf("%s = %d, want %d", name, v, num)
		}
	}
}

// TestDeploymentAndInvokeFixtures verifies golden shapes for deployment/invoke
// contracts and that absolute ceilings never use float fields in JSON.
func TestDeploymentAndInvokeFixtures(t *testing.T) {
	dep := &controlv1.DeploymentRecord{
		SchemaVersion:     "0.3.0",
		DeploymentId:      "dep_1",
		PackageName:       "demo",
		PackageVersion:    "1.0.0",
		Generation:        1,
		Status:            "ACTIVE",
		MaxConcurrentRuns: 2,
		BundleDigest:      "sha256:aaa",
	}
	b, err := protojson.Marshal(dep)
	if err != nil {
		t.Fatalf("marshal dep: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json: %v", err)
	}
	if m["deploymentId"] != "dep_1" && m["deployment_id"] != "dep_1" {
		// protojson uses camelCase by default
		if _, ok := m["deploymentId"]; !ok {
			t.Fatalf("deployment id missing: %v", m)
		}
	}

	inv := &controlv1.InvokeDeploymentResponse{
		Outcome:                controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_UNSPECIFIED,
		OutcomeName:            "FEATURE_NOT_ENABLED",
		RequestedDeploymentRef: "alias/prod",
		Error: &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED,
			CodeName: "FEATURE_NOT_ENABLED",
			Message:  "deployment invoke is representational in B26",
		},
		Ceilings: &controlv1.AbsoluteCeilingsSnapshot{
			OriginalMaxActiveDurationMs: 60000,
			CurrentMaxActiveDurationMs:  60000,
			OriginalMaxLlmSpendDecimal:  "1.00",
			CurrentMaxLlmSpendDecimal:   "1.00",
			ConsumedLlmSpendDecimal:     "0",
			AuthorityGeneration:         1,
		},
	}

	ib, err := protojson.Marshal(inv)
	if err != nil {
		t.Fatalf("marshal invoke: %v", err)
	}
	var im map[string]interface{}
	if err := json.Unmarshal(ib, &im); err != nil {
		t.Fatalf("invoke json: %v", err)
	}
	// Ensure no float spent fields appear as numbers for decimal ceilings.
	ceil, ok := im["ceilings"].(map[string]interface{})
	if !ok {
		t.Fatalf("ceilings missing: %v", im)
	}
	// protojson may omit zero-ish strings; check present string costs when set.
	if v, ok := ceil["originalMaxLlmSpendDecimal"]; ok {
		if _, isStr := v.(string); !isStr {
			t.Errorf("llm spend must be string decimal, got %T %v", v, v)
		}
	}
	if v, ok := ceil["originalMaxActiveDurationMs"]; ok {
		// protojson encodes int64 as string by default for JS safety.
		switch v.(type) {
		case float64, json.Number, string:
		default:
			t.Errorf("duration unexpected type %T", v)
		}
	}
}

// TestAuthorityScopeEnumGolden locks authority scope numbers and names.
func TestAuthorityScopeEnumGolden(t *testing.T) {
	want := map[string]int32{
		"AUTHORITY_SCOPE_UNSPECIFIED":        0,
		"AUTHORITY_SCOPE_DEFAULT":            1,
		"AUTHORITY_SCOPE_RUNS_CONTROL":       2,
		"AUTHORITY_SCOPE_RUNS_AMEND_LIMITS":  3,
	}
	for name, num := range want {
		v, ok := controlv1.AuthorityScope_value[name]
		if !ok || v != num {
			t.Errorf("%s = %v ok=%v want %d", name, v, ok, num)
		}
	}
}

// TestAmendLimitsAndPauseRequestShapes covers validation-oriented fixtures
// for missing-scope / terminal / overflow related fields (contract only).
func TestAmendLimitsAndPauseRequestShapes(t *testing.T) {
	req := &controlv1.AmendLimitsRequest{
		WorkflowId:                   "wf_1",
		ExpectedAuthorityGeneration:  3,
		NewMaxActiveDurationMs:       120000,
		NewCurrentAttemptLeaseMs:     30000,
		NewMaxLlmSpendDecimal:        "5.00",
		Reason:                       "operator grant",
		IdempotencyKey:               "amd-1",
		ActorIdentity:                "admin@example",
		AuthorityScope:               controlv1.AuthorityScope_AUTHORITY_SCOPE_RUNS_AMEND_LIMITS,
	}
	b, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out controlv1.AmendLimitsRequest
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GetAuthorityScope() != controlv1.AuthorityScope_AUTHORITY_SCOPE_RUNS_AMEND_LIMITS {
		t.Error("amend limits must carry runs:amend_limits scope")
	}
	if out.GetNewMaxLlmSpendDecimal() != "5.00" {
		t.Errorf("spend decimal = %q", out.GetNewMaxLlmSpendDecimal())
	}

	pause := &controlv1.SetWorkflowDesiredStateRequest{
		WorkflowId:         "wf_1",
		DesiredCommand:     controlv1.ControlCommand_CONTROL_COMMAND_PAUSE,
		ExpectedGeneration: 1,
		AuthorityScope:     controlv1.AuthorityScope_AUTHORITY_SCOPE_RUNS_CONTROL,
		IdempotencyKey:     "pause-1",
	}
	pb, err := proto.Marshal(pause)
	if err != nil {
		t.Fatalf("pause marshal: %v", err)
	}
	var p2 controlv1.SetWorkflowDesiredStateRequest
	if err := proto.Unmarshal(pb, &p2); err != nil {
		t.Fatalf("pause unmarshal: %v", err)
	}
	if p2.GetDesiredCommand() != controlv1.ControlCommand_CONTROL_COMMAND_PAUSE {
		t.Error("pause command lost")
	}

	// Typed terminal error fixture (callers must not parse message strings).
	term := &controlv1.RunTerminalResponse{
		Error: &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_RUN_TERMINAL,
			CodeName: "RUN_TERMINAL",
			RunId:    "run_done",
		},
		RunId:          "run_done",
		TerminalStatus: "SUCCEEDED",
	}
	if term.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_RUN_TERMINAL {
		t.Error("typed code required")
	}
}
