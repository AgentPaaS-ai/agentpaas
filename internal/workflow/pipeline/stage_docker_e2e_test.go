package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// All tests in this file require AGENTPAAS_DOCKER_TESTS=1 and a running
// Docker daemon (e.g. Colima). They create real Docker containers and
// networks to prove multi-stage isolation via RuntimeStageLauncher.

// ---------------------------------------------------------------------------
// Helper: dockerInspectRunning returns true if the container is running.
// ---------------------------------------------------------------------------

func dockerInspectRunning(ctx context.Context, t *testing.T, cid string) bool {
	t.Helper()
	// Retry up to 5s; Docker daemon may lag behind the SDK's Create+Start.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "docker", "inspect",
			"-f", "{{.State.Running}}", cid).CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out)) == "true"
		}
		// Container not found yet — wait and retry.
		if strings.Contains(string(out), "No such object") || strings.Contains(err.Error(), "exit status 1") {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		t.Logf("docker inspect %s: %v (output: %s)", cid[:12], err, strings.TrimSpace(string(out)))
		return false
	}
	t.Logf("docker inspect %s: timed out after 5s", cid[:12])
	return false
}

// ---------------------------------------------------------------------------
// Test 1: Two-stage isolation — separate containers and networks
// ---------------------------------------------------------------------------

func TestDockerE2E_TwoStageIsolation_SeparateContainersAndNetworks(t *testing.T) {
	requireDockerE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── Setup real Docker runtime ────────────────────────────────────────
	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	if dr == nil {
		t.Fatal("NewDockerRuntime() returned nil")
	}
	if ver, verr := dr.ServerVersion(ctx); verr != nil {
		t.Logf("Warning: ServerVersion: %v", verr)
	} else {
		t.Logf("Docker Engine version: %s", ver)
	}

	workflowID := fmt.Sprintf("b34-e2e-%d", time.Now().UnixNano())
	runID0 := workflowID + "-run0"
	runID1 := workflowID + "-run1"
	nodeID0 := "node-stage-0"
	nodeID1 := "node-stage-1"

	// ── Tracking for cleanup ─────────────────────────────────────────────
	var createdNetworks []runtime.NetworkID
	var createdContainers []runtime.ContainerID

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		for _, cid := range createdContainers {
			_ = dr.Stop(cleanCtx, cid, nil)
			_ = dr.Remove(cleanCtx, cid, true)
		}
		for _, nid := range createdNetworks {
			_ = dr.RemoveNetwork(cleanCtx, nid)
		}
		// Final orphan check.
		time.Sleep(2 * time.Second)
		containers, _ := dr.ListContainers(cleanCtx,
			runtime.LabelWorkflowID+"="+workflowID)
		if len(containers) > 0 {
			ids := make([]string, len(containers))
			for i, c := range containers {
				ids[i] = c.ID[:12]
			}
			t.Errorf("orphan containers for workflow %s: %v", workflowID, ids)
		}
		networks, _ := dr.ListNetworks(cleanCtx,
			runtime.LabelWorkflowID+"="+workflowID)
		if len(networks) > 0 {
			names := make([]string, len(networks))
			for i, n := range networks {
				names[i] = n.Name
			}
			t.Errorf("orphan networks for workflow %s: %v", workflowID, names)
		}
	}()

	// ── Create distinct networks for each stage ──────────────────────────
	net0Name := runtime.NetworkName("stage", "0-"+workflowID)
	net0Spec := runtime.NetworkSpec{
		Name:     net0Name,
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeNetInternal,
			runtime.LabelWorkflowID:   workflowID,
		},
	}
	net0ID, err := dr.CreateNetwork(ctx, net0Spec)
	if err != nil {
		t.Fatalf("CreateNetwork(net0): %v", err)
	}
	createdNetworks = append(createdNetworks, net0ID)
	t.Logf("Created network %s (ID: %s)", net0Name, net0ID)

	net1Name := runtime.NetworkName("stage", "1-"+workflowID)
	net1Spec := runtime.NetworkSpec{
		Name:     net1Name,
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeNetInternal,
			runtime.LabelWorkflowID:   workflowID,
		},
	}
	net1ID, err := dr.CreateNetwork(ctx, net1Spec)
	if err != nil {
		t.Fatalf("CreateNetwork(net1): %v", err)
	}
	createdNetworks = append(createdNetworks, net1ID)
	t.Logf("Created network %s (ID: %s)", net1Name, net1ID)

	// ── Assert distinct NetworkIDs ───────────────────────────────────────
	if net0ID == net1ID {
		t.Fatalf("expected distinct NetworkIDs, both got %s", net0ID)
	}

	// ── Create launcher ──────────────────────────────────────────────────
	launcher := NewRuntimeStageLauncher(dr)

	// ── Launch stage 0 on net0 ───────────────────────────────────────────
	job0 := &StageLaunchJob{
		Key:        LaunchIdempotencyKey(routedrun.WorkflowID(workflowID), routedrun.NodeID(nodeID0), 1),
		WorkflowID: routedrun.WorkflowID(workflowID),
		NodeID:     routedrun.NodeID(nodeID0),
		RunID:      routedrun.RunID(runID0),
		AttemptID:  routedrun.AttemptID("at-0"),
		Generation: 1,
		Image:      "alpine:3.20",
		Command:    []string{"sleep", "120"},
		NetworkID:  string(net0ID),
		StageOrder: 0,
		Status:     LaunchStatusPending,
	}

	if err := launcher.EnsureLaunch(ctx, job0); err != nil {
		t.Fatalf("EnsureLaunch(stage0): %v", err)
	}
	if job0.ContainerID == "" {
		t.Fatal("stage0 ContainerID not set after launch")
	}
	createdContainers = append(createdContainers, runtime.ContainerID(job0.ContainerID))
	t.Logf("Stage 0 container: %s", job0.ContainerID)

	// ── Verify stage 0 is running ────────────────────────────────────────
	time.Sleep(1 * time.Second)
	if !dockerInspectRunning(ctx, t, job0.ContainerID) {
		t.Fatal("stage0 container not running after launch")
	}
	status0, err := dr.Status(ctx, runtime.ContainerID(job0.ContainerID))
	if err != nil {
		t.Fatalf("Status(stage0): %v", err)
	}
	if status0 != runtime.ContainerStatusRunning {
		t.Errorf("stage0 status = %s, want running", status0)
	}

	// ── Verify labels on stage0 container ────────────────────────────────
	containers0, err := dr.ListContainers(ctx,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelNodeID+"="+nodeID0,
	)
	if err != nil {
		t.Fatalf("ListContainers(stage0): %v", err)
	}
	if len(containers0) != 1 {
		t.Fatalf("expected 1 stage0 container, got %d", len(containers0))
	}
	labels0 := containers0[0].Labels
	for _, want := range []string{
		runtime.LabelManagedBy,
		runtime.LabelResourceType,
		runtime.LabelRunID,
		runtime.LabelWorkflowID,
		runtime.LabelNodeID,
		runtime.LabelAttemptID,
		runtime.LabelPipelineStage,
		runtime.LabelStageOrder,
	} {
		if v, ok := labels0[want]; !ok {
			t.Errorf("stage0 label %q missing", want)
		} else {
			t.Logf("stage0 label %s = %s", want, v)
		}
	}
	// Verify no secret-like values in labels.
	for k, v := range labels0 {
		if strings.HasPrefix(v, "sk-") {
			t.Errorf("stage0 label %q contains secret-like value", k)
		}
	}

	// ── Verify stage0 is on net0 ─────────────────────────────────────────
	networks0, err := dr.InspectContainerNetworks(ctx, runtime.ContainerID(job0.ContainerID))
	if err != nil {
		t.Fatalf("InspectContainerNetworks(stage0): %v", err)
	}
	foundNet0 := false
	for _, n := range networks0 {
		if n.ID == string(net0ID) {
			foundNet0 = true
		}
	}
	if !foundNet0 {
		t.Errorf("stage0 not attached to net0 (%s)", net0ID)
	}

	// ── Fence stage 0 ────────────────────────────────────────────────────
	if err := launcher.FenceStage(ctx, workflowID, nodeID0); err != nil {
		t.Fatalf("FenceStage(stage0): %v", err)
	}
	time.Sleep(2 * time.Second) // give Docker time to process

	// ── Verify stage 0 is gone ───────────────────────────────────────────
	if dockerInspectRunning(ctx, t, job0.ContainerID) {
		t.Error("stage0 container still running after fence")
	}
	containers0After, err := dr.ListContainers(ctx,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelNodeID+"="+nodeID0,
	)
	if err != nil {
		t.Fatalf("ListContainers(stage0) after fence: %v", err)
	}
	if len(containers0After) > 0 {
		t.Errorf("stage0 containers still present after fence: %d", len(containers0After))
	}

	// Remove stage0 from tracking since it's been fenced.
	createdContainers = nil

	// ── Launch stage 1 on net1 ───────────────────────────────────────────
	job1 := &StageLaunchJob{
		Key:        LaunchIdempotencyKey(routedrun.WorkflowID(workflowID), routedrun.NodeID(nodeID1), 1),
		WorkflowID: routedrun.WorkflowID(workflowID),
		NodeID:     routedrun.NodeID(nodeID1),
		RunID:      routedrun.RunID(runID1),
		AttemptID:  routedrun.AttemptID("at-1"),
		Generation: 1,
		Image:      "alpine:3.20",
		Command:    []string{"sleep", "120"},
		NetworkID:  string(net1ID),
		StageOrder: 1,
		Status:     LaunchStatusPending,
	}

	if err := launcher.EnsureLaunch(ctx, job1); err != nil {
		t.Fatalf("EnsureLaunch(stage1): %v", err)
	}
	if job1.ContainerID == "" {
		t.Fatal("stage1 ContainerID not set after launch")
	}
	createdContainers = append(createdContainers, runtime.ContainerID(job1.ContainerID))
	t.Logf("Stage 1 container: %s", job1.ContainerID)

	// ── Verify stage 1 is running ────────────────────────────────────────
	time.Sleep(1 * time.Second)
	if !dockerInspectRunning(ctx, t, job1.ContainerID) {
		t.Fatal("stage1 container not running after launch")
	}
	status1, err := dr.Status(ctx, runtime.ContainerID(job1.ContainerID))
	if err != nil {
		t.Fatalf("Status(stage1): %v", err)
	}
	if status1 != runtime.ContainerStatusRunning {
		t.Errorf("stage1 status = %s, want running", status1)
	}

	// ── Distinct container IDs ───────────────────────────────────────────
	if job0.ContainerID == job1.ContainerID {
		t.Errorf("stage0 and stage1 share same ContainerID: %s", job0.ContainerID)
	}

	// ── Stage 1 is on net1, not net0 ────────────────────────────────────
	networks1, err := dr.InspectContainerNetworks(ctx, runtime.ContainerID(job1.ContainerID))
	if err != nil {
		t.Fatalf("InspectContainerNetworks(stage1): %v", err)
	}
	foundNet1 := false
	for _, n := range networks1 {
		if n.ID == string(net1ID) {
			foundNet1 = true
		}
	}
	if !foundNet1 {
		t.Errorf("stage1 not attached to net1 (%s)", net1ID)
	}

	// ── Stage 0 is still gone ────────────────────────────────────────────
	if dockerInspectRunning(ctx, t, job0.ContainerID) {
		t.Error("stage0 container reappeared after stage1 launch")
	}

	// ── Both stages have distinct NetworkIDs ─────────────────────────────
	if string(net0ID) == string(net1ID) {
		t.Error("net0 and net1 have same ID — networks not distinct")
	}

	t.Log("SUCCESS: two-stage Docker isolation with distinct containers and networks")
}

// ---------------------------------------------------------------------------
// Test 2: FenceStage idempotent, leaves no orphans
// ---------------------------------------------------------------------------

func TestDockerE2E_FenceStage_IdempotentAndNoOrphans(t *testing.T) {
	requireDockerE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	workflowID := fmt.Sprintf("b34-e2e-fence-%d", time.Now().UnixNano())
	nodeID := "node-fence"

	var createdNetworks []runtime.NetworkID
	var createdContainers []runtime.ContainerID

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		for _, cid := range createdContainers {
			_ = dr.Stop(cleanCtx, cid, nil)
			_ = dr.Remove(cleanCtx, cid, true)
		}
		for _, nid := range createdNetworks {
			_ = dr.RemoveNetwork(cleanCtx, nid)
		}
	}()

	// ── Create network ───────────────────────────────────────────────────
	netName := runtime.NetworkName("fence", workflowID)
	netID, err := dr.CreateNetwork(ctx, runtime.NetworkSpec{
		Name:     netName,
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeNetInternal,
			runtime.LabelWorkflowID:   workflowID,
		},
	})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	createdNetworks = append(createdNetworks, netID)

	// ── Launch one stage ─────────────────────────────────────────────────
	launcher := NewRuntimeStageLauncher(dr)

	job := &StageLaunchJob{
		Key:        LaunchIdempotencyKey(routedrun.WorkflowID(workflowID), routedrun.NodeID(nodeID), 1),
		WorkflowID: routedrun.WorkflowID(workflowID),
		NodeID:     routedrun.NodeID(nodeID),
		RunID:      routedrun.RunID(workflowID + "-run"),
		AttemptID:  routedrun.AttemptID("at-fence"),
		Generation: 1,
		Image:      "alpine:3.20",
		Command:    []string{"sleep", "120"},
		NetworkID:  string(netID),
		StageOrder: 0,
		Status:     LaunchStatusPending,
	}

	if err := launcher.EnsureLaunch(ctx, job); err != nil {
		t.Fatalf("EnsureLaunch: %v", err)
	}
	createdContainers = append(createdContainers, runtime.ContainerID(job.ContainerID))
	t.Logf("Container: %s", job.ContainerID)

	time.Sleep(1 * time.Second)
	if !dockerInspectRunning(ctx, t, job.ContainerID) {
		t.Fatal("container not running after launch")
	}

	// ── Fence twice — should be idempotent ───────────────────────────────
	if err := launcher.FenceStage(ctx, workflowID, nodeID); err != nil {
		t.Fatalf("FenceStage (first): %v", err)
	}
	createdContainers = nil // already removed by fence

	// Second fence should not error (container already gone).
	if err := launcher.FenceStage(ctx, workflowID, nodeID); err != nil {
		t.Fatalf("FenceStage (second): %v", err)
	}

	// ── List by workflow label → empty ───────────────────────────────────
	time.Sleep(2 * time.Second)
	containers, err := dr.ListContainers(ctx,
		runtime.LabelWorkflowID+"="+workflowID,
		runtime.LabelNodeID+"="+nodeID,
	)
	if err != nil {
		t.Fatalf("ListContainers after fence: %v", err)
	}
	if len(containers) > 0 {
		ids := make([]string, len(containers))
		for i, c := range containers {
			ids[i] = c.ID[:12]
		}
		t.Errorf("orphan containers after fence: %v", ids)
	}

	t.Log("SUCCESS: FenceStage idempotent, zero orphans")
}

// ---------------------------------------------------------------------------
// Test 3: EnsureLaunch idempotent — same job key twice → one container
// ---------------------------------------------------------------------------

func TestDockerE2E_EnsureLaunch_Idempotent(t *testing.T) {
	requireDockerE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	workflowID := fmt.Sprintf("b34-e2e-idem-%d", time.Now().UnixNano())
	nodeID := "node-idem"

	var createdNetworks []runtime.NetworkID
	var createdContainers []runtime.ContainerID

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		for _, cid := range createdContainers {
			_ = dr.Stop(cleanCtx, cid, nil)
			_ = dr.Remove(cleanCtx, cid, true)
		}
		for _, nid := range createdNetworks {
			_ = dr.RemoveNetwork(cleanCtx, nid)
		}
		// Zero-orphans check.
		time.Sleep(2 * time.Second)
		containers, _ := dr.ListContainers(cleanCtx,
			runtime.LabelWorkflowID+"="+workflowID)
		if len(containers) > 0 {
			ids := make([]string, len(containers))
			for i, c := range containers {
				ids[i] = c.ID[:12]
			}
			t.Errorf("orphan containers after idempotency test: %v", ids)
		}
	}()

	// ── Create network ───────────────────────────────────────────────────
	netName := runtime.NetworkName("idem", workflowID)
	netID, err := dr.CreateNetwork(ctx, runtime.NetworkSpec{
		Name:     netName,
		Internal: true,
		Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelResourceType: runtime.ResourceTypeNetInternal,
			runtime.LabelWorkflowID:   workflowID,
		},
	})
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	createdNetworks = append(createdNetworks, netID)

	// ── First launch ─────────────────────────────────────────────────────
	launcher := NewRuntimeStageLauncher(dr)

	job := &StageLaunchJob{
		Key:        LaunchIdempotencyKey(routedrun.WorkflowID(workflowID), routedrun.NodeID(nodeID), 1),
		WorkflowID: routedrun.WorkflowID(workflowID),
		NodeID:     routedrun.NodeID(nodeID),
		RunID:      routedrun.RunID(workflowID + "-run"),
		AttemptID:  routedrun.AttemptID("at-idem"),
		Generation: 1,
		Image:      "alpine:3.20",
		Command:    []string{"sleep", "120"},
		NetworkID:  string(netID),
		StageOrder: 0,
		Status:     LaunchStatusPending,
	}

	if err := launcher.EnsureLaunch(ctx, job); err != nil {
		t.Fatalf("EnsureLaunch (first): %v", err)
	}
	cidFirst := job.ContainerID
	createdContainers = append(createdContainers, runtime.ContainerID(cidFirst))
	t.Logf("First launch container: %s", cidFirst)

	time.Sleep(1 * time.Second)
	if !dockerInspectRunning(ctx, t, cidFirst) {
		t.Fatal("container not running after first launch")
	}

	// ── Count containers for workflow ────────────────────────────────────
	containersBefore, err := dr.ListContainers(ctx,
		runtime.LabelWorkflowID+"="+workflowID,
	)
	if err != nil {
		t.Fatalf("ListContainers before second launch: %v", err)
	}
	countBefore := len(containersBefore)
	t.Logf("Containers before second launch: %d", countBefore)

	// ── Second launch — same key, must be idempotent ─────────────────────
	if err := launcher.EnsureLaunch(ctx, job); err != nil {
		t.Fatalf("EnsureLaunch (second): %v", err)
	}

	// ── Same ContainerID ─────────────────────────────────────────────────
	if job.ContainerID != cidFirst {
		t.Errorf("second launch changed ContainerID from %s to %s", cidFirst, job.ContainerID)
	}

	// ── Still exactly one container ──────────────────────────────────────
	containersAfter, err := dr.ListContainers(ctx,
		runtime.LabelWorkflowID+"="+workflowID,
	)
	if err != nil {
		t.Fatalf("ListContainers after second launch: %v", err)
	}
	if len(containersAfter) != 1 {
		t.Errorf("expected 1 container after second launch, got %d: %v",
			len(containersAfter), containersAfter)
	}

	t.Log("SUCCESS: EnsureLaunch idempotent — same key, one container")
}

// ---------------------------------------------------------------------------
// B34.5-C Docker e2e: Reconciler + RuntimeStageLauncher integration
// ---------------------------------------------------------------------------

// TestB345DockerE2E_ReconcileLaunchesStageContainer verifies that the
// Reconciler with RuntimeStageLauncher creates a real Docker container for
// stage0 with correct labels, image, command, and network isolation.
func TestB345DockerE2E_ReconcileLaunchesStageContainer(t *testing.T) {
	requireDockerE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// ── Setup Docker runtime ────────────────────────────────────────────
	dr, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime: %v", err)
	}

	workflowID := fmt.Sprintf("b345-e2e-%d", time.Now().UnixNano())

	// ── Tracking for cleanup ────────────────────────────────────────────
	var createdContainers []runtime.ContainerID
	var createdNetworks []runtime.NetworkID

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		for _, cid := range createdContainers {
			_ = dr.Stop(cleanCtx, cid, nil)
			_ = dr.Remove(cleanCtx, cid, true)
		}
		for _, nid := range createdNetworks {
			_ = dr.RemoveNetwork(cleanCtx, nid)
		}
		// Final orphan check.
		time.Sleep(2 * time.Second)
		containers, _ := dr.ListContainers(cleanCtx, runtime.LabelWorkflowID+"="+workflowID)
		if len(containers) > 0 {
			ids := make([]string, len(containers))
			for i, c := range containers {
				ids[i] = c.ID[:12]
			}
			t.Errorf("orphan containers for workflow %s: %v", workflowID, ids)
		}
		networks, _ := dr.ListNetworks(cleanCtx, runtime.LabelWorkflowID+"="+workflowID)
		if len(networks) > 0 {
			names := make([]string, len(networks))
			for i, n := range networks {
				names[i] = n.Name
			}
			t.Errorf("orphan networks for workflow %s: %v", workflowID, names)
		}
	}()

	// ── Seed a pipeline workflow via controller ─────────────────────────
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()

	// Use SeedPipelineWorkflow but override the workflow ID to a known prefix
	// for label-based filtering. The seed creates a random wfID; we'll use
	// its Store for controller operations and register it manually.
	// Since SeedPipelineWorkflow uses a random wfID, we instead create
	// nodes manually with a known workflowID.

	// Create workflow.
	wfID := routedrun.WorkflowID(workflowID)
	now := time.Now().UTC()
	wf := &routedrun.WorkflowRecord{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		WorkflowID:          wfID,
		WorkflowKind:        "pipeline",
		Status:              routedrun.WorkflowStatusRunning,
		Generation:          1,
		MaxActiveDurationMs: 3600000,
		MaxAttemptLeaseMs:   600000,
		AuthorityGeneration: 1,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Create stage0 (READY) and stage1 (PENDING).
	node0ID, _ := routedrun.NewNodeID()
	run0ID, _ := routedrun.NewRunID()
	node0 := &routedrun.PipelineNode{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		NodeID:        node0ID,
		WorkflowID:    wfID,
		Status:        routedrun.NodeStatusReady,
		RunID:         run0ID,
		StageOrder:    0,
		PackageName:   "test-pkg",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.CreateNode(ctx, node0); err != nil {
		t.Fatalf("CreateNode stage0: %v", err)
	}
	run0 := &routedrun.RunRecord{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		RunID:               run0ID,
		WorkflowID:          wfID,
		Status:              routedrun.RunStatusPending,
		RunKind:             "pipeline_stage",
		MaxActiveDurationMs: 3600000,
		MaxAttemptLeaseMs:   600000,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	nid0 := node0ID
	run0.NodeID = &nid0
	if err := store.CreateRun(ctx, run0); err != nil {
		t.Fatalf("CreateRun stage0: %v", err)
	}

	node1ID, _ := routedrun.NewNodeID()
	run1ID, _ := routedrun.NewRunID()
	node1 := &routedrun.PipelineNode{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		NodeID:        node1ID,
		WorkflowID:    wfID,
		Status:        routedrun.NodeStatusPending,
		RunID:         run1ID,
		StageOrder:    1,
		PackageName:   "test-pkg",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.CreateNode(ctx, node1); err != nil {
		t.Fatalf("CreateNode stage1: %v", err)
	}
	run1 := &routedrun.RunRecord{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		RunID:               run1ID,
		WorkflowID:          wfID,
		Status:              routedrun.RunStatusPending,
		RunKind:             "pipeline_stage",
		MaxActiveDurationMs: 3600000,
		MaxAttemptLeaseMs:   600000,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	nid1 := node1ID
	run1.NodeID = &nid1
	if err := store.CreateRun(ctx, run1); err != nil {
		t.Fatalf("CreateRun stage1: %v", err)
	}

	// Init controller generations.
	ctrl.initSeedGenerations([]routedrun.NodeID{node0ID, node1ID}, map[routedrun.NodeID]routedrun.RunID{
		node0ID: run0ID,
		node1ID: run1ID,
	})

	// ── Create reconciler with RuntimeStageLauncher ─────────────────────
	launcher := NewRuntimeStageLauncher(dr)
	reconciler := &Reconciler{
		Ctrl:          ctrl,
		Launches:      launches,
		Launcher:      launcher,
		NetworkDriver: dr,
	}

	// ── ReconcileOnce → claim stage0, create network, launch container ──
	t.Logf("Calling ReconcileOnce for workflow %s", wfID)
	claim, err := reconciler.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
	if claim == nil {
		t.Fatal("expected claim, got nil")
	}
	t.Logf("Claimed node: %s runID=%s", claim.NodeID, claim.RunID)

	// ── Verify launch job ──────────────────────────────────────────────
	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 launch job, got %d", len(jobs))
	}
	job := jobs[0]

	// Stage0 should have ContainerID set.
	if job.ContainerID == "" {
		t.Fatal("launch job ContainerID is empty after EnsureLaunch")
	}
	cid := runtime.ContainerID(job.ContainerID)
	createdContainers = append(createdContainers, cid)
	t.Logf("Stage0 container ID: %s", cid)

	// Stage0 should have NetworkID.
	if job.NetworkID == "" {
		t.Fatal("launch job NetworkID is empty")
	}
	createdNetworks = append(createdNetworks, runtime.NetworkID(job.NetworkID))

	// ── Verify container is running ────────────────────────────────────
	status, err := dr.Status(ctx, cid)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != runtime.ContainerStatusRunning {
		t.Fatalf("container status=%v want RUNNING", status)
	}

	// ── Verify container has correct labels ────────────────────────────
	containers, err := dr.ListContainers(ctx, runtime.LabelWorkflowID+"="+workflowID)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container for workflow, got %d", len(containers))
	}
	labels := containers[0].Labels
	if labels[runtime.LabelWorkflowID] != workflowID {
		t.Errorf("LabelWorkflowID=%q want %q", labels[runtime.LabelWorkflowID], workflowID)
	}
	if labels[runtime.LabelNodeID] != string(node0ID) {
		t.Errorf("LabelNodeID=%q want %q", labels[runtime.LabelNodeID], node0ID)
	}
	if labels[runtime.LabelRunID] != string(run0ID) {
		t.Errorf("LabelRunID=%q want %q", labels[runtime.LabelRunID], run0ID)
	}

	// ── Verify node0 is RUNNING ────────────────────────────────────────
	node0After, err := store.GetNode(ctx, node0ID)
	if err != nil {
		t.Fatalf("GetNode stage0: %v", err)
	}
	if node0After.Status != routedrun.NodeStatusRunning {
		t.Errorf("stage0 status=%s want RUNNING", node0After.Status)
	}

	// ── Fence stage0 ───────────────────────────────────────────────────
	t.Log("Fencing stage0")
	if err := launcher.FenceStage(ctx, workflowID, string(node0ID)); err != nil {
		t.Fatalf("FenceStage: %v", err)
	}
	// Clear container tracking for cleanup (already removed by Fence).
	createdContainers = nil

	// ── Verify container is gone ───────────────────────────────────────
	time.Sleep(1 * time.Second)
	status, _ = dr.Status(ctx, cid)
	if status != runtime.ContainerStatusRemoved && status != runtime.ContainerStatusUnknown {
		t.Errorf("after fence: container status=%v want REMOVED", status)
	}

	t.Logf("B34.5 Docker E2E: stage0 launched, labeled, fenced — SUCCESS")
}
