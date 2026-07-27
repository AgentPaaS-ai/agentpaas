package pipeline

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// ---------------------------------------------------------------------------
// RuntimeStageLauncher
// ---------------------------------------------------------------------------

// RuntimeStageLauncher implements StageLauncher using a runtime.RuntimeDriver.
// It creates real containers via the driver for pipeline stage isolation.
type RuntimeStageLauncher struct {
	Driver runtime.RuntimeDriver

	mu     sync.Mutex
	active map[string]runtime.ContainerID // launchKey -> container
}

// NewRuntimeStageLauncher creates a RuntimeStageLauncher backed by the given
// RuntimeDriver.
func NewRuntimeStageLauncher(driver runtime.RuntimeDriver) *RuntimeStageLauncher {
	return &RuntimeStageLauncher{
		Driver: driver,
		active: make(map[string]runtime.ContainerID),
	}
}

// EnsureLaunch implements StageLauncher. It is idempotent for the job's Key:
// if a container is already recorded and Inspect shows it running, returns nil.
// Otherwise builds the spec from the job's metadata and creates+starts the
// container via the driver.
func (l *RuntimeStageLauncher) EnsureLaunch(ctx context.Context, job *StageLaunchJob) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if this job already has a recorded container.
	if cid, ok := l.active[job.Key]; ok {
		status, err := l.Driver.Status(ctx, cid)
		if err != nil {
			return fmt.Errorf("RuntimeStageLauncher: check existing container %s: %w", cid, err)
		}
		if status == runtime.ContainerStatusRunning {
			return nil // already running, idempotent
		}
		// Container exists but is not running — remove stale record and recreate.
		delete(l.active, job.Key)
	}

	// Build the launch request from the job's metadata.
	req := StageLaunchRequest{
		WorkflowID:       string(job.WorkflowID),
		NodeID:           string(job.NodeID),
		RunID:            string(job.RunID),
		AttemptID:        string(job.AttemptID),
		StageOrder:       job.StageOrder,
		PackageDigest:    job.PackageDigest,
		PolicyDigest:     job.PolicyDigest,
		Image:            job.Image,
		Command:          job.Command,
		LeaseGeneration:  job.Generation,
		NetworkID:        job.NetworkID,
	}

	spec, err := BuildStageContainerSpec(req)
	if err != nil {
		return fmt.Errorf("RuntimeStageLauncher: build spec: %w", err)
	}

	// Create and start the container.
	cid, err := l.Driver.Create(ctx, spec)
	if err != nil {
		return fmt.Errorf("RuntimeStageLauncher: create: %w", err)
	}
	if err := l.Driver.Start(ctx, cid); err != nil {
		return fmt.Errorf("RuntimeStageLauncher: start: %w", err)
	}

	// Record the container ID.
	l.active[job.Key] = cid
	job.ContainerID = string(cid)
	job.Status = LaunchStatusStarted
	job.UpdatedAt = time.Now().UTC()

	return nil
}

// ---------------------------------------------------------------------------
// FenceStage
// ---------------------------------------------------------------------------

// FenceStage stops and removes containers associated with the given
// workflowID and nodeID combination. It looks up by label filter for
// the workflow and then checks nodeID match from the active map.
//
// Call this before launching a new stage for the same node to ensure
// clean isolation (no prior stage containers lingering).
func (l *RuntimeStageLauncher) FenceStage(ctx context.Context, workflowID, nodeID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Build label filter for this workflow.
	labelFilter := fmt.Sprintf("%s=%s", runtime.LabelWorkflowID, workflowID)

	containers, err := l.Driver.ListContainers(ctx, labelFilter)
	if err != nil {
		return fmt.Errorf("FenceStage: list containers: %w", err)
	}

	for _, info := range containers {
		// Check if this container belongs to the target node.
		if info.Labels[runtime.LabelNodeID] != nodeID {
			continue
		}

		cid := runtime.ContainerID(info.ID)

		// Stop the container with a timeout so Docker waits for it to exit
		// before we call Remove. A nil timeout is fire-and-forget and races
		// with the subsequent Remove.
		if info.Status == runtime.ContainerStatusRunning {
			timeout := 10 * time.Second
			if err := l.Driver.Stop(ctx, cid, &timeout); err != nil {
				return fmt.Errorf("FenceStage: stop %s: %w", cid, err)
			}
		}

		// Remove the container. Force=true to handle any remaining state.
		// Docker may return "already in progress" / not-found under concurrent
		// fence+cleanup; those are success for our purposes.
		if err := l.Driver.Remove(ctx, cid, true); err != nil {
			msg := err.Error()
			if !strings.Contains(msg, "already in progress") &&
				!strings.Contains(msg, "No such container") &&
				!strings.Contains(msg, "is not running") {
				return fmt.Errorf("FenceStage: remove %s: %w", cid, err)
			}
		}

		// Clean up active map entries pointing to this container.
		for key, ac := range l.active {
			if ac == cid {
				delete(l.active, key)
			}
		}
	}

	return nil
}
