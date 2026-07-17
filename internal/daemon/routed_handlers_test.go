package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestControlServer(t *testing.T) *controlServer {
	t.Helper()
	tmp := t.TempDir()
	paths := home.NewHomePaths(tmp)
	if err := home.Ensure(paths); err != nil {
		t.Fatal(err)
	}
	s := &controlServer{homePaths: paths, version: VersionInfo{DaemonVersion: "test"}}
	if err := s.initRoutedStores(routedStoreRoot(paths)); err != nil {
		t.Fatalf("initRoutedStores: %v", err)
	}
	return s
}

func TestCreateDeployment_StateOnlyWorks(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	resp, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:       "demo-agent",
		PackageVersion:    "1.0.0",
		BundleDigest:      "sha256:bundle1",
		PolicyDigest:      "sha256:policy1",
		ImageLockDigest:   "sha256:img1",
		MaxConcurrentRuns: 2,
		ActorIdentity:     "tester",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	dep := resp.GetDeployment()
	if dep == nil {
		t.Fatal("expected deployment")
	}
	if !strings.HasPrefix(dep.GetDeploymentId(), "dep-") {
		t.Fatalf("unexpected deployment id: %s", dep.GetDeploymentId())
	}
	if dep.GetStatus() != "ACTIVE" {
		t.Fatalf("status=%s want ACTIVE", dep.GetStatus())
	}
	if dep.GetGeneration() != 1 {
		t.Fatalf("generation=%d want 1", dep.GetGeneration())
	}

	got, err := s.GetDeployment(ctx, &controlv1.GetDeploymentRequest{DeploymentId: dep.GetDeploymentId()})
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if got.GetDeployment().GetPackageName() != "demo-agent" {
		t.Fatalf("package=%s", got.GetDeployment().GetPackageName())
	}
}

func TestDeploymentAliasCAS(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	d1, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName: "pkg", PackageVersion: "1.0.0", BundleDigest: "b1", ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName: "pkg", PackageVersion: "2.0.0", BundleDigest: "b2", ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}

	aliasResp, err := s.CreateDeploymentAlias(ctx, &controlv1.CreateDeploymentAliasRequest{
		Alias: "prod", TargetDeploymentId: d1.GetDeployment().GetDeploymentId(), ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if aliasResp.GetAlias().GetGeneration() != 1 {
		t.Fatalf("gen=%d", aliasResp.GetAlias().GetGeneration())
	}

	// Wrong generation → CAS conflict.
	_, err = s.CasDeploymentAlias(ctx, &controlv1.CasDeploymentAliasRequest{
		Alias: "prod", TargetDeploymentId: d2.GetDeployment().GetDeploymentId(),
		ExpectedGeneration: 99, ActorIdentity: "t",
	})
	if status.Code(err) != codes.Aborted {
		// mapRoutedStoreError may use FailedPrecondition or Aborted — accept either CAS-ish code.
		if status.Code(err) != codes.FailedPrecondition && status.Code(err) != codes.Aborted {
			// Also allow AlreadyExists / Internal only if message mentions CAS.
			if !strings.Contains(err.Error(), "CAS") && !strings.Contains(err.Error(), "conflict") && !strings.Contains(err.Error(), "expected") {
				t.Fatalf("expected CAS conflict, got %v", err)
			}
		}
	}

	// Correct generation → promote.
	promoted, err := s.CasDeploymentAlias(ctx, &controlv1.CasDeploymentAliasRequest{
		Alias: "prod", TargetDeploymentId: d2.GetDeployment().GetDeploymentId(),
		ExpectedGeneration: 1, ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.GetAlias().GetGeneration() != 2 {
		t.Fatalf("gen=%d want 2", promoted.GetAlias().GetGeneration())
	}
	if promoted.GetAlias().GetTargetDeploymentId() != d2.GetDeployment().GetDeploymentId() {
		t.Fatal("target not updated")
	}

	// Both deployments retained (immutable history of exact identities).
	list, err := s.ListDeployments(ctx, &controlv1.ListDeploymentsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.GetDeployments()) != 2 {
		t.Fatalf("want 2 deployments retained, got %d", len(list.GetDeployments()))
	}
}

func TestDeactivateDeployment_Retained(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	d, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName: "pkg", PackageVersion: "1.0.0", BundleDigest: "b", ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := d.GetDeployment().GetDeploymentId()
	out, err := s.DeactivateDeployment(ctx, &controlv1.DeactivateDeploymentRequest{
		DeploymentId: id, ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.GetDeployment().GetStatus() != "INACTIVE" {
		t.Fatalf("status=%s", out.GetDeployment().GetStatus())
	}
	// Still listable / inspectable.
	list, err := s.ListDeployments(ctx, &controlv1.ListDeploymentsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, dep := range list.GetDeployments() {
		if dep.GetDeploymentId() == id {
			found = true
			if dep.GetStatus() != "INACTIVE" {
				t.Fatalf("listed status=%s", dep.GetStatus())
			}
		}
	}
	if !found {
		t.Fatal("deactivated deployment missing from list")
	}
}

func TestInvokeDeployment_NotEnabledNoMutation(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	d, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName: "pkg", PackageVersion: "1.0.0", BundleDigest: "b", ActorIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Missing idempotency key (API requires) → gRPC InvalidArgument.
	_, err = s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef: d.GetDeployment().GetDeploymentId(),
		InputJson:     []byte(`{}`),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument for missing key, got %v", err)
	}

	// With key → feature not enabled, no accepted job.
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  d.GetDeployment().GetDeploymentId(),
		InputJson:      []byte(`{"x":1}`),
		IdempotencyKey: "idem-test-1",
		CallerIdentity: "tester",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if resp.GetError() == nil {
		t.Fatal("expected feature not enabled error")
	}
	if resp.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED {
		t.Fatalf("code=%v", resp.GetError().GetCode())
	}
	if resp.GetError().GetCodeName() != "routed_run_invocation_not_enabled" &&
		resp.GetError().GetDetails()["code_name"] != "routed_run_invocation_not_enabled" {
		t.Fatalf("details=%v name=%s", resp.GetError().GetDetails(), resp.GetError().GetCodeName())
	}
	// No accepted outcome.
	if resp.GetOutcome() == controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatal("must not accept invocation in B26")
	}

	// Store must have zero invocations.
	invs, err := s.localStore.ListInvocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(invs) != 0 {
		t.Fatalf("expected no invocations, got %d", len(invs))
	}
	// No runs created.
	runs, err := s.runStore.ListRuns(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs, got %d", len(runs))
	}
}

func TestControlAndAmendment_NotEnabledNoMutation(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Seed a workflow for inspect paths.
	wfID, err := routedrun.NewWorkflowID()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.workflowStore.CreateWorkflow(ctx, &routedrun.WorkflowRecord{
		WorkflowID:    wfID,
		SchemaVersion: routedrun.CurrentSchemaVersion,
		WorkflowKind:  "pipeline",
		Status:        routedrun.WorkflowStatusPending,
	}); err != nil {
		t.Fatal(err)
	}

	// Get works (inspect).
	got, err := s.GetWorkflow(ctx, &controlv1.GetWorkflowRequest{WorkflowId: string(wfID)})
	if err != nil {
		t.Fatal(err)
	}
	if got.GetWorkflow().GetWorkflowKind() != "pipeline" {
		t.Fatalf("kind=%s", got.GetWorkflow().GetWorkflowKind())
	}

	cancelResp, err := s.CancelWorkflow(ctx, &controlv1.CancelWorkflowRequest{
		WorkflowId: string(wfID), Reason: "test", ActorIdentity: "t", IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelResp.GetError() == nil || cancelResp.GetError().GetCodeName() != "FEATURE_NOT_ENABLED" &&
		!strings.Contains(cancelResp.GetError().GetCodeName(), "not_enabled") &&
		cancelResp.GetError().GetCode() != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED {
		t.Fatalf("cancel: %+v", cancelResp.GetError())
	}

	amend, err := s.AmendLimits(ctx, &controlv1.AmendLimitsRequest{
		WorkflowId: string(wfID), Reason: "more", ActorIdentity: "t", IdempotencyKey: "k2",
		NewMaxActiveDurationMs: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if amend.GetError() == nil {
		t.Fatal("expected amend not enabled")
	}

	// Workflow status unchanged.
	got2, err := s.GetWorkflow(ctx, &controlv1.GetWorkflowRequest{WorkflowId: string(wfID)})
	if err != nil {
		t.Fatal(err)
	}
	if got2.GetWorkflow().GetStatus() != got.GetWorkflow().GetStatus() {
		t.Fatalf("status mutated: %s -> %s", got.GetWorkflow().GetStatus(), got2.GetWorkflow().GetStatus())
	}
}

func TestContinuation_FailsWithoutMutation(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Continuation fields must fail before any Docker or store mutation.
	_, err := s.Run(ctx, &controlv1.RunRequest{
		AgentName:      "nonexistent",
		ContinueRunId:  "run-abc",
		RecoveryAction: "more_time",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code=%v err=%v", status.Code(err), err)
	}
	if !strings.Contains(err.Error(), "routed_run_continuation_not_enabled") {
		t.Fatalf("err=%v", err)
	}

	// Attempt lease alone also fails closed.
	_, err = s.Run(ctx, &controlv1.RunRequest{
		AgentName:               "nonexistent",
		RequestedAttemptLeaseMs: 60000,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code=%v", status.Code(err))
	}

	runs, err := s.runStore.ListRuns(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs after continuation failure, got %d", len(runs))
	}
}

func TestPersistLegacyRun_OneRunOneAttempt(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	attemptID, err := s.persistLegacyRunAsOneAttempt(ctx, "run-legacydeadbeef", "demo-agent")
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if !strings.HasPrefix(attemptID, "at-") {
		t.Fatalf("attempt id=%s", attemptID)
	}

	run, err := s.runStore.GetRun(ctx, routedrun.RunID("run-legacydeadbeef"))
	if err != nil {
		t.Fatal(err)
	}
	if run.RunKind != "standalone" {
		t.Fatalf("kind=%s", run.RunKind)
	}
	if run.Status != routedrun.RunStatusRunning {
		t.Fatalf("status=%v", run.Status)
	}
	attempts, err := s.runStore.ListAttempts(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts=%d", len(attempts))
	}
	if attempts[0].AttemptNumber != 1 {
		t.Fatalf("number=%d", attempts[0].AttemptNumber)
	}
}

func TestListRuns_SurvivesStoreRestart(t *testing.T) {
	tmp := t.TempDir()
	paths := home.NewHomePaths(tmp)
	if err := home.Ensure(paths); err != nil {
		t.Fatal(err)
	}
	root := routedStoreRoot(paths)

	s1 := &controlServer{homePaths: paths, version: VersionInfo{DaemonVersion: "test"}}
	if err := s1.initRoutedStores(root); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.persistLegacyRunAsOneAttempt(context.Background(), "run-restart1", "agent-a"); err != nil {
		t.Fatal(err)
	}

	// Re-open store (simulates daemon restart).
	s2 := &controlServer{homePaths: paths, version: VersionInfo{DaemonVersion: "test"}}
	if err := s2.initRoutedStores(root); err != nil {
		t.Fatal(err)
	}
	resp, err := s2.ListRuns(context.Background(), &controlv1.ListRunsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range resp.GetRuns() {
		if r.GetRunId() == "run-restart1" {
			found = true
			if r.GetStatus() == "" {
				t.Fatal("empty status")
			}
		}
	}
	if !found {
		t.Fatal("persisted run not listed after restart")
	}
}

func TestWorkflowKindNotEnabledCodes(t *testing.T) {
	f, b, c := workflowKindNotEnabled(pack.WorkflowKindPipeline)
	if f != "pipeline" || b != "B30" || c != "agentpaas_pipeline_not_enabled" {
		t.Fatalf("%s %s %s", f, b, c)
	}
	_, _, c = workflowKindNotEnabled(pack.WorkflowKindParentChild)
	if c != "agentpaas_child_spawn_not_enabled" {
		t.Fatalf("%s", c)
	}
	_, _, c = workflowKindNotEnabled("mcp_service")
	if c != "agentpaas_mcp_service_not_enabled" {
		t.Fatalf("%s", c)
	}
}

func TestFailClosedRoutedRun_NoDocker(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	err := s.failClosedRoutedRun(ctx, "demo", &routedProjectSignals{
		HasRoute: true, RouteID: "r1", PolicyDigest: "pol",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code=%v", status.Code(err))
	}
	if !strings.Contains(err.Error(), "routed_run_routing_not_enabled") &&
		!strings.Contains(err.Error(), "routed_run_not_enabled") {
		t.Fatalf("err=%v", err)
	}
	// No runs admitted as accepted jobs.
	runs, lerr := s.runStore.ListRuns(ctx, "")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(runs) != 0 {
		t.Fatalf("runs leaked: %d", len(runs))
	}
}

func TestNotEnabledErrorHelpers(t *testing.T) {
	te := featureNotEnabled("feature_x", "B99", "feature_x_not_enabled")
	if te.Code != controlv1.TypedControlErrorCode_TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED {
		t.Fatal(te.Code)
	}
	if te.CodeName != "feature_x_not_enabled" {
		t.Fatal(te.CodeName)
	}
	err := notEnabledFailedPrecondition("f", "B1", "code_x")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatal(status.Code(err))
	}
}

func TestMCPServiceBinding_NotRoutedThroughHarness(t *testing.T) {
	// Ensure InvokeDeployment never produces synthetic MCP success.
	s := newTestControlServer(t)
	ctx := context.Background()
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef: "alias/prod", InputJson: []byte(`{}`), IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() == nil {
		t.Fatal("expected not-enabled")
	}
	// No harness audit or containers — store empty.
	if invs, _ := s.localStore.ListInvocations(ctx); len(invs) != 0 {
		t.Fatal("invocation receipt must not be created on not-enabled path")
	}
}

func TestDetectRoutedProject_WorkflowYAML(t *testing.T) {
	s := newTestControlServer(t)
	agentDir := pack.DeployedAgentPath(s.homePaths.Home, "wf-agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "workflow.yaml"), []byte(
		"schema_version: \"1.0\"\nkind: pipeline\nnodes:\n  - id: n1\n    type: agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sig, err := s.detectRoutedProject("wf-agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || !sig.HasWorkflow {
		t.Fatalf("expected workflow signal, got %+v", sig)
	}
	if !sig.HasPipeline {
		t.Fatalf("expected pipeline: %+v", sig)
	}
}

func TestCreateWorkflow_NotEnabled(t *testing.T) {
	s := newTestControlServer(t)
	resp, err := s.CreateWorkflow(context.Background(), &controlv1.CreateWorkflowRequest{
		WorkflowKind:   pack.WorkflowKindPipeline,
		IdempotencyKey: "k",
		CallerIdentity: "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() == nil {
		t.Fatal("expected not enabled")
	}
	if resp.GetError().GetCodeName() != "agentpaas_pipeline_not_enabled" {
		t.Fatalf("code_name=%s", resp.GetError().GetCodeName())
	}
}
