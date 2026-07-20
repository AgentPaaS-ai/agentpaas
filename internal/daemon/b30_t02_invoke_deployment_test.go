package daemon

import (
	"context"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// seedActiveDepForInvoke creates an active standalone deployment and returns
// its deployment ID for use in InvokeDeployment calls.
func seedActiveDepForInvoke(t *testing.T, s *controlServer) string {
	t.Helper()
	ctx := context.Background()
	d, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:    "demo-agent",
		PackageVersion: "1.0.0",
		BundleDigest:   "sha256:bundle1",
		PolicyDigest:   "sha256:policy1",
		ImageLockDigest: "sha256:img1",
		ActorIdentity:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	return d.GetDeployment().GetDeploymentId()
}

// invokeReq builds a minimal valid InvokeDeploymentRequest.
func invokeReq(depID, key, caller, input string) *controlv1.InvokeDeploymentRequest {
	return &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  depID,
		InputJson:      []byte(input),
		IdempotencyKey: key,
		CallerIdentity: caller,
	}
}

// TestInvokeDeployment_AdmitsAndReturnsReceipt verifies the happy path: a
// valid request admits the invocation via the durable store and the response
// carries invocation/workflow/run IDs with no attempt ID yet (attempt is
// empty until the T05 supervisor claim creates it) (spec line 274-276).
func TestInvokeDeployment_AdmitsAndReturnsReceipt(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	// Outcome must be ACCEPTED (not the old FEATURE_NOT_ENABLED stub).
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("outcome=%v want ACCEPTED", resp.GetOutcome())
	}
	if resp.GetError() != nil {
		t.Fatalf("unexpected typed error: %+v", resp.GetError())
	}
	// Receipt identity must be populated.
	if resp.GetInvocationId() == "" {
		t.Fatal("invocation_id empty")
	}
	if resp.GetWorkflowId() == "" {
		t.Fatal("workflow_id empty")
	}
	if resp.GetRunId() == "" {
		t.Fatal("run_id empty")
	}
	if resp.GetResolvedDeploymentId() != depID {
		t.Fatalf("resolved_deployment_id=%s want %s", resp.GetResolvedDeploymentId(), depID)
	}
	if resp.GetAdmittedAt() == nil {
		t.Fatal("admitted_at empty")
	}
	// No run_id field for attempt; the response carries run-level IDs only.
	// The store must now hold exactly one invocation.
	invs, err := s.localStore.ListInvocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invs))
	}
	// No attempts yet (admission creates READY launch intent, not an attempt).
	atts, err := s.runStore.ListAttempts(ctx, routedrun.RunID(resp.GetRunId()))
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("admission must not create attempts, got %d", len(atts))
	}
}

// TestInvokeDeployment_IdempotentReplayReturnsOriginal verifies a second call
// with the same caller+key+intent returns the SAME receipt and creates no new
// job (assert the store's invocation count stays at 1).
func TestInvokeDeployment_IdempotentReplayReturnsOriginal(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	r1, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-replay", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("first InvokeDeployment: %v", err)
	}
	r2, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-replay", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("second InvokeDeployment: %v", err)
	}
	// Idempotent replay returns the SAME receipt.
	if r2.GetInvocationId() != r1.GetInvocationId() {
		t.Fatalf("idempotent replay: inv1=%s inv2=%s", r1.GetInvocationId(), r2.GetInvocationId())
	}
	if r2.GetRunId() != r1.GetRunId() {
		t.Fatalf("idempotent replay: run1=%s run2=%s", r1.GetRunId(), r2.GetRunId())
	}
	// Outcome must be IDEMPOTENT_REPLAY (not a fresh ACCEPTED).
	if r2.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_IDEMPOTENT_REPLAY {
		t.Fatalf("outcome=%v want IDEMPOTENT_REPLAY", r2.GetOutcome())
	}
	// Store must still hold exactly one invocation.
	invs, err := s.localStore.ListInvocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 1 {
		t.Fatalf("idempotent replay created %d invocations, want 1", len(invs))
	}
}

// TestInvokeDeployment_ChangedIntentReturnsConflict verifies that the same
// idempotency key with a different input digest returns a typed conflict
// error and creates no new job.
func TestInvokeDeployment_ChangedIntentReturnsConflict(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	if _, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-conflict", "tester", `{"x":1}`)); err != nil {
		t.Fatalf("first InvokeDeployment: %v", err)
	}
	// Same key, different input -> typed conflict (idempotency conflict).
	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-conflict", "tester", `{"x":2}`))
	if err != nil {
		t.Fatalf("second InvokeDeployment returned gRPC error (should be typed): %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_IDEMPOTENCY_CONFLICT {
		t.Fatalf("outcome=%v want IDEMPOTENCY_CONFLICT", resp.GetOutcome())
	}
	if resp.GetError() == nil {
		t.Fatal("expected typed conflict error")
	}
	if resp.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_IDEMPOTENCY_CONFLICT &&
		resp.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_CHANGED_IDEMPOTENCY_PAYLOAD {
		t.Fatalf("error code=%v want IDEMPOTENCY_CONFLICT or CHANGED_IDEMPOTENCY_PAYLOAD", resp.GetError().GetCode())
	}
	// Store must still hold exactly one invocation.
	invs, _ := s.localStore.ListInvocations(ctx)
	if len(invs) != 1 {
		t.Fatalf("conflict created %d invocations, want 1", len(invs))
	}
}

// TestInvokeDeployment_InactiveDeploymentRejected verifies an inactive
// deployment returns a typed error and creates no job.
func TestInvokeDeployment_InactiveDeploymentRejected(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)
	// Deactivate the deployment.
	if _, err := s.DeactivateDeployment(ctx, &controlv1.DeactivateDeploymentRequest{
		DeploymentId: depID, ActorIdentity: "tester",
	}); err != nil {
		t.Fatalf("DeactivateDeployment: %v", err)
	}
	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-inactive", "tester", `{}`))
	if err != nil {
		t.Fatalf("InvokeDeployment returned gRPC error (should be typed): %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_DEPLOYMENT_INACTIVE {
		t.Fatalf("outcome=%v want DEPLOYMENT_INACTIVE", resp.GetOutcome())
	}
	if resp.GetError() == nil {
		t.Fatal("expected typed error for inactive deployment")
	}
	if resp.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_DEPLOYMENT_INACTIVE {
		t.Fatalf("error code=%v want DEPLOYMENT_INACTIVE", resp.GetError().GetCode())
	}
	// No invocation created.
	invs, _ := s.localStore.ListInvocations(ctx)
	if len(invs) != 0 {
		t.Fatalf("inactive deployment created %d invocations, want 0", len(invs))
	}
}

// TestInvokeDeployment_AlreadyRunningReturnsRetryable verifies that with
// default-one concurrency, two simultaneous invocations of the same
// deployment admit exactly one; the second gets ALREADY_RUNNING.
func TestInvokeDeployment_AlreadyRunningReturnsRetryable(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	r1, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-run-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("first InvokeDeployment: %v", err)
	}
	if r1.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("first outcome=%v want ACCEPTED", r1.GetOutcome())
	}
	// Second invocation with a DIFFERENT idempotency key hits the concurrency
	// cap (default max_concurrent_runs=1 -> ALREADY_RUNNING).
	r2, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-run-2", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("second InvokeDeployment returned gRPC error (should be typed): %v", err)
	}
	if r2.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ALREADY_RUNNING {
		t.Fatalf("second outcome=%v want ALREADY_RUNNING", r2.GetOutcome())
	}
	if r2.GetError() == nil {
		t.Fatal("expected typed error for ALREADY_RUNNING")
	}
	if r2.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_ALREADY_RUNNING {
		t.Fatalf("error code=%v want ALREADY_RUNNING", r2.GetError().GetCode())
	}
	// Exactly one invocation was created.
	invs, _ := s.localStore.ListInvocations(ctx)
	if len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invs))
	}
}

// TestInvokeDeployment_MissingFields_InvalidArgument verifies the validation
// guards (missing deployment_ref / idempotency_key) still return gRPC
// InvalidArgument before any state mutation.
func TestInvokeDeployment_MissingFields_InvalidArgument(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	// Missing idempotency_key.
	_, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef: depID,
		InputJson:     []byte(`{}`),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing key: code=%v want InvalidArgument", status.Code(err))
	}
	// Missing deployment_ref.
	_, err = s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		IdempotencyKey: "k", CallerIdentity: "c",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing ref: code=%v want InvalidArgument", status.Code(err))
	}
	// Missing caller_identity -> treated as invalid (store requires it).
	_, err = s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef: depID, IdempotencyKey: "k", InputJson: []byte(`{}`),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing caller: code=%v want InvalidArgument", status.Code(err))
	}
}

// TestInvokeDeployment_StoreNotInit verifies the handler fails closed when
// the routed store is not initialized.
func TestInvokeDeployment_StoreNotInit(t *testing.T) {
	s := &controlServer{version: VersionInfo{DaemonVersion: "test"}}
	ctx := context.Background()
	_, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  "dep-x",
		InputJson:      []byte(`{}`),
		IdempotencyKey: "k",
		CallerIdentity: "c",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("store not init: code=%v want FailedPrecondition", status.Code(err))
	}
	if !strings.Contains(err.Error(), "routed store") {
		t.Fatalf("error message: %v", err)
	}
}

// TestInvokeDeployment_AbsoluteCeilingsPopulated verifies the response carries
// an AbsoluteCeilingsSnapshot reflecting the requested initial ceilings.
func TestInvokeDeployment_AbsoluteCeilingsPopulated(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:                depID,
		InputJson:                    []byte(`{}`),
		IdempotencyKey:               "idem-ceil",
		CallerIdentity:               "tester",
		InitialMaxActiveDurationMs:   120000,
		InitialAttemptLeaseMs:        60000,
		InitialMaxCostUsdDecimal:     "2.50",
	})
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	c := resp.GetCeilings()
	if c == nil {
		t.Fatal("ceilings snapshot empty")
	}
	if c.GetOriginalMaxActiveDurationMs() != 120000 {
		t.Fatalf("active=%d want 120000", c.GetOriginalMaxActiveDurationMs())
	}
	if c.GetOriginalAttemptLeaseMs() != 60000 {
		t.Fatalf("lease=%d want 60000", c.GetOriginalAttemptLeaseMs())
	}
	if c.GetOriginalMaxLlmSpendDecimal() != "2.50" {
		t.Fatalf("spend=%s want 2.50", c.GetOriginalMaxLlmSpendDecimal())
	}
}

// TestGetInvocation_ReadsAdmittedRecord verifies GetInvocation returns the
// durable admission receipt by invocation ID (thin pass-through).
func TestGetInvocation_ReadsAdmittedRecord(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	ir, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-get-inv", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	got, err := s.GetInvocation(ctx, &controlv1.GetInvocationRequest{InvocationId: ir.GetInvocationId()})
	if err != nil {
		t.Fatalf("GetInvocation: %v", err)
	}
	rec := got.GetInvocation()
	if rec == nil {
		t.Fatal("invocation record empty")
	}
	if rec.GetInvocationId() != ir.GetInvocationId() {
		t.Fatalf("invocation_id=%s want %s", rec.GetInvocationId(), ir.GetInvocationId())
	}
	if rec.GetWorkflowId() != ir.GetWorkflowId() {
		t.Fatalf("workflow_id=%s want %s", rec.GetWorkflowId(), ir.GetWorkflowId())
	}
	if rec.GetRunId() != ir.GetRunId() {
		t.Fatalf("run_id=%s want %s", rec.GetRunId(), ir.GetRunId())
	}
	if rec.GetResolvedDeploymentId() != depID {
		t.Fatalf("resolved_deployment_id=%s want %s", rec.GetResolvedDeploymentId(), depID)
	}
	if rec.GetAdmittedAt() == nil {
		t.Fatal("admitted_at empty")
	}
}

// TestGetInvocation_NotFound verifies GetInvocation returns NotFound for an
// unknown invocation ID.
func TestGetInvocation_NotFound(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.GetInvocation(ctx, &controlv1.GetInvocationRequest{InvocationId: "inv-missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code=%v want NotFound", status.Code(err))
	}
}

// TestGetInvocation_MissingField verifies the validation guard.
func TestGetInvocation_MissingField(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.GetInvocation(ctx, &controlv1.GetInvocationRequest{InvocationId: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code=%v want InvalidArgument", status.Code(err))
	}
}

// TestGetRunStatus_ReadsRunRecord verifies GetRunStatus returns the run
// record status (thin pass-through to RunStore.GetRun).
func TestGetRunStatus_ReadsRunRecord(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)
	ir, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-status", "tester", `{}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	got, err := s.GetRunStatus(ctx, &controlv1.GetRunStatusRequest{RunId: ir.GetRunId()})
	if err != nil {
		t.Fatalf("GetRunStatus: %v", err)
	}
	if got.GetRunId() != ir.GetRunId() {
		t.Fatalf("run_id=%s want %s", got.GetRunId(), ir.GetRunId())
	}
	if got.GetWorkflowId() != ir.GetWorkflowId() {
		t.Fatalf("workflow_id=%s want %s", got.GetWorkflowId(), ir.GetWorkflowId())
	}
	// Admission creates a PENDING run (READY launch intent, no attempt).
	if got.GetStatus() != "PENDING" {
		t.Fatalf("status=%s want PENDING", got.GetStatus())
	}
	if got.GetRunKind() != "standalone" {
		t.Fatalf("run_kind=%s want standalone", got.GetRunKind())
	}
}

// TestGetRunStatus_NotFound verifies GetRunStatus returns NotFound for an
// unknown run ID.
func TestGetRunStatus_NotFound(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.GetRunStatus(ctx, &controlv1.GetRunStatusRequest{RunId: "run-missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code=%v want NotFound", status.Code(err))
	}
}

// TestGetRunResult_EmptyUntilT05 verifies GetRunResult returns the run
// envelope with empty result content (result lands with T05/T08). The run
// must resolve (not NotFound) but the result fields are empty.
func TestGetRunResult_EmptyUntilT05(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)
	ir, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-result", "tester", `{}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	got, err := s.GetRunResult(ctx, &controlv1.GetRunResultRequest{RunId: ir.GetRunId()})
	if err != nil {
		t.Fatalf("GetRunResult: %v", err)
	}
	if got.GetRunId() != ir.GetRunId() {
		t.Fatalf("run_id=%s want %s", got.GetRunId(), ir.GetRunId())
	}
	// Result content is empty until T05 writes results.
	if got.GetResultDigest() != "" {
		t.Fatalf("result_digest=%s want empty (T05)", got.GetResultDigest())
	}
	if got.GetStructuredResult() != "" {
		t.Fatalf("structured_result=%s want empty (T05)", got.GetStructuredResult())
	}
	if got.GetAttemptId() != "" {
		t.Fatalf("attempt_id=%s want empty (T05)", got.GetAttemptId())
	}
}

// TestGetRunResult_NotFound verifies GetRunResult returns NotFound for an
// unknown run ID.
func TestGetRunResult_NotFound(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.GetRunResult(ctx, &controlv1.GetRunResultRequest{RunId: "run-missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("code=%v want NotFound", status.Code(err))
	}
}
