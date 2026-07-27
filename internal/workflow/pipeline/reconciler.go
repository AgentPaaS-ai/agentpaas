package pipeline

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// Reconciler
// ---------------------------------------------------------------------------

// Reconciler drives pipeline stage advancement: claim → launch → ack.
type Reconciler struct {
	Ctrl     *Controller
	Launches LaunchStore
	Launcher StageLauncher
	// NetworkDriver is an optional runtime driver for creating per-stage
	// networks. When non-nil, ReconcileOnce creates a stage-private network
	// and sets NetworkID on the launch job before calling EnsureLaunch.
	// When nil, networks are not created (FakeLauncher path).
	NetworkDriver runtime.RuntimeDriver
}

// ReconcileOnce advances at most one stage claim/launch/ack for the workflow.
//
// Steps:
//  1. Check for a node in LAUNCHING state. If found, ensure launch and ack
//     (recovery path for crashes between claim and ack).
//  2. If no LAUNCHING node, call ClaimNextReady.
//  3. On successful claim: PutIfAbsent a PENDING StageLaunchJob, call
//     Launcher.EnsureLaunch, then AcknowledgeRunning.
//  4. Return the claim (nil if nothing to do).
func (r *Reconciler) ReconcileOnce(ctx context.Context, workflowID routedrun.WorkflowID) (*Claim, error) {
	// ── Step 1: Recovery — find any LAUNCHING node and finalize its launch ──
	nodes, err := r.Ctrl.Store.ListNodes(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("reconcile once: %w", err)
	}

	for _, n := range nodes {
		if n.Status != routedrun.NodeStatusLaunching {
			continue
		}

		// Found a LAUNCHING node. Look up or create the launch job.
		launchGen := int64(1) // default for first claim
		key := LaunchIdempotencyKey(workflowID, n.NodeID, launchGen)

		// Try to find an existing launch job for this node.
		jobs, err := r.Launches.ListByWorkflow(ctx, workflowID)
		if err != nil {
			return nil, fmt.Errorf("reconcile once: list launches: %w", err)
		}
		var existing *StageLaunchJob
		for _, j := range jobs {
			if j.NodeID == n.NodeID && j.WorkflowID == workflowID {
				existing = j
				key = j.Key
				launchGen = j.Generation
				break
			}
		}

		if existing == nil {
			// No launch job yet — claim was created but launch not persisted.
			// Create the launch job now.
			now := time.Now().UTC()
			job := &StageLaunchJob{
				Key:        key,
				WorkflowID: workflowID,
				NodeID:     n.NodeID,
				RunID:      n.RunID,
				StageOrder: n.StageOrder,
				Generation: launchGen,
				Status:     LaunchStatusPending,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			r.fillStageJobDefaults(ctx, job)
			stored, created, err := r.Launches.PutIfAbsent(ctx, job)
			if err != nil {
				return nil, fmt.Errorf("reconcile once: put launch job: %w", err)
			}
			if !created {
				existing = stored
			} else {
				existing = job
			}
		}

		// Ensure the launcher has started it (idempotent).
		if err := r.Launcher.EnsureLaunch(ctx, existing); err != nil {
			return nil, fmt.Errorf("reconcile once: ensure launch: %w", err)
		}

		// Update launch job status in the store.
		existing.Status = LaunchStatusStarted
		existing.UpdatedAt = time.Now().UTC()
		_ = r.Launches.Update(ctx, existing)

		// Acknowledge running if the node is still LAUNCHING.
		claim := &Claim{
			WorkflowID:      workflowID,
			NodeID:          n.NodeID,
			RunID:           n.RunID,
			LaunchGeneration: launchGen,
			LaunchKey:       key,
		}
		// We need the attempt — load the runs and find the latest attempt.
		runs, err := findAttemptForLaunching(ctx, r.Ctrl.Store, n.RunID)
		if err != nil {
			return nil, fmt.Errorf("reconcile once: find attempt: %w", err)
		}
		if runs != nil {
			claim.Attempt = runs
		}

		if err := r.Ctrl.AcknowledgeRunning(ctx, claim); err != nil {
			return nil, fmt.Errorf("reconcile once: ack running: %w", err)
		}

		return claim, nil
	}

	// ── Step 2: No LAUNCHING node — claim next READY ──
	claim, err := r.Ctrl.ClaimNextReady(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("reconcile once: claim: %w", err)
	}
	if claim == nil {
		return nil, nil
	}

	// ── Step 3: Persist launch job ──
	now := time.Now().UTC()
	job := &StageLaunchJob{
		Key:        claim.LaunchKey,
		WorkflowID: claim.WorkflowID,
		NodeID:     claim.NodeID,
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Generation: claim.LaunchGeneration,
		StageOrder: claim.StageOrder,
		Status:     LaunchStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Fill stage defaults (image, command, network) before persisting.
	r.fillStageJobDefaults(ctx, job)

	existing, created, err := r.Launches.PutIfAbsent(ctx, job)
	if err != nil {
		return nil, fmt.Errorf("reconcile once: put launch job: %w", err)
	}

	// Use existing when job already persisted (double launch guard).
	toLaunch := job
	if !created {
		toLaunch = existing
		// Update claim's LaunchKey/LaunchGeneration from the existing
		// persisted record so they match the actual stored key.
		claim.LaunchKey = existing.Key
		claim.LaunchGeneration = existing.Generation
	}

	// ── Step 4: Ensure launch (idempotent) ──
	if err := r.Launcher.EnsureLaunch(ctx, toLaunch); err != nil {
		return nil, fmt.Errorf("reconcile once: ensure launch: %w", err)
	}

	// Update job status to STARTED on the stored record.
	toLaunch.Status = LaunchStatusStarted
	toLaunch.UpdatedAt = time.Now().UTC()
	_ = r.Launches.Update(ctx, toLaunch)

	// ── Step 5: Acknowledge running ──
	if err := r.Ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		return nil, fmt.Errorf("reconcile once: ack running: %w", err)
	}

	return claim, nil
}

// findAttemptForLaunching finds the latest attempt for a run, used during
// LAUNCHING recovery when we need to build a Claim with an Attempt.
func findAttemptForLaunching(ctx context.Context, store PipelineStore, runID routedrun.RunID) (*routedrun.AttemptRecord, error) {
	attempts, err := store.ListAttempts(ctx, runID)
	if err != nil {
		return nil, err
	}
	if len(attempts) == 0 {
		return nil, nil
	}
	// Return the last attempt (highest attempt number).
	latest := attempts[0]
	for _, a := range attempts[1:] {
		if a.AttemptNumber > latest.AttemptNumber {
			latest = a
		}
	}
	return latest, nil
}

// defaultStageImage is the fallback container image for pipeline stages when
// no real stage image has been resolved. Override with
// AGENTPAAS_PIPELINE_STAGE_IMAGE for tests or production pack path.
const defaultStageImage = "alpine:3.20"

// defaultStageCommand is the fallback command for pipeline stages. The sleep
// keeps the container alive long enough for tests to inspect it.
var defaultStageCommand = []string{"sleep", "30"}

// fillStageJobDefaults sets Image, Command, and (if NetworkDriver is wired)
// NetworkID on a StageLaunchJob that has not yet been launched. It does NOT
// overwrite fields already set (e.g. from a prior recovery path).
//
// The image is resolved from AGENTPAAS_PIPELINE_STAGE_IMAGE if set, else
// alpine:3.20. The command defaults to ["sleep","30"].
func (r *Reconciler) fillStageJobDefaults(ctx context.Context, job *StageLaunchJob) {
	// Image: env override → default.
	if job.Image == "" {
		if img := os.Getenv("AGENTPAAS_PIPELINE_STAGE_IMAGE"); img != "" {
			job.Image = img
		} else {
			job.Image = defaultStageImage
		}
	}
	// Command: use default if empty.
	if len(job.Command) == 0 {
		job.Command = append(job.Command, defaultStageCommand...)
	}
	// NetworkID: create per-stage network if driver is available.
	if job.NetworkID == "" && r.NetworkDriver != nil {
		netName := fmt.Sprintf("ap-stage-%s-%s", job.WorkflowID, job.NodeID)
		labels := map[string]string{
			runtime.LabelWorkflowID: string(job.WorkflowID),
			runtime.LabelNodeID:     string(job.NodeID),
			"agentpaas.role":        "stage-network",
		}
		spec := runtime.NetworkSpec{
			Name:     netName,
			Internal: true,
			Labels:   labels,
		}
		netID, err := r.NetworkDriver.CreateNetwork(ctx, spec)
		if err != nil {
			// Network creation failure is non-fatal for reconciliation;
			// the launcher will fail with a clear error on EnsureLaunch.
			return
		}
		job.NetworkID = string(netID)
	}
}
