// Package pipeline provides B34 pipeline and handoff conformance validation,
// plus the linear pipeline controller for durable stage advancement.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Controller errors
// ---------------------------------------------------------------------------

var (
	ErrNothingReady      = errors.New("pipeline: nothing ready to claim")
	ErrCASConflict       = errors.New("pipeline: CAS conflict")
	ErrHandoffMissing    = errors.New("pipeline: handoff required for non-final stage")
	ErrInvalidTransition = errors.New("pipeline: invalid state transition")
)

// ---------------------------------------------------------------------------
// Narrow store interface
// ---------------------------------------------------------------------------

// PipelineStore is the narrow store interface the controller needs.
type PipelineStore interface {
	// Workflow
	CreateWorkflow(ctx context.Context, wf *routedrun.WorkflowRecord) error
	GetWorkflow(ctx context.Context, workflowID routedrun.WorkflowID) (*routedrun.WorkflowRecord, error)
	UpdateWorkflow(ctx context.Context, wf *routedrun.WorkflowRecord, expectedGeneration int64) error

	// Nodes
	CreateNode(ctx context.Context, node *routedrun.PipelineNode) error
	GetNode(ctx context.Context, nodeID routedrun.NodeID) (*routedrun.PipelineNode, error)
	UpdateNode(ctx context.Context, node *routedrun.PipelineNode, expectedGeneration int64) error
	ListNodes(ctx context.Context, workflowID routedrun.WorkflowID) ([]*routedrun.PipelineNode, error)

	// Runs
	CreateRun(ctx context.Context, run *routedrun.RunRecord) error
	GetRun(ctx context.Context, runID routedrun.RunID) (*routedrun.RunRecord, error)
	UpdateRun(ctx context.Context, run *routedrun.RunRecord, expectedGeneration int64) error

	// Attempts
	CreateAttempt(ctx context.Context, attempt *routedrun.AttemptRecord) error
	GetAttempt(ctx context.Context, attemptID routedrun.AttemptID) (*routedrun.AttemptRecord, error)
	UpdateAttempt(ctx context.Context, attempt *routedrun.AttemptRecord, expectedGeneration int64) error
	ListAttempts(ctx context.Context, runID routedrun.RunID) ([]*routedrun.AttemptRecord, error)

	// Handoffs
	CommitHandoff(ctx context.Context, handoff *routedrun.HandoffEnvelope) error
	GetHandoff(ctx context.Context, handoffID routedrun.HandoffID) (*routedrun.HandoffEnvelope, error)
	ListHandoffs(ctx context.Context, workflowID routedrun.WorkflowID) ([]*routedrun.HandoffEnvelope, error)
}

// Compile-time check: MemoryStore satisfies PipelineStore.
var _ PipelineStore = (*routedrun.MemoryStore)(nil)

// ---------------------------------------------------------------------------
// Controller
// ---------------------------------------------------------------------------

// Controller advances pipeline node/run state via CAS with exactly-once
// logical effects for linear pipelines.
type Controller struct {
	Store   PipelineStore
	nodeGen map[routedrun.NodeID]int64 // tracks node CAS generations
	runGen  map[routedrun.RunID]int64  // tracks run CAS generations
	attGen  map[routedrun.AttemptID]int64 // tracks attempt CAS generations
}

// NewController constructs a Controller backed by the given store.
func NewController(store PipelineStore) *Controller {
	return &Controller{
		Store:   store,
		nodeGen: make(map[routedrun.NodeID]int64),
		runGen:  make(map[routedrun.RunID]int64),
		attGen:  make(map[routedrun.AttemptID]int64),
	}
}

// initSeedGenerations sets generation=1 for nodes and runs created during seed.
func (c *Controller) initSeedGenerations(nodeIDs []routedrun.NodeID, runIDs map[routedrun.NodeID]routedrun.RunID) {
	for _, nid := range nodeIDs {
		c.nodeGen[nid] = 1
		if rid, ok := runIDs[nid]; ok {
			c.runGen[rid] = 1
		}
	}
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Claim represents a claimed node that is now LAUNCHING.
type Claim struct {
	WorkflowID routedrun.WorkflowID
	NodeID     routedrun.NodeID
	RunID      routedrun.RunID
	Attempt    *routedrun.AttemptRecord
}

// StageSuccess is the request payload for CommitStageSuccess.
type StageSuccess struct {
	WorkflowID routedrun.WorkflowID
	NodeID     routedrun.NodeID
	RunID      routedrun.RunID
	AttemptID  routedrun.AttemptID
	Handoff    *routedrun.HandoffEnvelope
}

// StageFailure is the request payload for CommitStageFailure.
type StageFailure struct {
	WorkflowID routedrun.WorkflowID
	NodeID     routedrun.NodeID
	RunID      routedrun.RunID
	AttemptID  routedrun.AttemptID
}

// ---------------------------------------------------------------------------
// SeedPipelineWorkflow
// ---------------------------------------------------------------------------

// SeedPipelineWorkflow creates a workflow + N nodes for testing.
// stage0 is READY, rest are PENDING, each with a precreated run.
// Returns the workflow ID and ordered node IDs.
// Initializes the controller's generation maps for the seeded records.
func SeedPipelineWorkflow(ctx context.Context, ctrl *Controller, stageCount int) (routedrun.WorkflowID, []routedrun.NodeID, error) {
	if stageCount < 2 {
		return "", nil, fmt.Errorf("pipeline: stage count must be >= 2, got %d", stageCount)
	}

	wfID, err := routedrun.NewWorkflowID()
	if err != nil {
		return "", nil, fmt.Errorf("seed pipeline workflow: %w", err)
	}
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
	if err := ctrl.Store.CreateWorkflow(ctx, wf); err != nil {
		return "", nil, fmt.Errorf("seed pipeline workflow: %w", err)
	}

	nodeIDs := make([]routedrun.NodeID, stageCount)
	runIDs := make(map[routedrun.NodeID]routedrun.RunID, stageCount)
	for i := 0; i < stageCount; i++ {
		status := routedrun.NodeStatusPending
		if i == 0 {
			status = routedrun.NodeStatusReady
		}

		nodeID, err := routedrun.NewNodeID()
		if err != nil {
			return "", nil, fmt.Errorf("seed pipeline workflow: %w", err)
		}
		runID, err := routedrun.NewRunID()
		if err != nil {
			return "", nil, fmt.Errorf("seed pipeline workflow: %w", err)
		}

		nid := nodeID
		node := &routedrun.PipelineNode{
			SchemaVersion:  routedrun.CurrentSchemaVersion,
			NodeID:         nodeID,
			WorkflowID:     wfID,
			Status:         status,
			RunID:          runID,
			StageOrder:     i,
			PackageName:    "test-pkg",
			PackageVersion: "0.1.0",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := ctrl.Store.CreateNode(ctx, node); err != nil {
			return "", nil, fmt.Errorf("seed pipeline workflow node %d: %w", i, err)
		}

		run := &routedrun.RunRecord{
			SchemaVersion:       routedrun.CurrentSchemaVersion,
			RunID:               runID,
			WorkflowID:          wfID,
			Status:              routedrun.RunStatusPending,
			RunKind:             "pipeline_stage",
			NodeID:              &nid,
			MaxActiveDurationMs: 3600000,
			MaxAttemptLeaseMs:   600000,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := ctrl.Store.CreateRun(ctx, run); err != nil {
			return "", nil, fmt.Errorf("seed pipeline workflow run %d: %w", i, err)
		}

		nodeIDs[i] = nodeID
		runIDs[nodeID] = runID
	}

	// Initialize generation tracking.
	ctrl.initSeedGenerations(nodeIDs, runIDs)

	return wfID, nodeIDs, nil
}

// ---------------------------------------------------------------------------
// ClaimNextReady
// ---------------------------------------------------------------------------

// ClaimNextReady CAS-claims the earliest READY node → LAUNCHING, creates
// attempt+lease. Returns nil,nil if nothing to claim or PAUSE_REQUESTED state.
func (c *Controller) ClaimNextReady(ctx context.Context, workflowID routedrun.WorkflowID) (*Claim, error) {
	// Check workflow state first.
	wf, err := c.Store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("claim next ready: %w", err)
	}
	if wf.Status == routedrun.WorkflowStatusPauseRequested {
		return nil, nil
	}
	if wf.Status.IsTerminal() {
		return nil, nil
	}

	// Find earliest READY node.
	nodes, err := c.Store.ListNodes(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("claim next ready: %w", err)
	}

	// Sort by stage order.
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})

	var target *routedrun.PipelineNode
	for _, n := range nodes {
		if n.Status == routedrun.NodeStatusReady {
			target = n
			break
		}
	}
	if target == nil {
		return nil, nil
	}

	// Get current generation (default to 1 if not tracked).
	nodeGen := c.nodeGen[target.NodeID]
	if nodeGen == 0 {
		nodeGen = 1
	}

	// CAS-update node: READY → LAUNCHING.
	target.Status = routedrun.NodeStatusLaunching
	target.UpdatedAt = time.Now().UTC()
	if err := c.Store.UpdateNode(ctx, target, nodeGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return nil, ErrCASConflict
		}
		return nil, fmt.Errorf("claim next ready: %w", err)
	}
	c.nodeGen[target.NodeID] = nodeGen + 1

	// Create attempt with lease.
	attempt := &routedrun.AttemptRecord{
		SchemaVersion: routedrun.CurrentSchemaVersion,
		RunID:         target.RunID,
		WorkflowID:    workflowID,
		Status:        routedrun.AttemptStatusPending,
		AttemptNumber: 1,
		Lease: &routedrun.AttemptLease{
			SchemaVersion: routedrun.CurrentSchemaVersion,
			RunID:         target.RunID,
			DurationMs:    600000,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := c.Store.CreateAttempt(ctx, attempt); err != nil {
		return nil, fmt.Errorf("claim next ready: %w", err)
	}
	c.attGen[attempt.AttemptID] = 1

	return &Claim{
		WorkflowID: workflowID,
		NodeID:     target.NodeID,
		RunID:      target.RunID,
		Attempt:    attempt,
	}, nil
}

// ---------------------------------------------------------------------------
// AcknowledgeRunning
// ---------------------------------------------------------------------------

// AcknowledgeRunning moves LAUNCHING → RUNNING for the claimed node,
// and PENDING → RUNNING for the run and attempt.
func (c *Controller) AcknowledgeRunning(ctx context.Context, claim *Claim) error {
	// Validate and update node: LAUNCHING → RUNNING.
	node, err := c.Store.GetNode(ctx, claim.NodeID)
	if err != nil {
		return fmt.Errorf("acknowledge running: %w", err)
	}
	if err := routedrun.ValidateNodeTransition(node.Status, routedrun.NodeStatusRunning); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}

	nodeGen := c.nodeGen[claim.NodeID]
	if nodeGen == 0 {
		nodeGen = 1
	}
	node.Status = routedrun.NodeStatusRunning
	node.UpdatedAt = time.Now().UTC()
	if err := c.Store.UpdateNode(ctx, node, nodeGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("acknowledge running: %w", err)
	}
	c.nodeGen[claim.NodeID] = nodeGen + 1

	// Update run: PENDING → RUNNING.
	run, err := c.Store.GetRun(ctx, claim.RunID)
	if err != nil {
		return fmt.Errorf("acknowledge running: %w", err)
	}
	if err := routedrun.ValidateRunTransition(run.Status, routedrun.RunStatusRunning); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}
	runGen := c.runGen[claim.RunID]
	if runGen == 0 {
		runGen = 1
	}
	run.Status = routedrun.RunStatusRunning
	run.UpdatedAt = time.Now().UTC()
	if err := c.Store.UpdateRun(ctx, run, runGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("acknowledge running: %w", err)
	}
	c.runGen[claim.RunID] = runGen + 1

	// Update attempt: PENDING → RUNNING.
	attempt, err := c.Store.GetAttempt(ctx, claim.Attempt.AttemptID)
	if err != nil {
		return fmt.Errorf("acknowledge running: %w", err)
	}
	if err := routedrun.ValidateAttemptTransition(attempt.Status, routedrun.AttemptStatusRunning); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}
	attGen := c.attGen[claim.Attempt.AttemptID]
	if attGen == 0 {
		attGen = 1
	}
	attempt.Status = routedrun.AttemptStatusRunning
	attempt.UpdatedAt = time.Now().UTC()
	if err := c.Store.UpdateAttempt(ctx, attempt, attGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("acknowledge running: %w", err)
	}
	c.attGen[claim.Attempt.AttemptID] = attGen + 1

	return nil
}

// ---------------------------------------------------------------------------
// CommitStageSuccess
// ---------------------------------------------------------------------------

// CommitStageSuccess atomically validates handoff, marks node+run SUCCEEDED,
// commits handoff, marks next node READY or workflow SUCCEEDED if final.
// Idempotent if called twice with same success.
func (c *Controller) CommitStageSuccess(ctx context.Context, req StageSuccess) error {
	// Idempotency check: if node is already SUCCEEDED, return nil.
	node, err := c.Store.GetNode(ctx, req.NodeID)
	if err != nil {
		return fmt.Errorf("commit stage success: %w", err)
	}
	if node.Status == routedrun.NodeStatusSucceeded {
		return nil
	}

	// Determine if this is the final stage.
	nodes, err := c.Store.ListNodes(ctx, req.WorkflowID)
	if err != nil {
		return fmt.Errorf("commit stage success: %w", err)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].StageOrder < nodes[j].StageOrder
	})
	isFinal := node.StageOrder == len(nodes)-1

	// Validate handoff: non-nil for non-final stages.
	if !isFinal && req.Handoff == nil {
		return ErrHandoffMissing
	}

	// Validate node transition.
	if err := routedrun.ValidateNodeTransition(node.Status, routedrun.NodeStatusSucceeded); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}

	now := time.Now().UTC()

	// Update node: → SUCCEEDED.
	nodeGen := c.nodeGen[req.NodeID]
	if nodeGen == 0 {
		nodeGen = 1
	}
	node.Status = routedrun.NodeStatusSucceeded
	node.UpdatedAt = now
	if err := c.Store.UpdateNode(ctx, node, nodeGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("commit stage success: %w", err)
	}
	c.nodeGen[req.NodeID] = nodeGen + 1

	// Update run: → SUCCEEDED.
	run, err := c.Store.GetRun(ctx, req.RunID)
	if err != nil {
		return fmt.Errorf("commit stage success: %w", err)
	}
	if err := routedrun.ValidateRunTransition(run.Status, routedrun.RunStatusSucceeded); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}
	runGen := c.runGen[req.RunID]
	if runGen == 0 {
		runGen = 1
	}
	run.Status = routedrun.RunStatusSucceeded
	run.TerminatedAt = &now
	run.UpdatedAt = now
	if err := c.Store.UpdateRun(ctx, run, runGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("commit stage success: %w", err)
	}
	c.runGen[req.RunID] = runGen + 1

	// Update attempt: → SUCCEEDED.
	attempt, err := c.Store.GetAttempt(ctx, req.AttemptID)
	if err != nil {
		return fmt.Errorf("commit stage success: %w", err)
	}
	if attempt.Status != routedrun.AttemptStatusSucceeded {
		if err := routedrun.ValidateAttemptTransition(attempt.Status, routedrun.AttemptStatusSucceeded); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
		}
		attGen := c.attGen[req.AttemptID]
		if attGen == 0 {
			attGen = 1
		}
		attempt.Status = routedrun.AttemptStatusSucceeded
		attempt.TerminatedAt = &now
		attempt.UpdatedAt = now
		if err := c.Store.UpdateAttempt(ctx, attempt, attGen); err != nil {
			if errors.Is(err, routedrun.ErrCASConflict) {
				return ErrCASConflict
			}
			return fmt.Errorf("commit stage success: %w", err)
		}
		c.attGen[req.AttemptID] = attGen + 1
	}

	// Handle next stage / final.
	if isFinal {
		// Mark workflow SUCCEEDED.
		wf, err := c.Store.GetWorkflow(ctx, req.WorkflowID)
		if err != nil {
			return fmt.Errorf("commit stage success: %w", err)
		}
		if wf.Status != routedrun.WorkflowStatusSucceeded {
			if err := routedrun.ValidateWorkflowTransition(wf.Status, routedrun.WorkflowStatusSucceeded); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
			}
			wf.Status = routedrun.WorkflowStatusSucceeded
			wf.TerminatedAt = &now
			wf.UpdatedAt = now
			if err := c.Store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
				return fmt.Errorf("commit stage success: %w", err)
			}
		}
	} else {
		// Commit handoff.
		if req.Handoff.HandoffID == "" {
			hid, err := routedrun.NewHandoffID()
			if err != nil {
				return fmt.Errorf("commit stage success: %w", err)
			}
			req.Handoff.HandoffID = hid
		}
		req.Handoff.CreatedAt = now
		if req.Handoff.SchemaVersion == "" {
			req.Handoff.SchemaVersion = routedrun.CurrentSchemaVersion
		}
		if err := c.Store.CommitHandoff(ctx, req.Handoff); err != nil {
			return fmt.Errorf("commit stage success: %w", err)
		}

		// Find next node and set it to READY.
		next := c.findNextNode(nodes, node.StageOrder)
		if next != nil {
			nextNodeGen := c.nodeGen[next.NodeID]
			if nextNodeGen == 0 {
				nextNodeGen = 1
			}
			next.Status = routedrun.NodeStatusReady
			next.UpdatedAt = now
			hid := req.Handoff.HandoffID
			next.IncomingHandoffID = &hid
			if err := c.Store.UpdateNode(ctx, next, nextNodeGen); err != nil {
				if !errors.Is(err, routedrun.ErrCASConflict) {
					return fmt.Errorf("commit stage success: %w", err)
				}
			}
			c.nodeGen[next.NodeID] = nextNodeGen + 1
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// CommitStageFailure
// ---------------------------------------------------------------------------

// CommitStageFailure marks node/run failed and workflow failed; no next READY.
func (c *Controller) CommitStageFailure(ctx context.Context, req StageFailure) error {
	// Idempotency check.
	node, err := c.Store.GetNode(ctx, req.NodeID)
	if err != nil {
		return fmt.Errorf("commit stage failure: %w", err)
	}
	if node.Status == routedrun.NodeStatusFailed {
		return nil
	}

	// Validate transition.
	if err := routedrun.ValidateNodeTransition(node.Status, routedrun.NodeStatusFailed); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
	}

	now := time.Now().UTC()

	// Update node → FAILED.
	nodeGen := c.nodeGen[req.NodeID]
	if nodeGen == 0 {
		nodeGen = 1
	}
	node.Status = routedrun.NodeStatusFailed
	node.UpdatedAt = now
	if err := c.Store.UpdateNode(ctx, node, nodeGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("commit stage failure: %w", err)
	}
	c.nodeGen[req.NodeID] = nodeGen + 1

	// Update run → FAILED.
	run, err := c.Store.GetRun(ctx, req.RunID)
	if err != nil {
		return fmt.Errorf("commit stage failure: %w", err)
	}
	runGen := c.runGen[req.RunID]
	if runGen == 0 {
		runGen = 1
	}
	run.Status = routedrun.RunStatusFailed
	run.TerminatedAt = &now
	run.UpdatedAt = now
	if err := c.Store.UpdateRun(ctx, run, runGen); err != nil {
		if errors.Is(err, routedrun.ErrCASConflict) {
			return ErrCASConflict
		}
		return fmt.Errorf("commit stage failure: %w", err)
	}
	c.runGen[req.RunID] = runGen + 1

	// Update attempt → FAILED.
	attempt, err := c.Store.GetAttempt(ctx, req.AttemptID)
	if err != nil {
		return fmt.Errorf("commit stage failure: %w", err)
	}
	if attempt.Status != routedrun.AttemptStatusFailed {
		if err := routedrun.ValidateAttemptTransition(attempt.Status, routedrun.AttemptStatusFailed); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
		}
		attGen := c.attGen[req.AttemptID]
		if attGen == 0 {
			attGen = 1
		}
		attempt.Status = routedrun.AttemptStatusFailed
		attempt.TerminatedAt = &now
		attempt.UpdatedAt = now
		if err := c.Store.UpdateAttempt(ctx, attempt, attGen); err != nil {
			if errors.Is(err, routedrun.ErrCASConflict) {
				return ErrCASConflict
			}
			return fmt.Errorf("commit stage failure: %w", err)
		}
		c.attGen[req.AttemptID] = attGen + 1
	}

	// Mark workflow FAILED.
	wf, err := c.Store.GetWorkflow(ctx, req.WorkflowID)
	if err != nil {
		return fmt.Errorf("commit stage failure: %w", err)
	}
	if wf.Status != routedrun.WorkflowStatusFailed {
		if err := routedrun.ValidateWorkflowTransition(wf.Status, routedrun.WorkflowStatusFailed); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidTransition, err)
		}
		wf.Status = routedrun.WorkflowStatusFailed
		wf.TerminatedAt = &now
		wf.UpdatedAt = now
		if err := c.Store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
			return fmt.Errorf("commit stage failure: %w", err)
		}
	}

	return nil
}

// findNextNode returns the node with the next stage order, or nil if none.
func (c *Controller) findNextNode(nodes []*routedrun.PipelineNode, currentOrder int) *routedrun.PipelineNode {
	for _, n := range nodes {
		if n.StageOrder == currentOrder+1 {
			return n
		}
	}
	return nil
}