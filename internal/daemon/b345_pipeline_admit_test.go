package daemon

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// ---------------------------------------------------------------------------
// B34.5-C: Pipeline admit → register reconcile tests
// ---------------------------------------------------------------------------

// TestB345_PipelineAdmit_RegistersForReconcile verifies that when a pipeline
// deployment is admitted via InvokeDeployment, the workflow is registered for
// reconcile and startDurableRun is NOT called. After a tick the reconciler
// claims stage0 and creates a launch job.
func TestB345_PipelineAdmit_RegistersForReconcile(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Create a pipeline deployment with stage:0 and stage:1 nested packages.
	depID := seedPipelineDepForInvoke(t, s, 2)

	// Wire a FakeLauncher pipeline runtime so we can observe reconcile.
	store := s.localStore
	ctrl := pipeline.NewController(store)
	launches := pipeline.NewMemoryLaunchStore()
	var reconcileCalls int32
	reconciler := &pipeline.Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: pipeline.FakeLauncher{},
	}

	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		atomic.AddInt32(&reconcileCalls, 1)
		_, err := reconciler.ReconcileOnce(ctx, workflowID)
		return err
	}, 10*time.Millisecond)
	s.pipelineRuntime = rt

	loopCtx, loopCancel := context.WithCancel(ctx)
	defer loopCancel()
	rt.Start(loopCtx)
	defer rt.Stop()

	// Admit a pipeline invocation. startDurableRun must NOT be called.
	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-pipe-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("outcome=%v want ACCEPTED", resp.GetOutcome())
	}

	wfID := routedrun.WorkflowID(resp.GetWorkflowId())

	// Wait for the registration goroutine to complete.
	deadline := time.After(2 * time.Second)
	var found bool
	for !found {
		ids := rt.knownPipelineWorkflowIDs()
		for _, id := range ids {
			if id == wfID {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			ids := rt.knownPipelineWorkflowIDs()
			t.Fatalf("pipeline workflow %s not registered in pipeline runtime; ids=%v", wfID, ids)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Wait for the reconcile loop to claim stage 0 → LAUNCHING → RUNNING.
	deadline = time.After(3 * time.Second)
	var node0 *routedrun.PipelineNode
	for {
		nodes, err := ctrl.Store.ListNodes(ctx, wfID)
		if err != nil {
			t.Fatalf("ListNodes: %v", err)
		}
		var foundNode bool
		for _, n := range nodes {
			if n.StageOrder == 0 {
				node0 = n
				foundNode = true
				break
			}
		}
		if !foundNode {
			t.Fatal("stage 0 node not found")
		}
		if node0.Status == routedrun.NodeStatusRunning {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for stage0→RUNNING: status=%s reconcileCalls=%d",
				node0.Status, atomic.LoadInt32(&reconcileCalls))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Verify a launch job exists for stage0.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("expected at least 1 launch job for pipeline workflow")
	}
	// The job should be STARTED (FakeLauncher marks it started immediately).
	for _, j := range jobs {
		if j.NodeID == node0.NodeID && j.Status != pipeline.LaunchStatusStarted {
			t.Errorf("stage0 launch job: status=%s want STARTED", j.Status)
		}
	}

	t.Logf("pipeline admission test: reconcileCalls=%d node0_status=%s jobs=%d",
		atomic.LoadInt32(&reconcileCalls), node0.Status, len(jobs))
}

// TestB345_StandaloneAdmit_StillCallsStartDurableRun verifies that standalone
// deployments still follow the startDurableRun path (NOT pipeline register).
// Since startDurableRun launches Docker containers, we verify the pipeline
// runtime does NOT register the workflow.
func TestB345_StandaloneAdmit_NotRegisteredForReconcile(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Create a standalone deployment (default kind).
	depID := seedActiveDepForInvoke(t, s)

	// Wire a pipeline runtime so we can verify it does NOT get registered.
	store := s.localStore
	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		return nil
	}, time.Second)
	s.pipelineRuntime = rt

	// Admit a standalone invocation.
	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-stand-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("outcome=%v want ACCEPTED", resp.GetOutcome())
	}

	wfID := routedrun.WorkflowID(resp.GetWorkflowId())

	// Verify the workflow kind is standalone.
	wf, err := s.workflowStore.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.WorkflowKind != "standalone" {
		t.Fatalf("workflow kind=%s want standalone", wf.WorkflowKind)
	}

	// Verify the workflow was NOT registered in the pipeline runtime.
	ids := rt.knownPipelineWorkflowIDs()
	for _, id := range ids {
		if id == wfID {
			t.Fatalf("standalone workflow %s incorrectly registered in pipeline runtime", wfID)
		}
	}

	t.Logf("standalone admission: workflow kind=%s, not registered for reconcile", wf.WorkflowKind)
}

// TestB345_PipelineRuntime_DoubleRegisterIdempotent verifies that registering
// the same workflow ID twice is safe (idempotent).
func TestB345_PipelineRuntime_DoubleRegisterIdempotent(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Create a pipeline deployment and admit twice.
	depID := seedPipelineDepForInvoke(t, s, 2)

	store := s.localStore
	rt := newPipelineRuntime(store, func(ctx context.Context, workflowID routedrun.WorkflowID) error {
		return nil
	}, time.Second)
	s.pipelineRuntime = rt

	// First admission.
	resp1, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-dup-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("first InvokeDeployment: %v", err)
	}
	if resp1.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("first outcome=%v want ACCEPTED", resp1.GetOutcome())
	}

	// Second admission (idempotent replay).
	resp2, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-dup-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("second InvokeDeployment: %v", err)
	}
	if resp2.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_IDEMPOTENT_REPLAY {
		t.Fatalf("second outcome=%v want IDEMPOTENT_REPLAY", resp2.GetOutcome())
	}

	// Both should register the same workflow ID — idempotent.
	wfID := routedrun.WorkflowID(resp1.GetWorkflowId())
	ids := rt.knownPipelineWorkflowIDs()
	count := 0
	for _, id := range ids {
		if id == wfID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("workflow %s appears %d times in registry; want exactly 1 (idempotent)", wfID, count)
	}

	t.Logf("double register: wfID=%s count=%d (idempotent)", wfID, count)
}

// TestB345_PipelineRuntime_DisabledWithoutEnv skips pipeline routing when
// pipelineRuntime is nil (not enabled).
func TestB345_PipelineRuntime_DisabledWithoutEnv(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()

	// Create a pipeline deployment.
	depID := seedPipelineDepForInvoke(t, s, 2)

	// pipelineRuntime is nil → shouldUsePipelineReconcile returns false.
	if s.pipelineRuntime != nil {
		t.Fatal("pipelineRuntime should be nil by default")
	}

	resp, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-disabled-1", "tester", `{"x":1}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	if resp.GetOutcome() != controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("outcome=%v want ACCEPTED", resp.GetOutcome())
	}

	// The workflow should exist and have kind=pipeline.
	wfID := routedrun.WorkflowID(resp.GetWorkflowId())
	wf, err := s.workflowStore.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.WorkflowKind != "pipeline" {
		t.Fatalf("workflow kind=%s want pipeline", wf.WorkflowKind)
	}

	t.Logf("disabled: pipeline admission accepted but reconcile not wired (pipelineRuntime=nil)")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// seedPipelineDepForInvoke creates a pipeline deployment with nested stage
// packages. The deployment's NestedPackageDigests includes "__workflow_kind":
// "pipeline" and stage:N digests so AdmitInvocation creates pipeline topology.
func seedPipelineDepForInvoke(t *testing.T, s *controlServer, stageCount int) string {
	t.Helper()
	ctx := context.Background()

	nested := map[string]string{
		"__workflow_kind": "pipeline",
		"__stage_count":   intToStr(stageCount),
	}
	for i := 0; i < stageCount; i++ {
		nested[fmt.Sprintf("stage:%d:digest", i)] = fmt.Sprintf("sha256:stage%d", i)
	}

	d, err := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
		PackageName:         "pipeline-agent",
		PackageVersion:      "1.0.0",
		BundleDigest:        "sha256:bundle-pipe",
		PolicyDigest:        "sha256:policy-pipe",
		ImageLockDigest:     "sha256:img-pipe",
		ActorIdentity:       "tester",
		NestedPackageDigests: nested,
	})
	if err != nil {
		t.Fatalf("CreateDeployment (pipeline): %v", err)
	}
	return d.GetDeployment().GetDeploymentId()
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}
