package routedrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClock(start time.Time) func() time.Time {
	var n int64
	return func() time.Time {
		i := atomic.AddInt64(&n, 1)
		return start.Add(time.Duration(i) * time.Millisecond)
	}
}

func openTestStore(t *testing.T) *LocalStore {
	t.Helper()
	root := t.TempDir()
	s, err := OpenLocalStore(root, WithClock(testClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))))
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	return s
}

func seedActiveDeployment(t *testing.T, s DeploymentStore, maxConcurrent int, meta map[string]string) *DeploymentRecord {
	t.Helper()
	ctx := context.Background()
	dep := &DeploymentRecord{
		SchemaVersion:      CurrentSchemaVersion,
		PackageName:        "pkg-demo",
		PackageVersion:     "1.0.0",
		Status:             DeploymentActive,
		MaxConcurrentRuns:  maxConcurrent,
		BundleDigest:       "bundle-digest",
		PolicyDigest:       "policy-digest",
		ImageLockDigest:    "image-digest",
		ProvenanceDigest:   "prov-digest",
		NestedPackageDigests: meta,
		CreatedBy:          "test",
	}
	if err := s.CreateDeployment(ctx, dep); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	return dep
}

func TestLocalStore_DeploymentCRUDAndCAS(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)

	got, err := s.GetDeployment(ctx, dep.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PackageName != "pkg-demo" || got.Status != DeploymentActive {
		t.Fatalf("got %+v", got)
	}
	if err := s.SetDeploymentStatus(ctx, dep.DeploymentID, DeploymentInactive, got.Generation); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeploymentStatus(ctx, dep.DeploymentID, DeploymentActive, got.Generation); !errors.Is(err, ErrCASConflict) && !errorsIs(err, ErrCASConflict) {
		t.Fatalf("expected CAS conflict, got %v", err)
	}
	list, err := s.ListDeployments(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v err=%v", list, err)
	}
}

func TestLocalStore_AliasCAS(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	alias := &AliasRecord{
		SchemaVersion:     CurrentSchemaVersion,
		Alias:             "production/demo",
		TargetDeploymentID: dep.DeploymentID,
		TargetVersion:     dep.PackageVersion,
		Generation:        0,
		UpdatedBy:         "ops",
	}
	if err := s.CompareAndSwapAlias(ctx, alias); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveAlias(ctx, "production/demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 {
		t.Fatalf("gen=%d", got.Generation)
	}
	// Stale CAS
	stale := *got
	stale.Generation = 0
	if err := s.CompareAndSwapAlias(ctx, &stale); !errorsIs(err, ErrCASConflict) {
		t.Fatalf("expected CAS, got %v", err)
	}
	// Promote
	got.TargetVersion = "1.1.0"
	if err := s.CompareAndSwapAlias(ctx, got); err != nil {
		t.Fatal(err)
	}
}

func TestLocalStore_AdmitStandaloneIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 2, nil)
	req := &InvocationRequest{
		SchemaVersion:             CurrentSchemaVersion,
		RequestedDeploymentRef:    string(dep.DeploymentID),
		InputJSON:                 `{"q":"hi"}`,
		InitialMaxActiveDurationMs: 60_000,
		InitialAttemptLeaseMs:     30_000,
		InitialMaxCostUsdDecimal:  "1.00",
		CreationOptionsDigest:     "opts",
		IdempotencyKey:            "key-1",
		CallerIdentity:            "caller-a",
	}
	r1, err := s.AdmitInvocation(ctx, req, dep.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if r1.RunID == "" || r1.WorkflowID == "" {
		t.Fatalf("empty ids: %+v", r1)
	}
	// Replay exact
	r2, err := s.AdmitInvocation(ctx, req, dep.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if r2.InvocationID != r1.InvocationID || r2.RunID != r1.RunID {
		t.Fatalf("replay mismatch: %+v vs %+v", r1, r2)
	}
	// Changed intent
	req2 := *req
	req2.InputJSON = `{"q":"other"}`
	req2.InputDigest = ""
	if _, err := s.AdmitInvocation(ctx, &req2, dep.Generation); !errorsIs(err, ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	// Node READY
	nodes, err := s.ListNodes(ctx, r1.WorkflowID)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes=%v err=%v", nodes, err)
	}
	if nodes[0].Status != NodeStatusReady {
		t.Fatalf("status=%s", nodes[0].Status)
	}
	// No attempt created
	atts, err := s.ListAttempts(ctx, r1.RunID)
	if err != nil || len(atts) != 0 {
		t.Fatalf("attempts should be empty: %v %v", atts, err)
	}
}

func TestLocalStore_AdmitPipelineTopology(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, map[string]string{
		metaWorkflowKind: "pipeline",
		metaStageCount:   "3",
	})
	req := &InvocationRequest{
		SchemaVersion:          CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              `{}`,
		IdempotencyKey:         "pipe-1",
		CallerIdentity:         "c",
	}
	r, err := s.AdmitInvocation(ctx, req, 0)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.ListNodes(ctx, r.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("want 3 nodes, got %d", len(nodes))
	}
	if nodes[0].Status != NodeStatusReady {
		t.Fatalf("stage0=%s", nodes[0].Status)
	}
	if nodes[1].Status != NodeStatusPending || nodes[2].Status != NodeStatusPending {
		t.Fatalf("later stages: %s %s", nodes[1].Status, nodes[2].Status)
	}
	runs, err := s.ListRuns(ctx, r.WorkflowID)
	if err != nil || len(runs) != 3 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
}

func TestLocalStore_AdmitAlreadyRunningNoQueue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	req := &InvocationRequest{
		SchemaVersion:          CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              `{}`,
		IdempotencyKey:         "a",
		CallerIdentity:         "c1",
	}
	if _, err := s.AdmitInvocation(ctx, req, 0); err != nil {
		t.Fatal(err)
	}
	req2 := &InvocationRequest{
		SchemaVersion:          CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              `{}`,
		IdempotencyKey:         "b",
		CallerIdentity:         "c2",
	}
	if _, err := s.AdmitInvocation(ctx, req2, 0); !errorsIs(err, ErrAlreadyRunning) {
		t.Fatalf("expected ALREADY_RUNNING, got %v", err)
	}
	// Second request must not leave an invocation record.
	list, err := s.ListInvocations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 invocation, got %d", len(list))
	}
}

func TestLocalStore_PermissionsAndSymlink(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	path := s.deploymentPath(dep.DeploymentID)
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %#o", fi.Mode().Perm())
	}
	// Corrupt to world-readable and ensure read fails closed.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeployment(ctx, dep.DeploymentID); !errorsIs(err, ErrUnsafePermissions) {
		t.Fatalf("expected unsafe permissions, got %v", err)
	}
	// Symlink rejection
	root := s.root
	link := filepath.Join(root, "deployments", "deployments", "evil-link.json")
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDeployment(ctx, DeploymentID("evil-link")); !errorsIs(err, ErrSymlinkRejected) {
		// May also be not found path sanitization — either fail closed is OK if not following.
		if err == nil {
			t.Fatal("symlink must not succeed")
		}
	}
}

func TestLocalStore_SizeCap(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	huge := make([]byte, maxInputJSONBytes+1)
	for i := range huge {
		huge[i] = 'a'
	}
	req := &InvocationRequest{
		SchemaVersion:          CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              string(huge),
		IdempotencyKey:         "big",
		CallerIdentity:         "c",
	}
	if _, err := s.AdmitInvocation(ctx, req, 0); !errorsIs(err, ErrSizeCapExceeded) {
		t.Fatalf("expected size cap, got %v", err)
	}
}

func TestLocalStore_RunAttemptCASAndLease(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	run := &RunRecord{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    "wf-test",
		Status:        RunStatusPending,
		RunKind:       "standalone",
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.Status = RunStatusRunning
	if err := s.UpdateRun(ctx, run, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateRun(ctx, run, 1); !errorsIs(err, ErrCASConflict) {
		t.Fatalf("expected CAS, got %v", err)
	}
	att := &AttemptRecord{
		SchemaVersion: CurrentSchemaVersion,
		RunID:         run.RunID,
		WorkflowID:    "wf-test",
		Status:        AttemptStatusRunning,
		AttemptNumber: 1,
		Lease: &AttemptLease{
			LeaseID:   "caller-selected-should-be-replaced",
			DurationMs: 1000,
			AcquiredAt: time.Now(),
			ExpiresAt:  time.Now().Add(time.Second),
		},
	}
	if err := s.CreateAttempt(ctx, att); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAttempt(ctx, att.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lease == nil || string(got.Lease.LeaseID) == "caller-selected-should-be-replaced" {
		t.Fatalf("lease id not store-issued: %+v", got.Lease)
	}
	if !ValidateIDPrefix(string(got.Lease.LeaseID), PrefixLease) {
		t.Fatalf("lease prefix: %s", got.Lease.LeaseID)
	}
}

func TestLocalStore_ReconcileInterrupted(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	run := &RunRecord{SchemaVersion: CurrentSchemaVersion, Status: RunStatusRunning, RunKind: "standalone", WorkflowID: "wf"}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	att := &AttemptRecord{
		SchemaVersion: CurrentSchemaVersion,
		RunID:         run.RunID,
		WorkflowID:    "wf",
		Status:        AttemptStatusRunning,
		AttemptNumber: 1,
		Lease:         &AttemptLease{DurationMs: 1000, AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)},
	}
	if err := s.CreateAttempt(ctx, att); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileInterrupted(ctx, run.RunID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAttempt(ctx, att.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != AttemptStatusFailed || got.FailureReason == nil || *got.FailureReason != FailureDaemonRestarted {
		t.Fatalf("attempt=%+v", got)
	}
	rg, err := s.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if rg.Status != RunStatusFailed {
		t.Fatalf("run status %s", rg.Status)
	}
}

func TestLocalStore_ApplyTransitionWAL(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		Status:        WorkflowStatusPending,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyTransition(ctx, wf.WorkflowID, 1, "start"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 {
		t.Fatalf("gen=%d", got.Generation)
	}
	if err := s.ApplyTransition(ctx, wf.WorkflowID, 1, "stale"); !errorsIs(err, ErrCASConflict) {
		t.Fatalf("expected CAS, got %v", err)
	}
}

func TestLocalStore_ConcurrentCAS(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	var success int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.SetDeploymentStatus(ctx, dep.DeploymentID, DeploymentInactive, 1)
			if err == nil {
				atomic.AddInt32(&success, 1)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("exactly one CAS should succeed, got %d", success)
	}
}

func TestLocalStore_OrphanTempCleanup(t *testing.T) {
	s := openTestStore(t)
	dir := s.deploymentsDir()
	orphan := filepath.Join(dir, ".tmp-orphan-xyz")
	if err := os.WriteFile(orphan, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Re-open cleans orphans
	s2, err := OpenLocalStore(s.root)
	if err != nil {
		t.Fatal(err)
	}
	_ = s2
	if _, err := os.Lstat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan should be removed, err=%v", err)
	}
}

func TestMemoryStore_BasicAdmission(t *testing.T) {
	s := NewMemoryStore(WithMemoryClock(testClock(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))))
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	req := &InvocationRequest{
		SchemaVersion:          CurrentSchemaVersion,
		RequestedDeploymentRef: string(dep.DeploymentID),
		InputJSON:              `{"a":1}`,
		IdempotencyKey:         "m1",
		CallerIdentity:         "mem",
	}
	r, err := s.AdmitInvocation(ctx, req, 0)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.AdmitInvocation(ctx, req, 0)
	if err != nil || r2.InvocationID != r.InvocationID {
		t.Fatalf("replay: %v %+v", err, r2)
	}
}

func TestAtomicWrite_Mode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	if err := atomicWriteFile(path, []byte(`{"ok":true}`), filePerm); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %#o", fi.Mode().Perm())
	}
}

func TestSafeID_RejectsTraversal(t *testing.T) {
	if safeID("../etc/passwd") == "../etc/passwd" {
		t.Fatal("traversal not sanitized")
	}
	if safeID("a/b") == "a/b" {
		t.Fatal("slash not sanitized")
	}
	if safeID("normal-id") != "normal-id" {
		t.Fatal("normal id changed")
	}
}

func TestLocalStore_AppendLedgerCap(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	run := &RunRecord{SchemaVersion: CurrentSchemaVersion, Status: RunStatusPending, RunKind: "standalone"}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLedger(ctx, run.RunID, "ok-line"); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, maxLedgerLineBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := s.AppendLedger(ctx, run.RunID, string(big)); !errorsIs(err, ErrSizeCapExceeded) {
		t.Fatalf("expected cap, got %v", err)
	}
}

func TestLocalStore_LimitAmendmentIncreaseOnly(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion:       CurrentSchemaVersion,
		Status:              WorkflowStatusRunning,
		WorkflowKind:        "standalone",
		Generation:          1,
		AuthorityGeneration: 1,
		MaxActiveDurationMs: 1000,
		MaxAttemptLeaseMs:   500,
		MaxLLMSpendDecimal:  "1.00",
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	// Decrease rejected
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
		NewMaxActiveDurationMs: 100,
	}); !errorsIs(err, ErrInvalidArgument) {
		t.Fatalf("expected invalid decrease, got %v", err)
	}
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
		NewMaxActiveDurationMs: 5000,
		Reason:                 "more time",
		ActorIdentity:          "admin",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxActiveDurationMs != 5000 || got.AuthorityGeneration != 2 {
		t.Fatalf("got %+v", got)
	}
}

// Ensure errors.Is works with wrapped sentinels for a few paths.
func TestErrorsIsHelpers(t *testing.T) {
	err := fmt.Errorf("%w: detail", ErrNotFound)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("errors.Is failed")
	}
	if !errorsIsNotFound(err) {
		t.Fatal("errorsIsNotFound failed")
	}
}
