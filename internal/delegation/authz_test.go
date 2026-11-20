package delegation

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Helpers for tests

func makeTestSnapshot() *CommunicationSnapshot {
	bindings := []WorkflowDelegationBinding{
		{
			BindingID:            "report.verify",
			Operation:            "",
			CalleePackageName:    "report-verifier",
			CalleePackageVersion: "1.0.0",
			CalleeBundleDigest:   "sha256:callee-digest",
			CallerPackageName:    "weather-agent",
			MaxDataClass:         "internal",
			ArtifactAudience:     []string{"orchestrator"},
			DeadlineMs:           60000,
			MaxCostUSDDecimal:    "0.75",
		},
		{
			BindingID:            "data.analyze",
			Operation:            "analyze",
			CalleePackageName:    "data-analyzer",
			CalleePackageVersion: "2.0.0",
			CalleeBundleDigest:   "sha256:analyzer-digest",
			CallerPackageName:    "",
			MaxDataClass:         "confidential",
			ArtifactAudience:     []string{"orchestrator", "downstream"},
			DeadlineMs:           120000,
			MaxCostUSDDecimal:    "1.50",
		},
	}
	snap := &CommunicationSnapshot{
		SchemaVersion:        CurrentSchemaVersion,
		SnapshotGeneration:   3,
		WorkflowID:           "wf-test-snapshot",
		TenantID:             "tenant-test",
		CallerDeploymentID:   "dep-caller-1",
		CallerPackageName:    "weather-agent",
		CallerPackageDigest:  "sha256:caller-digest",
		Bindings:             bindings,
	}
	// Compute digest.
	dg, err := ComputeSnapshotDigest(snap)
	if err != nil {
		panic(fmt.Sprintf("compute snapshot digest: %v", err))
	}
	snap.SnapshotDigest = dg
	return snap
}

func makeCalleeIngressAllow() []CalleeIngressRule {
	return []CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "sha256:caller-digest",
			AllowedBindings:     []string{"report.verify"},
			MaxDataClass:        "internal",
		},
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "",
			AllowedBindings:     []string{"data.analyze"},
			MaxDataClass:        "confidential",
		},
	}
}

func makeAuthzRequest(snap *CommunicationSnapshot) AuthorizeRequest {
	return AuthorizeRequest{
		Snapshot:            snap,
		BindingID:           "report.verify",
		Operation:           "",
		CallerDeploymentID:  "dep-caller-1",
		CallerPackageDigest: "sha256:caller-digest",
		CalleePackageName:   "report-verifier",
		CalleePackageVersion:"1.0.0",
		CalleeBundleDigest:  "sha256:callee-digest",
		DataClass:           "internal",
		CalleeIngressAllow:  makeCalleeIngressAllow(),
		Now:                 time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// 1. Happy path both sides allow
// ---------------------------------------------------------------------------

func TestAuthz_HappyPath(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)

	decision := AuthorizeDelegation(&req)
	if !decision.Allowed {
		t.Fatalf("expected Allowed=true, got false; denial=%s caller=%+v callee=%+v",
			decision.DenialCode, decision.CallerDecision, decision.CalleeDecision)
	}
	if decision.DenialCode != "" {
		t.Errorf("expected empty denial code, got %q", decision.DenialCode)
	}
	if !decision.CallerDecision.Allowed {
		t.Error("expected caller decision allowed")
	}
	if !decision.CalleeDecision.Allowed {
		t.Error("expected callee decision allowed")
	}
	if decision.Binding == nil {
		t.Fatal("expected non-nil binding")
	}
	if decision.Binding.BindingID != "report.verify" {
		t.Errorf("expected binding_id=report.verify, got %q", decision.Binding.BindingID)
	}
}

// ---------------------------------------------------------------------------
// 2. Caller-authorized / callee-denied -> DENY_CALLEE_POLICY
// ---------------------------------------------------------------------------

func TestAuthz_CallerAllowedCalleeDenied(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Empty ingress (no matching rule).
	req.CalleeIngressAllow = []CalleeIngressRule{}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCalleePolicy {
		t.Errorf("expected %s, got %s", DenyCalleePolicy, decision.DenialCode)
	}
	if !decision.CallerDecision.Allowed {
		t.Error("expected caller decision allowed")
	}
	if decision.CalleeDecision.Allowed {
		t.Error("expected callee decision denied")
	}
}

// ---------------------------------------------------------------------------
// 3. Caller-denied / callee-authorized -> DENY_CALLER_BINDING
// ---------------------------------------------------------------------------

func TestAuthz_CallerDeniedCalleeAllowed(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Wrong callee digest — caller side fails on exact pin, but callee ingress
	// still matches the caller + binding.
	req.CalleeBundleDigest = "sha256:wrong-digest"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
	if decision.CallerDecision.Allowed {
		t.Error("expected caller decision denied")
	}
	if !decision.CalleeDecision.Allowed {
		t.Errorf("expected callee decision allowed, got: %s", decision.CalleeDecision.ReasonDetail)
	}
}

// ---------------------------------------------------------------------------
// 4. Both denied
// ---------------------------------------------------------------------------

func TestAuthz_BothDenied(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Wrong binding = caller denied.
	req.BindingID = "nonexistent.binding"
	// Also wrong data class = callee would deny too.
	req.CalleeIngressAllow = []CalleeIngressRule{}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	// Caller code preferred when caller failed.
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s (caller preferred), got %s", DenyCallerBinding, decision.DenialCode)
	}
	if decision.CallerDecision.Allowed {
		t.Error("expected caller denied")
	}
	if decision.CalleeDecision.Allowed {
		t.Error("expected callee denied")
	}
}

// ---------------------------------------------------------------------------
// 5. Snapshot caller digest mismatch
// ---------------------------------------------------------------------------

func TestAuthz_SnapshotDigestMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Different caller digest than what snapshot pins.
	req.CallerPackageDigest = "sha256:evil-digest"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenySnapshotMismatch {
		t.Errorf("expected %s, got %s", DenySnapshotMismatch, decision.DenialCode)
	}
}

func TestAuthz_SnapshotDeploymentMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.CallerDeploymentID = "dep-evil"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenySnapshotMismatch {
		t.Errorf("expected %s, got %s", DenySnapshotMismatch, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 6. Wrong callee digest (registry moved) denied
// ---------------------------------------------------------------------------

func TestAuthz_CalleeBundleDigestMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.CalleeBundleDigest = "sha256:moved-digest"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
}

func TestAuthz_CalleePackageVersionMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.CalleePackageVersion = "2.0.0"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 7. Unknown binding
// ---------------------------------------------------------------------------

func TestAuthz_UnknownBinding(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.BindingID = "unknown.binding"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
	if decision.CallerDecision.Evaluated && decision.CallerDecision.Allowed {
		t.Error("caller should be denied for unknown binding")
	}
}

// ---------------------------------------------------------------------------
// 8. Data class escalation denied (both sides as applicable)
// ---------------------------------------------------------------------------

func TestAuthz_DataClassEscalationCallerSide(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// binding MaxDataClass is "internal", but request is "confidential"
	req.DataClass = "confidential"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
	if decision.CallerDecision.Allowed {
		t.Error("caller should deny data class escalation")
	}
}

func TestAuthz_DataClassEscalationCalleeSide(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// callee ingress rule MaxDataClass is "internal", request "confidential"
	req.DataClass = "confidential"
	// Modify the ingress rule to be more restrictive too.
	req.CalleeIngressAllow = []CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "sha256:caller-digest",
			AllowedBindings:     []string{"report.verify"},
			MaxDataClass:        "internal",
		},
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	// Both sides deny data class escalation; caller code preferred.
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s (caller denied first), got %s", DenyCallerBinding, decision.DenialCode)
	}
	if decision.CallerDecision.Allowed {
		t.Error("caller should deny data class escalation")
	}
	if decision.CalleeDecision.Allowed {
		t.Error("callee should also deny data class escalation")
	}
}

// ---------------------------------------------------------------------------
// 9. Unpromoted callee via lookup
// ---------------------------------------------------------------------------

func TestAuthz_UnpromotedCallee(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.PromotedLookup = func(pkgName, version, digest string) (bool, error) {
		return false, nil
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyUnpromoted {
		t.Errorf("expected %s, got %s", DenyUnpromoted, decision.DenialCode)
	}
}

func TestAuthz_PromotedCalleeSucceeds(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.PromotedLookup = func(pkgName, version, digest string) (bool, error) {
		return true, nil
	}

	decision := AuthorizeDelegation(&req)
	if !decision.Allowed {
		t.Fatalf("expected Allowed=true, got denial=%s", decision.DenialCode)
	}
}

func TestAuthz_PromotedLookupErrorIsDenied(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.PromotedLookup = func(pkgName, version, digest string) (bool, error) {
		return false, fmt.Errorf("db down")
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false on promotion lookup error")
	}
	if decision.DenialCode != DenyUnpromoted {
		t.Errorf("expected %s, got %s", DenyUnpromoted, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 10. Empty ingress allow list denies
// ---------------------------------------------------------------------------

func TestAuthz_EmptyIngressAllowList(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.CalleeIngressAllow = []CalleeIngressRule{}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false")
	}
	if decision.DenialCode != DenyCalleePolicy {
		t.Errorf("expected %s, got %s", DenyCalleePolicy, decision.DenialCode)
	}
	if !decision.CallerDecision.Allowed {
		t.Error("caller should be allowed")
	}
	if decision.CalleeDecision.Allowed {
		t.Error("callee should be denied")
	}
}

// ---------------------------------------------------------------------------
// 11. Operation mismatch
// ---------------------------------------------------------------------------

func TestAuthz_OperationMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.BindingID = "data.analyze"
	req.Operation = "wrong_op"
	req.CalleePackageName = "data-analyzer"
	req.CalleePackageVersion = "2.0.0"
	req.CalleeBundleDigest = "sha256:analyzer-digest"
	req.DataClass = "confidential"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false for operation mismatch")
	}
	if decision.DenialCode != DenyCallerBinding {
		t.Errorf("expected %s, got %s", DenyCallerBinding, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 12. Nil snapshot
// ---------------------------------------------------------------------------

func TestAuthz_NilSnapshot(t *testing.T) {
	req := AuthorizeRequest{
		Snapshot:           nil,
		BindingID:          "report.verify",
		CallerDeploymentID: "dep-caller-1",
		CallerPackageDigest:"sha256:caller-digest",
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false for nil snapshot")
	}
	if decision.DenialCode != DenySnapshotMismatch {
		t.Errorf("expected %s, got %s", DenySnapshotMismatch, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 13. Empty bindings
// ---------------------------------------------------------------------------

func TestAuthz_EmptyBindings(t *testing.T) {
	snap := &CommunicationSnapshot{
		SchemaVersion:       CurrentSchemaVersion,
		SnapshotGeneration:  1,
		WorkflowID:           "wf-test",
		TenantID:             "tenant-test",
		CallerDeploymentID:   "dep-caller-1",
		CallerPackageName:    "weather-agent",
		CallerPackageDigest:  "sha256:caller-digest",
		Bindings:             []WorkflowDelegationBinding{},
	}
	dg, _ := ComputeSnapshotDigest(snap)
	snap.SnapshotDigest = dg

	req := makeAuthzRequest(snap)
	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false for empty bindings")
	}
	if decision.DenialCode != DenySnapshotMismatch {
		t.Errorf("expected %s, got %s", DenySnapshotMismatch, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 14. Snapshot digest stability
// ---------------------------------------------------------------------------

func TestSnapshotDigest_Deterministic(t *testing.T) {
	snap1 := makeTestSnapshot()
	dg1, err := ComputeSnapshotDigest(snap1)
	if err != nil {
		t.Fatalf("ComputeSnapshotDigest: %v", err)
	}

	// Same logical snapshot, different pointer.
	snap2 := makeTestSnapshot()
	dg2, err := ComputeSnapshotDigest(snap2)
	if err != nil {
		t.Fatalf("ComputeSnapshotDigest 2: %v", err)
	}

	if dg1 != dg2 {
		t.Errorf("expected deterministic digest: %s != %s", dg1, dg2)
	}

	// Different caller package digest should produce different digest.
	snap3 := makeTestSnapshot()
	snap3.CallerPackageDigest = "sha256:different"
	dg3, err := ComputeSnapshotDigest(snap3)
	if err != nil {
		t.Fatalf("ComputeSnapshotDigest 3: %v", err)
	}
	if dg1 == dg3 {
		t.Error("expected different digest for different snapshot")
	}
}

// ---------------------------------------------------------------------------
// 15. AuthzAuditRecord
// ---------------------------------------------------------------------------

func TestAuthzAuditRecord(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	decision := AuthorizeDelegation(&req)

	record := NewAuthzAuditRecord(
		"task-123",
		snap.WorkflowID,
		snap.SnapshotGeneration,
		snap.SnapshotDigest,
		req.BindingID,
		decision,
	)

	if record.TaskID != "task-123" {
		t.Errorf("expected task-123, got %s", record.TaskID)
	}
	if record.DenialCode != "" {
		t.Errorf("expected empty denial code for allowed decision, got %s", record.DenialCode)
	}
	if record.SnapshotDigest != snap.SnapshotDigest {
		t.Errorf("digest mismatch")
	}
	if record.DecidedAt.IsZero() {
		t.Error("DecidedAt should not be zero")
	}
}

// ---------------------------------------------------------------------------
// 16. CalleeIngress digest match: empty digest = any digest
// ---------------------------------------------------------------------------

func TestAuthz_CalleeIngressEmptyDigestAnyMatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Set callee ingress with empty CallerPackageDigest (any digest of that name).
	req.CalleeIngressAllow = []CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "", // any digest
			AllowedBindings:     []string{"report.verify"},
			MaxDataClass:        "internal",
		},
	}

	decision := AuthorizeDelegation(&req)
	if !decision.Allowed {
		t.Fatalf("expected Allowed=true with any-digest rule; denial=%s", decision.DenialCode)
	}
}

func TestAuthz_CalleeIngressExactDigestMismatch(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	// Exact digest rule with wrong digest.
	req.CalleeIngressAllow = []CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "sha256:other-caller-digest", // exact but wrong
			AllowedBindings:     []string{"report.verify"},
			MaxDataClass:        "internal",
		},
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false when exact digest mismatch in ingress")
	}
	if decision.DenialCode != DenyCalleePolicy {
		t.Errorf("expected %s, got %s", DenyCalleePolicy, decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 17. Snapshot digest is always stable for the same logical content
// ---------------------------------------------------------------------------

func TestSnapshotDigestJSONRoundtrip(t *testing.T) {
	snap := makeTestSnapshot()

	// Marshal/unmarshal roundtrip -> same digest.
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var snap2 CommunicationSnapshot
	if err := json.Unmarshal(b, &snap2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Recompute on the round-tripped snapshot.
	dg2, err := ComputeSnapshotDigest(&snap2)
	if err != nil {
		t.Fatalf("ComputeSnapshotDigest on roundtrip: %v", err)
	}

	if snap.SnapshotDigest != dg2 {
		t.Errorf("round-trip digest mismatch: %s != %s", snap.SnapshotDigest, dg2)
	}
}

// ---------------------------------------------------------------------------
// 18. Both sides always filled even when denied early
// ---------------------------------------------------------------------------

func TestAuthz_BothSidesEvaluated(t *testing.T) {
	// Scenario: caller fails (unknown binding), but callee side is also evaluated.
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.BindingID = "nonexistent.binding"

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected false")
	}
	if !decision.CallerDecision.Evaluated {
		t.Error("caller must be evaluated")
	}
	if !decision.CalleeDecision.Evaluated {
		t.Error("callee must be evaluated even when caller fails")
	}
}

// ---------------------------------------------------------------------------
// 19. Caller package name used as default when binding.CallerPackageName empty
// ---------------------------------------------------------------------------

func TestAuthz_DefaultCallerPackageName(t *testing.T) {
	snap := makeTestSnapshot()
	// Use the second binding which has empty CallerPackageName.
	req := makeAuthzRequest(snap)
	req.BindingID = "data.analyze"
	req.Operation = "analyze" // must match binding.Operation
	req.CalleePackageName = "data-analyzer"
	req.CalleePackageVersion = "2.0.0"
	req.CalleeBundleDigest = "sha256:analyzer-digest"
	req.DataClass = "confidential"

	decision := AuthorizeDelegation(&req)
	if !decision.Allowed {
		t.Fatalf("expected Allowed=true when CallerPackageName empty (defaults to snapshot caller); denial=%s", decision.DenialCode)
	}
}

// ---------------------------------------------------------------------------
// 20. Callee ingress wildcard binding IDs not allowed (empty = none)
// ---------------------------------------------------------------------------

func TestAuthz_CalleeIngressEmptyAllowedBindings(t *testing.T) {
	snap := makeTestSnapshot()
	req := makeAuthzRequest(snap)
	req.CalleeIngressAllow = []CalleeIngressRule{
		{
			CallerPackageName:   "weather-agent",
			CallerPackageDigest: "sha256:caller-digest",
			AllowedBindings:     []string{}, // empty = none allowed
			MaxDataClass:        "internal",
		},
	}

	decision := AuthorizeDelegation(&req)
	if decision.Allowed {
		t.Fatal("expected Allowed=false when AllowedBindings is empty")
	}
	if decision.DenialCode != DenyCalleePolicy {
		t.Errorf("expected %s, got %s", DenyCalleePolicy, decision.DenialCode)
	}
}
