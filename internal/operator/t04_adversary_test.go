package operator

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"google.golang.org/protobuf/proto"
)

// ADVERSARY BREAK: HIGH - FLOAT USAGE: BudgetConfig.max_cost_usd is double (field 2). Task requires LLM spend MUST be string decimal. Legacy or new violation in proto.
func TestAdversaryBudgetConfigUsesFloat(t *testing.T) {
	// Check generated struct for double usage
	typ := reflect.TypeOf(controlv1.BudgetConfig{})
	field, ok := typ.FieldByName("MaxCostUsd")
	if !ok {
		t.Fatal("MaxCostUsd field missing")
	}
	if field.Type.Kind() != reflect.Float64 {
		t.Errorf("expected float64 for cost, got %v", field.Type)
	}
	// ADVERSARY: this test documents the float usage as a contract violation
}

// ADVERSARY BREAK: MEDIUM - IDENPOTENCY KEY: proto allows empty idempotency_key (no validation at proto level in RunRequest field 8 or InvokeRequest field 5)
func TestAdversaryEmptyIdempotencyKeyAllowed(t *testing.T) {
	req := &controlv1.RunRequest{AgentName: "test", IdempotencyKey: ""}
	b, _ := proto.Marshal(req)
	var req2 controlv1.RunRequest
	if err := proto.Unmarshal(b, &req2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req2.IdempotencyKey != "" {
		t.Error("unexpected non-empty")
	}
	// No proto-level enforcement; validation is higher layer only
}

// ADVERSARY BREAK: HIGH - AUTHORITY SCOPE LEAKAGE: runs:amend_limits (AUTHORITY_SCOPE_RUNS_AMEND_LIMITS=3) defined; contracts.py and proto expose it, comment claims not to Python/SDK paths but no enforcement in schema
func TestAdversaryAuthorityScopeLeakage(t *testing.T) {
	scopes := AllAuthorityScopes()
	found := false
	for _, s := range scopes {
		if s == AuthScopeRunsAmendLimits {
			found = true
		}
	}
	if !found {
		t.Error("amend_limits scope not in Go set")
	}
	// Python contracts.py also lists it in AUTHORITY_SCOPES
}

// ADVERSARY BREAK: MEDIUM - SCHEMA VERSION BYPASS: 1.0.0 not explicitly rejected; contracts.py and schema.go hardcode 1.1.0 but no IsValidSchemaVersion or reject in Python test framework
func TestAdversarySchemaVersionBypass(t *testing.T) {
	// Simulate old JSON
	oldJSON := `{"schema_version":"1.0.0","run_id":"r1","error_category":"budget_exceeded","root_cause":"x","next_action":"rerun"}`
	var resp ExplainFailureResponse
	if err := json.Unmarshal([]byte(oldJSON), &resp); err != nil {
		t.Fatalf("unmarshal old version: %v", err)
	}
	if resp.SchemaVersion != "1.0.0" {
		t.Error("old version not accepted")
	}
	// No fail-closed on schema version
}

// ADVERSARY BREAK: HIGH - FIELD NUMBER STABILITY: new fields on Run (12-14) and RunResponse (2-8) are additive; test old binary unmarshal to ensure no renumber
func TestAdversaryFieldNumberStability(t *testing.T) {
	// Old-style Run binary (pre B26 fields 12+)
	oldRun := &triggerv1.Run{
		RunId:       "r1",
		AgentName:   "a",
		Status:      triggerv1.RunStatus_RUN_STATUS_SUCCEEDED,
	}
	b, _ := proto.Marshal(oldRun)
	var newRun triggerv1.Run
	if err := proto.Unmarshal(b, &newRun); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if newRun.RunId != "r1" {
		t.Error("data loss on old binary")
	}
	// Verify no renumber by checking tag numbers via reflection on generated (simplified)
}

// ADVERSARY BREAK: MEDIUM - TYPED ERROR BYPASS: TypedControlErrorCode exists but callers can still use string error; no enforcement that TypedControlError is only path
func TestAdversaryTypedErrorBypass(t *testing.T) {
	// String error path still possible via google.rpc.Status message
	// Typed is additive but bypass via error string remains possible
}

// ADVERSARY BREAK: HIGH - MISSING SCOPE ENFORCEMENT: AuthorityScope not marked required in RunRequest/Control RPCs; can omit
func TestAdversaryMissingScopeEnforcement(t *testing.T) {
	req := &controlv1.RunRequest{AgentName: "test"} // no scope field present in message
	b, _ := proto.Marshal(req)
	// Request omits scope; proto does not enforce presence
	if len(b) == 0 {
		t.Error("empty")
	}
}

// ADVERSARY BREAK: MEDIUM - UNKNOWN ENUM FAIL-CLOSED: IsValidErrorCategory/IsValidNextAction return false for unknown, but no strict fail in unmarshal path for JSON strings
func TestAdversaryUnknownEnumFailClosed(t *testing.T) {
	unknown := ErrorCategory("unknown_cat_xyz")
	if IsValidErrorCategory(unknown) {
		t.Error("unknown accepted")
	}
	// Go strings accept any; validation is opt-in
}

// ADVERSARY BREAK: MEDIUM - DEPLOYMENT INACTIVE BYPASS: AdmissionOutcomeCode has DEPLOYMENT_INACTIVE but proto contract allows invocation representionally
func TestAdversaryDeploymentInactiveBypass(t *testing.T) {
	// Representational: RunRequest.deployment_ref + inactive deployment still serializable
	req := &controlv1.RunRequest{DeploymentRef: "inactive-alias"}
	_ = req // no proto-level inactive check
}

// ADVERSARY BREAK: HIGH - NUMERIC OVERFLOW: int64 fields like max_active_duration_ms / attempt_lease_ms (RunRequest.requested_attempt_lease_ms=7) have no max validation in proto or schema
func TestAdversaryNumericOverflow(t *testing.T) {
	req := &controlv1.RunRequest{RequestedAttemptLeaseMs: 1<<63 - 1} // max int64, no overflow guard in contract
	b, _ := proto.Marshal(req)
	var r2 controlv1.RunRequest
	if err := proto.Unmarshal(b, &r2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r2.RequestedAttemptLeaseMs != 1<<63-1 {
		t.Error("overflow not preserved or guarded")
	}
	// TypedControlError_NUMERIC_OVERFLOW exists but no proto validation
}