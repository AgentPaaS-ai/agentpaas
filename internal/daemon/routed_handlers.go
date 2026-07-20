package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Typed not-enabled helpers (B26 representational surface)
// ---------------------------------------------------------------------------

// featureNotEnabled builds a TypedControlError for a feature gated to a later block.
func featureNotEnabled(feature, block, codeName string) *controlv1.TypedControlError {
	if codeName == "" {
		codeName = "FEATURE_NOT_ENABLED"
	}
	return &controlv1.TypedControlError{
		Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED,
		CodeName: codeName,
		Message:  fmt.Sprintf("%s is not enabled until %s", feature, block),
		Details: map[string]string{
			"feature":          feature,
			"enabled_in_block": block,
			"code_name":        codeName,
		},
	}
}

// notEnabledFailedPrecondition returns a gRPC FailedPrecondition with a stable
// code_name in the message for legacy Run-path gates that use gRPC status.
func notEnabledFailedPrecondition(feature, block, codeName string) error {
	if codeName == "" {
		codeName = "FEATURE_NOT_ENABLED"
	}
	return status.Errorf(codes.FailedPrecondition,
		"%s: %s is not enabled until %s", codeName, feature, block)
}

// ---------------------------------------------------------------------------
// Deployment CRUD — state-only, fully enabled in B26
// ---------------------------------------------------------------------------

func (s *controlServer) CreateDeployment(ctx context.Context, req *controlv1.CreateDeploymentRequest) (*controlv1.CreateDeploymentResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetPackageName() == "" {
		return nil, status.Error(codes.InvalidArgument, "package_name is required")
	}
	if req.GetPackageVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "package_version is required")
	}
	if req.GetBundleDigest() == "" {
		return nil, status.Error(codes.InvalidArgument, "bundle_digest is required")
	}

	maxConcurrent := int(req.GetMaxConcurrentRuns())
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	actor := req.GetActorIdentity()
	if actor == "" {
		actor = "local"
	}

	dep := &routedrun.DeploymentRecord{
		SchemaVersion:        routedrun.CurrentSchemaVersion,
		PackageName:          req.GetPackageName(),
		PackageVersion:       req.GetPackageVersion(),
		Status:               routedrun.DeploymentActive,
		MaxConcurrentRuns:    maxConcurrent,
		BundleDigest:         req.GetBundleDigest(),
		PolicyDigest:         req.GetPolicyDigest(),
		ImageLockDigest:      req.GetImageLockDigest(),
		ProvenanceDigest:     req.GetProvenanceDigest(),
		NestedPackageDigests: copyStringMap(req.GetNestedPackageDigests()),
		CreatedBy:            actor,
	}
	now := time.Now().UTC()
	dep.ActivatedAt = &now

	if err := s.deploymentStore.CreateDeployment(ctx, dep); err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.CreateDeploymentResponse{
		Deployment: deploymentToProto(dep),
	}, nil
}

func (s *controlServer) GetDeployment(ctx context.Context, req *controlv1.GetDeploymentRequest) (*controlv1.GetDeploymentResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetDeploymentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "deployment_id is required")
	}
	dep, err := s.deploymentStore.GetDeployment(ctx, routedrun.DeploymentID(req.GetDeploymentId()))
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.GetDeploymentResponse{Deployment: deploymentToProto(dep)}, nil
}

func (s *controlServer) ListDeployments(ctx context.Context, req *controlv1.ListDeploymentsRequest) (*controlv1.ListDeploymentsResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	list, err := s.deploymentStore.ListDeployments(ctx)
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	pkgFilter := strings.TrimSpace(req.GetPackageName())
	out := make([]*controlv1.DeploymentRecord, 0, len(list))
	for _, d := range list {
		if pkgFilter != "" && d.PackageName != pkgFilter {
			continue
		}
		out = append(out, deploymentToProto(d))
	}
	return &controlv1.ListDeploymentsResponse{Deployments: out}, nil
}

func (s *controlServer) DeactivateDeployment(ctx context.Context, req *controlv1.DeactivateDeploymentRequest) (*controlv1.DeactivateDeploymentResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetDeploymentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "deployment_id is required")
	}
	id := routedrun.DeploymentID(req.GetDeploymentId())
	dep, err := s.deploymentStore.GetDeployment(ctx, id)
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	if dep.Status == routedrun.DeploymentInactive {
		return &controlv1.DeactivateDeploymentResponse{Deployment: deploymentToProto(dep)}, nil
	}
	if err := s.deploymentStore.SetDeploymentStatus(ctx, id, routedrun.DeploymentInactive, dep.Generation); err != nil {
		return nil, mapRoutedStoreError(err)
	}
	updated, err := s.deploymentStore.GetDeployment(ctx, id)
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.DeactivateDeploymentResponse{Deployment: deploymentToProto(updated)}, nil
}

func (s *controlServer) CreateDeploymentAlias(ctx context.Context, req *controlv1.CreateDeploymentAliasRequest) (*controlv1.CreateDeploymentAliasResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetAlias() == "" {
		return nil, status.Error(codes.InvalidArgument, "alias is required")
	}
	if req.GetTargetDeploymentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_deployment_id is required")
	}
	dep, err := s.deploymentStore.GetDeployment(ctx, routedrun.DeploymentID(req.GetTargetDeploymentId()))
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	// Create only when alias does not exist.
	if existing, rerr := s.deploymentStore.ResolveAlias(ctx, req.GetAlias()); rerr == nil && existing != nil {
		return nil, status.Errorf(codes.AlreadyExists, "alias %q already exists (use CasDeploymentAlias to update)", req.GetAlias())
	} else if rerr != nil && !errors.Is(rerr, routedrun.ErrNotFound) {
		return nil, mapRoutedStoreError(rerr)
	}
	actor := req.GetActorIdentity()
	if actor == "" {
		actor = "local"
	}
	alias := &routedrun.AliasRecord{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		Alias:              req.GetAlias(),
		TargetDeploymentID: dep.DeploymentID,
		TargetVersion:      dep.PackageVersion,
		Generation:         0, // create
		UpdatedBy:          actor,
	}
	if err := s.deploymentStore.CompareAndSwapAlias(ctx, alias); err != nil {
		return nil, mapRoutedStoreError(err)
	}
	got, err := s.deploymentStore.ResolveAlias(ctx, req.GetAlias())
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.CreateDeploymentAliasResponse{Alias: aliasToProto(got)}, nil
}

func (s *controlServer) GetDeploymentAlias(ctx context.Context, req *controlv1.GetDeploymentAliasRequest) (*controlv1.GetDeploymentAliasResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetAlias() == "" {
		return nil, status.Error(codes.InvalidArgument, "alias is required")
	}
	got, err := s.deploymentStore.ResolveAlias(ctx, req.GetAlias())
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.GetDeploymentAliasResponse{Alias: aliasToProto(got)}, nil
}

func (s *controlServer) ListDeploymentAliases(ctx context.Context, req *controlv1.ListDeploymentAliasesRequest) (*controlv1.ListDeploymentAliasesResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	list, err := s.deploymentStore.ListAliases(ctx)
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	out := make([]*controlv1.DeploymentAliasRecord, 0, len(list))
	for _, a := range list {
		out = append(out, aliasToProto(a))
	}
	return &controlv1.ListDeploymentAliasesResponse{Aliases: out}, nil
}

func (s *controlServer) CasDeploymentAlias(ctx context.Context, req *controlv1.CasDeploymentAliasRequest) (*controlv1.CasDeploymentAliasResponse, error) {
	if s.deploymentStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}
	if req.GetAlias() == "" {
		return nil, status.Error(codes.InvalidArgument, "alias is required")
	}
	if req.GetTargetDeploymentId() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_deployment_id is required")
	}
	dep, err := s.deploymentStore.GetDeployment(ctx, routedrun.DeploymentID(req.GetTargetDeploymentId()))
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	actor := req.GetActorIdentity()
	if actor == "" {
		actor = "local"
	}
	alias := &routedrun.AliasRecord{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		Alias:              req.GetAlias(),
		TargetDeploymentID: dep.DeploymentID,
		TargetVersion:      dep.PackageVersion,
		Generation:         req.GetExpectedGeneration(),
		UpdatedBy:          actor,
	}
	if err := s.deploymentStore.CompareAndSwapAlias(ctx, alias); err != nil {
		return nil, mapRoutedStoreError(err)
	}
	got, err := s.deploymentStore.ResolveAlias(ctx, req.GetAlias())
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.CasDeploymentAliasResponse{Alias: aliasToProto(got)}, nil
}

// ---------------------------------------------------------------------------
// Invocation / workflow / control — fail closed (no mutation)
// ---------------------------------------------------------------------------

func (s *controlServer) InvokeDeployment(ctx context.Context, req *controlv1.InvokeDeploymentRequest) (*controlv1.InvokeDeploymentResponse, error) {
	// Validate inputs without mutating state.
	if req.GetDeploymentRef() == "" {
		return nil, status.Error(codes.InvalidArgument, "deployment_ref is required")
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if strings.TrimSpace(req.GetCallerIdentity()) == "" {
		return nil, status.Error(codes.InvalidArgument, "caller_identity is required")
	}
	// The routed admission store must be initialized; fail closed otherwise.
	if s.localStore == nil {
		return nil, status.Error(codes.FailedPrecondition, "routed store not initialized")
	}

	// Snapshot the set of invocation IDs that exist for this caller+key
	// BEFORE the admission call. This lets us distinguish a fresh ACCEPTED
	// (new invocation ID) from an IDEMPOTENT_REPLAY (the store returned the
	// prior receipt because the caller+key+intent matched exactly). We do
	// NOT short-circuit on a prior record, because the store must also
	// detect a CHANGED intent (same key, different digest) and return
	// ErrIdempotencyConflict — short-circuiting would mask that.
	priorIDs := s.invocationIDsForCaller(ctx, req.GetCallerIdentity(), req.GetIdempotencyKey())

	// Build the durable InvocationRequest from the proto request. The store
	// computes the canonical intent digest and the input digest (when not
	// supplied) internally.
	admitReq := &routedrun.InvocationRequest{
		SchemaVersion:              routedrun.CurrentSchemaVersion,
		RequestedDeploymentRef:     req.GetDeploymentRef(),
		InputJSON:                  string(req.GetInputJson()),
		InputDigest:                "", // store computes from InputJSON when empty
		InitialMaxActiveDurationMs: req.GetInitialMaxActiveDurationMs(),
		InitialAttemptLeaseMs:      req.GetInitialAttemptLeaseMs(),
		InitialMaxCostUsdDecimal:   req.GetInitialMaxCostUsdDecimal(),
		CreationOptionsDigest:      "opts-default",
		IdempotencyKey:             req.GetIdempotencyKey(),
		CallerIdentity:             req.GetCallerIdentity(),
	}

	receipt, err := s.localStore.AdmitInvocation(ctx, admitReq, 0)
	if err != nil {
		// Map the typed store error to a typed control response. We never
		// return a gRPC error for admission outcomes — the caller receives
		// a structured InvokeDeploymentResponse with the typed error so it
		// can branch on the AdmissionOutcomeCode without string matching.
		return s.invokeDeploymentErrorResponse(req, err), nil
	}

	outcome := controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED
	if _, exists := priorIDs[string(receipt.InvocationID)]; exists {
		// The returned invocation pre-existed this call → exact replay.
		outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_IDEMPOTENT_REPLAY
	}

	return s.invokeDeploymentReceiptResponse(req, receipt, outcome), nil
}

// invocationIDsForCaller returns the set of invocation IDs already admitted
// for the given caller (scoped by idempotency key when the store supports
// it). Used to distinguish a fresh ACCEPTED from an IDEMPOTENT_REPLAY without
// short-circuiting the store's changed-intent conflict detection.
func (s *controlServer) invocationIDsForCaller(ctx context.Context, callerIdentity, idempotencyKey string) map[string]struct{} {
	out := make(map[string]struct{})
	if s.localStore == nil {
		return out
	}
	// The store's GetInvocationByIdempotency returns the prior receipt for
	// exact caller+key, or a not-found error. A hit means a prior admission
	// exists; record its invocation ID.
	if prior, err := s.localStore.GetInvocationByIdempotency(ctx, callerIdentity, idempotencyKey); err == nil && prior != nil {
		out[string(prior.InvocationID)] = struct{}{}
	}
	return out
}

// invokeDeploymentReceiptResponse builds a successful InvokeDeploymentResponse
// from an admitted receipt. The response carries invocation/workflow/run IDs
// only; the attempt ID is empty until the T05 supervisor claim creates it
// (spec line 274-276).
func (s *controlServer) invokeDeploymentReceiptResponse(req *controlv1.InvokeDeploymentRequest, receipt *routedrun.InvocationReceipt, outcome controlv1.AdmissionOutcomeCode) *controlv1.InvokeDeploymentResponse {
	resp := &controlv1.InvokeDeploymentResponse{
		Outcome:                outcome,
		OutcomeName:            outcome.String(),
		InvocationId:           string(receipt.InvocationID),
		WorkflowId:             string(receipt.WorkflowID),
		RunId:                  string(receipt.RunID),
		RequestedDeploymentRef: req.GetDeploymentRef(),
		ResolvedDeploymentId:   string(receipt.ResolvedDeploymentID),
		ResolvedDeploymentVersion: receipt.ResolvedDeploymentVersion,
		Ceilings: &controlv1.AbsoluteCeilingsSnapshot{
			OriginalMaxActiveDurationMs: receipt.InitialMaxActiveDurationMs,
			CurrentMaxActiveDurationMs:  receipt.InitialMaxActiveDurationMs,
			OriginalAttemptLeaseMs:      receipt.InitialAttemptLeaseMs,
			CurrentAttemptLeaseMs:       receipt.InitialAttemptLeaseMs,
			OriginalMaxLlmSpendDecimal:  receipt.InitialMaxCostUsdDecimal,
			CurrentMaxLlmSpendDecimal:   receipt.InitialMaxCostUsdDecimal,
			AuthorityGeneration:         1,
		},
	}
	if !receipt.AdmittedAt.IsZero() {
		resp.AdmittedAt = timestamppb.New(receipt.AdmittedAt)
	}
	return resp
}

// invokeDeploymentErrorResponse maps a typed admission-store error to a typed
// InvokeDeploymentResponse (never a gRPC error). The caller branches on the
// AdmissionOutcomeCode / TypedControlErrorCode.
func (s *controlServer) invokeDeploymentErrorResponse(req *controlv1.InvokeDeploymentRequest, err error) *controlv1.InvokeDeploymentResponse {
	resp := &controlv1.InvokeDeploymentResponse{
		RequestedDeploymentRef: req.GetDeploymentRef(),
	}
	switch {
	case errors.Is(err, routedrun.ErrAlreadyRunning):
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ALREADY_RUNNING
		resp.OutcomeName = resp.Outcome.String()
		// Attach the original receipt if the store can still resolve it.
		if prior, perr := s.localStore.GetInvocationByIdempotency(context.Background(), req.GetCallerIdentity(), req.GetIdempotencyKey()); perr == nil && prior != nil {
			resp.InvocationId = string(prior.InvocationID)
			resp.WorkflowId = string(prior.WorkflowID)
			resp.RunId = string(prior.RunID)
			resp.ResolvedDeploymentId = string(prior.ResolvedDeploymentID)
			resp.ResolvedDeploymentVersion = prior.ResolvedDeploymentVersion
		}
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_ALREADY_RUNNING,
			CodeName: "ALREADY_RUNNING",
			Message:  err.Error(),
		}
	case errors.Is(err, routedrun.ErrIdempotencyConflict):
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_IDEMPOTENCY_CONFLICT
		resp.OutcomeName = resp.Outcome.String()
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_CHANGED_IDEMPOTENCY_PAYLOAD,
			CodeName: "CHANGED_IDEMPOTENCY_PAYLOAD",
			Message:  err.Error(),
		}
	case errors.Is(err, routedrun.ErrDeploymentInactive):
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_DEPLOYMENT_INACTIVE
		resp.OutcomeName = resp.Outcome.String()
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_DEPLOYMENT_INACTIVE,
			CodeName: "DEPLOYMENT_INACTIVE",
			Message:  err.Error(),
		}
	case errors.Is(err, routedrun.ErrNotFound):
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_UNSPECIFIED
		resp.OutcomeName = "DEPLOYMENT_NOT_FOUND"
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_DEPLOYMENT_INACTIVE,
			CodeName: "DEPLOYMENT_NOT_FOUND",
			Message:  err.Error(),
		}
	case errors.Is(err, routedrun.ErrInvalidArgument), errors.Is(err, routedrun.ErrSizeCapExceeded):
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_UNSPECIFIED
		resp.OutcomeName = "INVALID_ARGUMENT"
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_UNSPECIFIED,
			CodeName: "INVALID_ARGUMENT",
			Message:  err.Error(),
		}
	default:
		resp.Outcome = controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_UNSPECIFIED
		resp.OutcomeName = "INTERNAL"
		resp.Error = &controlv1.TypedControlError{
			Code:     controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_UNSPECIFIED,
			CodeName: "INTERNAL",
			Message:  err.Error(),
		}
	}
	return resp
}

// ---------------------------------------------------------------------------
// B30-T02 Part A: invocation / run status / run result get APIs
//
// These are thin pass-throughs to the durable RunStore / InvocationStore.
// For Part A:
//   - GetInvocation reads the invocation record (admission receipt) by ID.
//   - GetRunStatus reads the run record.
//   - GetRunResult returns empty/not-found until T05/T08 write results.
// The real implementations (supervisor claim, result store) come with T05.
// ---------------------------------------------------------------------------

// GetInvocation returns the durable admission record (receipt) for an
// invocation by ID. Thin pass-through to the admission store's
// ListInvocations (no direct by-ID lookup exists on the interface yet).
func (s *controlServer) GetInvocation(ctx context.Context, req *controlv1.GetInvocationRequest) (*controlv1.GetInvocationResponse, error) {
	if req.GetInvocationId() == "" {
		return nil, status.Error(codes.InvalidArgument, "invocation_id is required")
	}
	if s.localStore == nil {
		return &controlv1.GetInvocationResponse{
			Error: featureNotEnabled("invocation_lookup", "B30", "routed_store_not_initialized"),
		}, nil
	}
	invs, err := s.localStore.ListInvocations(ctx)
	if err != nil {
		return nil, mapRoutedStoreError(err)
	}
	for _, r := range invs {
		if string(r.InvocationID) == req.GetInvocationId() {
			return &controlv1.GetInvocationResponse{
				Invocation: invocationReceiptToProto(r),
			}, nil
		}
	}
	return nil, status.Errorf(codes.NotFound, "invocation %q not found", req.GetInvocationId())
}

// GetRunStatus returns the durable run record status. Thin pass-through to
// RunStore.GetRun.
func (s *controlServer) GetRunStatus(ctx context.Context, req *controlv1.GetRunStatusRequest) (*controlv1.GetRunStatusResponse, error) {
	if req.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	if s.runStore == nil {
		return &controlv1.GetRunStatusResponse{
			Error: featureNotEnabled("run_status", "B30", "routed_store_not_initialized"),
		}, nil
	}
	run, err := s.runStore.GetRun(ctx, routedrun.RunID(req.GetRunId()))
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "run %q not found", req.GetRunId())
		}
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.GetRunStatusResponse{
		RunId:       string(run.RunID),
		WorkflowId:  string(run.WorkflowID),
		Status:      run.Status.String(),
		RunKind:     run.RunKind,
		Generation:  1,
	}, nil
}

// GetRunResult returns the terminal result for a run. For T02 Part A this
// returns not-found until T05/T08 write results to the protected result
// store. The pass-through reads the run; if the run is terminal, an empty
// result envelope is returned (result content lands with T05).
func (s *controlServer) GetRunResult(ctx context.Context, req *controlv1.GetRunResultRequest) (*controlv1.GetRunResultResponse, error) {
	if req.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	if s.runStore == nil {
		return &controlv1.GetRunResultResponse{
			Error: featureNotEnabled("run_result", "B30", "routed_store_not_initialized"),
		}, nil
	}
	run, err := s.runStore.GetRun(ctx, routedrun.RunID(req.GetRunId()))
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "run %q not found", req.GetRunId())
		}
		return nil, mapRoutedStoreError(err)
	}
	resp := &controlv1.GetRunResultResponse{
		RunId:        string(run.RunID),
		WorkflowId:   string(run.WorkflowID),
		// AttemptID empty until T05; result content empty until T05/T08.
		TerminalStatus: run.Status.String(),
	}
	if run.TerminatedAt != nil && !run.TerminatedAt.IsZero() {
		resp.FinishedAt = timestamppb.New(*run.TerminatedAt)
	}
	return resp, nil
}

// invocationReceiptToProto maps a routedrun.InvocationReceipt to the proto
// InvocationRecord.
func invocationReceiptToProto(r *routedrun.InvocationReceipt) *controlv1.InvocationRecord {
	if r == nil {
		return nil
	}
	out := &controlv1.InvocationRecord{
		SchemaVersion:              r.SchemaVersion,
		InvocationId:              string(r.InvocationID),
		WorkflowId:                string(r.WorkflowID),
		RunId:                     string(r.RunID),
		ResolvedDeploymentId:      string(r.ResolvedDeploymentID),
		ResolvedDeploymentVersion: r.ResolvedDeploymentVersion,
		ResolvedDeploymentDigest:  r.ResolvedDeploymentDigest,
		RequestedDeploymentRef:    r.RequestedDeploymentRef,
		InvocationIntentDigest:    r.InvocationIntentDigest,
		CallerIdentity:            r.CallerIdentity,
		InitialMaxActiveDurationMs: r.InitialMaxActiveDurationMs,
		InitialAttemptLeaseMs:     r.InitialAttemptLeaseMs,
		InitialMaxCostUsdDecimal:  r.InitialMaxCostUsdDecimal,
	}
	if !r.AdmittedAt.IsZero() {
		out.AdmittedAt = timestamppb.New(r.AdmittedAt)
	}
	return out
}

func (s *controlServer) CreateWorkflow(ctx context.Context, req *controlv1.CreateWorkflowRequest) (*controlv1.CreateWorkflowResponse, error) {
	_ = ctx // unused context; interface compliance
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	feature, block, code := workflowKindNotEnabled(req.GetWorkflowKind())
	return &controlv1.CreateWorkflowResponse{
		Error: featureNotEnabled(feature, block, code),
	}, nil
}

func (s *controlServer) GetWorkflow(ctx context.Context, req *controlv1.GetWorkflowRequest) (*controlv1.GetWorkflowResponse, error) {
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	if s.workflowStore == nil {
		return &controlv1.GetWorkflowResponse{
			Error: featureNotEnabled("workflow_runtime", "B28", "routed_run_workflow_not_enabled"),
		}, nil
	}
	wf, err := s.workflowStore.GetWorkflow(ctx, routedrun.WorkflowID(req.GetWorkflowId()))
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "workflow %q not found", req.GetWorkflowId())
		}
		return nil, mapRoutedStoreError(err)
	}
	return &controlv1.GetWorkflowResponse{Workflow: workflowToProto(wf)}, nil
}

func (s *controlServer) CancelWorkflow(ctx context.Context, req *controlv1.CancelWorkflowRequest) (*controlv1.CancelWorkflowResponse, error) {
	_ = ctx // unused context; interface compliance
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	return &controlv1.CancelWorkflowResponse{
		Error: featureNotEnabled("workflow_control", "B35", "routed_run_control_not_enabled"),
	}, nil
}

func (s *controlServer) SetWorkflowDesiredState(ctx context.Context, req *controlv1.SetWorkflowDesiredStateRequest) (*controlv1.SetWorkflowDesiredStateResponse, error) {
	_ = ctx // unused context; interface compliance
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	return &controlv1.SetWorkflowDesiredStateResponse{
		WorkflowId:      req.GetWorkflowId(),
		DesiredCommand:  req.GetDesiredCommand(),
		Error:           featureNotEnabled("workflow_control", "B35", "routed_run_control_not_enabled"),
	}, nil
}

func (s *controlServer) RestartWorkflow(ctx context.Context, req *controlv1.RestartWorkflowRequest) (*controlv1.RestartWorkflowResponse, error) {
	_ = ctx // unused context; interface compliance
	if req.GetSourceWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "source_workflow_id is required")
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	return &controlv1.RestartWorkflowResponse{
		SourceWorkflowId: req.GetSourceWorkflowId(),
		Error:            featureNotEnabled("workflow_restart", "B35", "routed_run_control_not_enabled"),
	}, nil
}

func (s *controlServer) AmendLimits(ctx context.Context, req *controlv1.AmendLimitsRequest) (*controlv1.AmendLimitsResponse, error) {
	_ = ctx // unused context; interface compliance
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	if strings.TrimSpace(req.GetIdempotencyKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if strings.TrimSpace(req.GetReason()) == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}
	return &controlv1.AmendLimitsResponse{
		WorkflowId: req.GetWorkflowId(),
		Error:      featureNotEnabled("limit_amendment", "B35", "routed_run_amendment_not_enabled"),
	}, nil
}

func (s *controlServer) GetWorkflowGraph(ctx context.Context, req *controlv1.GetWorkflowGraphRequest) (*controlv1.GetWorkflowGraphResponse, error) {
	if req.GetWorkflowId() == "" {
		return nil, status.Error(codes.InvalidArgument, "workflow_id is required")
	}
	if s.workflowStore == nil {
		return &controlv1.GetWorkflowGraphResponse{
			Error: featureNotEnabled("workflow_graph", "B28", "routed_run_workflow_not_enabled"),
		}, nil
	}
	wf, err := s.workflowStore.GetWorkflow(ctx, routedrun.WorkflowID(req.GetWorkflowId()))
	if err != nil {
		if errors.Is(err, routedrun.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "workflow %q not found", req.GetWorkflowId())
		}
		return nil, mapRoutedStoreError(err)
	}
	// Inspect is allowed (state read). Runtime start remains gated.
	nodes, _ := s.workflowStore.ListNodes(ctx, wf.WorkflowID) // best-effort list; empty on fail
	services, _ := s.workflowStore.ListServices(ctx, wf.WorkflowID) // best-effort list; empty on fail
	handoffs, _ := s.workflowStore.ListHandoffs(ctx, wf.WorkflowID) // best-effort list; empty on fail
	batches, _ := s.workflowStore.ListChildBatches(ctx, wf.WorkflowID) // best-effort list; empty on fail

	resp := &controlv1.GetWorkflowGraphResponse{
		Workflow: workflowToProto(wf),
	}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &controlv1.WorkflowNodeStatus{
			SchemaVersion: n.SchemaVersion,
			NodeId:        string(n.NodeID),
			WorkflowId:    string(n.WorkflowID),
			Status:        n.Status.String(),
			RunId:         string(n.RunID),
			StageOrder:    int32(n.StageOrder),
			PackageName:   n.PackageName,
			PackageVersion: n.PackageVersion,
		})
	}
	for _, svc := range services {
		resp.Services = append(resp.Services, &controlv1.ServiceBindingStatus{
			SchemaVersion:  svc.SchemaVersion,
			ServiceId:      string(svc.ServiceID),
			WorkflowId:     string(svc.WorkflowID),
			Status:         svc.Status.String(),
			PackageName:    svc.PackageName,
			PackageVersion: svc.PackageVersion,
			ServiceName:    svc.ServiceName,
		})
	}
	for _, h := range handoffs {
		resp.Handoffs = append(resp.Handoffs, &controlv1.HandoffMetadata{
			SchemaVersion: h.SchemaVersion,
			HandoffId:     string(h.HandoffID),
			WorkflowId:    string(h.WorkflowID),
			SourceNodeId:  string(h.SourceNodeID),
			TargetNodeId:  string(h.TargetNodeID),
		})
	}
	for _, b := range batches {
		resp.ChildBatches = append(resp.ChildBatches, &controlv1.ChildBatchStatus{
			SchemaVersion:    b.SchemaVersion,
			BatchId:          string(b.ChildBatchID),
			ParentWorkflowId: string(b.WorkflowID),
			Status:           b.Status.String(),
			ChildCount:       int32(len(b.ChildRunIDs)),
		})
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// Legacy run helpers: routed detection, fail-closed gates, persist one/one
// ---------------------------------------------------------------------------

// routedProjectSignals describes why a project is considered "routed".
type routedProjectSignals struct {
	HasRoute       bool
	RouteID        string
	HasWorkflow    bool
	WorkflowKind   string
	PolicyDigest   string
	HasMCPService  bool
	HasPipeline    bool
	HasChildSpawn  bool
}

// detectRoutedProject inspects deployed/installed agent artifacts for Route or
// workflow.yaml. Returns nil when the agent is a legacy (non-routed) project.
func (s *controlServer) detectRoutedProject(agentName string, isInstalled bool) (*routedProjectSignals, error) {
	if s.homePaths == nil {
		return nil, nil
	}
	var deployedDir string
	if isInstalled {
		// Installed agents: look under state install path for workflow/agent yaml.
		// Best-effort; if unreadable, treat as non-routed.
		name, pub8, ok := parseInstalledDir(agentName)
		if !ok {
			return nil, nil
		}
		deployedDir = filepath.Join(s.homePaths.State, "installed", name+"@"+pub8)
		if _, err := os.Stat(deployedDir); err != nil {
			// Fallback: try agents path style used by some installs.
			deployedDir = pack.DeployedAgentPath(s.homePaths.Home, agentName)
		}
	} else {
		deployedDir = pack.DeployedAgentPath(s.homePaths.Home, agentName)
	}

	sig := &routedProjectSignals{}

	// Prefer lockfile metadata when present.
	if lock, err := pack.LoadDeployedLock(s.homePaths.Home, agentName); err == nil && lock != nil {
		if lock.AgentYAML != nil && lock.AgentYAML.LLM.Route != "" {
			sig.HasRoute = true
			sig.RouteID = lock.AgentYAML.LLM.Route
		}
		if lock.WorkflowYAML != nil {
			sig.HasWorkflow = true
			sig.WorkflowKind = lock.WorkflowYAML.Kind
			sig.HasPipeline = lock.WorkflowYAML.Kind == pack.WorkflowKindPipeline
			sig.HasChildSpawn = lock.WorkflowYAML.Kind == pack.WorkflowKindParentChild
			sig.HasMCPService = len(lock.WorkflowYAML.Services) > 0
		}
		if lock.PolicyDigest != "" {
			sig.PolicyDigest = lock.PolicyDigest
		}
	}

	// agent.yaml route (deployed copy)
	agentPath := filepath.Join(deployedDir, "agent.yaml")
	if data, err := os.ReadFile(agentPath); err == nil {
		var ay pack.AgentYAML
		if yaml.Unmarshal(data, &ay) == nil && ay.LLM.Route != "" {
			sig.HasRoute = true
			sig.RouteID = ay.LLM.Route
		}
	}

	// workflow.yaml on disk
	wfPath := filepath.Join(deployedDir, "workflow.yaml")
	if data, err := os.ReadFile(wfPath); err == nil && len(data) > 0 {
		var wf pack.WorkflowYAML
		if yaml.Unmarshal(data, &wf) == nil {
			sig.HasWorkflow = true
			sig.WorkflowKind = wf.Kind
			sig.HasPipeline = wf.Kind == pack.WorkflowKindPipeline
			sig.HasChildSpawn = wf.Kind == pack.WorkflowKindParentChild
			sig.HasMCPService = len(wf.Services) > 0
		} else {
			// Unparseable but present — still treated as routed envelope.
			sig.HasWorkflow = true
		}
	}

	if !sig.HasRoute && !sig.HasWorkflow {
		return nil, nil
	}
	return sig, nil
}

// parseInstalledDir is a thin local helper to avoid importing install package
// cycles in unit tests; mirrors install.ParseInstalledAgentDir shape.
func parseInstalledDir(agentName string) (name, pub8 string, ok bool) {
	// Format: name@pub8 (pub8 is 8 hex chars) or more complex installed keys.
	at := strings.LastIndex(agentName, "@")
	if at <= 0 || at == len(agentName)-1 {
		return "", "", false
	}
	return agentName[:at], agentName[at+1:], true
}

// failClosedRoutedRun validates and (best-effort) records route/workflow
// placeholders then returns a typed not-enabled error. Never creates Docker
// resources or synthetic MCP/handoff results.
func (s *controlServer) failClosedRoutedRun(ctx context.Context, agentName string, sig *routedProjectSignals) error {
	// Persist inspectable placeholder metadata when store is available.
	// No invocation/admission — state-only snapshot for consent/inspect.
	if s.workflowStore != nil && sig != nil {
		if err := s.persistRoutedInspectPlaceholder(ctx, agentName, sig); err != nil {
			log.Printf("daemon: %s failed: %v", "s.persistRoutedInspectPlaceholder", err)
		}
	}

	switch {
	case sig != nil && sig.HasMCPService:
		return notEnabledFailedPrecondition("mcp_service", "B29", "agentpaas_mcp_service_not_enabled")
	case sig != nil && sig.HasPipeline:
		return notEnabledFailedPrecondition("pipeline", "B30", "agentpaas_pipeline_not_enabled")
	case sig != nil && sig.HasChildSpawn:
		return notEnabledFailedPrecondition("child_spawn", "B31", "agentpaas_child_spawn_not_enabled")
	case sig != nil && sig.HasRoute:
		return notEnabledFailedPrecondition("routed_run", "B32", "routed_run_routing_not_enabled")
	default:
		return notEnabledFailedPrecondition("routed_run", "B28", "routed_run_not_enabled")
	}
}

// persistRoutedInspectPlaceholder stores a workflow inspect record with
// route-policy and catalog-snapshot placeholders so inspect is deterministic
// without enabling runtime. Failures are ignored (best-effort).
func (s *controlServer) persistRoutedInspectPlaceholder(ctx context.Context, agentName string, sig *routedProjectSignals) error {
	if s.workflowStore == nil || sig == nil {
		return nil
	}
	// Use a stable inspect ID derived from agent name to keep inspect deterministic.
	// Prefix with "inspect-" so it never collides with real workflow IDs from admission.
	wfID := routedrun.WorkflowID("inspect-" + sanitizeID(agentName))
	existing, err := s.workflowStore.GetWorkflow(ctx, wfID)
	if err == nil && existing != nil {
		return nil // already persisted
	}
	kind := sig.WorkflowKind
	if kind == "" {
		kind = pack.WorkflowKindStandalone
	}
	meta := map[string]string{
		"agent_name":             agentName,
		"route_id":               sig.RouteID,
		"route_policy_ref":       "placeholder:route-policy:" + sig.RouteID,
		"catalog_snapshot_ref":   "placeholder:catalog-snapshot:pending-B32",
		"policy_digest":          sig.PolicyDigest,
		"experimental":           "true",
		"runtime_enabled":        "false",
	}
	_ = meta // reserved for NestedPackageDigests if we later create a deployment
	wf := &routedrun.WorkflowRecord{
		SchemaVersion:      routedrun.CurrentSchemaVersion,
		WorkflowID:         wfID,
		WorkflowKind:       kind,
		Status:             routedrun.WorkflowStatusPending,
		Generation:         1,
		PolicyDigest:       sig.PolicyDigest,
		CatalogSnapshotRef: "placeholder:catalog-snapshot:pending-B32",
		AuthorityGeneration: 1,
	}
	if err := s.workflowStore.CreateWorkflow(ctx, wf); err != nil {
		// Already exists or store issue — non-fatal for fail-closed path.
		return err
	}
	return nil
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "agent"
	}
	if len(out) > 64 {
		return out[:64]
	}
	return out
}

// persistLegacyRunAsOneAttempt writes a one-run/one-attempt record for a
// legacy agent run so list/summarize can read from the store after restart.
// Non-fatal: store errors are logged via return value only.
// If attemptID is non-empty, it is used as the attempt ID; otherwise an ID is generated.
func (s *controlServer) persistLegacyRunAsOneAttempt(ctx context.Context, runID, agentName, attemptID string) (string, error) {
	if s.runStore == nil {
		return "", nil
	}
	// Minimal standalone workflow shell so RunRecord.WorkflowID is non-empty.
	wfID := routedrun.WorkflowID("legacy-wf-" + runID)
	if s.workflowStore != nil {
		if err := s.workflowStore.CreateWorkflow(ctx, &routedrun.WorkflowRecord{
			SchemaVersion:       routedrun.CurrentSchemaVersion,
			WorkflowID:          wfID,
			WorkflowKind:        pack.WorkflowKindStandalone,
			Status:              routedrun.WorkflowStatusRunning,
			Generation:          1,
			AuthorityGeneration: 1,
		}); err != nil {
			// Best-effort shell workflow for legacy runs; run/attempt still persist below.
			log.Printf("daemon: create legacy workflow shell %s: %v", wfID, err)
		}
	}
	run := &routedrun.RunRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		RunID:         routedrun.RunID(runID),
		WorkflowID:    wfID,
		Status:        routedrun.RunStatusRunning,
		RunKind:       "standalone",
	}
	if err := s.runStore.CreateRun(ctx, run); err != nil {
		return "", err
	}
	att := &routedrun.AttemptRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		RunID:         routedrun.RunID(runID),
		WorkflowID:    wfID,
		Status:        routedrun.AttemptStatusRunning,
		AttemptNumber: 1,
	}
	if attemptID != "" {
		att.AttemptID = routedrun.AttemptID(attemptID)
	}
	if err := s.runStore.CreateAttempt(ctx, att); err != nil {
		return "", err
	}
	return string(att.AttemptID), nil
}

// updateLegacyRunStatus best-effort updates the store status for a legacy run.
func (s *controlServer) updateLegacyRunStatus(ctx context.Context, runID, statusLabel string) {
	if s.runStore == nil {
		return
	}
	run, err := s.runStore.GetRun(ctx, routedrun.RunID(runID))
	if err != nil || run == nil {
		return
	}
	switch statusLabel {
	case "succeeded":
		run.Status = routedrun.RunStatusSucceeded
	case "failed":
		run.Status = routedrun.RunStatusFailed
	case "cancelled":
		run.Status = routedrun.RunStatusCancelled
	case "running":
		run.Status = routedrun.RunStatusRunning
	default:
		return
	}
	now := time.Now().UTC()
	if run.Status.IsTerminal() {
		run.TerminatedAt = &now
	}
	// Generation for UpdateRun is 1 after CreateRun (writeJSON gen=1).
	if err := s.runStore.UpdateRun(ctx, run, 1); err != nil {
		log.Printf("daemon: update legacy run status %s: %v", runID, err)
	}
}

// ---------------------------------------------------------------------------
// Proto converters / error mapping
// ---------------------------------------------------------------------------

func deploymentToProto(d *routedrun.DeploymentRecord) *controlv1.DeploymentRecord {
	if d == nil {
		return nil
	}
	out := &controlv1.DeploymentRecord{
		SchemaVersion:        d.SchemaVersion,
		DeploymentId:         string(d.DeploymentID),
		PackageName:          d.PackageName,
		PackageVersion:       d.PackageVersion,
		Generation:           d.Generation,
		Status:               d.Status.String(),
		MaxConcurrentRuns:    int32(d.MaxConcurrentRuns),
		BundleDigest:         d.BundleDigest,
		PolicyDigest:         d.PolicyDigest,
		ImageLockDigest:      d.ImageLockDigest,
		ProvenanceDigest:     d.ProvenanceDigest,
		NestedPackageDigests: copyStringMap(d.NestedPackageDigests),
		CreatedBy:            d.CreatedBy,
	}
	if !d.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(d.CreatedAt)
	}
	if d.ActivatedAt != nil {
		out.ActivatedAt = timestamppb.New(*d.ActivatedAt)
	}
	if d.DeactivatedAt != nil {
		out.DeactivatedAt = timestamppb.New(*d.DeactivatedAt)
	}
	return out
}

func aliasToProto(a *routedrun.AliasRecord) *controlv1.DeploymentAliasRecord {
	if a == nil {
		return nil
	}
	out := &controlv1.DeploymentAliasRecord{
		SchemaVersion:      a.SchemaVersion,
		Alias:              a.Alias,
		TargetDeploymentId: string(a.TargetDeploymentID),
		TargetVersion:      a.TargetVersion,
		Generation:         a.Generation,
		UpdatedBy:          a.UpdatedBy,
	}
	if !a.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(a.UpdatedAt)
	}
	return out
}

func workflowToProto(wf *routedrun.WorkflowRecord) *controlv1.WorkflowRecord {
	if wf == nil {
		return nil
	}
	out := &controlv1.WorkflowRecord{
		SchemaVersion:         wf.SchemaVersion,
		WorkflowId:            string(wf.WorkflowID),
		WorkflowKind:          wf.WorkflowKind,
		InvocationId:          string(wf.InvocationID),
		DeploymentId:          string(wf.DeploymentID),
		Status:                wf.Status.String(),
		Generation:            wf.Generation,
		PolicyDigest:          wf.PolicyDigest,
		MaxActiveDurationMs:   wf.MaxActiveDurationMs,
		MaxAttemptLeaseMs:     wf.MaxAttemptLeaseMs,
		MaxLlmSpendDecimal:    wf.MaxLLMSpendDecimal,
		AuthorityGeneration:   wf.AuthorityGeneration,
	}
	if !wf.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(wf.CreatedAt)
	}
	if !wf.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(wf.UpdatedAt)
	}
	if wf.TerminatedAt != nil {
		out.TerminatedAt = timestamppb.New(*wf.TerminatedAt)
	}
	return out
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mapRoutedStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, routedrun.ErrNotFound):
		return status.Errorf(codes.NotFound, "%v", err)
	case errors.Is(err, routedrun.ErrAlreadyExists):
		return status.Errorf(codes.AlreadyExists, "%v", err)
	case errors.Is(err, routedrun.ErrCASConflict):
		return status.Errorf(codes.Aborted, "%v", err)
	case errors.Is(err, routedrun.ErrInvalidArgument):
		return status.Errorf(codes.InvalidArgument, "%v", err)
	case errors.Is(err, routedrun.ErrDeploymentInactive):
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	case errors.Is(err, routedrun.ErrIdempotencyConflict):
		return status.Errorf(codes.AlreadyExists, "%v", err)
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}

func workflowKindNotEnabled(kind string) (feature, block, code string) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case pack.WorkflowKindPipeline:
		return "pipeline", "B30", "agentpaas_pipeline_not_enabled"
	case pack.WorkflowKindParentChild:
		return "child_spawn", "B31", "agentpaas_child_spawn_not_enabled"
	case "mcp_service", "mcp":
		return "mcp_service", "B29", "agentpaas_mcp_service_not_enabled"
	default:
		return "workflow_runtime", "B28", "routed_run_workflow_not_enabled"
	}
}

// ensure unused import of yaml is satisfied even if build tags change.
var _ = yaml.Unmarshal // compile-time interface/import assertion
