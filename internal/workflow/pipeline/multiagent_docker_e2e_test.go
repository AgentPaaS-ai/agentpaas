package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// TestB34MultiAgentE2E_ThreeStageHermesAbsent proves the full multi-agent
// pipeline pattern on real Docker, Hermes-absent:
//
//   1 parent workflow, 3 sequential stage agents (separate containers +
//   networks), handoffs between stages, workflow SUCCEEDED, inspectable
//   evidence, cleanup with zero orphans.
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestB34MultiAgentE2E_ThreeStageHermesAbsent(t *testing.T) {
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

	// ── Setup controller + Reconciler with real launcher ─────────────────
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	launcher := NewRuntimeStageLauncher(dr)
	reconciler := &Reconciler{
		Ctrl:          ctrl,
		Launches:      launches,
		Launcher:      launcher,
		NetworkDriver: dr,
	}

	// ── Seed 3-stage pipeline ────────────────────────────────────────────
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	if len(nodeIDs) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodeIDs))
	}
	t.Logf("Workflow: %s  Nodes: %v", wfID, nodeIDs)

	// ── Tracking for cleanup ─────────────────────────────────────────────
	var createdNetworks []runtime.NetworkID
	// Launch key dedup tracking.
	launchKeys := make(map[string]int)
	// Record container IDs per stage for distinctness checks.
	stageContainers := make([]string, 3)

	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		for _, nid := range createdNetworks {
			_ = dr.RemoveNetwork(cleanCtx, nid)
		}
		// Final orphan check.
		time.Sleep(2 * time.Second)
		containers, _ := dr.ListContainers(cleanCtx,
			runtime.LabelWorkflowID+"="+string(wfID))
		if len(containers) > 0 {
			ids := make([]string, len(containers))
			for i, c := range containers {
				ids[i] = c.ID[:12]
			}
			t.Errorf("orphan containers for workflow %s: %v", wfID, ids)
		}
		networks, _ := dr.ListNetworks(cleanCtx,
			runtime.LabelWorkflowID+"="+string(wfID))
		if len(networks) > 0 {
			names := make([]string, len(networks))
			for i, n := range networks {
				names[i] = n.Name
			}
			t.Errorf("orphan networks for workflow %s: %v", wfID, names)
		}
	}()

	// ── Stage loop: claim+launch → commit → fence ───────────────────────
	for stage := 0; stage < 3; stage++ {
		t.Logf("--- Stage %d ---", stage)

		// ── ReconcileOnce: claim, create network, launch container, ack ──
		claim, err := reconciler.ReconcileOnce(ctx, wfID)
		if err != nil {
			t.Fatalf("stage %d ReconcileOnce: %v", stage, err)
		}
		if claim == nil {
			t.Fatalf("stage %d ReconcileOnce: expected claim, got nil", stage)
		}
		if claim.NodeID != nodeIDs[stage] {
			t.Errorf("stage %d: claim.NodeID=%s, want %s", stage, claim.NodeID, nodeIDs[stage])
		}

		// Track launch key for duplicate detection.
		if prevStage, exists := launchKeys[claim.LaunchKey]; exists {
			t.Errorf("stage %d: duplicate launch key %q (also used at stage %d)",
				stage, claim.LaunchKey, prevStage)
		}
		launchKeys[claim.LaunchKey] = stage

		t.Logf("  claim: node=%s run=%s launchKey=%s",
			claim.NodeID, claim.RunID, claim.LaunchKey)

		// ── Verify node is RUNNING ───────────────────────────────────────
		node, err := store.GetNode(ctx, claim.NodeID)
		if err != nil {
			t.Fatalf("stage %d GetNode: %v", stage, err)
		}
		if node.Status != routedrun.NodeStatusRunning {
			t.Errorf("stage %d post-claim: want RUNNING, got %s", stage, node.Status)
		}

		// ── Verify launch job has container ID set ───────────────────────
		job, err := launches.Get(ctx, claim.LaunchKey)
		if err != nil {
			t.Fatalf("stage %d get launch job: %v", stage, err)
		}
		if job.ContainerID == "" {
			t.Fatal("launch job ContainerID is empty after EnsureLaunch")
		}
		cid := job.ContainerID
		stageContainers[stage] = cid
		t.Logf("  container: %s", cid)

		// ── Assert container ID distinct from prior stages ───────────────
		for prev := 0; prev < stage; prev++ {
			if stageContainers[prev] != "" && stageContainers[prev] == cid {
				t.Errorf("stage %d container %s == stage %d container — not distinct",
					stage, cid, prev)
			}
		}

		// ── Verify container is actually running ─────────────────────────
		time.Sleep(1 * time.Second)
		if !dockerInspectRunning(ctx, t, cid) {
			t.Fatalf("stage %d container %s not running after launch", stage, cid[:12])
		}
		status, err := dr.Status(ctx, runtime.ContainerID(cid))
		if err != nil {
			t.Fatalf("stage %d Status: %v", stage, err)
		}
		if status != runtime.ContainerStatusRunning {
			t.Errorf("stage %d status = %s, want running", stage, status)
		}

		// ── Verify labels include workflow_id, node_id, stage_order ──────
		containers, err := dr.ListContainers(ctx,
			runtime.LabelWorkflowID+"="+string(wfID),
			runtime.LabelNodeID+"="+string(claim.NodeID),
		)
		if err != nil {
			t.Fatalf("stage %d ListContainers: %v", stage, err)
		}
		if len(containers) != 1 {
			t.Fatalf("stage %d: expected 1 container, got %d", stage, len(containers))
		}
		labels := containers[0].Labels
		wantStageOrder := strconv.Itoa(stage)
		for _, want := range []string{
			runtime.LabelWorkflowID,
			runtime.LabelNodeID,
			runtime.LabelRunID,
			runtime.LabelAttemptID,
			runtime.LabelPipelineStage,
			runtime.LabelStageOrder,
		} {
			if v, ok := labels[want]; !ok {
				t.Errorf("stage %d label %q missing", stage, want)
			} else {
				t.Logf("  label %s = %s", want, v)
				if want == runtime.LabelStageOrder && v != wantStageOrder {
					t.Errorf("stage %d label %q: want %q, got %q",
						stage, want, wantStageOrder, v)
				}
			}
		}
		// Verify no secret-like values in labels.
		for k, v := range labels {
			if strings.HasPrefix(v, "sk-") {
				t.Errorf("stage %d label %q contains secret-like value", stage, k)
			}
		}

		// ── Assert NetworkID is set and distinct per stage ───────────────
		if job.NetworkID == "" {
			t.Fatalf("stage %d launch job NetworkID is empty", stage)
		}
		createdNetworks = append(createdNetworks, runtime.NetworkID(job.NetworkID))
		t.Logf("  network: %s", job.NetworkID)
		for prev := 0; prev < stage; prev++ {
			prevJob, _ := launches.Get(ctx, launchKeyForNode(wfID, nodeIDs[prev], 1))
			if prevJob != nil && prevJob.NetworkID != "" && prevJob.NetworkID == job.NetworkID {
				t.Errorf("stage %d and stage %d share same NetworkID: %s",
					stage, prev, job.NetworkID)
			}
		}

		// ── Commit stage success ─────────────────────────────────────────
		success := StageSuccess{
			WorkflowID: wfID,
			NodeID:     claim.NodeID,
			RunID:      claim.RunID,
			AttemptID:  claim.Attempt.AttemptID,
		}

		// Non-final stages require a handoff.
		if stage < 2 {
			nextNodeID := nodeIDs[stage+1]
			success.Handoff = &routedrun.HandoffEnvelope{
				WorkflowID:   wfID,
				SourceNodeID: claim.NodeID,
				TargetNodeID: nextNodeID,
				ContextJSON:  fmt.Sprintf(`{"stage":%d,"marker":"agent-%d"}`, stage, stage),
			}
		}

		if err := ctrl.CommitStageSuccess(ctx, success); err != nil {
			t.Fatalf("stage %d CommitStageSuccess: %v", stage, err)
		}
		t.Logf("  commit: success")

		// ── Fence stage container ────────────────────────────────────────
		if err := launcher.FenceStage(ctx, string(wfID), string(claim.NodeID)); err != nil {
			t.Fatalf("stage %d FenceStage: %v", stage, err)
		}
		t.Logf("  fence: done")
	}

	// ── Assert workflow Status == SUCCEEDED ──────────────────────────────
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Status != routedrun.WorkflowStatusSucceeded {
		t.Errorf("workflow status: want SUCCEEDED, got %s", wf.Status)
	}
	t.Logf("Workflow status: %s", wf.Status)

	// ── BuildPipelineInspect → 3 nodes all SUCCEEDED, ordered by stage ──
	summary, err := BuildPipelineInspect(ctx, store, wfID)
	if err != nil {
		t.Fatalf("BuildPipelineInspect: %v", err)
	}
	if len(summary.Nodes) != 3 {
		t.Fatalf("inspect: expected 3 nodes, got %d", len(summary.Nodes))
	}

	wantStatus := routedrun.NodeStatusSucceeded.String()
	for i, n := range summary.Nodes {
		if n.StageOrder != i {
			t.Errorf("Node[%d].StageOrder: want %d, got %d", i, i, n.StageOrder)
		}
		if n.Status != wantStatus {
			t.Errorf("Node[%d].Status: want %s, got %s", i, wantStatus, n.Status)
		}
		t.Logf("  inspect node[%d]: id=%s stage=%d status=%s outcome=%s",
			i, n.NodeID, n.StageOrder, n.Status, n.Outcome)
	}

	// ── ListHandoffs → exactly 2 ────────────────────────────────────────
	handoffs, err := store.ListHandoffs(ctx, wfID)
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(handoffs) != 2 {
		t.Errorf("expected 2 handoffs, got %d", len(handoffs))
	}
	for _, h := range handoffs {
		t.Logf("  handoff: source=%s target=%s id=%s",
			h.SourceNodeID, h.TargetNodeID, h.HandoffID)
	}

	// ── No duplicate launch keys across stages ──────────────────────────
	t.Logf("  launch keys: %d (no duplicates)", len(launchKeys))

	// ── Verify nodes are ordered by stage ───────────────────────────────
	nodes, err := store.ListNodes(ctx, wfID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})
	for i, n := range nodes {
		if n.StageOrder != i {
			t.Errorf("node stage order: expected %d, got %d for NodeID=%s",
				i, n.StageOrder, n.NodeID)
		}
	}

	// ── Cleanup handled by defer (removes networks, checks orphans) ─────
	// Containers already fenced during the stage loop.
	// The defer at function top removes networks from createdNetworks and
	// asserts zero orphan containers and networks by workflow label.

	t.Logf("")
	t.Logf("B34 MULTI-AGENT E2E PASS hermes-absent containers=3 handoffs=2")
}

// launchKeyForNode is a helper to reconstruct the launch key for a known node.
func launchKeyForNode(wfID routedrun.WorkflowID, nodeID routedrun.NodeID, gen int64) string {
	return LaunchIdempotencyKey(wfID, nodeID, gen)
}
