package routedrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Topology metadata keys stored in DeploymentRecord.NestedPackageDigests.
const (
	metaWorkflowKind = "__workflow_kind"
	metaStageCount   = "__stage_count"
)

// LocalStore is a protected file-backed implementation of DeploymentStore,
// RunStore, and WorkflowStore under a locked directory layout.
type LocalStore struct {
	root string
	mu   sync.Mutex
	now  func() time.Time
	reg  *MigrationRegistry
}

// LocalStoreOption configures a LocalStore.
type LocalStoreOption func(*LocalStore)

// WithClock injects a clock for deterministic tests.
func WithClock(now func() time.Time) LocalStoreOption {
	return func(s *LocalStore) {
		if now != nil {
			s.now = now
		}
	}
}

// WithMigrationRegistry sets the schema migration registry.
func WithMigrationRegistry(reg *MigrationRegistry) LocalStoreOption {
	return func(s *LocalStore) {
		if reg != nil {
			s.reg = reg
		}
	}
}

// OpenLocalStore opens or initializes a local store rooted at root
// (typically ~/.agentpaas/state). Creates protected directory layout.
func OpenLocalStore(root string, opts ...LocalStoreOption) (*LocalStore, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: empty root", ErrInvalidArgument)
	}
	s := &LocalStore{
		root: root,
		now:  func() time.Time { return time.Now().UTC() },
		reg:  DefaultMigrationRegistry(),
	}
	for _, o := range opts {
		o(s)
	}
	if err := mkdirProtected(root); err != nil {
		return nil, err
	}
	for _, sub := range []string{
		filepath.Join(root, "deployments", "deployments"),
		filepath.Join(root, "deployments", "aliases"),
		filepath.Join(root, "deployments", "invocations"),
		filepath.Join(root, "deployments", "transactions"),
		filepath.Join(root, "runs"),
		filepath.Join(root, "workflows"),
	} {
		if err := mkdirProtected(sub); err != nil {
			return nil, err
		}
	}
	if err := cleanupOrphanTemps(root); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *LocalStore) deploymentsDir() string {
	return filepath.Join(s.root, "deployments", "deployments")
}
func (s *LocalStore) aliasesDir() string {
	return filepath.Join(s.root, "deployments", "aliases")
}
func (s *LocalStore) invocationsDir() string {
	return filepath.Join(s.root, "deployments", "invocations")
}
func (s *LocalStore) runsDir() string {
	return filepath.Join(s.root, "runs")
}
func (s *LocalStore) workflowsDir() string {
	return filepath.Join(s.root, "workflows")
}

// Ensure LocalStore implements the three store interfaces.
var (
	_ DeploymentStore = (*LocalStore)(nil)
	_ RunStore        = (*LocalStore)(nil)
	_ WorkflowStore   = (*LocalStore)(nil)
)

// ---------------------------------------------------------------------------
// DeploymentStore
// ---------------------------------------------------------------------------

func (s *LocalStore) CreateDeployment(ctx context.Context, dep *DeploymentRecord) error {
	_ = ctx
	if dep == nil {
		return fmt.Errorf("%w: nil deployment", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if dep.DeploymentID == "" {
		id, err := NewDeploymentID()
		if err != nil {
			return err
		}
		dep.DeploymentID = id
	}
	if dep.SchemaVersion == "" {
		dep.SchemaVersion = CurrentSchemaVersion
	}
	if dep.Generation == 0 {
		dep.Generation = 1
	}
	if dep.CreatedAt.IsZero() {
		dep.CreatedAt = s.now()
	}
	if dep.MaxConcurrentRuns <= 0 {
		dep.MaxConcurrentRuns = 1
	}
	path := s.deploymentPath(dep.DeploymentID)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: deployment %s", ErrAlreadyExists, dep.DeploymentID)
	}
	return s.writeJSON(path, dep.Generation, dep)
}

func (s *LocalStore) GetDeployment(ctx context.Context, deploymentID DeploymentID) (*DeploymentRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var dep DeploymentRecord
	if _, err := s.readJSON(s.deploymentPath(deploymentID), &dep); err != nil {
		return nil, err
	}
	return &dep, nil
}

func (s *LocalStore) ListDeployments(ctx context.Context) ([]*DeploymentRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.deploymentsDir()
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*DeploymentRecord, 0, len(names))
	for _, name := range names {
		var dep DeploymentRecord
		if _, err := s.readJSON(filepath.Join(dir, name), &dep); err != nil {
			return nil, err
		}
		out = append(out, &dep)
	}
	return out, nil
}

func (s *LocalStore) SetDeploymentStatus(ctx context.Context, deploymentID DeploymentID, status DeploymentStatus, expectedGeneration int64) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.deploymentPath(deploymentID)
	var dep DeploymentRecord
	gen, err := s.readJSON(path, &dep)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: deployment %s expected %d got %d", ErrCASConflict, deploymentID, expectedGeneration, gen)
	}
	dep.Status = status
	dep.Generation = expectedGeneration + 1
	now := s.now()
	switch status {
	case DeploymentActive:
		dep.ActivatedAt = &now
	case DeploymentInactive:
		dep.DeactivatedAt = &now
	}
	return s.writeJSON(path, dep.Generation, &dep)
}

func (s *LocalStore) CompareAndSwapAlias(ctx context.Context, alias *AliasRecord) error {
	_ = ctx
	if alias == nil || alias.Alias == "" {
		return fmt.Errorf("%w: alias", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.aliasPath(alias.Alias)
	var existing AliasRecord
	gen, err := s.readJSON(path, &existing)
	if err != nil && !os.IsNotExist(err) && !errorsIsNotFound(err) {
		return err
	}
	// Work on a copy so we never mutate the caller's struct.
	cp := *alias
	if gen > 0 || existing.Alias != "" {
		// Update path: cp.Generation is the expected current generation.
		if existing.Generation != cp.Generation {
			return fmt.Errorf("%w: alias %s expected %d got %d", ErrCASConflict, cp.Alias, cp.Generation, existing.Generation)
		}
		cp.Generation = existing.Generation + 1
	} else {
		// Create
		if cp.Generation == 0 {
			cp.Generation = 1
		}
	}
	if cp.SchemaVersion == "" {
		cp.SchemaVersion = CurrentSchemaVersion
	}
	cp.UpdatedAt = s.now()
	return s.writeJSON(path, cp.Generation, &cp)
}

func (s *LocalStore) ResolveAlias(ctx context.Context, alias string) (*AliasRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec AliasRecord
	if _, err := s.readJSON(s.aliasPath(alias), &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *LocalStore) ListAliases(ctx context.Context) ([]*AliasRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.aliasesDir()
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*AliasRecord, 0, len(names))
	for _, name := range names {
		var rec AliasRecord
		if _, err := s.readJSON(filepath.Join(dir, name), &rec); err != nil {
			return nil, err
		}
		out = append(out, &rec)
	}
	return out, nil
}

func (s *LocalStore) AdmitInvocation(ctx context.Context, request *InvocationRequest, expectedDeploymentGeneration int64) (*InvocationReceipt, error) {
	_ = ctx
	if request == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidArgument)
	}
	if request.IdempotencyKey == "" || request.CallerIdentity == "" {
		return nil, fmt.Errorf("%w: idempotency_key and caller_identity required", ErrInvalidArgument)
	}
	if err := checkStringCap("input_json", request.InputJSON); err != nil {
		return nil, err
	}
	if len(request.InputJSON) > maxInputJSONBytes {
		return nil, fmt.Errorf("%w: input_json", ErrSizeCapExceeded)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	intent := computeIntentDigest(request)

	// 1. Idempotency lookup
	if receipt, rec, err := s.loadIdempotency(request.CallerIdentity, request.IdempotencyKey); err == nil {
		if rec.InvocationIntentDigest == intent {
			// Exact replay — return existing receipt even if alias moved.
			return receipt, nil
		}
		return nil, fmt.Errorf("%w: key %s", ErrIdempotencyConflict, request.IdempotencyKey)
	} else if !errorsIsNotFound(err) {
		return nil, err
	}

	// 2. Resolve deployment (alias or exact).
	dep, err := s.resolveDeploymentRefLocked(request.RequestedDeploymentRef)
	if err != nil {
		return nil, err
	}
	if expectedDeploymentGeneration != 0 && dep.Generation != expectedDeploymentGeneration {
		return nil, fmt.Errorf("%w: deployment generation expected %d got %d", ErrCASConflict, expectedDeploymentGeneration, dep.Generation)
	}
	if dep.Status != DeploymentActive {
		return nil, fmt.Errorf("%w: %s", ErrDeploymentInactive, dep.DeploymentID)
	}

	// 3. Concurrency check — slot-holding workflows for this deployment.
	holding, err := s.countSlotHoldingLocked(dep.DeploymentID)
	if err != nil {
		return nil, err
	}
	max := dep.MaxConcurrentRuns
	if max <= 0 {
		max = 1
	}
	if holding >= max {
		// Never persist queued state on ALREADY_RUNNING.
		return nil, fmt.Errorf("%w: deployment %s holding %d max %d", ErrAlreadyRunning, dep.DeploymentID, holding, max)
	}

	// 4. Allocate identities and topology records.
	invID, err := NewInvocationID()
	if err != nil {
		return nil, err
	}
	wfID, err := NewWorkflowID()
	if err != nil {
		return nil, err
	}
	now := s.now()
	kind := topologyKind(dep)
	stageCount := topologyStageCount(dep, kind)

	wf := &WorkflowRecord{
		SchemaVersion:      CurrentSchemaVersion,
		WorkflowID:         wfID,
		WorkflowKind:       kind,
		InvocationID:       invID,
		DeploymentID:       dep.DeploymentID,
		Status:             WorkflowStatusPending,
		Generation:         1,
		PolicyDigest:       dep.PolicyDigest,
		MaxActiveDurationMs: request.InitialMaxActiveDurationMs,
		MaxAttemptLeaseMs:  request.InitialAttemptLeaseMs,
		MaxLLMSpendDecimal: request.InitialMaxCostUsdDecimal,
		AuthorityGeneration: 1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	var pairs []nodeRun
	var primaryRunID RunID

	switch kind {
	case "pipeline":
		for i := 0; i < stageCount; i++ {
			nodeID, err := NewNodeID()
			if err != nil {
				return nil, err
			}
			runID, err := NewRunID()
			if err != nil {
				return nil, err
			}
			if i == 0 {
				primaryRunID = runID
			}
			st := NodeStatusPending
			if i == 0 {
				st = NodeStatusReady
			}
			nid := nodeID
			pairs = append(pairs, nodeRun{
				node: &PipelineNode{
					SchemaVersion: CurrentSchemaVersion,
					NodeID:        nodeID,
					WorkflowID:    wfID,
					Status:        st,
					RunID:         runID,
					StageOrder:    i,
					PackageName:   stagePackageName(dep, i),
					PackageVersion: stagePackageVersion(dep, i),
					CreatedAt:     now,
					UpdatedAt:     now,
				},
				run: &RunRecord{
					SchemaVersion:      CurrentSchemaVersion,
					RunID:              runID,
					WorkflowID:         wfID,
					Status:             RunStatusPending,
					RunKind:            "pipeline_stage",
					PolicyDigest:       dep.PolicyDigest,
					NodeID:             &nid,
					MaxActiveDurationMs: request.InitialMaxActiveDurationMs,
					MaxAttemptLeaseMs:  request.InitialAttemptLeaseMs,
					MaxLLMSpendDecimal: request.InitialMaxCostUsdDecimal,
					CreatedAt:          now,
					UpdatedAt:          now,
				},
			})
		}
	case "parent_child":
		nodeID, err := NewNodeID()
		if err != nil {
			return nil, err
		}
		runID, err := NewRunID()
		if err != nil {
			return nil, err
		}
		primaryRunID = runID
		nid := nodeID
		pairs = append(pairs, nodeRun{
			node: &PipelineNode{
				SchemaVersion: CurrentSchemaVersion,
				NodeID:        nodeID,
				WorkflowID:    wfID,
				Status:        NodeStatusReady,
				RunID:         runID,
				StageOrder:    0,
				PackageName:   dep.PackageName,
				PackageVersion: dep.PackageVersion,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			run: &RunRecord{
				SchemaVersion:      CurrentSchemaVersion,
				RunID:              runID,
				WorkflowID:         wfID,
				Status:             RunStatusPending,
				RunKind:            "parent",
				PolicyDigest:       dep.PolicyDigest,
				NodeID:             &nid,
				MaxActiveDurationMs: request.InitialMaxActiveDurationMs,
				MaxAttemptLeaseMs:  request.InitialAttemptLeaseMs,
				MaxLLMSpendDecimal: request.InitialMaxCostUsdDecimal,
				CreatedAt:          now,
				UpdatedAt:          now,
			},
		})
	default: // standalone
		nodeID, err := NewNodeID()
		if err != nil {
			return nil, err
		}
		runID, err := NewRunID()
		if err != nil {
			return nil, err
		}
		primaryRunID = runID
		nid := nodeID
		pairs = append(pairs, nodeRun{
			node: &PipelineNode{
				SchemaVersion: CurrentSchemaVersion,
				NodeID:        nodeID,
				WorkflowID:    wfID,
				Status:        NodeStatusReady,
				RunID:         runID,
				StageOrder:    0,
				PackageName:   dep.PackageName,
				PackageVersion: dep.PackageVersion,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			run: &RunRecord{
				SchemaVersion:      CurrentSchemaVersion,
				RunID:              runID,
				WorkflowID:         wfID,
				Status:             RunStatusPending,
				RunKind:            "standalone",
				PolicyDigest:       dep.PolicyDigest,
				NodeID:             &nid,
				MaxActiveDurationMs: request.InitialMaxActiveDurationMs,
				MaxAttemptLeaseMs:  request.InitialAttemptLeaseMs,
				MaxLLMSpendDecimal: request.InitialMaxCostUsdDecimal,
				CreatedAt:          now,
				UpdatedAt:          now,
			},
		})
	}

	receipt := &InvocationReceipt{
		SchemaVersion:             CurrentSchemaVersion,
		InvocationID:              invID,
		WorkflowID:                wfID,
		RunID:                     primaryRunID,
		ResolvedDeploymentID:      dep.DeploymentID,
		ResolvedDeploymentVersion: dep.PackageVersion,
		ResolvedDeploymentDigest:  dep.BundleDigest,
		NestedPackageDigests:      copyStringMap(dep.NestedPackageDigests),
		RequestedDeploymentRef:    request.RequestedDeploymentRef,
		InvocationIntentDigest:    intent,
		CallerIdentity:            request.CallerIdentity,
		InitialMaxActiveDurationMs: request.InitialMaxActiveDurationMs,
		InitialAttemptLeaseMs:     request.InitialAttemptLeaseMs,
		InitialMaxCostUsdDecimal:  request.InitialMaxCostUsdDecimal,
		AdmittedAt:                now,
	}

	idem := &DurableIdempotencyRecord{
		SchemaVersion:          CurrentSchemaVersion,
		InvocationID:           invID,
		CallerIdentity:         request.CallerIdentity,
		IdempotencyKey:         request.IdempotencyKey,
		InvocationIntentDigest: intent,
		Outcome:                AdmissionAccepted,
		CreatedAt:              now,
	}

	// 5. Persist atomically via admission transaction record + materialization.
	if err := s.commitAdmission(request, receipt, idem, wf, pairs); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *LocalStore) commitAdmission(req *InvocationRequest, receipt *InvocationReceipt, idem *DurableIdempotencyRecord, wf *WorkflowRecord, pairs []nodeRun) error {
	// Write all durable state; on failure, best-effort cleanup is not required
	// for crash safety if we write idempotency last — order: workflow/nodes/runs
	// first, then receipt+idempotency. Replay of partial admission without
	// idempotency record allows retry; with idempotency, replay returns receipt.
	if err := s.writeJSON(s.workflowPath(wf.WorkflowID), wf.Generation, wf); err != nil {
		return err
	}
	for _, p := range pairs {
		if err := s.writeJSON(s.nodePath(wf.WorkflowID, p.node.NodeID), 1, p.node); err != nil {
			return err
		}
		if err := mkdirProtected(filepath.Join(s.runsDir(), safeID(string(p.run.RunID)))); err != nil {
			return err
		}
		if err := s.writeJSON(s.runPath(p.run.RunID), 1, p.run); err != nil {
			return err
		}
	}
	if err := s.writeJSON(s.receiptPath(receipt.InvocationID), 1, receipt); err != nil {
		return err
	}
	if err := s.writeJSON(s.idempotencyPath(req.CallerIdentity, req.IdempotencyKey), 1, idem); err != nil {
		return err
	}
	// Initialize active-time ledger for the admitted workflow.
	if err := s.saveActiveTimeLedgerLocked(wf.WorkflowID, &ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		UpdatedAt:     s.now(),
	}, 1); err != nil {
		return err
	}
	// Index receipt by invocation for ListInvocations.
	return nil
}

// nodeRun is defined in AdmitInvocation scope — need package level for commitAdmission
type nodeRun struct {
	node *PipelineNode
	run  *RunRecord
}

func (s *LocalStore) GetInvocationByIdempotency(ctx context.Context, callerIdentity, idempotencyKey string) (*InvocationReceipt, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	receipt, _, err := s.loadIdempotency(callerIdentity, idempotencyKey)
	return receipt, err
}

func (s *LocalStore) ListInvocations(ctx context.Context) ([]*InvocationReceipt, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.invocationsDir(), "_receipts")
	names, err := listJSONNames(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*InvocationReceipt{}, nil
		}
		// listJSONNames returns empty on missing
		if errorsIsNotFound(err) {
			return []*InvocationReceipt{}, nil
		}
	}
	if names == nil {
		// try walk receipts
		_ = dir
	}
	// Receipts stored under invocations/_receipts/<inv_id>.json
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*InvocationReceipt{}, nil
	}
	names, err = listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*InvocationReceipt, 0, len(names))
	for _, name := range names {
		var r InvocationReceipt
		if _, err := s.readJSON(filepath.Join(dir, name), &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, nil
}

func (s *LocalStore) receiptPath(id InvocationID) string {
	return filepath.Join(s.invocationsDir(), "_receipts", safeID(string(id))+".json")
}

func (s *LocalStore) loadIdempotency(caller, key string) (*InvocationReceipt, *DurableIdempotencyRecord, error) {
	var rec DurableIdempotencyRecord
	if _, err := s.readJSON(s.idempotencyPath(caller, key), &rec); err != nil {
		return nil, nil, err
	}
	var receipt InvocationReceipt
	if _, err := s.readJSON(s.receiptPath(rec.InvocationID), &receipt); err != nil {
		return nil, nil, err
	}
	return &receipt, &rec, nil
}

func (s *LocalStore) resolveDeploymentRefLocked(ref string) (*DeploymentRecord, error) {
	// Exact deployment ID: dep-...
	if strings.HasPrefix(ref, PrefixDeployment) {
		var dep DeploymentRecord
		if _, err := s.readJSON(s.deploymentPath(DeploymentID(ref)), &dep); err != nil {
			return nil, err
		}
		return &dep, nil
	}
	// Alias
	var alias AliasRecord
	if _, err := s.readJSON(s.aliasPath(ref), &alias); err != nil {
		return nil, err
	}
	var dep DeploymentRecord
	if _, err := s.readJSON(s.deploymentPath(alias.TargetDeploymentID), &dep); err != nil {
		return nil, err
	}
	return &dep, nil
}

func (s *LocalStore) countSlotHoldingLocked(depID DeploymentID) (int, error) {
	dir := s.workflowsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var wf WorkflowRecord
		path := filepath.Join(dir, e.Name(), "workflow.json")
		if _, err := s.readJSON(path, &wf); err != nil {
			if errorsIsNotFound(err) || os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if wf.DeploymentID != depID {
			continue
		}
		// Slot-holding: PENDING, RUNNING, PAUSE_REQUESTED
		switch wf.Status {
		case WorkflowStatusPending, WorkflowStatusRunning, WorkflowStatusPauseRequested:
			n++
		}
	}
	return n, nil
}

func (s *LocalStore) deploymentPath(id DeploymentID) string {
	return filepath.Join(s.deploymentsDir(), safeID(string(id))+".json")
}
func (s *LocalStore) aliasPath(alias string) string {
	return filepath.Join(s.aliasesDir(), escapeAlias(alias)+".json")
}
func (s *LocalStore) idempotencyPath(caller, key string) string {
	scope := safeID(caller)
	if len(scope) > 64 {
		sum := sha256.Sum256([]byte(caller))
		scope = "c-" + hex.EncodeToString(sum[:12])
	}
	return filepath.Join(s.invocationsDir(), scope, idempotencyPathKey(caller, key)+".json")
}
func (s *LocalStore) workflowPath(id WorkflowID) string {
	return filepath.Join(s.workflowsDir(), safeID(string(id)), "workflow.json")
}
func (s *LocalStore) nodePath(wf WorkflowID, node NodeID) string {
	return filepath.Join(s.workflowsDir(), safeID(string(wf)), "nodes", safeID(string(node))+".json")
}
func (s *LocalStore) runPath(id RunID) string {
	return filepath.Join(s.runsDir(), safeID(string(id)), "run.json")
}
func (s *LocalStore) attemptPath(run RunID, att AttemptID) string {
	return filepath.Join(s.runsDir(), safeID(string(run)), "attempts", safeID(string(att))+".json")
}

// ---------------------------------------------------------------------------
// RunStore
// ---------------------------------------------------------------------------

func (s *LocalStore) CreateRun(ctx context.Context, run *RunRecord) error {
	_ = ctx
	if run == nil {
		return fmt.Errorf("%w: nil run", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.RunID == "" {
		id, err := NewRunID()
		if err != nil {
			return err
		}
		run.RunID = id
	}
	if run.SchemaVersion == "" {
		run.SchemaVersion = CurrentSchemaVersion
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = s.now()
	}
	run.UpdatedAt = s.now()
	path := s.runPath(run.RunID)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: run %s", ErrAlreadyExists, run.RunID)
	}
	return s.writeJSON(path, 1, run)
}

func (s *LocalStore) GetRun(ctx context.Context, runID RunID) (*RunRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var run RunRecord
	if _, err := s.readJSON(s.runPath(runID), &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetRunGeneration returns the persisted envelope generation of the run record.
// The supervisor (B30-T05) uses this to drive compare-and-swap transitions on
// runs whose RunRecord struct does not itself carry a Generation field.
func (s *LocalStore) GetRunGeneration(ctx context.Context, runID RunID) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var run RunRecord
	gen, err := s.readJSON(s.runPath(runID), &run)
	if err != nil {
		return 0, err
	}
	return gen, nil
}

// GetAttemptGeneration returns the persisted envelope generation of the attempt
// record. The supervisor (B30-T05) uses this to drive compare-and-swap
// transitions on attempts whose AttemptRecord struct does not itself carry a
// Generation field.
func (s *LocalStore) GetAttemptGeneration(ctx context.Context, attemptID AttemptID) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	att, err := s.findAttemptLocked(attemptID)
	if err != nil {
		return 0, err
	}
	path := s.attemptPath(att.RunID, att.AttemptID)
	var rec AttemptRecord
	gen, err := s.readJSON(path, &rec)
	if err != nil {
		return 0, err
	}
	return gen, nil
}

func (s *LocalStore) UpdateRun(ctx context.Context, run *RunRecord, expectedGeneration int64) error {
	_ = ctx
	if run == nil {
		return fmt.Errorf("%w: nil run", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.runPath(run.RunID)
	var existing RunRecord
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: run %s expected %d got %d", ErrCASConflict, run.RunID, expectedGeneration, gen)
	}
	run.UpdatedAt = s.now()
	if run.SchemaVersion == "" {
		run.SchemaVersion = CurrentSchemaVersion
	}
	return s.writeJSON(path, expectedGeneration+1, run)
}

func (s *LocalStore) CreateAttempt(ctx context.Context, attempt *AttemptRecord) error {
	_ = ctx
	if attempt == nil {
		return fmt.Errorf("%w: nil attempt", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if attempt.AttemptID == "" {
		id, err := NewAttemptID()
		if err != nil {
			return err
		}
		attempt.AttemptID = id
	}
	// Lease IDs are store-issued fencing tokens only.
	if attempt.Lease != nil {
		if attempt.Lease.LeaseID != "" {
			// Reject caller-selected values by overwriting with store-generated token.
			lid, err := NewLeaseID()
			if err != nil {
				return err
			}
			attempt.Lease.LeaseID = lid
		} else {
			lid, err := NewLeaseID()
			if err != nil {
				return err
			}
			attempt.Lease.LeaseID = lid
		}
		if attempt.Lease.LeaseToken == "" {
			tok, err := generateID("tok-")
			if err != nil {
				return err
			}
			attempt.Lease.LeaseToken = tok
		}
		attempt.Lease.SchemaVersion = CurrentSchemaVersion
		attempt.Lease.AttemptID = attempt.AttemptID
		attempt.Lease.RunID = attempt.RunID
	}
	if attempt.SchemaVersion == "" {
		attempt.SchemaVersion = CurrentSchemaVersion
	}
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = s.now()
	}
	attempt.UpdatedAt = s.now()
	path := s.attemptPath(attempt.RunID, attempt.AttemptID)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: attempt %s", ErrAlreadyExists, attempt.AttemptID)
	}
	return s.writeJSON(path, 1, attempt)
}

func (s *LocalStore) GetAttempt(ctx context.Context, attemptID AttemptID) (*AttemptRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	// Attempts live under run dirs; scan if needed. Prefer index by walking runs.
	att, err := s.findAttemptLocked(attemptID)
	if err != nil {
		return nil, err
	}
	return att, nil
}

func (s *LocalStore) findAttemptLocked(attemptID AttemptID) (*AttemptRecord, error) {
	dir := s.runsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: attempt %s", ErrNotFound, attemptID)
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "attempts", safeID(string(attemptID))+".json")
		var att AttemptRecord
		if _, err := s.readJSON(path, &att); err == nil {
			return &att, nil
		}
	}
	return nil, fmt.Errorf("%w: attempt %s", ErrNotFound, attemptID)
}

func (s *LocalStore) UpdateAttempt(ctx context.Context, attempt *AttemptRecord, expectedGeneration int64) error {
	_ = ctx
	if attempt == nil {
		return fmt.Errorf("%w: nil attempt", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.attemptPath(attempt.RunID, attempt.AttemptID)
	var existing AttemptRecord
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: attempt %s expected %d got %d", ErrCASConflict, attempt.AttemptID, expectedGeneration, gen)
	}
	// Never accept caller-selected lease IDs on update either.
	if attempt.Lease != nil && existing.Lease != nil {
		if attempt.Lease.LeaseID != "" && attempt.Lease.LeaseID != existing.Lease.LeaseID {
			// New lease must be store-generated — if caller changed it, regenerate.
			lid, err := NewLeaseID()
			if err != nil {
				return err
			}
			attempt.Lease.LeaseID = lid
		}
	} else if attempt.Lease != nil && attempt.Lease.LeaseID == "" {
		lid, err := NewLeaseID()
		if err != nil {
			return err
		}
		attempt.Lease.LeaseID = lid
	}
	attempt.UpdatedAt = s.now()
	return s.writeJSON(path, expectedGeneration+1, attempt)
}

func (s *LocalStore) ListRuns(ctx context.Context, workflowID WorkflowID) ([]*RunRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.runsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*RunRecord{}, nil
		}
		return nil, err
	}
	out := make([]*RunRecord, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var run RunRecord
		if _, err := s.readJSON(filepath.Join(dir, e.Name(), "run.json"), &run); err != nil {
			if errorsIsNotFound(err) || os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if workflowID != "" && run.WorkflowID != workflowID {
			continue
		}
		out = append(out, &run)
		if len(out) >= maxRecordsPerList {
			break
		}
	}
	return out, nil
}

func (s *LocalStore) ListAttempts(ctx context.Context, runID RunID) ([]*AttemptRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.runsDir(), safeID(string(runID)), "attempts")
	names, err := listJSONNames(dir)
	if err != nil {
		if os.IsNotExist(err) || errorsIsNotFound(err) {
			return []*AttemptRecord{}, nil
		}
		// missing dir
		if _, e := os.Lstat(dir); os.IsNotExist(e) {
			return []*AttemptRecord{}, nil
		}
		return nil, err
	}
	out := make([]*AttemptRecord, 0, len(names))
	for _, name := range names {
		var att AttemptRecord
		if _, err := s.readJSON(filepath.Join(dir, name), &att); err != nil {
			return nil, err
		}
		out = append(out, &att)
	}
	return out, nil
}

func (s *LocalStore) AppendLedger(ctx context.Context, runID RunID, entry string) error {
	_ = ctx
	if len(entry) > maxLedgerLineBytes {
		return fmt.Errorf("%w: ledger line", ErrSizeCapExceeded)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.runsDir(), safeID(string(runID)), "ledger.jsonl")
	return appendJSONL(path, []byte(entry))
}

func (s *LocalStore) ReconcileInterrupted(ctx context.Context, runID RunID) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var run RunRecord
	gen, err := s.readJSON(s.runPath(runID), &run)
	if err != nil {
		return err
	}
	// Fail non-terminal attempts with DAEMON_RESTARTED; revoke leases.
	atts, err := s.listAttemptsLocked(runID)
	if err != nil {
		return err
	}
	now := s.now()
	reason := FailureDaemonRestarted
	for _, att := range atts {
		if att.Status.IsTerminal() {
			continue
		}
		attPath := s.attemptPath(runID, att.AttemptID)
		agen, err := s.readJSON(attPath, att)
		if err != nil {
			return err
		}
		att.Status = AttemptStatusFailed
		att.FailureReason = &reason
		if att.Lease != nil {
			// Revoke: clear token / expire immediately.
			att.Lease.ExpiresAt = now
			att.Lease.LeaseToken = ""
		}
		att.UpdatedAt = now
		att.TerminatedAt = &now
		if err := s.writeJSON(attPath, agen+1, att); err != nil {
			return err
		}
	}
	if !run.Status.IsTerminal() {
		run.Status = RunStatusFailed
		run.UpdatedAt = now
		run.TerminatedAt = &now
		if err := s.writeJSON(s.runPath(runID), gen+1, &run); err != nil {
			return err
		}
	}
	// Conservative close of open active-time segment: do not invent wall time.
	if run.WorkflowID != "" {
		if err := s.closeActiveTimeSegmentConservativelyLocked(run.WorkflowID); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStore) listAttemptsLocked(runID RunID) ([]*AttemptRecord, error) {
	dir := filepath.Join(s.runsDir(), safeID(string(runID)), "attempts")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*AttemptRecord, 0, len(names))
	for _, name := range names {
		var att AttemptRecord
		if _, err := s.readJSON(filepath.Join(dir, name), &att); err != nil {
			return nil, err
		}
		out = append(out, &att)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// WorkflowStore
// ---------------------------------------------------------------------------

func (s *LocalStore) CreateWorkflow(ctx context.Context, wf *WorkflowRecord) error {
	_ = ctx
	if wf == nil {
		return fmt.Errorf("%w: nil workflow", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if wf.WorkflowID == "" {
		id, err := NewWorkflowID()
		if err != nil {
			return err
		}
		wf.WorkflowID = id
	}
	if wf.SchemaVersion == "" {
		wf.SchemaVersion = CurrentSchemaVersion
	}
	if wf.Generation == 0 {
		wf.Generation = 1
	}
	if wf.CreatedAt.IsZero() {
		wf.CreatedAt = s.now()
	}
	wf.UpdatedAt = s.now()
	path := s.workflowPath(wf.WorkflowID)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: workflow %s", ErrAlreadyExists, wf.WorkflowID)
	}
	if err := s.writeJSON(path, wf.Generation, wf); err != nil {
		return err
	}
	// Initialize active-time ledger for the workflow.
	return s.saveActiveTimeLedgerLocked(wf.WorkflowID, &ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		UpdatedAt:     s.now(),
	}, 1)
}

func (s *LocalStore) GetWorkflow(ctx context.Context, workflowID WorkflowID) (*WorkflowRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var wf WorkflowRecord
	if _, err := s.readJSON(s.workflowPath(workflowID), &wf); err != nil {
		return nil, err
	}
	return &wf, nil
}

func (s *LocalStore) UpdateWorkflow(ctx context.Context, wf *WorkflowRecord, expectedGeneration int64) error {
	_ = ctx
	if wf == nil {
		return fmt.Errorf("%w: nil workflow", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.workflowPath(wf.WorkflowID)
	var existing WorkflowRecord
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration || existing.Generation != expectedGeneration {
		return fmt.Errorf("%w: workflow %s expected %d got %d", ErrCASConflict, wf.WorkflowID, expectedGeneration, existing.Generation)
	}
	// Resume / re-acquire slot: entering a slot-holding status from a
	// non-holding status must respect deployment concurrency under the same lock.
	if !holdsConcurrencySlot(existing.Status) && holdsConcurrencySlot(wf.Status) {
		if err := s.checkConcurrencyForResumeLocked(existing.DeploymentID); err != nil {
			return err
		}
	}
	wf.Generation = expectedGeneration + 1
	wf.UpdatedAt = s.now()
	if err := s.writeJSON(path, wf.Generation, wf); err != nil {
		return err
	}
	return s.syncActiveTimeOnStatusChangeLocked(wf.WorkflowID, existing.Status, wf.Status)
}

func (s *LocalStore) ListWorkflows(ctx context.Context) ([]*WorkflowRecord, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.workflowsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*WorkflowRecord{}, nil
		}
		return nil, err
	}
	out := make([]*WorkflowRecord, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var wf WorkflowRecord
		if _, err := s.readJSON(filepath.Join(dir, e.Name(), "workflow.json"), &wf); err != nil {
			if errorsIsNotFound(err) || os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, &wf)
	}
	return out, nil
}

func (s *LocalStore) CreateNode(ctx context.Context, node *PipelineNode) error {
	_ = ctx
	if node == nil {
		return fmt.Errorf("%w: nil node", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if node.NodeID == "" {
		id, err := NewNodeID()
		if err != nil {
			return err
		}
		node.NodeID = id
	}
	if node.SchemaVersion == "" {
		node.SchemaVersion = CurrentSchemaVersion
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = s.now()
	}
	node.UpdatedAt = s.now()
	path := s.nodePath(node.WorkflowID, node.NodeID)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w: node %s", ErrAlreadyExists, node.NodeID)
	}
	return s.writeJSON(path, 1, node)
}

func (s *LocalStore) GetNode(ctx context.Context, nodeID NodeID) (*PipelineNode, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.findNodeLocked(nodeID)
}

func (s *LocalStore) findNodeLocked(nodeID NodeID) (*PipelineNode, error) {
	dir := s.workflowsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: node %s", ErrNotFound, nodeID)
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "nodes", safeID(string(nodeID))+".json")
		var node PipelineNode
		if _, err := s.readJSON(path, &node); err == nil {
			return &node, nil
		}
	}
	return nil, fmt.Errorf("%w: node %s", ErrNotFound, nodeID)
}

func (s *LocalStore) UpdateNode(ctx context.Context, node *PipelineNode, expectedGeneration int64) error {
	_ = ctx
	if node == nil {
		return fmt.Errorf("%w: nil node", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.nodePath(node.WorkflowID, node.NodeID)
	var existing PipelineNode
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: node %s expected %d got %d", ErrCASConflict, node.NodeID, expectedGeneration, gen)
	}
	node.UpdatedAt = s.now()
	return s.writeJSON(path, expectedGeneration+1, node)
}

func (s *LocalStore) ListNodes(ctx context.Context, workflowID WorkflowID) ([]*PipelineNode, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "nodes")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*PipelineNode{}, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*PipelineNode, 0, len(names))
	for _, name := range names {
		var n PipelineNode
		if _, err := s.readJSON(filepath.Join(dir, name), &n); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StageOrder < out[j].StageOrder })
	return out, nil
}

func (s *LocalStore) RegisterService(ctx context.Context, svc *MCPServiceBinding) error {
	_ = ctx
	if svc == nil {
		return fmt.Errorf("%w: nil service", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc.ServiceID == "" {
		id, err := NewServiceID()
		if err != nil {
			return err
		}
		svc.ServiceID = id
	}
	if svc.SchemaVersion == "" {
		svc.SchemaVersion = CurrentSchemaVersion
	}
	if svc.CreatedAt.IsZero() {
		svc.CreatedAt = s.now()
	}
	svc.UpdatedAt = s.now()
	path := filepath.Join(s.workflowsDir(), safeID(string(svc.WorkflowID)), "services", safeID(string(svc.ServiceID))+".json")
	return s.writeJSON(path, 1, svc)
}

func (s *LocalStore) UpdateService(ctx context.Context, svc *MCPServiceBinding, expectedGeneration int64) error {
	_ = ctx
	if svc == nil {
		return fmt.Errorf("%w: nil service", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.workflowsDir(), safeID(string(svc.WorkflowID)), "services", safeID(string(svc.ServiceID))+".json")
	var existing MCPServiceBinding
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: service %s", ErrCASConflict, svc.ServiceID)
	}
	svc.UpdatedAt = s.now()
	return s.writeJSON(path, expectedGeneration+1, svc)
}

func (s *LocalStore) ListServices(ctx context.Context, workflowID WorkflowID) ([]*MCPServiceBinding, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "services")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*MCPServiceBinding{}, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*MCPServiceBinding, 0, len(names))
	for _, name := range names {
		var svc MCPServiceBinding
		if _, err := s.readJSON(filepath.Join(dir, name), &svc); err != nil {
			return nil, err
		}
		out = append(out, &svc)
	}
	return out, nil
}

func (s *LocalStore) CommitHandoff(ctx context.Context, handoff *HandoffEnvelope) error {
	_ = ctx
	if handoff == nil {
		return fmt.Errorf("%w: nil handoff", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if handoff.HandoffID == "" {
		id, err := NewHandoffID()
		if err != nil {
			return err
		}
		handoff.HandoffID = id
	}
	if handoff.SchemaVersion == "" {
		handoff.SchemaVersion = CurrentSchemaVersion
	}
	if handoff.CreatedAt.IsZero() {
		handoff.CreatedAt = s.now()
	}
	path := filepath.Join(s.workflowsDir(), safeID(string(handoff.WorkflowID)), "handoffs", safeID(string(handoff.HandoffID))+".json")
	// Idempotent: if exists with same ID, success.
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	return s.writeJSON(path, 1, handoff)
}

func (s *LocalStore) GetHandoff(ctx context.Context, handoffID HandoffID) (*HandoffEnvelope, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.workflowsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: handoff %s", ErrNotFound, handoffID)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "handoffs", safeID(string(handoffID))+".json")
		var h HandoffEnvelope
		if _, err := s.readJSON(path, &h); err == nil {
			return &h, nil
		}
	}
	return nil, fmt.Errorf("%w: handoff %s", ErrNotFound, handoffID)
}

func (s *LocalStore) ListHandoffs(ctx context.Context, workflowID WorkflowID) ([]*HandoffEnvelope, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "handoffs")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*HandoffEnvelope{}, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*HandoffEnvelope, 0, len(names))
	for _, name := range names {
		var h HandoffEnvelope
		if _, err := s.readJSON(filepath.Join(dir, name), &h); err != nil {
			return nil, err
		}
		out = append(out, &h)
	}
	return out, nil
}

func (s *LocalStore) CreateChildBatch(ctx context.Context, batch *ChildBatch) error {
	_ = ctx
	if batch == nil {
		return fmt.Errorf("%w: nil batch", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if batch.ChildBatchID == "" {
		id, err := NewChildBatchID()
		if err != nil {
			return err
		}
		batch.ChildBatchID = id
	}
	if batch.SchemaVersion == "" {
		batch.SchemaVersion = CurrentSchemaVersion
	}
	if batch.CreatedAt.IsZero() {
		batch.CreatedAt = s.now()
	}
	batch.UpdatedAt = s.now()
	path := filepath.Join(s.workflowsDir(), safeID(string(batch.WorkflowID)), "child-batches", safeID(string(batch.ChildBatchID))+".json")
	return s.writeJSON(path, 1, batch)
}

func (s *LocalStore) UpdateChildBatch(ctx context.Context, batch *ChildBatch, expectedGeneration int64) error {
	_ = ctx
	if batch == nil {
		return fmt.Errorf("%w: nil batch", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.workflowsDir(), safeID(string(batch.WorkflowID)), "child-batches", safeID(string(batch.ChildBatchID))+".json")
	var existing ChildBatch
	gen, err := s.readJSON(path, &existing)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: child batch %s", ErrCASConflict, batch.ChildBatchID)
	}
	batch.UpdatedAt = s.now()
	return s.writeJSON(path, expectedGeneration+1, batch)
}

func (s *LocalStore) ListChildBatches(ctx context.Context, workflowID WorkflowID) ([]*ChildBatch, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "child-batches")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*ChildBatch{}, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*ChildBatch, 0, len(names))
	for _, name := range names {
		var b ChildBatch
		if _, err := s.readJSON(filepath.Join(dir, name), &b); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, nil
}

func (s *LocalStore) CommitChildResult(ctx context.Context, result *ChildResult) error {
	_ = ctx
	if result == nil {
		return fmt.Errorf("%w: nil child result", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.ChildResultID == "" {
		id, err := NewChildResultID()
		if err != nil {
			return err
		}
		result.ChildResultID = id
	}
	if result.SchemaVersion == "" {
		result.SchemaVersion = CurrentSchemaVersion
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = s.now()
	}
	// Need workflow id from batch — store under child-results keyed by batch.
	// Locate batch by scanning workflows.
	wfID, err := s.findBatchWorkflowLocked(result.ChildBatchID)
	if err != nil {
		return err
	}
	path := filepath.Join(s.workflowsDir(), safeID(string(wfID)), "child-results", safeID(string(result.ChildResultID))+".json")
	if _, err := os.Lstat(path); err == nil {
		// Idempotent commit.
		return nil
	}
	return s.writeJSON(path, 1, result)
}

func (s *LocalStore) findBatchWorkflowLocked(batchID ChildBatchID) (WorkflowID, error) {
	dir := s.workflowsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("%w: child batch %s", ErrNotFound, batchID)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "child-batches", safeID(string(batchID))+".json")
		var b ChildBatch
		if _, err := s.readJSON(path, &b); err == nil {
			return b.WorkflowID, nil
		}
	}
	return "", fmt.Errorf("%w: child batch %s", ErrNotFound, batchID)
}

func (s *LocalStore) ListChildResults(ctx context.Context, childBatchID ChildBatchID) ([]*ChildResult, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	wfID, err := s.findBatchWorkflowLocked(childBatchID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.workflowsDir(), safeID(string(wfID)), "child-results")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return []*ChildResult{}, nil
	}
	names, err := listJSONNames(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*ChildResult, 0)
	for _, name := range names {
		var r ChildResult
		if _, err := s.readJSON(filepath.Join(dir, name), &r); err != nil {
			return nil, err
		}
		if r.ChildBatchID != childBatchID {
			continue
		}
		out = append(out, &r)
	}
	return out, nil
}

func (s *LocalStore) RequestControl(ctx context.Context, req *ControlRequest) error {
	_ = ctx
	if req == nil {
		return fmt.Errorf("%w: nil control request", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Idempotency: same workflow + key returns the original ControlRequestID.
	if req.IdempotencyKey != "" {
		if existing, err := s.findControlByIdempotencyLocked(req.WorkflowID, req.IdempotencyKey); err == nil {
			*req = *existing
			return nil
		} else if !errorsIsNotFound(err) {
			return err
		}
	}
	if req.ControlRequestID == "" {
		id, err := NewControlRequestID()
		if err != nil {
			return err
		}
		req.ControlRequestID = id
	}
	if req.SchemaVersion == "" {
		req.SchemaVersion = CurrentSchemaVersion
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = s.now()
	}
	// Persist control request.
	path := filepath.Join(s.workflowsDir(), safeID(string(req.WorkflowID)), "controls", safeID(string(req.ControlRequestID))+".json")
	if err := s.writeJSON(path, 1, req); err != nil {
		return err
	}
	// Update desired state with cancel precedence.
	ds := &DesiredState{
		SchemaVersion:    CurrentSchemaVersion,
		WorkflowID:       req.WorkflowID,
		DesiredCommand:   req.Command,
		ControlRequestID: req.ControlRequestID,
		Generation:       req.ExpectedGeneration,
		CancelPrecedence: req.Command == ControlCancel,
		CreatedAt:        s.now(),
	}
	// If existing desired is cancel, keep cancel.
	dsPath := filepath.Join(s.workflowsDir(), safeID(string(req.WorkflowID)), "desired_state.json")
	var existing DesiredState
	if _, err := s.readJSON(dsPath, &existing); err == nil {
		if existing.CancelPrecedence && existing.DesiredCommand == ControlCancel && req.Command != ControlCancel {
			// Cancel wins.
			return nil
		}
	}
	return s.writeJSON(dsPath, ds.Generation, ds)
}

func (s *LocalStore) findControlByIdempotencyLocked(wfID WorkflowID, key string) (*ControlRequest, error) {
	dir := filepath.Join(s.workflowsDir(), safeID(string(wfID)), "controls")
	names, err := listJSONNames(dir)
	if err != nil {
		if os.IsNotExist(err) || errorsIsNotFound(err) {
			return nil, fmt.Errorf("%w: control idempotency", ErrNotFound)
		}
		return nil, err
	}
	for _, name := range names {
		var cr ControlRequest
		if _, err := s.readJSON(filepath.Join(dir, name), &cr); err != nil {
			if errorsIsNotFound(err) {
				continue
			}
			return nil, err
		}
		if cr.IdempotencyKey == key {
			return &cr, nil
		}
	}
	return nil, fmt.Errorf("%w: control idempotency", ErrNotFound)
}

func (s *LocalStore) GetDesiredState(ctx context.Context, workflowID WorkflowID) (*DesiredState, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var ds DesiredState
	if _, err := s.readJSON(filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "desired_state.json"), &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}

func (s *LocalStore) AppendControlResult(ctx context.Context, req *ControlRequest, result interface{}) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.workflowsDir(), safeID(string(req.WorkflowID)), "controls.jsonl")
	payload := map[string]interface{}{
		"schema_version": CurrentSchemaVersion,
		"request":        req,
		"result":         result,
		"at":             s.now(),
	}
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return appendJSONL(path, line)
}

func (s *LocalStore) AppendLimitAmendment(ctx context.Context, workflowID WorkflowID, expectedAuthorityGeneration int64, amendment *LimitAmendment) error {
	_ = ctx
	if amendment == nil {
		return fmt.Errorf("%w: nil amendment", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.workflowPath(workflowID)
	var wf WorkflowRecord
	gen, err := s.readJSON(path, &wf)
	if err != nil {
		return err
	}

	// Idempotency by workflow + key before applying authority CAS.
	if amendment.IdempotencyKey != "" {
		if existing, err := s.findAmendmentByIdempotencyLocked(workflowID, amendment.IdempotencyKey); err == nil {
			if amendmentPayloadEqual(existing, amendment) {
				*amendment = *existing
				return nil // IDEMPOTENT_REPLAY
			}
			return fmt.Errorf("%w: amendment key %s", ErrIdempotencyConflict, amendment.IdempotencyKey)
		} else if !errorsIsNotFound(err) {
			return err
		}
	}

	if wf.AuthorityGeneration != expectedAuthorityGeneration {
		return fmt.Errorf("%w: authority generation expected %d got %d", ErrCASConflict, expectedAuthorityGeneration, wf.AuthorityGeneration)
	}
	// Increase-only checks.
	if amendment.NewMaxActiveDurationMs != 0 && amendment.NewMaxActiveDurationMs < wf.MaxActiveDurationMs {
		return fmt.Errorf("%w: max_active_duration decrease not allowed", ErrInvalidArgument)
	}
	if amendment.NewCurrentAttemptLeaseMs != 0 && amendment.NewCurrentAttemptLeaseMs < wf.MaxAttemptLeaseMs {
		return fmt.Errorf("%w: attempt lease decrease not allowed", ErrInvalidArgument)
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
	wf.Generation = gen + 1
	wf.UpdatedAt = s.now()

	if amendment.AmendmentID == "" {
		id, err := NewLimitAmendmentID()
		if err != nil {
			return err
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

	apath := filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "amendments", safeID(string(amendment.AmendmentID))+".json")
	if err := s.writeJSON(apath, 1, amendment); err != nil {
		return err
	}
	return s.writeJSON(path, wf.Generation, &wf)
}

func (s *LocalStore) findAmendmentByIdempotencyLocked(wfID WorkflowID, key string) (*LimitAmendment, error) {
	dir := filepath.Join(s.workflowsDir(), safeID(string(wfID)), "amendments")
	names, err := listJSONNames(dir)
	if err != nil {
		if os.IsNotExist(err) || errorsIsNotFound(err) {
			return nil, fmt.Errorf("%w: amendment idempotency", ErrNotFound)
		}
		return nil, err
	}
	for _, name := range names {
		var a LimitAmendment
		if _, err := s.readJSON(filepath.Join(dir, name), &a); err != nil {
			if errorsIsNotFound(err) {
				continue
			}
			return nil, err
		}
		if a.IdempotencyKey == key {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("%w: amendment idempotency", ErrNotFound)
}

func amendmentPayloadEqual(existing, incoming *LimitAmendment) bool {
	return existing.NewMaxActiveDurationMs == incoming.NewMaxActiveDurationMs &&
		existing.NewCurrentAttemptLeaseMs == incoming.NewCurrentAttemptLeaseMs &&
		existing.NewMaxLLMSpendDecimal == incoming.NewMaxLLMSpendDecimal &&
		existing.Reason == incoming.Reason &&
		existing.ActorIdentity == incoming.ActorIdentity
}

func (s *LocalStore) ApplyTransition(ctx context.Context, workflowID WorkflowID, expectedGeneration int64, command string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	// Optional structured ops in command: if JSON object with "operations", use them.
	var ops []WALOp
	var probe struct {
		Operations []WALOp `json:"operations"`
	}
	if json.Unmarshal([]byte(command), &probe) == nil && len(probe.Operations) > 0 {
		ops = probe.Operations
	}
	return s.applyTransitionLocked(workflowID, expectedGeneration, command, ops)
}

func (s *LocalStore) loadWorkflowCAS(wfID WorkflowID) (*WorkflowRecord, int64, error) {
	var wf WorkflowRecord
	gen, err := s.readJSON(s.workflowPath(wfID), &wf)
	if err != nil {
		return nil, 0, err
	}
	// Prefer record generation if present.
	if wf.Generation != 0 {
		gen = wf.Generation
	}
	return &wf, gen, nil
}

// ---------------------------------------------------------------------------
// IO helpers on LocalStore
// ---------------------------------------------------------------------------

func (s *LocalStore) writeJSON(path string, gen int64, v any) error {
	if err := rejectSymlinkInRoot(s.root, path); err != nil {
		// Path may not fully exist yet; check only existing prefix under root.
		if !errors.Is(err, ErrSymlinkRejected) && !errors.Is(err, ErrInvalidPath) {
			// fall through to write with leaf checks
		} else if errors.Is(err, ErrSymlinkRejected) {
			return err
		} else if errors.Is(err, ErrInvalidPath) {
			return err
		}
	}
	// Re-check leaf/parent always.
	if err := rejectSymlinkPath(path); err != nil && !os.IsNotExist(err) {
		if errors.Is(err, ErrSymlinkRejected) {
			return err
		}
	}
	data, err := marshalPersisted(gen, v)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, filePerm)
}

func (s *LocalStore) readJSON(path string, v any) (int64, error) {
	if err := rejectSymlinkInRoot(s.root, path); err != nil {
		if errors.Is(err, ErrSymlinkRejected) || errors.Is(err, ErrInvalidPath) {
			// If path does not exist yet, Rel still works; missing components
			// return nil from rejectSymlinkLeaf. InvalidPath is hard fail.
			if errors.Is(err, ErrInvalidPath) {
				return 0, err
			}
			if errors.Is(err, ErrSymlinkRejected) {
				return 0, err
			}
		}
	}
	data, err := readFileStrict(path, maxStateFileBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return 0, err
	}
	// Optional migrate-on-read for known older versions.
	ver, verr := extractSchemaVersion(data)
	if verr == nil && s.reg != nil && ver != s.reg.Current() {
		if !s.reg.IsSupported(ver) {
			return 0, fmt.Errorf("%w: %s", ErrUnknownSchemaVersion, ver)
		}
		// Fail closed on read path for unknown; for supported old, migrate file first.
		if err := s.reg.MigrateFile(path); err != nil {
			return 0, err
		}
		data, err = readFileStrict(path, maxStateFileBytes)
		if err != nil {
			return 0, err
		}
	}
	gen, err := unmarshalPersisted(data, v)
	if err != nil {
		// Also try bare record (no envelope) for forward compatibility.
		if jerr := json.Unmarshal(data, v); jerr == nil {
			return 1, nil
		}
		return 0, err
	}
	return gen, nil
}

func listJSONNames(dir string) ([]string, error) {
	if err := rejectSymlinkPath(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		// If dir missing
		if _, e := os.Lstat(dir); os.IsNotExist(e) {
			return nil, nil
		}
		if errorsIs(err, ErrSymlinkRejected) {
			return nil, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".tmp-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		// Symlink check per entry.
		if err := rejectSymlinkPath(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
		names = append(names, name)
		if len(names) >= maxRecordsPerList {
			break
		}
	}
	sort.Strings(names)
	return names, nil
}

func errorsIsNotFound(err error) bool {
	return err != nil && (errorsIs(err, ErrNotFound) || os.IsNotExist(err))
}

func errorsIs(err, target error) bool {
	return err != nil && target != nil && errors.Is(err, target)
}

// ---------------------------------------------------------------------------
// Intent + topology helpers
// ---------------------------------------------------------------------------

func computeIntentDigest(req *InvocationRequest) string {
	// Canonical intent over fields that affect execution/authority.
	type intent struct {
		Ref     string `json:"requested_deployment_ref"`
		Input   string `json:"input_digest"`
		Active  int64  `json:"initial_max_active_duration_ms"`
		Lease   int64  `json:"initial_attempt_lease_ms"`
		Cost    string `json:"initial_max_cost_usd_decimal"`
		Options string `json:"creation_options_digest"`
	}
	inDigest := req.InputDigest
	if inDigest == "" && req.InputJSON != "" {
		sum := sha256.Sum256([]byte(req.InputJSON))
		inDigest = hex.EncodeToString(sum[:])
	}
	raw, _ := json.Marshal(intent{
		Ref:     req.RequestedDeploymentRef,
		Input:   inDigest,
		Active:  req.InitialMaxActiveDurationMs,
		Lease:   req.InitialAttemptLeaseMs,
		Cost:    req.InitialMaxCostUsdDecimal,
		Options: req.CreationOptionsDigest,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func topologyKind(dep *DeploymentRecord) string {
	if dep.NestedPackageDigests != nil {
		if k := dep.NestedPackageDigests[metaWorkflowKind]; k != "" {
			return k
		}
	}
	return "standalone"
}

func topologyStageCount(dep *DeploymentRecord, kind string) int {
	if kind != "pipeline" {
		return 1
	}
	if dep.NestedPackageDigests != nil {
		if c := dep.NestedPackageDigests[metaStageCount]; c != "" {
			n, err := strconv.Atoi(c)
			if err == nil && n > 0 && n <= 64 {
				return n
			}
		}
		// Count stage:N keys.
		max := -1
		for k := range dep.NestedPackageDigests {
			if strings.HasPrefix(k, "stage:") {
				rest := strings.TrimPrefix(k, "stage:")
				// stage:0 or stage:0:digest
				parts := strings.Split(rest, ":")
				if n, err := strconv.Atoi(parts[0]); err == nil && n > max {
					max = n
				}
			}
		}
		if max >= 0 {
			return max + 1
		}
	}
	return 1
}

func stagePackageName(dep *DeploymentRecord, i int) string {
	if dep.NestedPackageDigests != nil {
		if n := dep.NestedPackageDigests[fmt.Sprintf("stage:%d:name", i)]; n != "" {
			return n
		}
	}
	if i == 0 {
		return dep.PackageName
	}
	return fmt.Sprintf("%s#stage-%d", dep.PackageName, i)
}

func stagePackageVersion(dep *DeploymentRecord, i int) string {
	if dep.NestedPackageDigests != nil {
		if v := dep.NestedPackageDigests[fmt.Sprintf("stage:%d:version", i)]; v != "" {
			return v
		}
	}
	return dep.PackageVersion
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		// Strip internal topology metadata from receipt nested digests? Keep all for audit.
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Concurrency slot helpers + active-time ledger
// ---------------------------------------------------------------------------

func holdsConcurrencySlot(st WorkflowStatus) bool {
	switch st {
	case WorkflowStatusPending, WorkflowStatusRunning, WorkflowStatusPauseRequested:
		return true
	default:
		return false
	}
}

func chargesActiveTime(st WorkflowStatus) bool {
	switch st {
	case WorkflowStatusRunning, WorkflowStatusPauseRequested:
		return true
	default:
		return false
	}
}

func (s *LocalStore) checkConcurrencyForResumeLocked(depID DeploymentID) error {
	if depID == "" {
		return nil
	}
	var dep DeploymentRecord
	if _, err := s.readJSON(s.deploymentPath(depID), &dep); err != nil {
		// No deployment bound (seeded workflows) — skip concurrency gate.
		if errorsIsNotFound(err) {
			return nil
		}
		return err
	}
	holding, err := s.countSlotHoldingLocked(depID)
	if err != nil {
		return err
	}
	max := dep.MaxConcurrentRuns
	if max <= 0 {
		max = 1
	}
	if holding >= max {
		return fmt.Errorf("%w: deployment %s holding %d max %d", ErrAlreadyRunning, depID, holding, max)
	}
	return nil
}

func (s *LocalStore) activeTimePath(wfID WorkflowID) string {
	return filepath.Join(s.workflowsDir(), safeID(string(wfID)), "active_time.json")
}

// GetActiveTimeLedger loads the workflow active-time ledger.
func (s *LocalStore) GetActiveTimeLedger(ctx context.Context, workflowID WorkflowID) (*ActiveTimeLedger, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	ledger, _, err := s.loadActiveTimeLedgerLocked(workflowID)
	return ledger, err
}

// GetActiveTimeLedgerGeneration returns the file generation of the active-time
// ledger, for use with CAS writes via PutActiveTimeLedger.
func (s *LocalStore) GetActiveTimeLedgerGeneration(ctx context.Context, workflowID WorkflowID) (int64, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	_, gen, err := s.loadActiveTimeLedgerLocked(workflowID)
	return gen, err
}

// PutActiveTimeLedger persists the workflow active-time ledger.
// expectedGeneration is the file generation expected. Pass 0 to bypass CAS.
func (s *LocalStore) PutActiveTimeLedger(ctx context.Context, workflowID WorkflowID, ledger *ActiveTimeLedger, expectedGeneration int64) error {
	_ = ctx
	if ledger == nil {
		return fmt.Errorf("%w: nil active time ledger", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ledger.SchemaVersion == "" {
		ledger.SchemaVersion = CurrentSchemaVersion
	}
	ledger.UpdatedAt = s.now()
	return s.saveActiveTimeLedgerLocked(workflowID, ledger, expectedGeneration)
}

func (s *LocalStore) loadActiveTimeLedgerLocked(wfID WorkflowID) (*ActiveTimeLedger, int64, error) {
	var ledger ActiveTimeLedger
	gen, err := s.readJSON(s.activeTimePath(wfID), &ledger)
	if err != nil {
		return nil, 0, err
	}
	return &ledger, gen, nil
}

func (s *LocalStore) saveActiveTimeLedgerLocked(wfID WorkflowID, ledger *ActiveTimeLedger, expectedGeneration int64) error {
	if ledger.SchemaVersion == "" {
		ledger.SchemaVersion = CurrentSchemaVersion
	}
	return s.writeJSON(s.activeTimePath(wfID), expectedGeneration, ledger)
}

func (s *LocalStore) closeActiveTimeSegmentConservativelyLocked(wfID WorkflowID) error {
	ledger, gen, err := s.loadActiveTimeLedgerLocked(wfID)
	if err != nil {
		if errorsIsNotFound(err) {
			return nil
		}
		return err
	}
	if ledger.RunningSegmentStartMs == nil {
		return nil
	}
	// Conservative: clear open segment without charging unknown wall time.
	ledger.RunningSegmentStartMs = nil
	ledger.UpdatedAt = s.now()
	return s.saveActiveTimeLedgerLocked(wfID, ledger, gen+1)
}

func (s *LocalStore) syncActiveTimeOnStatusChangeLocked(wfID WorkflowID, from, to WorkflowStatus) error {
	if from == to {
		return nil
	}
	ledger, gen, err := s.loadActiveTimeLedgerLocked(wfID)
	if err != nil {
		if errorsIsNotFound(err) {
			ledger = &ActiveTimeLedger{SchemaVersion: CurrentSchemaVersion}
			gen = 0
		} else {
			return err
		}
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
		// Ensure closed on terminal; if still charging, fold remaining segment.
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
	return s.saveActiveTimeLedgerLocked(wfID, ledger, gen+1)
}
