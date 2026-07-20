package routedrun

// Ignored context values (`_ = ctx`) satisfy interface signatures without cancellation for in-process stores; reviewed as legitimate. // unused context; interface compliance

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-memory implementation of DeploymentStore, RunStore, and
// WorkflowStore for deterministic tests.
type MemoryStore struct {
	mu  sync.RWMutex
	now func() time.Time

	deployments map[DeploymentID]*DeploymentRecord
	aliases     map[string]*AliasRecord
	// idempotency: caller\x00key -> record
	idempotency map[string]*DurableIdempotencyRecord
	receipts    map[InvocationID]*InvocationReceipt

	workflows      map[WorkflowID]*WorkflowRecord
	workflowGen    map[WorkflowID]int64 // mirrors record.Generation
	nodes          map[NodeID]*PipelineNode
	nodeGen        map[NodeID]int64
	services       map[ServiceID]*MCPServiceBinding
	serviceGen     map[ServiceID]int64
	handoffs       map[HandoffID]*HandoffEnvelope
	childBatches   map[ChildBatchID]*ChildBatch
	childBatchGen  map[ChildBatchID]int64
	childResults   map[ChildResultID]*ChildResult
	desired        map[WorkflowID]*DesiredState
	amendments     map[LimitAmendmentID]*LimitAmendment
	controls       map[ControlRequestID]*ControlRequest
	controlResults []controlResultEntry
	activeTime     map[WorkflowID]*ActiveTimeLedger

	runs       map[RunID]*RunRecord
	runGen     map[RunID]int64
	attempts   map[AttemptID]*AttemptRecord
	attemptGen map[AttemptID]int64
	ledgers    map[RunID][]string
}

type controlResultEntry struct {
	Req    *ControlRequest
	Result interface{}
}

// NewMemoryStore constructs an empty in-memory store.
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		now:           func() time.Time { return time.Now().UTC() },
		deployments:   make(map[DeploymentID]*DeploymentRecord),
		aliases:       make(map[string]*AliasRecord),
		idempotency:   make(map[string]*DurableIdempotencyRecord),
		receipts:      make(map[InvocationID]*InvocationReceipt),
		workflows:     make(map[WorkflowID]*WorkflowRecord),
		workflowGen:   make(map[WorkflowID]int64),
		nodes:         make(map[NodeID]*PipelineNode),
		nodeGen:       make(map[NodeID]int64),
		services:      make(map[ServiceID]*MCPServiceBinding),
		serviceGen:    make(map[ServiceID]int64),
		handoffs:      make(map[HandoffID]*HandoffEnvelope),
		childBatches:  make(map[ChildBatchID]*ChildBatch),
		childBatchGen: make(map[ChildBatchID]int64),
		childResults:  make(map[ChildResultID]*ChildResult),
		desired:       make(map[WorkflowID]*DesiredState),
		amendments:    make(map[LimitAmendmentID]*LimitAmendment),
		controls:      make(map[ControlRequestID]*ControlRequest),
		activeTime:    make(map[WorkflowID]*ActiveTimeLedger),
		runs:          make(map[RunID]*RunRecord),
		runGen:        make(map[RunID]int64),
		attempts:      make(map[AttemptID]*AttemptRecord),
		attemptGen:    make(map[AttemptID]int64),
		ledgers:       make(map[RunID][]string),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// MemoryStoreOption configures a MemoryStore.
type MemoryStoreOption func(*MemoryStore)

// WithMemoryClock injects a fake clock.
func WithMemoryClock(now func() time.Time) MemoryStoreOption {
	return func(s *MemoryStore) {
		if now != nil {
			s.now = now
		}
	}
}

var (
	_ DeploymentStore = (*MemoryStore)(nil)
	_ RunStore        = (*MemoryStore)(nil)
	_ WorkflowStore   = (*MemoryStore)(nil)
)

func memKey(caller, key string) string { return caller + "\x00" + key }

// MemoryStore.CreateDeployment creates the deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateDeployment(ctx context.Context, dep *DeploymentRecord) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if dep == nil {
		return fmt.Errorf("%w: nil deployment", ErrInvalidArgument)
	}
	if dep.DeploymentID == "" {
		id, err := NewDeploymentID()
		if err != nil {
			return fmt.Errorf("memory store create deployment: %w", err)
		}
		dep.DeploymentID = id
	}
	if _, ok := s.deployments[dep.DeploymentID]; ok {
		return fmt.Errorf("%w: deployment %s", ErrAlreadyExists, dep.DeploymentID)
	}
	if dep.SchemaVersion == "" {
		dep.SchemaVersion = CurrentSchemaVersion
	}
	if dep.Generation == 0 {
		dep.Generation = 1
	}
	if dep.MaxConcurrentRuns <= 0 {
		dep.MaxConcurrentRuns = 1
	}
	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = s.now()
	}
	cp := *dep
	s.deployments[dep.DeploymentID] = &cp
	return nil
}

// MemoryStore.GetDeployment returns the deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetDeployment(ctx context.Context, deploymentID DeploymentID) (*DeploymentRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.deployments[deploymentID]
	if !ok {
		return nil, fmt.Errorf("%w: deployment %s", ErrNotFound, deploymentID)
	}
	cp := *d
	return &cp, nil
}

// MemoryStore.ListDeployments lists the deployments.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListDeployments(ctx context.Context) ([]*DeploymentRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*DeploymentRecord, 0, len(s.deployments))
	for _, d := range s.deployments {
		cp := *d
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.SetDeploymentStatus sets the deployment status.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) SetDeploymentStatus(ctx context.Context, deploymentID DeploymentID, status DeploymentStatus, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.deployments[deploymentID]
	if !ok {
		return fmt.Errorf("%w: deployment %s", ErrNotFound, deploymentID)
	}
	if d.Generation != expectedGeneration {
		return fmt.Errorf("%w: deployment %s", ErrCASConflict, deploymentID)
	}
	d.Status = status
	d.Generation++
	now := s.now()
	if status == DeploymentActive {
		d.ActivatedAt = &now
	} else {
		d.DeactivatedAt = &now
	}
	return nil
}

// MemoryStore.CompareAndSwapAlias compares and swap alias.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CompareAndSwapAlias(ctx context.Context, alias *AliasRecord) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if alias == nil || alias.Alias == "" {
		return fmt.Errorf("%w: alias", ErrInvalidArgument)
	}
	existing, ok := s.aliases[alias.Alias]
	// Work on a copy so we never mutate the caller's struct.
	cp := *alias
	if ok {
		if existing.Generation != cp.Generation {
			return fmt.Errorf("%w: alias %s", ErrCASConflict, alias.Alias)
		}
		cp.Generation = existing.Generation + 1
	} else if cp.Generation == 0 {
		cp.Generation = 1
	}
	if cp.SchemaVersion == "" {
		cp.SchemaVersion = CurrentSchemaVersion
	}
	cp.UpdatedAt = s.now()
	s.aliases[alias.Alias] = &cp
	return nil
}

// MemoryStore.ResolveAlias resolves the alias.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ResolveAlias(ctx context.Context, alias string) (*AliasRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.aliases[alias]
	if !ok {
		return nil, fmt.Errorf("%w: alias %s", ErrNotFound, alias)
	}
	cp := *a
	return &cp, nil
}

// MemoryStore.ListAliases lists the aliases.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListAliases(ctx context.Context) ([]*AliasRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AliasRecord, 0, len(s.aliases))
	for _, a := range s.aliases {
		cp := *a
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.AdmitInvocation admits invocation.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) AdmitInvocation(ctx context.Context, request *InvocationRequest, expectedDeploymentGeneration int64) (*InvocationReceipt, error) {
	_ = ctx // interface compliance; store ops are local
	if err := validateMemoryAdmitRequest(request); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	intent := computeIntentDigest(request)
	if receipt, err := s.replayIdempotentAdmission(request, intent); receipt != nil || err != nil {
		return receipt, err
	}

	dep, err := s.resolveActiveDeploymentForAdmit(request.RequestedDeploymentRef, expectedDeploymentGeneration)
	if err != nil {
		return nil, err
	}
	if err := s.enforceDeploymentConcurrencyQuota(dep); err != nil {
		return nil, err
	}

	now := s.now()
	topo, err := buildMemoryAdmissionTopology(request, dep, intent, now)
	if err != nil {
		return nil, err
	}
	s.persistAdmissionTopology(topo, request, intent, now)

	cp := *topo.receipt
	return &cp, nil
}

func validateMemoryAdmitRequest(request *InvocationRequest) error {
	if request == nil {
		return fmt.Errorf("%w: nil request", ErrInvalidArgument)
	}
	if request.IdempotencyKey == "" || request.CallerIdentity == "" {
		return fmt.Errorf("%w: idempotency required", ErrInvalidArgument)
	}
	return nil
}

// replayIdempotentAdmission returns an existing receipt on exact intent match.
// On miss it returns (nil, nil). On conflict it returns the error.
func (s *MemoryStore) replayIdempotentAdmission(request *InvocationRequest, intent string) (*InvocationReceipt, error) {
	ik := memKey(request.CallerIdentity, request.IdempotencyKey)
	rec, ok := s.idempotency[ik]
	if !ok {
		return nil, nil
	}
	if rec.InvocationIntentDigest == intent {
		r := *s.receipts[rec.InvocationID]
		return &r, nil
	}
	return nil, fmt.Errorf("%w: key %s", ErrIdempotencyConflict, request.IdempotencyKey)
}

func (s *MemoryStore) resolveActiveDeploymentForAdmit(ref string, expectedDeploymentGeneration int64) (*DeploymentRecord, error) {
	dep, err := s.resolveDepLocked(ref)
	if err != nil {
		return nil, fmt.Errorf("memory store admit invocation: %w", err)
	}
	if expectedDeploymentGeneration != 0 && dep.Generation != expectedDeploymentGeneration {
		return nil, fmt.Errorf("%w: deployment generation", ErrCASConflict)
	}
	if dep.Status != DeploymentActive {
		return nil, fmt.Errorf("%w: %s", ErrDeploymentInactive, dep.DeploymentID)
	}
	return dep, nil
}

func (s *MemoryStore) enforceDeploymentConcurrencyQuota(dep *DeploymentRecord) error {
	holding := 0
	for _, wf := range s.workflows {
		if wf.DeploymentID != dep.DeploymentID {
			continue
		}
		switch wf.Status {
		case WorkflowStatusPending, WorkflowStatusRunning, WorkflowStatusPauseRequested:
			holding++
		}
	}
	max := dep.MaxConcurrentRuns
	if max <= 0 {
		max = 1
	}
	if holding >= max {
		return fmt.Errorf("%w: holding %d", ErrAlreadyRunning, holding)
	}
	return nil
}

type memoryAdmissionTopology struct {
	invID        InvocationID
	wfID         WorkflowID
	primaryRunID RunID
	wf           *WorkflowRecord
	nodes        []*PipelineNode
	runs         []*RunRecord
	receipt      *InvocationReceipt
}

func buildMemoryAdmissionTopology(request *InvocationRequest, dep *DeploymentRecord, intent string, now time.Time) (*memoryAdmissionTopology, error) {
	invID, err := NewInvocationID()
	if err != nil {
		return nil, fmt.Errorf("memory store admit invocation: %w", err)
	}
	wfID, err := NewWorkflowID()
	if err != nil {
		return nil, fmt.Errorf("memory store admit invocation: %w", err)
	}
	kind := topologyKind(dep)
	stageCount := topologyStageCount(dep, kind)
	wf := newPendingWorkflowRecord(wfID, invID, dep, kind, request, now)

	pairs, primaryRunID, err := buildAdmissionNodeRuns(request, dep, wfID, kind, stageCount, now)
	if err != nil {
		return nil, err
	}
	nodes := make([]*PipelineNode, 0, len(pairs))
	runs := make([]*RunRecord, 0, len(pairs))
	for _, p := range pairs {
		nodes = append(nodes, p.node)
		runs = append(runs, p.run)
	}
	receipt := newInvocationReceipt(request, dep, invID, wfID, primaryRunID, intent, now)
	return &memoryAdmissionTopology{
		invID:        invID,
		wfID:         wfID,
		primaryRunID: primaryRunID,
		wf:           wf,
		nodes:        nodes,
		runs:         runs,
		receipt:      receipt,
	}, nil
}

func (s *MemoryStore) persistAdmissionTopology(topo *memoryAdmissionTopology, request *InvocationRequest, intent string, now time.Time) {
	s.workflows[topo.wfID] = topo.wf
	s.workflowGen[topo.wfID] = 1
	for _, node := range topo.nodes {
		s.nodes[node.NodeID] = node
		s.nodeGen[node.NodeID] = 1
	}
	for _, run := range topo.runs {
		s.runs[run.RunID] = run
		s.runGen[run.RunID] = 1
	}
	s.receipts[topo.invID] = topo.receipt
	ik := memKey(request.CallerIdentity, request.IdempotencyKey)
	s.idempotency[ik] = newAcceptedIdempotencyRecord(request, topo.invID, intent, now)
	s.activeTime[topo.wfID] = &ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		UpdatedAt:     now,
	}
}

func (s *MemoryStore) resolveDepLocked(ref string) (*DeploymentRecord, error) {
	if d, ok := s.deployments[DeploymentID(ref)]; ok {
		return d, nil
	}
	if a, ok := s.aliases[ref]; ok {
		d, ok := s.deployments[a.TargetDeploymentID]
		if !ok {
			return nil, fmt.Errorf("%w: deployment %s", ErrNotFound, a.TargetDeploymentID)
		}
		return d, nil
	}
	return nil, fmt.Errorf("%w: deployment ref %s", ErrNotFound, ref)
}

// MemoryStore.GetInvocationByIdempotency returns the invocation by idempotency.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetInvocationByIdempotency(ctx context.Context, callerIdentity, idempotencyKey string) (*InvocationReceipt, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.idempotency[memKey(callerIdentity, idempotencyKey)]
	if !ok {
		return nil, fmt.Errorf("%w: idempotency", ErrNotFound)
	}
	r := *s.receipts[rec.InvocationID]
	return &r, nil
}

// MemoryStore.ListInvocations lists the invocations.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListInvocations(ctx context.Context) ([]*InvocationReceipt, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*InvocationReceipt, 0, len(s.receipts))
	for _, r := range s.receipts {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.CreateRun creates the run.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateRun(ctx context.Context, run *RunRecord) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.RunID == "" {
		id, err := NewRunID()
		if err != nil {
			return fmt.Errorf("memory store create run: %w", err)
		}
		run.RunID = id
	}
	if _, ok := s.runs[run.RunID]; ok {
		return fmt.Errorf("%w: run %s", ErrAlreadyExists, run.RunID)
	}
	if run.SchemaVersion == "" {
		run.SchemaVersion = CurrentSchemaVersion
	}
	cp := *run
	s.runs[run.RunID] = &cp
	s.runGen[run.RunID] = 1
	return nil
}

// MemoryStore.GetRun returns the run.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetRun(ctx context.Context, runID RunID) (*RunRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[runID]
	if !ok {
		return nil, fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}
	cp := *r
	return &cp, nil
}

// MemoryStore.UpdateRun updates the run.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateRun(ctx context.Context, run *RunRecord, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runGen[run.RunID] != expectedGeneration {
		return fmt.Errorf("%w: run %s", ErrCASConflict, run.RunID)
	}
	run.UpdatedAt = s.now()
	cp := *run
	s.runs[run.RunID] = &cp
	s.runGen[run.RunID] = expectedGeneration + 1
	return nil
}

// MemoryStore.CreateAttempt creates the attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateAttempt(ctx context.Context, attempt *AttemptRecord) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt.AttemptID == "" {
		id, err := NewAttemptID()
		if err != nil {
			return fmt.Errorf("memory store create attempt: %w", err)
		}
		attempt.AttemptID = id
	}
	if attempt.Lease != nil {
		lid, err := NewLeaseID()
		if err != nil {
			return fmt.Errorf("memory store create attempt: %w", err)
		}
		attempt.Lease.LeaseID = lid
		if attempt.Lease.LeaseToken == "" {
			tok, err := generateID("tok-")
			if err != nil {
				return fmt.Errorf("memory store create attempt: %w", err)
			}
			attempt.Lease.LeaseToken = tok
		}
	}
	cp := *attempt
	if attempt.Lease != nil {
		l := *attempt.Lease
		cp.Lease = &l
	}
	s.attempts[attempt.AttemptID] = &cp
	s.attemptGen[attempt.AttemptID] = 1
	return nil
}

// MemoryStore.GetAttempt returns the attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetAttempt(ctx context.Context, attemptID AttemptID) (*AttemptRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.attempts[attemptID]
	if !ok {
		return nil, fmt.Errorf("%w: attempt %s", ErrNotFound, attemptID)
	}
	cp := *a
	return &cp, nil
}

// MemoryStore.UpdateAttempt updates the attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateAttempt(ctx context.Context, attempt *AttemptRecord, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attemptGen[attempt.AttemptID] != expectedGeneration {
		return fmt.Errorf("%w: attempt %s", ErrCASConflict, attempt.AttemptID)
	}
	if attempt.Lease != nil && attempt.Lease.LeaseID == "" {
		lid, err := NewLeaseID()
		if err != nil {
			return fmt.Errorf("memory store update attempt: %w", err)
		}
		attempt.Lease.LeaseID = lid
	}
	cp := *attempt
	s.attempts[attempt.AttemptID] = &cp
	s.attemptGen[attempt.AttemptID] = expectedGeneration + 1
	return nil
}

// MemoryStore.ListRuns lists the runs.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListRuns(ctx context.Context, workflowID WorkflowID) ([]*RunRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*RunRecord, 0)
	for _, r := range s.runs {
		if workflowID != "" && r.WorkflowID != workflowID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.ListAttempts lists the attempts.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListAttempts(ctx context.Context, runID RunID) ([]*AttemptRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AttemptRecord, 0)
	for _, a := range s.attempts {
		if a.RunID != runID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.AppendLedger appends ledger.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) AppendLedger(ctx context.Context, runID RunID, entry string) error {
	_ = ctx // interface compliance; store ops are local
	if len(entry) > maxLedgerLineBytes {
		return fmt.Errorf("%w: ledger line", ErrSizeCapExceeded)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledgers[runID] = append(s.ledgers[runID], entry)
	return nil
}

// MemoryStore.ReconcileInterrupted reconciles interrupted.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ReconcileInterrupted(ctx context.Context, runID RunID) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("%w: run %s", ErrNotFound, runID)
	}
	now := s.now()
	reason := FailureDaemonRestarted
	for _, a := range s.attempts {
		if a.RunID != runID || a.Status.IsTerminal() {
			continue
		}
		a.Status = AttemptStatusFailed
		a.FailureReason = &reason
		if a.Lease != nil {
			a.Lease.ExpiresAt = now
			a.Lease.LeaseToken = ""
		}
		a.UpdatedAt = now
		a.TerminatedAt = &now
		s.attemptGen[a.AttemptID]++
	}
	if !run.Status.IsTerminal() {
		run.Status = RunStatusFailed
		run.UpdatedAt = now
		run.TerminatedAt = &now
		s.runGen[runID]++
	}
	// Conservative close of open active-time segment.
	if run.WorkflowID != "" {
		if ledger, ok := s.activeTime[run.WorkflowID]; ok && ledger.RunningSegmentStartMs != nil {
			ledger.RunningSegmentStartMs = nil
			ledger.UpdatedAt = now
		}
	}
	return nil
}

// MemoryStore.CreateWorkflow creates the workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateWorkflow(ctx context.Context, wf *WorkflowRecord) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if wf.WorkflowID == "" {
		id, err := NewWorkflowID()
		if err != nil {
			return fmt.Errorf("memory store create workflow: %w", err)
		}
		wf.WorkflowID = id
	}
	if _, ok := s.workflows[wf.WorkflowID]; ok {
		return fmt.Errorf("%w: workflow %s", ErrAlreadyExists, wf.WorkflowID)
	}
	if wf.Generation == 0 {
		wf.Generation = 1
	}
	cp := *wf
	s.workflows[wf.WorkflowID] = &cp
	s.workflowGen[wf.WorkflowID] = cp.Generation
	s.activeTime[wf.WorkflowID] = &ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		UpdatedAt:     s.now(),
	}
	return nil
}

// MemoryStore.GetWorkflow returns the workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetWorkflow(ctx context.Context, workflowID WorkflowID) (*WorkflowRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	wf, ok := s.workflows[workflowID]
	if !ok {
		return nil, fmt.Errorf("%w: workflow %s", ErrNotFound, workflowID)
	}
	cp := *wf
	return &cp, nil
}

// MemoryStore.UpdateWorkflow updates the workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateWorkflow(ctx context.Context, wf *WorkflowRecord, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workflowGen[wf.WorkflowID] != expectedGeneration {
		return fmt.Errorf("%w: workflow %s", ErrCASConflict, wf.WorkflowID)
	}
	existing := s.workflows[wf.WorkflowID]
	if existing == nil {
		return fmt.Errorf("%w: workflow %s", ErrNotFound, wf.WorkflowID)
	}
	// Resume re-acquire concurrency under the same write lock as AdmitInvocation.
	if !holdsConcurrencySlot(existing.Status) && holdsConcurrencySlot(wf.Status) {
		if err := s.checkConcurrencyForResumeLocked(existing.DeploymentID); err != nil {
			return fmt.Errorf("memory store update workflow: %w", err)
		}
	}
	from := existing.Status
	wf.Generation = expectedGeneration + 1
	wf.UpdatedAt = s.now()
	cp := *wf
	s.workflows[wf.WorkflowID] = &cp
	s.workflowGen[wf.WorkflowID] = wf.Generation
	s.syncActiveTimeOnStatusChangeLocked(wf.WorkflowID, from, wf.Status)
	return nil
}

// MemoryStore.ListWorkflows lists the workflows.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListWorkflows(ctx context.Context) ([]*WorkflowRecord, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*WorkflowRecord, 0, len(s.workflows))
	for _, wf := range s.workflows {
		cp := *wf
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.CreateNode creates the node.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateNode(ctx context.Context, node *PipelineNode) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if node.NodeID == "" {
		id, err := NewNodeID()
		if err != nil {
			return fmt.Errorf("memory store create node: %w", err)
		}
		node.NodeID = id
	}
	cp := *node
	s.nodes[node.NodeID] = &cp
	s.nodeGen[node.NodeID] = 1
	return nil
}

// MemoryStore.GetNode returns the node.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetNode(ctx context.Context, nodeID NodeID) (*PipelineNode, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("%w: node %s", ErrNotFound, nodeID)
	}
	cp := *n
	return &cp, nil
}

// MemoryStore.UpdateNode updates the node.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateNode(ctx context.Context, node *PipelineNode, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.nodeGen[node.NodeID] != expectedGeneration {
		return fmt.Errorf("%w: node %s", ErrCASConflict, node.NodeID)
	}
	cp := *node
	s.nodes[node.NodeID] = &cp
	s.nodeGen[node.NodeID] = expectedGeneration + 1
	return nil
}

// MemoryStore.ListNodes lists the nodes.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListNodes(ctx context.Context, workflowID WorkflowID) ([]*PipelineNode, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*PipelineNode, 0)
	for _, n := range s.nodes {
		if n.WorkflowID != workflowID {
			continue
		}
		cp := *n
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.RegisterService registers service.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) RegisterService(ctx context.Context, svc *MCPServiceBinding) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc.ServiceID == "" {
		id, err := NewServiceID()
		if err != nil {
			return fmt.Errorf("memory store register service: %w", err)
		}
		svc.ServiceID = id
	}
	cp := *svc
	s.services[svc.ServiceID] = &cp
	s.serviceGen[svc.ServiceID] = 1
	return nil
}

// MemoryStore.UpdateService updates the service.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateService(ctx context.Context, svc *MCPServiceBinding, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serviceGen[svc.ServiceID] != expectedGeneration {
		return fmt.Errorf("%w: service %s", ErrCASConflict, svc.ServiceID)
	}
	cp := *svc
	s.services[svc.ServiceID] = &cp
	s.serviceGen[svc.ServiceID] = expectedGeneration + 1
	return nil
}

// MemoryStore.ListServices lists the services.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListServices(ctx context.Context, workflowID WorkflowID) ([]*MCPServiceBinding, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*MCPServiceBinding, 0)
	for _, svc := range s.services {
		if svc.WorkflowID != workflowID {
			continue
		}
		cp := *svc
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.CommitHandoff commits handoff.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CommitHandoff(ctx context.Context, handoff *HandoffEnvelope) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if handoff.HandoffID == "" {
		id, err := NewHandoffID()
		if err != nil {
			return fmt.Errorf("memory store commit handoff: %w", err)
		}
		handoff.HandoffID = id
	}
	if _, ok := s.handoffs[handoff.HandoffID]; ok {
		return nil
	}
	cp := *handoff
	s.handoffs[handoff.HandoffID] = &cp
	return nil
}

// MemoryStore.GetHandoff returns the handoff.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetHandoff(ctx context.Context, handoffID HandoffID) (*HandoffEnvelope, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.handoffs[handoffID]
	if !ok {
		return nil, fmt.Errorf("%w: handoff %s", ErrNotFound, handoffID)
	}
	cp := *h
	return &cp, nil
}

// MemoryStore.ListHandoffs lists the handoffs.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListHandoffs(ctx context.Context, workflowID WorkflowID) ([]*HandoffEnvelope, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*HandoffEnvelope, 0)
	for _, h := range s.handoffs {
		if h.WorkflowID != workflowID {
			continue
		}
		cp := *h
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.CreateChildBatch creates the child batch.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CreateChildBatch(ctx context.Context, batch *ChildBatch) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if batch.ChildBatchID == "" {
		id, err := NewChildBatchID()
		if err != nil {
			return fmt.Errorf("memory store create child batch: %w", err)
		}
		batch.ChildBatchID = id
	}
	cp := *batch
	s.childBatches[batch.ChildBatchID] = &cp
	s.childBatchGen[batch.ChildBatchID] = 1
	return nil
}

// MemoryStore.UpdateChildBatch updates the child batch.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) UpdateChildBatch(ctx context.Context, batch *ChildBatch, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.childBatchGen[batch.ChildBatchID] != expectedGeneration {
		return fmt.Errorf("%w: batch %s", ErrCASConflict, batch.ChildBatchID)
	}
	cp := *batch
	s.childBatches[batch.ChildBatchID] = &cp
	s.childBatchGen[batch.ChildBatchID] = expectedGeneration + 1
	return nil
}

// MemoryStore.ListChildBatches lists the child batches.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListChildBatches(ctx context.Context, workflowID WorkflowID) ([]*ChildBatch, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ChildBatch, 0)
	for _, b := range s.childBatches {
		if b.WorkflowID != workflowID {
			continue
		}
		cp := *b
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.CommitChildResult commits child result.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) CommitChildResult(ctx context.Context, result *ChildResult) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.ChildResultID == "" {
		id, err := NewChildResultID()
		if err != nil {
			return fmt.Errorf("memory store commit child result: %w", err)
		}
		result.ChildResultID = id
	}
	if _, ok := s.childResults[result.ChildResultID]; ok {
		return nil
	}
	if _, ok := s.childBatches[result.ChildBatchID]; !ok {
		return fmt.Errorf("%w: child batch %s", ErrNotFound, result.ChildBatchID)
	}
	cp := *result
	s.childResults[result.ChildResultID] = &cp
	return nil
}

// MemoryStore.ListChildResults lists the child results.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ListChildResults(ctx context.Context, childBatchID ChildBatchID) ([]*ChildResult, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ChildResult, 0)
	for _, r := range s.childResults {
		if r.ChildBatchID != childBatchID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// MemoryStore.RequestControl requests control.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) RequestControl(ctx context.Context, req *ControlRequest) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	// Idempotency: same workflow + key returns the original ControlRequestID.
	if req.IdempotencyKey != "" {
		for _, cr := range s.controls {
			if cr.WorkflowID == req.WorkflowID && cr.IdempotencyKey == req.IdempotencyKey {
				*req = *cr
				return nil
			}
		}
	}
	if req.ControlRequestID == "" {
		id, err := NewControlRequestID()
		if err != nil {
			return fmt.Errorf("memory store request control: %w", err)
		}
		req.ControlRequestID = id
	}
	cp := *req
	s.controls[req.ControlRequestID] = &cp
	if existing, ok := s.desired[req.WorkflowID]; ok && existing.CancelPrecedence && req.Command != ControlCancel {
		return nil
	}
	s.desired[req.WorkflowID] = &DesiredState{
		SchemaVersion:    CurrentSchemaVersion,
		WorkflowID:       req.WorkflowID,
		DesiredCommand:   req.Command,
		ControlRequestID: req.ControlRequestID,
		Generation:       req.ExpectedGeneration,
		CancelPrecedence: req.Command == ControlCancel,
		CreatedAt:        s.now(),
	}
	return nil
}

// MemoryStore.GetDesiredState returns the desired state.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) GetDesiredState(ctx context.Context, workflowID WorkflowID) (*DesiredState, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	ds, ok := s.desired[workflowID]
	if !ok {
		return nil, fmt.Errorf("%w: desired state", ErrNotFound)
	}
	cp := *ds
	return &cp, nil
}

// MemoryStore.AppendControlResult appends control result.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) AppendControlResult(ctx context.Context, req *ControlRequest, result interface{}) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	s.controlResults = append(s.controlResults, controlResultEntry{Req: req, Result: result})
	return nil
}

// MemoryStore.AppendLimitAmendment appends limit amendment.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) AppendLimitAmendment(ctx context.Context, workflowID WorkflowID, expectedAuthorityGeneration int64, amendment *LimitAmendment) error {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	wf, ok := s.workflows[workflowID]
	if !ok {
		return fmt.Errorf("%w: workflow %s", ErrNotFound, workflowID)
	}

	// Idempotency by workflow + key before authority CAS.
	if amendment.IdempotencyKey != "" {
		for _, a := range s.amendments {
			if a.WorkflowID == workflowID && a.IdempotencyKey == amendment.IdempotencyKey {
				if amendmentPayloadEqual(a, amendment) {
					*amendment = *a
					return nil // IDEMPOTENT_REPLAY
				}
				return fmt.Errorf("%w: amendment key %s", ErrIdempotencyConflict, amendment.IdempotencyKey)
			}
		}
	}

	if wf.AuthorityGeneration != expectedAuthorityGeneration {
		return fmt.Errorf("%w: authority", ErrCASConflict)
	}
	if amendment.NewMaxActiveDurationMs != 0 && amendment.NewMaxActiveDurationMs < wf.MaxActiveDurationMs {
		return fmt.Errorf("%w: decrease not allowed", ErrInvalidArgument)
	}
	amendment.BeforeMaxActiveDurationMs = wf.MaxActiveDurationMs
	amendment.BeforeMaxAttemptLeaseMs = wf.MaxAttemptLeaseMs
	amendment.BeforeMaxLLMSpendDecimal = wf.MaxLLMSpendDecimal
	if amendment.NewMaxActiveDurationMs > 0 {
		wf.MaxActiveDurationMs = amendment.NewMaxActiveDurationMs
	}
	if amendment.NewCurrentAttemptLeaseMs > 0 {
		wf.MaxAttemptLeaseMs = amendment.NewCurrentAttemptLeaseMs
	}
	if amendment.NewMaxLLMSpendDecimal != "" {
		wf.MaxLLMSpendDecimal = amendment.NewMaxLLMSpendDecimal
	}
	wf.AuthorityGeneration = expectedAuthorityGeneration + 1
	wf.Generation++
	s.workflowGen[workflowID] = wf.Generation
	if amendment.AmendmentID == "" {
		id, err := NewLimitAmendmentID()
		if err != nil {
			return fmt.Errorf("memory store append limit amendment: %w", err)
		}
		amendment.AmendmentID = id
	}
	amendment.SchemaVersion = CurrentSchemaVersion
	amendment.WorkflowID = workflowID
	amendment.ExpectedAuthorityGeneration = expectedAuthorityGeneration
	amendment.NewAuthorityGeneration = wf.AuthorityGeneration
	amendment.AfterMaxActiveDurationMs = wf.MaxActiveDurationMs
	amendment.AfterMaxAttemptLeaseMs = wf.MaxAttemptLeaseMs
	amendment.AfterMaxLLMSpendDecimal = wf.MaxLLMSpendDecimal
	if amendment.CreatedAt.IsZero() {
		amendment.CreatedAt = s.now()
	}
	cp := *amendment
	s.amendments[amendment.AmendmentID] = &cp
	return nil
}

// MemoryStore.ApplyTransition applies transition.
//
// It returns an error if the operation fails or inputs are invalid.
func (s *MemoryStore) ApplyTransition(ctx context.Context, workflowID WorkflowID, expectedGeneration int64, command string) error {
	_ = ctx     // interface compliance; store ops are local
	_ = command // interface compliance; memory store is no-op command
	s.mu.Lock()
	defer s.mu.Unlock()
	wf, ok := s.workflows[workflowID]
	if !ok {
		return fmt.Errorf("%w: workflow %s", ErrNotFound, workflowID)
	}
	if s.workflowGen[workflowID] != expectedGeneration {
		return fmt.Errorf("%w: workflow %s", ErrCASConflict, workflowID)
	}
	wf.Generation = expectedGeneration + 1
	wf.UpdatedAt = s.now()
	s.workflowGen[workflowID] = wf.Generation
	return nil
}

func (s *MemoryStore) checkConcurrencyForResumeLocked(depID DeploymentID) error {
	if depID == "" {
		return nil
	}
	dep, ok := s.deployments[depID]
	if !ok {
		return nil
	}
	holding := 0
	for _, wf := range s.workflows {
		if wf.DeploymentID != depID {
			continue
		}
		if holdsConcurrencySlot(wf.Status) {
			holding++
		}
	}
	max := dep.MaxConcurrentRuns
	if max <= 0 {
		max = 1
	}
	if holding >= max {
		return fmt.Errorf("%w: holding %d", ErrAlreadyRunning, holding)
	}
	return nil
}

// GetActiveTimeLedger loads the in-memory active-time ledger.
func (s *MemoryStore) GetActiveTimeLedger(ctx context.Context, workflowID WorkflowID) (*ActiveTimeLedger, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.RLock()
	defer s.mu.RUnlock()
	ledger, ok := s.activeTime[workflowID]
	if !ok {
		return nil, fmt.Errorf("%w: active time ledger %s", ErrNotFound, workflowID)
	}
	cp := *ledger
	if ledger.RunningSegmentStartMs != nil {
		v := *ledger.RunningSegmentStartMs
		cp.RunningSegmentStartMs = &v
	}
	return &cp, nil
}

// PutActiveTimeLedger stores the active-time ledger.
// expectedGeneration is the CAS generation. Pass 0 to bypass CAS.
func (s *MemoryStore) PutActiveTimeLedger(ctx context.Context, workflowID WorkflowID, ledger *ActiveTimeLedger, expectedGeneration int64) error {
	_ = ctx // interface compliance; store ops are local
	if ledger == nil {
		return fmt.Errorf("%w: nil active time ledger", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ledger.SchemaVersion == "" {
		ledger.SchemaVersion = CurrentSchemaVersion
	}
	ledger.UpdatedAt = s.now()
	cp := *ledger
	if ledger.RunningSegmentStartMs != nil {
		v := *ledger.RunningSegmentStartMs
		cp.RunningSegmentStartMs = &v
	}
	s.activeTime[workflowID] = &cp
	return nil
}

// GetActiveTimeLedgerGeneration returns the CAS generation for the active-time
// ledger. MemoryStore does not enforce CAS so this returns 0.
func (s *MemoryStore) GetActiveTimeLedgerGeneration(ctx context.Context, workflowID WorkflowID) (int64, error) {
	_ = ctx // interface compliance; store ops are local
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.activeTime[workflowID]; ok {
		return 0, nil
	}
	return 0, fmt.Errorf("%w: active time ledger not found", ErrNotFound)
}

func (s *MemoryStore) syncActiveTimeOnStatusChangeLocked(wfID WorkflowID, from, to WorkflowStatus) {
	if from == to {
		return
	}
	ledger, ok := s.activeTime[wfID]
	if !ok {
		ledger = &ActiveTimeLedger{SchemaVersion: CurrentSchemaVersion}
		s.activeTime[wfID] = ledger
	}
	nowMs := s.now().UnixMilli()
	fromCharge := chargesActiveTime(from)
	toCharge := chargesActiveTime(to)
	if fromCharge && !toCharge {
		if ledger.RunningSegmentStartMs != nil {
			delta := nowMs - *ledger.RunningSegmentStartMs
			if delta > 0 {
				ledger.ConsumedMs += delta
			}
			ledger.RunningSegmentStartMs = nil
			ledger.SegmentStartWallMs = nil
		}
		if to == WorkflowStatusPaused || to == WorkflowStatusNeedsReplan {
			ledger.FrozenConsumedMs = ledger.ConsumedMs
		}
	}
	if !fromCharge && toCharge {
		ledger.RunningSegmentStartMs = &nowMs
		ledger.SegmentStartWallMs = &nowMs
		ledger.FrozenConsumedMs = 0
	}
	if to.IsTerminal() {
		if ledger.RunningSegmentStartMs != nil {
			delta := nowMs - *ledger.RunningSegmentStartMs
			if delta > 0 {
				ledger.ConsumedMs += delta
			}
			ledger.RunningSegmentStartMs = nil
			ledger.SegmentStartWallMs = nil
		}
	}
	ledger.UpdatedAt = s.now()
}
