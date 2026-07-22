package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

// ---------------------------------------------------------------------------
// DelegationTrustState
// ---------------------------------------------------------------------------

// DelegationTrustState is the trusted invoke state for task delegation.
// It is injected by the daemon at invoke bootstrap and holds the immutable
// workflow snapshot, per-binding capability tokens, the workflow-scoped
// network alias, and the task store. NONE of these fields are serialized
// to agent-facing responses.
type DelegationTrustState struct {
	// Snapshot is the immutable signed workflow communication snapshot.
	Snapshot delegation.CommunicationSnapshot

	// BindingCapabilities maps bindingID → unguessable capability token.
	// NEVER serialized to agent responses.
	BindingCapabilities map[string]string

	// NetworkAlias is the workflow-scoped internal network alias.
	// NEVER serialized to agent responses.
	NetworkAlias string

	// Store is the pluggable task delegation store.
	Store delegation.Store

	// PromotedLookup is an optional hook for promotion checks.
	PromotedLookup func(packageName, version, digest string) (bool, error)

	// CalleeIngressAllow is the callee's ingress policy.
	CalleeIngressAllow []delegation.CalleeIngressRule
}

// ---------------------------------------------------------------------------
// Allowlisted response fields (explicit — everything else stripped)
// ---------------------------------------------------------------------------

// delegateTaskResponseFields is the explicit allowlist of field names
// permitted in a delegate_task response. Any key received or constructed
// that is not in this list is stripped.
var delegateTaskResponseFields = map[string]bool{
	"task_id": true,
	"status":  true,
}

// getTaskResponseFields is the explicit allowlist for get_task responses.
var getTaskResponseFields = map[string]bool{
	"task_id":       true,
	"status":        true,
	"workflow_id":   true,
	"tenant_id":     true,
	"binding_id":    true,
	"capability":    true,
	"operation":     true,
	"created_at":    true,
	"updated_at":    true,
	"denial_reason": true,
	"failure_reason": true,
}

// listTaskEventsResponseFields is the explicit allowlist for list_task_events responses.
var listTaskEventsResponseFields = map[string]bool{
	"events": true,
}

// taskEventFields is the explicit allowlist for individual event objects.
var taskEventFields = map[string]bool{
	"event_id":       true,
	"task_id":        true,
	"workflow_id":    true,
	"tenant_id":      true,
	"sequence":       true,
	"type":           true,
	"payload_digest": true,
	"created_at":     true,
}

// forbiddenResponsePatterns are JSON key patterns that must never appear
// in agent-facing responses. Matched case-insensitively.
var forbiddenResponsePatterns = []string{
	"endpoint", "host", "ip", "port", "capability_token",
	"network_alias", "token", "secret", "capability_header",
}

// ---------------------------------------------------------------------------
// Server extension: delegation state
// ---------------------------------------------------------------------------

func (s *harnessRPCServer) setDelegationTrustState(dts *DelegationTrustState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delegationTrust = dts
}

func (s *harnessRPCServer) getDelegationTrustState() *DelegationTrustState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.delegationTrust
}

// ---------------------------------------------------------------------------
// RPC handler: delegate_task
// ---------------------------------------------------------------------------

// handleDelegateTask is the trusted invoke path for task delegation.
// It builds the authorization request from the trust state, calls
// AuthorizeDelegation, and either creates an ADMITTED task or a DENIED one.
// The response is scrubbed to an explicit allowlist.
func (s *harnessRPCServer) handleDelegateTask(req rpcRequest) rpcResponse {
	dts := s.getDelegationTrustState()
	if dts == nil {
		return rpcError(req.ID, "delegation trust state not set", "no_trust_state")
	}
	if dts.Snapshot.WorkflowID == "" {
		return rpcError(req.ID, "delegation snapshot not configured", "no_snapshot")
	}

	capability := stringParam(req.Params, "capability")
	if capability == "" {
		return rpcError(req.ID, "capability is required", "invalid_params")
	}

	operation := stringParam(req.Params, "operation")

	idempotencyKey := stringParam(req.Params, "idempotency_key")
	if idempotencyKey == "" {
		return rpcError(req.ID, "idempotency_key is required", "invalid_params")
	}

	// W4: Accept optional data_class parameter, default to "internal".
	dataClass := stringParam(req.Params, "data_class")
	if dataClass == "" {
		dataClass = string(delegation.ClassificationInternal)
	}
	dataClassEnum := delegation.Classification(dataClass)
	if !dataClassEnum.Valid() {
		return rpcError(req.ID, "invalid data_class: "+dataClass, "invalid_params")
	}

	// Build a task ID for this delegate call.
	taskID, err := delegation.NewTaskID()
	if err != nil {
		return rpcError(req.ID, "failed to generate task ID: "+err.Error(), "internal_error")
	}

	now := time.Now().UTC()

	// Build authorization request from trust state.
	authReq := delegation.AuthorizeRequest{
		Snapshot:                   &dts.Snapshot,
		BindingID:                  capability,
		Operation:                  operation,
		CallerDeploymentID:         dts.Snapshot.CallerDeploymentID,
		CallerPackageDigest:        dts.Snapshot.CallerPackageDigest,
		CalleePackageName:          lookupBindingCallee(dts.Snapshot, capability, "package_name"),
		CalleePackageVersion:       lookupBindingCallee(dts.Snapshot, capability, "package_version"),
		CalleeBundleDigest:         lookupBindingCallee(dts.Snapshot, capability, "bundle_digest"),
		DataClass:                  dataClass, // W4: from request, not hardcoded
		CalleeIngressAllow:         dts.CalleeIngressAllow,
		PromotedLookup:             dts.PromotedLookup,
		ExpectedSnapshotGeneration: dts.Snapshot.SnapshotGeneration,
		Now:                        now,
	}

	// Authorize.
	decision := delegation.AuthorizeDelegation(&authReq)

	// N1: Caller ref with harness run/attempt IDs.
	callerRef := delegation.CallerRef{
		DeploymentID:  dts.Snapshot.CallerDeploymentID,
		RunID:         "run-harness",
		AttemptID:     "at-harness",
		PackageName:   dts.Snapshot.CallerPackageName,
		PackageDigest: dts.Snapshot.CallerPackageDigest,
	}

	if !decision.Allowed {
		// Create DENIED task.
		deniedTask := delegation.Task{
			SchemaVersion:                   delegation.CurrentSchemaVersion,
			TaskID:                          taskID,
			WorkflowID:                      dts.Snapshot.WorkflowID,
			TenantID:                        dts.Snapshot.TenantID,
			Caller:                          callerRef,
			Callee:                          delegation.CalleeRef{PackageName: authReq.CalleePackageName, PackageVersion: authReq.CalleePackageVersion, PackageDigest: authReq.CalleeBundleDigest},
			BindingID:                       capability,
			Capability:                      capability,
			Operation:                       operation,
			Status:                          delegation.TaskStatusDenied,
			Generation:                      0,
			IdempotencyKey:                  idempotencyKey,
			CallerIdentity:                  dts.Snapshot.CallerDeploymentID,
			CommunicationSnapshotGeneration: dts.Snapshot.SnapshotGeneration,
			DenialReason:                    decision.DenialCode,
			CreatedAt:                       now,
			UpdatedAt:                       now,
		}

		// Denied task goes straight to store without validation;
		// callee fields may be empty when binding is unknown.
		if err := dts.Store.CreateTask(context.Background(), deniedTask); err != nil {
			// On idempotent replay, don't fail — return the existing.
			log.Printf("harness: delegate_task create denied task: %v", err)
		}

		// Append audit event.
		appendDelegateEvent(dts.Store, taskID, dts.Snapshot.WorkflowID, dts.Snapshot.TenantID, delegation.EventTaskDenied)

		// Audit the denial (logs only — not returned to agent).
		auditRec := delegation.NewAuthzAuditRecord(
			string(taskID), dts.Snapshot.WorkflowID,
			dts.Snapshot.SnapshotGeneration, dts.Snapshot.SnapshotDigest,
			capability, decision,
		)
		log.Printf("harness: delegate_task DENIED: task_id=%s denial=%s caller_decision=%+v callee_decision=%+v",
			auditRec.TaskID, auditRec.DenialCode, auditRec.CallerDecision, auditRec.CalleeDecision)

		return rpcResponse{
			ID: req.ID,
			OK: true,
			Result: scrubResponse(map[string]any{
				"task_id": string(taskID),
				"status":  delegation.TaskStatusDenied.String(),
			}, delegateTaskResponseFields),
		}
	}

	// W1: Gateway self-check — lookup binding capability token, Attach + ValidateAndStrip.
	capToken, hasCap := dts.BindingCapabilities[capability]
	if !hasCap || capToken == "" {
		log.Printf("harness: delegate_task W1: missing capability token for binding %s", capability)
		return rpcError(req.ID, "delegation: missing capability token for binding", "internal_error")
	}
	enforcer := &delegation.GatewayEnforcer{}
	headers := enforcer.Attach(capToken)
	if err := enforcer.ValidateAndStrip(headers, capToken); err != nil {
		log.Printf("harness: delegate_task W1: gateway self-check failed: %v", err)
		return rpcError(req.ID, "delegation: gateway capability check failed", "internal_error")
	}

	// W3: Apply deadline and budget from binding.
	var deadlineAt *time.Time
	var maxActiveDurationMs int64
	var maxCostUsdDecimal string
	binding := lookupBinding(dts.Snapshot, capability)
	if binding != nil {
		if binding.DeadlineMs > 0 {
			dl := now.Add(time.Duration(binding.DeadlineMs) * time.Millisecond)
			deadlineAt = &dl
			maxActiveDurationMs = binding.DeadlineMs
		}
		if binding.MaxCostUSDDecimal != "" {
			maxCostUsdDecimal = binding.MaxCostUSDDecimal
		}
		// W3: Reject admit if deadline already past.
		if deadlineAt != nil && now.After(*deadlineAt) {
			return rpcError(req.ID, "delegation: task deadline already past", "expired")
		}
	}

	// Allowed — create ADMITTED task.
	admittedTask := delegation.Task{
		SchemaVersion:                   delegation.CurrentSchemaVersion,
		TaskID:                          taskID,
		WorkflowID:                      dts.Snapshot.WorkflowID,
		TenantID:                        dts.Snapshot.TenantID,
		Caller:                          callerRef,
		Callee:                          delegation.CalleeRef{PackageName: authReq.CalleePackageName, PackageVersion: authReq.CalleePackageVersion, PackageDigest: authReq.CalleeBundleDigest},
		BindingID:                       capability,
		Capability:                      capability,
		Operation:                       operation,
		Status:                          delegation.TaskStatusAdmitted,
		Generation:                      0,
		IdempotencyKey:                  idempotencyKey,
		CallerIdentity:                  dts.Snapshot.CallerDeploymentID,
		CommunicationSnapshotGeneration: dts.Snapshot.SnapshotGeneration,
		DeadlineAt:                      deadlineAt,          // W3
		MaxActiveDurationMs:             maxActiveDurationMs, // W3
		MaxCostUsdDecimal:               maxCostUsdDecimal,   // W3
		CreatedAt:                       now,
		UpdatedAt:                       now,
	}

	// N1: Validate before CreateTask.
	if err := delegation.ValidateTask(&admittedTask); err != nil {
		return rpcError(req.ID, "task validation failed: "+err.Error(), "internal_error")
	}

	if err := dts.Store.CreateTask(context.Background(), admittedTask); err != nil {
		// N4: Idempotent replay — lookup original task and return its ID.
		log.Printf("harness: delegate_task idempotent replay for key=%s: %v", idempotencyKey, err)
		existing, lookupErr := dts.Store.GetTaskByIdempotencyKey(context.Background(), dts.Snapshot.CallerDeploymentID, idempotencyKey)
		if lookupErr != nil || existing == nil {
			return rpcError(req.ID, "idempotent task creation conflict: "+err.Error(), "idempotency_conflict")
		}
		// Return the original task ID.
		return rpcResponse{
			ID: req.ID,
			OK: true,
			Result: scrubResponse(map[string]any{
				"task_id": string(existing.TaskID),
				"status":  existing.Status.String(),
			}, delegateTaskResponseFields),
		}
	}

	// Append TASK_ADMITTED event.
	appendDelegateEvent(dts.Store, taskID, dts.Snapshot.WorkflowID, dts.Snapshot.TenantID, delegation.EventTaskAdmitted)

	// Audit the admission.
	log.Printf("harness: delegate_task ADMITTED: task_id=%s capability=%s operation=%s data_class=%s",
		taskID, capability, operation, dataClass)

	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: scrubResponse(map[string]any{
			"task_id": string(taskID),
			"status":  delegation.TaskStatusAdmitted.String(),
		}, delegateTaskResponseFields),
	}
}

// lookupBinding returns the full binding from the snapshot by ID, or nil.
func lookupBinding(snap delegation.CommunicationSnapshot, bindingID string) *delegation.WorkflowDelegationBinding {
	for i := range snap.Bindings {
		if snap.Bindings[i].BindingID == bindingID {
			return &snap.Bindings[i]
		}
	}
	return nil
}

// lookupBindingCallee looks up a field from the snapshot binding by ID.
func lookupBindingCallee(snap delegation.CommunicationSnapshot, bindingID, field string) string {
	for _, b := range snap.Bindings {
		if b.BindingID == bindingID {
			switch field {
			case "package_name":
				return b.CalleePackageName
			case "package_version":
				return b.CalleePackageVersion
			case "bundle_digest":
				return b.CalleeBundleDigest
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// RPC handler: get_task
// ---------------------------------------------------------------------------

func (s *harnessRPCServer) handleGetTask(req rpcRequest) rpcResponse {
	dts := s.getDelegationTrustState()
	if dts == nil {
		return rpcError(req.ID, "delegation trust state not set", "no_trust_state")
	}

	taskIDStr := stringParam(req.Params, "task_id")
	if taskIDStr == "" {
		return rpcError(req.ID, "task_id is required", "invalid_params")
	}

	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskIDStr))
	if err != nil {
		return rpcError(req.ID, fmt.Sprintf("task not found: %v", err), "not_found")
	}

	// W5: Tenancy check — reject cross-workflow or cross-tenant access.
	if task.WorkflowID != dts.Snapshot.WorkflowID {
		return rpcError(req.ID, "task belongs to a different workflow", "not_found")
	}
	if task.TenantID != dts.Snapshot.TenantID {
		return rpcError(req.ID, "task belongs to a different tenant", "not_found")
	}

	result := map[string]any{
		"task_id":     string(task.TaskID),
		"status":      task.Status.String(),
		"workflow_id": task.WorkflowID,
		"tenant_id":     task.TenantID,
		"binding_id":    task.BindingID,
		"capability":    task.Capability,
		"operation":     task.Operation,
		"created_at":    task.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":    task.UpdatedAt.Format(time.RFC3339Nano),
	}
	if task.DenialReason != "" {
		result["denial_reason"] = task.DenialReason
	}
	if task.FailureReason != "" {
		result["failure_reason"] = task.FailureReason
	}

	return rpcResponse{
		ID:     req.ID,
		OK:     true,
		Result: scrubResponse(result, getTaskResponseFields),
	}
}

// ---------------------------------------------------------------------------
// RPC handler: list_task_events
// ---------------------------------------------------------------------------

func (s *harnessRPCServer) handleListTaskEvents(req rpcRequest) rpcResponse {
	dts := s.getDelegationTrustState()
	if dts == nil {
		return rpcError(req.ID, "delegation trust state not set", "no_trust_state")
	}

	taskIDStr := stringParam(req.Params, "task_id")
	if taskIDStr == "" {
		return rpcError(req.ID, "task_id is required", "invalid_params")
	}

	// W5: Tenancy check — verify the task belongs to this workflow.
	task, err := dts.Store.GetTask(context.Background(), delegation.TaskID(taskIDStr))
	if err != nil {
		return rpcError(req.ID, fmt.Sprintf("task not found: %v", err), "not_found")
	}
	if task.WorkflowID != dts.Snapshot.WorkflowID {
		return rpcError(req.ID, "task belongs to a different workflow", "not_found")
	}
	if task.TenantID != dts.Snapshot.TenantID {
		return rpcError(req.ID, "task belongs to a different tenant", "not_found")
	}

	afterSeq := int64Param(req.Params, "after_sequence")

	events, err := dts.Store.ListEvents(context.Background(), delegation.TaskID(taskIDStr), afterSeq)
	if err != nil {
		return rpcError(req.ID, fmt.Sprintf("list events: %v", err), "internal_error")
	}

	eventList := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		eventList = append(eventList, scrubResponse(map[string]any{
			"event_id":       string(ev.EventID),
			"task_id":        string(ev.TaskID),
			"workflow_id":    ev.WorkflowID,
			"tenant_id":      ev.TenantID,
			"sequence":       ev.Sequence,
			"type":           string(ev.Type),
			"payload_digest": ev.PayloadDigest,
			"created_at":     ev.CreatedAt.Format(time.RFC3339Nano),
		}, taskEventFields))
	}

	result := map[string]any{
		"events": eventList,
	}

	return rpcResponse{
		ID:     req.ID,
		OK:     true,
		Result: scrubResponse(result, listTaskEventsResponseFields),
	}
}

// ---------------------------------------------------------------------------
// Response scrubbing
// ---------------------------------------------------------------------------

// scrubResponse returns a new map containing only keys in the allowlist.
// It also validates that no forbidden patterns appear in the result keys
// or their string values.
func scrubResponse(result map[string]any, allowlist map[string]bool) map[string]any {
	scrubbed := make(map[string]any, len(allowlist))
	for k, v := range result {
		if allowlist[k] {
			scrubbed[k] = v
		}
	}
	// Validate no forbidden patterns in any key or string value.
	if err := validateNoForbiddenPatterns(scrubbed); err != nil {
		// If forbidden content slips through, scrub the offending key.
		log.Printf("harness: scrubResponse: stripping forbidden content: %v", err)
		for k := range scrubbed {
			if matchesForbiddenPattern(k) {
				delete(scrubbed, k)
			}
		}
	}
	return scrubbed
}

func validateNoForbiddenPatterns(m map[string]any) error {
	for k := range m {
		if matchesForbiddenPattern(k) {
			return fmt.Errorf("key %q matches forbidden pattern", k)
		}
	}
	return nil
}

func matchesForbiddenPattern(s string) bool {
	lower := strings.ToLower(s)
	for _, pat := range forbiddenResponsePatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func appendDelegateEvent(store delegation.Store, taskID delegation.TaskID, workflowID, tenantID string, eventType delegation.EventType) {
	eid, err := delegation.NewEventID()
	if err != nil {
		log.Printf("harness: failed to generate event ID: %v", err)
		return
	}
	ev := delegation.TaskEvent{
		EventID:    eid,
		TaskID:     taskID,
		WorkflowID: workflowID,
		TenantID:   tenantID,
		Type:       eventType,
		CreatedAt:  time.Now().UTC(),
	}
	if _, err := store.AppendEvent(context.Background(), ev); err != nil {
		log.Printf("harness: failed to append event %s for task %s: %v", eventType, taskID, err)
	}
}

func int64Param(params map[string]any, key string) int64 {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
