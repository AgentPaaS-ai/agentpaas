package supervisor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// fakeClock wraps routedrun.FakeClock so tests can use the existing fake clock
// implementation. We embed it to satisfy routedrun.Clock (Now + NowMonotonic).
type fakeClock struct {
	*routedrun.FakeClock
}

func newFakeClock(initial time.Time) *fakeClock {
	return &fakeClock{FakeClock: routedrun.NewFakeClock(initial)}
}

// controlKeyForTest returns a deterministic 32-byte control key for a test
// attempt. Tests use this to compute valid HMACs and to seed the journal.
func controlKeyForTest(attemptID routedrun.AttemptID) []byte {
	key := make([]byte, 32)
	for i := 0; i < 32; i++ {
		key[i] = byte(i) ^ byte(attemptID[0])
	}
	return key
}

// canonicalProgress computes the canonical HMAC input bytes for a ProgressEvent
// (with HMAC field cleared). Mirrors the supervisor's verifyProgressHMAC.
func canonicalProgress(p ProgressEvent) []byte {
	cp := p
	cp.HMAC = ""
	b, _ := json.Marshal(cp)
	return b
}

func signProgress(p ProgressEvent, key []byte) ProgressEvent {
	mac := hmac.New(sha256.New, key)
	mac.Write(canonicalProgress(p))
	p.HMAC = hex.EncodeToString(mac.Sum(nil))
	return p
}

func canonicalResult(r ResultEvent) []byte {
	cr := r
	cr.HMAC = ""
	b, _ := json.Marshal(cr)
	return b
}

func signResult(r ResultEvent, key []byte) ResultEvent {
	mac := hmac.New(sha256.New, key)
	mac.Write(canonicalResult(r))
	r.HMAC = hex.EncodeToString(mac.Sum(nil))
	return r
}

func canonicalCheckpoint(c CheckpointEvent) []byte {
	cc := c
	cc.HMAC = ""
	b, _ := json.Marshal(cc)
	return b
}

func signCheckpoint(c CheckpointEvent, key []byte) CheckpointEvent {
	mac := hmac.New(sha256.New, key)
	mac.Write(canonicalCheckpoint(c))
	c.HMAC = hex.EncodeToString(mac.Sum(nil))
	return c
}

// fakeControlJournal is an in-memory control journal for tests. It records
// appended events keyed by sequence and verifies HMACs on read.
type fakeControlJournal struct {
	mu        sync.Mutex
	key       []byte
	events    []routedrun.InvokeJobEvent
	closed    bool
}

func newFakeControlJournal(key []byte) *fakeControlJournal {
	return &fakeControlJournal{key: key}
}

func (j *fakeControlJournal) Append(event routedrun.InvokeJobEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return errors.New("journal closed")
	}
	if event.SchemaVersion == "" {
		event.SchemaVersion = "1.0"
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	wantSeq := int64(len(j.events)) + 1
	if event.Sequence == 0 {
		event.Sequence = wantSeq
	}
	if event.Sequence != wantSeq {
		return fmt.Errorf("%w: sequence %d want %d", routedrun.ErrJournalSequenceConflict, event.Sequence, wantSeq)
	}
	event.HMAC = j.computeHMAC(event)
	j.events = append(j.events, event)
	return nil
}

func (j *fakeControlJournal) Read(fromSeq int64) ([]routedrun.InvokeJobEvent, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if fromSeq < 1 {
		fromSeq = 1
	}
	var out []routedrun.InvokeJobEvent
	for _, ev := range j.events {
		if ev.Sequence < fromSeq {
			continue
		}
		want := j.computeHMAC(ev)
		if !hmac.Equal([]byte(ev.HMAC), []byte(want)) {
			return nil, errors.New("hmac verification failed")
		}
		out = append(out, ev)
	}
	return out, nil
}

// Close is a no-op: the same journal instance is reused across OpenControlJournal
// calls, matching the real routedrun.ControlJournal which creates a new handle
// each time OpenControlJournal is called.
func (j *fakeControlJournal) Close() error {
	return nil
}

func (j *fakeControlJournal) computeHMAC(ev routedrun.InvokeJobEvent) string {
	mac := hmac.New(sha256.New, j.key)
	_, _ = fmt.Fprintf(mac, "%d|%s|%d|", ev.Sequence, ev.Timestamp.UTC().Format(time.RFC3339Nano), int(ev.EventKind))
	mac.Write([]byte(ev.Payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// fakeControlJournalFactory creates in-memory journals keyed by (run,attempt).
type fakeControlJournalFactory struct {
	mu       sync.Mutex
	journals map[string]*fakeControlJournal
	keys     map[string][]byte
}

func newFakeControlJournalFactory() *fakeControlJournalFactory {
	return &fakeControlJournalFactory{
		journals: make(map[string]*fakeControlJournal),
		keys:     make(map[string][]byte),
	}
}

func keyFor(runID routedrun.RunID, attemptID routedrun.AttemptID) string {
	return string(runID) + "/" + string(attemptID)
}

func (f *fakeControlJournalFactory) OpenControlJournal(runID routedrun.RunID, attemptID routedrun.AttemptID) (ControlJournalHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := keyFor(runID, attemptID)
	if j, ok := f.journals[k]; ok {
		return j, nil
	}
	key := f.keys[k]
	if key == nil {
		key = controlKeyForTest(attemptID)
		f.keys[k] = key
	}
	j := newFakeControlJournal(key)
	f.journals[k] = j
	return j, nil
}

func (f *fakeControlJournalFactory) get(runID routedrun.RunID, attemptID routedrun.AttemptID) *fakeControlJournal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.journals[keyFor(runID, attemptID)]
}

// KeyFor returns the control key for an attempt. This satisfies the optional
// interface the supervisor checks in loadOrCreateControlKey so the in-memory
// test journals share the key with the supervisor without a file round-trip.
func (f *fakeControlJournalFactory) KeyFor(runID routedrun.RunID, attemptID routedrun.AttemptID) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := keyFor(runID, attemptID)
	if key, ok := f.keys[k]; ok {
		return key, nil
	}
	// Provision on demand and open a journal so the key is recorded.
	key := controlKeyForTest(attemptID)
	f.keys[k] = key
	f.journals[k] = newFakeControlJournal(key)
	return key, nil
}

// fileResultStore is a simple file-backed ResultStore for tests. Results are
// persisted under <root>/runs/<runID>/result.json. SaveInvokeJobResult is
// idempotent: a second save with the same terminal status is a no-op.
type fileResultStore struct {
	root string
}

func newFileResultStore(root string) *fileResultStore {
	return &fileResultStore{root: root}
}

func (r *fileResultStore) resultPath(runID routedrun.RunID) string {
	return filepath.Join(r.root, "runs", string(runID), "result.json")
}

func (r *fileResultStore) SaveInvokeJobResult(ctx context.Context, result *routedrun.InvokeJobResult) error {
	_ = ctx
	if result == nil {
		return errors.New("nil result")
	}
	path := r.resultPath(result.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		// Idempotent: already committed.
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (r *fileResultStore) GetInvokeJobResult(ctx context.Context, runID routedrun.RunID) (*routedrun.InvokeJobResult, error) {
	_ = ctx
	path := r.resultPath(runID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, routedrun.ErrNotFound
		}
		return nil, err
	}
	var res routedrun.InvokeJobResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type testHarness struct {
	t        *testing.T
	clock    *fakeClock
	store    *routedrun.LocalStore
	results  *fileResultStore
	journals *fakeControlJournalFactory
	supervisor *Supervisor

	runID      routedrun.RunID
	workflowID  routedrun.WorkflowID
	attemptID  routedrun.AttemptID
	leaseID    routedrun.LeaseID
	controlKey []byte
}

func newTestHarness(t *testing.T, opts ...ClaimOptions) *testHarness {
	t.Helper()
	dir := t.TempDir()
	store, err := routedrun.OpenLocalStore(dir, routedrun.WithClock(func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}
	clock := newFakeClock(time.Unix(1_000_000, 0).UTC())
	results := newFileResultStore(dir)
	journals := newFakeControlJournalFactory()
	sup, err := NewSupervisor(store, results, journals, clock, dir)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	h := &testHarness{
		t:          t,
		clock:      clock,
		store:      store,
		results:    results,
		journals:  journals,
		supervisor: sup,
	}
	h.seedRunAndWorkflow()
	return h
}

// seedRunAndWorkflow creates a workflow, run, and a single non-terminal
// attempt so Claim has something to claim. The attempt is left PENDING.
func (h *testHarness) seedRunAndWorkflow() {
	ctx := context.Background()
	wf := &routedrun.WorkflowRecord{
		SchemaVersion:          routedrun.CurrentSchemaVersion,
		WorkflowKind:          "standalone",
		Status:                 routedrun.WorkflowStatusRunning,
		Generation:             1,
		MaxActiveDurationMs:    600_000,
		MaxAttemptLeaseMs:      300_000,
	}
	if err := h.store.CreateWorkflow(ctx, wf); err != nil {
		h.t.Fatalf("CreateWorkflow: %v", err)
	}
	h.workflowID = wf.WorkflowID
	run := &routedrun.RunRecord{
		SchemaVersion:       routedrun.CurrentSchemaVersion,
		RunID:               routedrun.RunID("run-test"),
		WorkflowID:          wf.WorkflowID,
		Status:              routedrun.RunStatusRunning,
		RunKind:             "standalone",
		MaxActiveDurationMs: 600_000,
		MaxAttemptLeaseMs:   300_000,
	}
	if err := h.store.CreateRun(ctx, run); err != nil {
		h.t.Fatalf("CreateRun: %v", err)
	}
	h.runID = run.RunID
	// Seed an active-time ledger with a running segment so active-time
	// accounting has something to reconcile.
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            0,
		RunningSegmentStartMs: ptrInt64(h.clock.NowMonotonic().UnixMilli()),
	}
	if err := h.store.PutActiveTimeLedger(ctx, wf.WorkflowID, ledger, 1); err != nil {
		h.t.Fatalf("PutActiveTimeLedger: %v", err)
	}
}

func (h *testHarness) claimAttempt() (routedrun.AttemptID, error) {
	ctx := context.Background()
	attID, err := h.supervisor.ClaimForRun(ctx, h.runID, "inv-test")
	if err != nil {
		return "", err
	}
	h.attemptID = attID
	// The Claim creates an attempt record; load it to get the lease ID.
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		h.t.Fatalf("GetAttempt after Claim: %v", err)
	}
	if att.Lease != nil {
		h.leaseID = att.Lease.LeaseID
	}
	h.controlKey = h.journals.get(h.runID, attID).key
	return attID, nil
}

func ptrInt64(v int64) *int64 { return &v }

// makeProgress builds and signs a ProgressEvent for the harness attempt.
func (h *testHarness) makeProgress(seq int64, phase string) ProgressEvent {
	p := ProgressEvent{
		AttemptID: h.attemptID,
		LeaseID:   h.leaseID,
		Sequence:  seq,
		Timestamp: h.clock.Now(),
		Phase:     phase,
	}
	return signProgress(p, h.controlKey)
}

func (h *testHarness) makeForgedProgress(seq int64, phase string) ProgressEvent {
	p := ProgressEvent{
		AttemptID: h.attemptID,
		LeaseID:   h.leaseID,
		Sequence:  seq,
		Timestamp: h.clock.Now(),
		Phase:     phase,
		HMAC:      "deadbeef", // forged HMAC
	}
	return p
}

func (h *testHarness) makeSuccessResult() ResultEvent {
	resultJSON := `{"ok":true}`
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(resultJSON)))
	r := ResultEvent{
		AttemptID:       h.attemptID,
		LeaseID:         h.leaseID,
		RunID:           h.runID,
		WorkflowID:      h.workflowID,
		InvocationID:    "inv-test",
		TerminalStatus:  routedrun.InvokeJobResultSucceeded,
		StructuredResult: resultJSON,
		ResultDigest:    digest,
	}
	return signResult(r, h.controlKey)
}

// makeForgedCheckpoint builds a CheckpointEvent with a bad HMAC for testing HMAC rejection.
// makeCheckpoint builds and signs a CheckpointEvent for the harness attempt.
func (h *testHarness) makeCheckpoint(cp *routedrun.SemanticCheckpoint) CheckpointEvent {
	c := CheckpointEvent{
		AttemptID:  h.attemptID,
		LeaseID:    h.leaseID,
		Checkpoint: cp,
	}
	return signCheckpoint(c, h.controlKey)
}

func (h *testHarness) attemptStatus() routedrun.AttemptStatus {
	att, err := h.store.GetAttempt(context.Background(), h.attemptID)
	if err != nil {
		h.t.Fatalf("GetAttempt: %v", err)
	}
	return att.Status
}

func (h *testHarness) ledger() *routedrun.ActiveTimeLedger {
	l, err := h.store.GetActiveTimeLedger(context.Background(), h.workflowID)
	if err != nil {
		h.t.Fatalf("GetActiveTimeLedger: %v", err)
	}
	return l
}

// ---------------------------------------------------------------------------
// Tests: liveness / stall
// ---------------------------------------------------------------------------

// TestHeartbeatPreventsStall verifies an authenticated heartbeat resets the
// stall timer: a heartbeat at T, then advance < stallTimeout, no stall.
func TestHeartbeatPreventsStall(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	env := h.envFor()
	// Send an authenticated heartbeat at T+500ms.
	h.clock.AdvanceMonotonic(500 * time.Millisecond)
	p := h.makeProgress(1, "working")
	if err := h.supervisor.TrackProgress(ctx, attID, p); err != nil {
		t.Fatalf("TrackProgress: %v", err)
	}
	// Advance to T+900ms (less than stallTimeout=1000ms since last activity).
	h.clock.AdvanceMonotonic(400 * time.Millisecond)
	stalled, err := h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall: %v", err)
	}
	if stalled {
		t.Fatal("authenticated heartbeat should prevent stall within timeout")
	}
	// Advance past stall timeout: now stalls.
	h.clock.AdvanceMonotonic(1100 * time.Millisecond)
	stalled, err = h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall after: %v", err)
	}
	if !stalled {
		t.Fatal("expected stall after >stallTimeout since last authenticated activity")
	}
}

// TestStdoutSpamDoesNotPreventStall: unauthenticated stdout does not reset the
// stall timer. After claiming and emitting stdout, advancing past the stall
// timeout stalls.
func TestStdoutSpamDoesNotPreventStall(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	env := h.envFor()
	// Emit unauthenticated stdout at T+500ms.
	h.clock.AdvanceMonotonic(500 * time.Millisecond)
	if err := h.supervisor.UnauthenticatedActivity(ctx, attID, "stdout spam"); err != nil {
		t.Fatalf("UnauthenticatedActivity: %v", err)
	}
	// Advance to T+1600ms (past stallTimeout 1000ms since claim at T=0ms).
	// The unauthenticated stdout at T+500ms should NOT have reset the timer.
	h.clock.AdvanceMonotonic(1100 * time.Millisecond)
	stalled, err := h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall: %v", err)
	}
	if !stalled {
		t.Fatal("unauthenticated stdout should NOT prevent stall")
	}
}

// TestGovernedOperationExemption: an in-flight model call extends the stall
// deadline to the operation deadline. While the operation is in flight, the
// stall timer does NOT fire at the raw stall timeout; it fires at the operation
// deadline (bounded by modelCallTimeoutMs, lease, and active time).
func TestGovernedOperationExemption(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	env := h.envFor()
	// Start a model call at T+500ms.
	h.clock.AdvanceMonotonic(500 * time.Millisecond)
	if err := h.supervisor.HandleModelStart(ctx, attID, h.leaseID); err != nil {
		t.Fatalf("HandleModelStart: %v", err)
	}
	// Advance past the raw stall timeout (1000ms) but within the model-call
	// operation deadline (5000ms): should NOT stall.
	h.clock.AdvanceMonotonic(1200 * time.Millisecond)
	stalled, err := h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall within op deadline: %v", err)
	}
	if stalled {
		t.Fatal("in-flight governed operation should exempt stall until operation deadline")
	}
	// Advance past the model-call operation deadline (5000ms from start): now
	// stalls.
	h.clock.AdvanceMonotonic(5100 * time.Millisecond)
	stalled, err = h.supervisor.CheckStall(ctx, attID, env)
	if err != nil {
		t.Fatalf("CheckStall after op deadline: %v", err)
	}
	if !stalled {
		t.Fatal("expected stall after in-flight operation exceeded its deadline")
	}
}

// TestForgedProgressRejected: progress without a valid HMAC is rejected and
// does NOT reset the stall timer.
func TestForgedProgressRejected(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	// Forged progress (bad HMAC) must be rejected.
	p := h.makeForgedProgress(1, "evil")
	if err := h.supervisor.TrackProgress(ctx, attID, p); !errors.Is(err, ErrInvalidHMAC) {
		t.Fatalf("TrackProgress forged: want ErrInvalidHMAC, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: finalization / races
// ---------------------------------------------------------------------------

// TestNoSuccessWithoutVerifiedResult: calling Finalize after the container
// exits zero (i.e. with no verified result event) does NOT mark the attempt
// succeeded; it marks it failed.
func TestNoSuccessWithoutVerifiedResult(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	// Container exits zero but no verified result event was committed.
	if err := h.supervisor.Finalize(ctx, attID); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED (no verified result)", got)
	}
}

// TestHandleResultFinalizesSuccess: a verified result event finalizes the
// attempt as SUCCEEDED and writes the InvokeJobResult.
func TestHandleResultFinalizesSuccess(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	r := h.makeSuccessResult()
	if err := h.supervisor.HandleResult(ctx, attID, r); err != nil {
		t.Fatalf("HandleResult: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusSucceeded {
		t.Fatalf("attempt status = %s, want SUCCEEDED", got)
	}
	res, err := h.results.GetInvokeJobResult(ctx, h.runID)
	if err != nil {
		t.Fatalf("GetInvokeJobResult: %v", err)
	}
	if res.TerminalStatus != routedrun.InvokeJobResultSucceeded {
		t.Fatalf("result status = %s, want SUCCEEDED", res.TerminalStatus)
	}
}

// TestDuplicateFinalizer: calling Finalize twice is idempotent - no double-
// write, no error on the second call.
func TestDuplicateFinalizer(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	if err := h.supervisor.Finalize(ctx, attID); err != nil {
		t.Fatalf("first Finalize: %v", err)
	}
	// Second Finalize is idempotent: no error, state unchanged.
	if err := h.supervisor.Finalize(ctx, attID); err != nil {
		t.Fatalf("second Finalize: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", got)
	}
}

// TestCancelPrecedenceOverLateSuccess: a Cancel wins over a late result event.
// After Cancel finalizes the attempt as CANCELLED, a late HandleResult must be
// rejected and not change the state.
func TestCancelPrecedenceOverLateSuccess(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("after Cancel: attempt status = %s, want CANCELLED", got)
	}
	// A late result event must be rejected.
	r := h.makeSuccessResult()
	if err := h.supervisor.HandleResult(ctx, attID, r); !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("late HandleResult: want ErrAlreadyTerminal, got %v", err)
	}
	// State unchanged.
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("after late result: attempt status = %s, want CANCELLED", got)
	}
}

// TestCancelIsIdempotent: calling Cancel twice is idempotent.
func TestCancelIsIdempotent(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	if err := h.supervisor.Cancel(ctx, attID); err != nil {
		t.Fatalf("second Cancel (idempotent): %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusCancelled {
		t.Fatalf("attempt status = %s, want CANCELLED", got)
	}
}

// TestForgedResultRejected: a result event with a forged HMAC is rejected and
// does NOT finalize.
func TestForgedResultRejected(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	r := h.makeSuccessResult()
	r.HMAC = "forged"
	if err := h.supervisor.HandleResult(ctx, attID, r); !errors.Is(err, ErrInvalidHMAC) {
		t.Fatalf("HandleResult forged: want ErrInvalidHMAC, got %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusRunning {
		t.Fatalf("after forged result: attempt status = %s, want RUNNING", got)
	}
}

// TestLateResultRejected: a result event with a stale lease ID is rejected.
func TestLateResultRejected(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	r := h.makeSuccessResult()
	r.LeaseID = routedrun.LeaseID("stale-lease")
	r = signResult(r, h.controlKey)
	if err := h.supervisor.HandleResult(ctx, attID, r); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("HandleResult stale lease: want ErrLeaseMismatch, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: reconcile / restart
// ---------------------------------------------------------------------------

// TestReconcileRevokesAmbiguousLease: on restart with an active lease but no
// committed terminal event, Reconcile revokes the lease and marks the attempt
// FAILED with reason "daemon_restart".
func TestReconcileRevokesAmbiguousLease(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	// Simulate a daemon restart: drop the in-memory tracker, then Reconcile.
	h.supervisor = mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := h.supervisor.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusFailed {
		t.Fatalf("attempt status = %s, want FAILED", att.Status)
	}
	if att.FailureReason == nil || !strings.Contains(att.FailureReason.String(), "DAEMON_RESTARTED") {
		t.Fatalf("failure reason = %v, want DAEMON_RESTARTED", att.FailureReason)
	}
	// Lease must be revoked (expired/cleared token).
	if att.Lease != nil && att.Lease.LeaseToken != "" {
		t.Fatalf("lease token = %q, want cleared", att.Lease.LeaseToken)
	}
}

// TestReconcileAcceptsCommittedTerminal: on restart with a committed
// SUCCEEDED event in the control journal, Reconcile does NOT replay work and
// leaves the terminal state in place.
func TestReconcileAcceptsCommittedTerminal(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	// Commit a verified SUCCEEDED result.
	r := h.makeSuccessResult()
	if err := h.supervisor.HandleResult(ctx, attID, r); err != nil {
		t.Fatalf("HandleResult: %v", err)
	}
	if got := h.attemptStatus(); got != routedrun.AttemptStatusSucceeded {
		t.Fatalf("pre-reconcile status = %s, want SUCCEEDED", got)
	}
	// Simulate restart: new supervisor, reconcile.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	// State must remain SUCCEEDED (not replayed to FAILED).
	att, err := h.store.GetAttempt(ctx, attID)
	if err != nil {
		t.Fatalf("GetAttempt: %v", err)
	}
	if att.Status != routedrun.AttemptStatusSucceeded {
		t.Fatalf("post-reconcile status = %s, want SUCCEEDED (no replay)", att.Status)
	}
}

// TestReconcilePreservesCheckpoint: on restart, a safe checkpoint is preserved
// for B39 continuation.
func TestReconcilePreservesCheckpoint(t *testing.T) {
	h := newTestHarness(t)
	attID, err := h.claimAttempt()
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	cp := &routedrun.SemanticCheckpoint{
		SchemaVersion:    routedrun.CurrentSchemaVersion,
		CheckpointID:     "cp-test-1",
		AttemptID:        attID,
		RunID:            h.runID,
		WorkflowID:       h.workflowID,
		LeaseID:          h.leaseID,
		Phase:            "phase-1",
		CompletedWork:    []string{"a", "b"},
		RemainingWork:    []string{"c"},
		SafeToResume:     true,
		Sequence:         1,
		CreatedAt:        h.clock.Now(),
	}
	// Commit the checkpoint via the supervisor.
	if err := h.supervisor.HandleCheckpoint(ctx, attID, h.makeCheckpoint(cp)); err != nil {
		t.Fatalf("HandleCheckpoint: %v", err)
	}
	// Simulate restart.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	// Checkpoint must still be retrievable.
	got, err := h.store.GetLatestCheckpoint(ctx, attID)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.CheckpointID != "cp-test-1" {
		t.Fatalf("checkpoint id = %s, want cp-test-1", got.CheckpointID)
	}
	if !got.SafeToResume {
		t.Fatal("checkpoint must remain safe-to-resume after reconcile")
	}
}

// ---------------------------------------------------------------------------
// Tests: active-time reconciliation
// ---------------------------------------------------------------------------

// TestActiveTimeFrozenDuringPause: a synthetic PAUSED workflow record that has
// advanced wall time (with an open running segment) does NOT accrue active
// time across Reconcile; the open segment is conservatively closed without
// charging unknown wall time.
func TestActiveTimeFrozenDuringPause(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	// Synthetic: set the workflow to PAUSED and the ledger to have an open
	// running segment that started in the (frozen) past.
	wf, err := h.store.GetWorkflow(ctx, h.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPaused
	if err := h.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow PAUSED: %v", err)
	}
	consumedBefore := int64(5_000)
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            consumedBefore,
		RunningSegmentStartMs: ptrInt64(h.clock.NowMonotonic().UnixMilli()),
	}
	if err := h.store.PutActiveTimeLedger(ctx, h.workflowID, ledger, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}
	// Advance the wall+monotonic clock significantly (simulating wall time
	// passing while PAUSED).
	h.clock.AdvanceMonotonic(600 * time.Second)
	// Reconcile.
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := h.ledger()
	if got.ConsumedMs != consumedBefore {
		t.Fatalf("consumed = %d, want %d (PAUSED must not accrue active time)", got.ConsumedMs, consumedBefore)
	}
	if got.RunningSegmentStartMs != nil {
		t.Fatal("PAUSED reconcile must close the open segment")
	}
}

// TestActiveTimeFrozenDuringNeedsReplan: a synthetic NEEDS_REPLAN record that
// has advanced wall time does NOT accrue active time.
func TestActiveTimeFrozenDuringNeedsReplan(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	wf, err := h.store.GetWorkflow(ctx, h.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusNeedsReplan
	if err := h.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow NEEDS_REPLAN: %v", err)
	}
	consumedBefore := int64(7_000)
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            consumedBefore,
		RunningSegmentStartMs: ptrInt64(h.clock.NowMonotonic().UnixMilli()),
	}
	if err := h.store.PutActiveTimeLedger(ctx, h.workflowID, ledger, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}
	h.clock.AdvanceMonotonic(300 * time.Second)
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := h.ledger()
	if got.ConsumedMs != consumedBefore {
		t.Fatalf("consumed = %d, want %d (NEEDS_REPLAN must not accrue)", got.ConsumedMs, consumedBefore)
	}
}

// TestActiveTimeChargedDuringPauseRequested: a synthetic PAUSE_REQUESTED record
// that has advanced wall time DOES accrue active time across Reconcile
// (PAUSE_REQUESTED is an accruing state per b30-summary.md).
func TestActiveTimeChargedDuringPauseRequested(t *testing.T) {
	h := newTestHarness(t)
	if _, err := h.claimAttempt(); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	ctx := context.Background()
	wf, err := h.store.GetWorkflow(ctx, h.workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	if err := h.store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow PAUSE_REQUESTED: %v", err)
	}
	consumedBefore := int64(3_000)
	segmentStart := h.clock.NowMonotonic().UnixMilli()
	ledger := &routedrun.ActiveTimeLedger{
		SchemaVersion:         routedrun.CurrentSchemaVersion,
		ConsumedMs:            consumedBefore,
		RunningSegmentStartMs: ptrInt64(segmentStart),
	}
	if err := h.store.PutActiveTimeLedger(ctx, h.workflowID, ledger, 1); err != nil {
		t.Fatalf("PutActiveTimeLedger: %v", err)
	}
	// Advance 200,000ms = 200s.
	h.clock.AdvanceMonotonic(200 * time.Second)
	sup2 := mustNewSupervisor(t, h.store, h.results, h.journals, h.clock, h.t.TempDir())
	if err := sup2.Reconcile(ctx, h.runID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := h.ledger()
	// PAUSE_REQUESTED accrues: consumed must have grown by ~200,000ms.
	if got.ConsumedMs <= consumedBefore {
		t.Fatalf("consumed = %d, want > %d (PAUSE_REQUESTED must accrue)", got.ConsumedMs, consumedBefore)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *testHarness) envFor() routedrun.TimeEnvelope {
	// Build a TimeEnvelope from the harness ceilings for stall checks.
	return routedrun.NewTimeEnvelope(
		600_000, // max active
		60_000,  // lease
		1_000,   // stall
		5_000,   // model call
	)
}

func mustNewSupervisor(t *testing.T, store *routedrun.LocalStore, results *fileResultStore, journals *fakeControlJournalFactory, clock *fakeClock, stateRoot string) *Supervisor {
	t.Helper()
	sup, err := NewSupervisor(store, results, journals, clock, stateRoot)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	return sup
}
