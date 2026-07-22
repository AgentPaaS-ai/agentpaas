package delegation

import (
	"time"
)

// ---------------------------------------------------------------------------
// Authorization types
// ---------------------------------------------------------------------------

// AuthorizeRequest carries all information needed for two-sided authorization.
type AuthorizeRequest struct {
	Snapshot *CommunicationSnapshot

	BindingID string
	Operation string

	// Caller's live identity (must match snapshot caller pin).
	CallerDeploymentID  string
	CallerPackageDigest string

	// Requested callee (must match binding pin).
	CalleePackageName    string
	CalleePackageVersion string
	CalleeBundleDigest   string

	DataClass string

	// CalleeIngressAllow is the callee's ingress policy from its package/deployment.
	CalleeIngressAllow []CalleeIngressRule

	// PromotedLookup is an optional hook: if set and returns false, DENY_UNPROMOTED.
	PromotedLookup func(packageName, version, digest string) (bool, error)

	// ExpectedSnapshotGeneration is the snapshot generation the caller
	// asserts. When non-zero, AuthorizeDelegation rejects with
	// DENY_SNAPSHOT_MISMATCH if it doesn't match the snapshot. Zero
	// value means "not enforced" (backward compat for tests that don't
	// set it).
	ExpectedSnapshotGeneration int64

	Now time.Time
}

// CalleeIngressRule defines who may call a callee.
type CalleeIngressRule struct {
	CallerPackageName   string   `json:"caller_package_name"`
	CallerPackageDigest string   `json:"caller_package_digest,omitempty"`
	AllowedBindings     []string `json:"allowed_bindings"`
	MaxDataClass        string   `json:"max_data_class"`
}

// AuthzDecision is the result of a two-sided authorization evaluation.
type AuthzDecision struct {
	Allowed      bool                 `json:"allowed"`
	CallerDecision SideDecision       `json:"caller_decision"`
	CalleeDecision SideDecision       `json:"callee_decision"`
	DenialCode   string               `json:"denial_code,omitempty"`
	Binding      *WorkflowDelegationBinding `json:"binding,omitempty"`
}

// SideDecision records one side's authorization outcome.
type SideDecision struct {
	Evaluated    bool   `json:"evaluated"`
	Allowed      bool   `json:"allowed"`
	ReasonCode   string `json:"reason_code,omitempty"`
	ReasonDetail string `json:"reason_detail,omitempty"`
}

// ---------------------------------------------------------------------------
// Ranking helpers (reuses pack handoff rank ordering)
// ---------------------------------------------------------------------------

// classificationRank returns the rank order of a data classification string.
// Lower = less restrictive. Matches pack.handoffClassificationRank ordering.
// public=0, internal=1, confidential=2, restricted=3.
func classificationRank(c string) int {
	switch c {
	case "public":
		return 0
	case "internal":
		return 1
	case "confidential":
		return 2
	case "restricted":
		return 3
	default:
		return -1
	}
}

// ---------------------------------------------------------------------------
// AuthorizeDelegation
// ---------------------------------------------------------------------------

// AuthorizeDelegation performs two-sided authorization:
// 1. Caller side: snapshot must name the binding + match caller/callee pins.
// 2. Callee side: ingress policy must allow the caller + binding.
// Both sides are always evaluated when possible. DenialCode prefers caller
// code if caller failed, else callee code.
func AuthorizeDelegation(req *AuthorizeRequest) AuthzDecision {
	decision := AuthzDecision{
		CallerDecision: SideDecision{Evaluated: false, Allowed: false},
		CalleeDecision: SideDecision{Evaluated: false, Allowed: false},
	}

	// ---- Snapshot pre-checks ----
	if req.Snapshot == nil {
		decision.DenialCode = DenySnapshotMismatch
		return decision
	}
	if len(req.Snapshot.Bindings) == 0 {
		decision.DenialCode = DenySnapshotMismatch
		return decision
	}

	// Verify caller identity matches snapshot pin.
	if req.CallerDeploymentID != req.Snapshot.CallerDeploymentID {
		decision.DenialCode = DenySnapshotMismatch
		return decision
	}
	if req.CallerPackageDigest != req.Snapshot.CallerPackageDigest {
		decision.DenialCode = DenySnapshotMismatch
		return decision
	}

	// Verify snapshot generation matches when caller asserts one.
	if req.ExpectedSnapshotGeneration != 0 &&
		req.ExpectedSnapshotGeneration != req.Snapshot.SnapshotGeneration {
		decision.DenialCode = DenySnapshotMismatch
		return decision
	}

	// ---- Caller side ----
	binding, _ := evaluateCallerSide(req)

	decision.CallerDecision.Evaluated = true
	if binding != nil {
		decision.CallerDecision.Allowed = true
		decision.Binding = binding
	} else {
		decision.CallerDecision.Allowed = false
		decision.CallerDecision.ReasonCode = DenyCallerBinding
	}

	// ---- Callee side ----
	if passed, reason := evaluateCalleeSide(req, binding); passed {
		decision.CalleeDecision.Evaluated = true
		decision.CalleeDecision.Allowed = true
	} else {
		decision.CalleeDecision.Evaluated = true
		decision.CalleeDecision.Allowed = false
		decision.CalleeDecision.ReasonCode = DenyCalleePolicy
		decision.CalleeDecision.ReasonDetail = reason
	}

	// ---- Optional promotion check ----
	if req.PromotedLookup != nil && decision.CallerDecision.Allowed && decision.CalleeDecision.Allowed {
		promoted, err := req.PromotedLookup(
			req.CalleePackageName,
			req.CalleePackageVersion,
			req.CalleeBundleDigest,
		)
		if err != nil || !promoted {
			decision.Allowed = false
			decision.DenialCode = DenyUnpromoted
			if err != nil {
				decision.CalleeDecision.ReasonDetail = "promotion lookup error: " + err.Error()
			}
			return decision
		}
	}

	// ---- Final decision ----
	if decision.CallerDecision.Allowed && decision.CalleeDecision.Allowed {
		decision.Allowed = true
		decision.DenialCode = ""
	} else {
		decision.Allowed = false
		// Prefer caller denial code if caller failed.
		if !decision.CallerDecision.Allowed {
			decision.DenialCode = DenyCallerBinding
		} else {
			decision.DenialCode = DenyCalleePolicy
		}
	}

	return decision
}

// evaluateCallerSide checks:
// - Binding exists in snapshot.
// - Operation matches (when binding.Operation non-empty).
// - Callee package name/version/digest match binding pins.
// - Data class <= binding.MaxDataClass.
// Returns (binding copy, denied reason empty=nil).
func evaluateCallerSide(req *AuthorizeRequest) (*WorkflowDelegationBinding, string) {
	snap := req.Snapshot
	binding, _, found := snap.findBinding(req.BindingID)
	if !found {
		return nil, "binding_id not found in snapshot: " + req.BindingID
	}

	// Operation check.
	if binding.Operation != "" && binding.Operation != req.Operation {
		return nil, "operation mismatch: expected " + binding.Operation + ", got " + req.Operation
	}

	// Callee pin check: exact name/version/digest.
	if binding.CalleePackageName != req.CalleePackageName {
		return nil, "callee package name mismatch: expected " + binding.CalleePackageName + ", got " + req.CalleePackageName
	}
	if binding.CalleePackageVersion != req.CalleePackageVersion {
		return nil, "callee package version mismatch: expected " + binding.CalleePackageVersion + ", got " + req.CalleePackageVersion
	}
	if binding.CalleeBundleDigest != req.CalleeBundleDigest {
		return nil, "callee bundle digest mismatch: expected " + binding.CalleeBundleDigest + ", got " + req.CalleeBundleDigest
	}

	// Data class check: request data class must be <= binding max.
	reqRank := classificationRank(req.DataClass)
	bindingRank := classificationRank(binding.MaxDataClass)
	if reqRank < 0 {
		return nil, "invalid data class: " + req.DataClass
	}
	if reqRank > bindingRank {
		return nil, "data class escalation: " + req.DataClass + " > " + binding.MaxDataClass
	}

	// Make a copy to return.
	cp := *binding
	return &cp, ""
}

// evaluateCalleeSide checks if any ingress rule matches the caller + binding.
// Returns (allowed, reason-if-denied).
func evaluateCalleeSide(req *AuthorizeRequest, callerBinding *WorkflowDelegationBinding) (bool, string) {
	if len(req.CalleeIngressAllow) == 0 {
		return false, "empty ingress allow list"
	}

	// Effective caller package name: use binding.CallerPackageName if set,
	// otherwise fall back to snapshot caller package name.
	effectiveCallerPackageName := req.Snapshot.CallerPackageName
	if callerBinding != nil && callerBinding.CallerPackageName != "" {
		effectiveCallerPackageName = callerBinding.CallerPackageName
	}

	for _, rule := range req.CalleeIngressAllow {
		// Check caller package name.
		if rule.CallerPackageName != effectiveCallerPackageName {
			continue
		}

		// Check caller package digest: if rule specifies exact digest, require match.
		if rule.CallerPackageDigest != "" && rule.CallerPackageDigest != req.CallerPackageDigest {
			continue
		}

		// Check binding ID is in allowed_bindings.
		bindingAllowed := false
		for _, allowed := range rule.AllowedBindings {
			if allowed == req.BindingID {
				bindingAllowed = true
				break
			}
		}
		if !bindingAllowed {
			continue
		}

		// Check data class.
		reqRank := classificationRank(req.DataClass)
		ruleRank := classificationRank(rule.MaxDataClass)
		if reqRank > ruleRank {
			continue
		}

		// All checks passed.
		return true, ""
	}

	return false, "no matching ingress rule for caller=" + effectiveCallerPackageName + " binding=" + req.BindingID
}

// ---------------------------------------------------------------------------
// AuthorizeDelegationWithPromotion
// ---------------------------------------------------------------------------

// AuthorizeDelegationWithPromotion is a convenience wrapper that binds a
// PromotedLookup to AuthorizeRequest and calls AuthorizeDelegation.
func AuthorizeDelegationWithPromotion(
	req *AuthorizeRequest,
	lookup func(packageName, version, digest string) (bool, error),
) AuthzDecision {
	req.PromotedLookup = lookup
	return AuthorizeDelegation(req)
}

// ---------------------------------------------------------------------------
// Audit record helper
// ---------------------------------------------------------------------------

// AuthzAuditRecord is a pure-data record of an authorization decision.
// Suitable for writing to the audit log.
type AuthzAuditRecord struct {
	TaskID             string       `json:"task_id"`
	WorkflowID         string       `json:"workflow_id"`
	SnapshotGeneration int64        `json:"snapshot_generation"`
	SnapshotDigest     string       `json:"snapshot_digest"`
	BindingID          string       `json:"binding_id"`
	CallerDecision     SideDecision `json:"caller_decision"`
	CalleeDecision     SideDecision `json:"callee_decision"`
	DenialCode         string       `json:"denial_code"`
	DecidedAt          time.Time    `json:"decided_at"`
}

// NewAuthzAuditRecord creates an audit record from an authorization decision.
func NewAuthzAuditRecord(
	taskID, workflowID string,
	snapshotGeneration int64,
	snapshotDigest string,
	bindingID string,
	decision AuthzDecision,
) AuthzAuditRecord {
	return AuthzAuditRecord{
		TaskID:             taskID,
		WorkflowID:         workflowID,
		SnapshotGeneration: snapshotGeneration,
		SnapshotDigest:     snapshotDigest,
		BindingID:          bindingID,
		CallerDecision:     decision.CallerDecision,
		CalleeDecision:     decision.CalleeDecision,
		DenialCode:         decision.DenialCode,
		DecidedAt:          time.Now().UTC(),
	}
}
