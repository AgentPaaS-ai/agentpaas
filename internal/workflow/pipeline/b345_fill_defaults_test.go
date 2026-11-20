package pipeline

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestB345_FillStageJobDefaults_FillsImageAndCommand verifies that
// fillStageJobDefaults sets the default image (alpine:3.20) and command
// (["sleep","30"]) when the job's Image and Command fields are empty.
func TestB345_FillStageJobDefaults_FillsImageAndCommand(t *testing.T) {
	r := &Reconciler{} // NetworkDriver=nil, so no network creation
	ctx := context.Background()

	job := &StageLaunchJob{
		Key:        "test-key",
		WorkflowID: "wf-1",
		NodeID:     "node-0",
		RunID:      "run-0",
		StageOrder: 0,
	}

	r.fillStageJobDefaults(ctx, job)

	if job.Image == "" {
		t.Fatal("expected Image to be filled")
	}
	if job.Image != defaultStageImage {
		t.Fatalf("Image=%q want %q", job.Image, defaultStageImage)
	}
	if len(job.Command) == 0 {
		t.Fatal("expected Command to be filled")
	}
	if job.Command[0] != "sleep" || job.Command[1] != "30" {
		t.Fatalf("Command=%v want [sleep 30]", job.Command)
	}
}

// TestB345_FillStageJobDefaults_PreservesExistingImage verifies that
// fillStageJobDefaults does not overwrite an already-set Image.
func TestB345_FillStageJobDefaults_PreservesExistingImage(t *testing.T) {
	r := &Reconciler{}
	ctx := context.Background()

	job := &StageLaunchJob{
		Key:        "test-key",
		WorkflowID: "wf-1",
		NodeID:     "node-0",
		RunID:      "run-0",
		StageOrder: 0,
		Image:      "my-custom-image:v1",
	}

	r.fillStageJobDefaults(ctx, job)

	if job.Image != "my-custom-image:v1" {
		t.Fatalf("Image=%q want my-custom-image:v1 (should not be overwritten)", job.Image)
	}
}

// TestB345_FillStageJobDefaults_StoresStageOrder verifies that stage order
// is preserved on the job after defaults are filled.
func TestB345_FillStageJobDefaults_StoresStageOrder(t *testing.T) {
	r := &Reconciler{}
	ctx := context.Background()

	for _, order := range []int{0, 1, 2} {
		job := &StageLaunchJob{
			Key:        "test-key",
			WorkflowID: "wf-1",
			NodeID:     "node-0",
			RunID:      "run-0",
			StageOrder: order,
		}
		r.fillStageJobDefaults(ctx, job)
		if job.StageOrder != order {
			t.Errorf("StageOrder=%d want %d", job.StageOrder, order)
		}
	}
}

// TestB345_ReconcileOnce_FillsDefaultsOnJob verifies that ReconcileOnce fills
// defaults (Image, Command) on the StageLaunchJob when creating it via the
// claim-next-ready path.
func TestB345_ReconcileOnce_FillsDefaultsOnJob(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	r := &Reconciler{
		Ctrl:     ctrl,
		Launches: launches,
		Launcher: FakeLauncher{},
	}

	// Seed a 2-stage pipeline workflow.
	wfID, _, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// First reconcile should claim stage0 and fill defaults.
	claim, err := r.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}

	// Verify the launch job has defaults filled.
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Image == "" {
		t.Fatal("launch job Image is empty")
	}
	if len(job.Command) == 0 {
		t.Fatal("launch job Command is empty")
	}
	if job.NodeID != claim.NodeID {
		t.Fatalf("launch job NodeID=%s want %s", job.NodeID, claim.NodeID)
	}
	if job.StageOrder != 0 {
		t.Fatalf("launch job StageOrder=%d want 0", job.StageOrder)
	}

	t.Logf("reconcile claim: nodeID=%s image=%s command=%v stageOrder=%d",
		claim.NodeID, job.Image, job.Command, job.StageOrder)
}
