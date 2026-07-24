package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// B33-T07 round-2 adversary matrix — NEW vectors only (do not duplicate
// adversary_t07_test.go). Focus: cleanup I/O windows, parent/key components,
// terminal status monotonicity, cancel-on-cleanup, hang timeouts, orphans,
// evidence injection, and demotion of committed status.

// ---------------------------------------------------------------------------
// Cleanup I/O window: capability / cancel residual (HIGH)
// ---------------------------------------------------------------------------

// delayStopDriver wraps fakeRuntimeDriver and blocks inside Stop until released.
// Used to open the cleanup I/O window for concurrent observation.
type delayStopDriver struct {
	inner       *fakeRuntimeDriver
	stopEntered chan struct{}
	releaseStop chan struct{}
	stopOnce    sync.Once
	listHang    chan struct{} // if non-nil, ListContainers blocks until closed
	removeHang  chan struct{} // if non-nil, RemoveNetwork blocks until closed
}

func newDelayStopDriver() *delayStopDriver {
	return &delayStopDriver{
		inner:       newFakeRuntimeDriver(),
		stopEntered: make(chan struct{}),
		releaseStop: make(chan struct{}),
	}
}

func (d *delayStopDriver) Create(ctx context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
	return d.inner.Create(ctx, spec)
}
func (d *delayStopDriver) Start(ctx context.Context, id runtime.ContainerID) error {
	return d.inner.Start(ctx, id)
}
func (d *delayStopDriver) Stop(ctx context.Context, id runtime.ContainerID, timeout *time.Duration) error {
	d.stopOnce.Do(func() { close(d.stopEntered) })
	select {
	case <-d.releaseStop:
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.inner.Stop(ctx, id, timeout)
}
func (d *delayStopDriver) Remove(ctx context.Context, id runtime.ContainerID, force bool) error {
	return d.inner.Remove(ctx, id, force)
}
func (d *delayStopDriver) Status(ctx context.Context, id runtime.ContainerID) (runtime.ContainerStatus, error) {
	return d.inner.Status(ctx, id)
}
func (d *delayStopDriver) Stats(ctx context.Context, id runtime.ContainerID) (runtime.ContainerStats, error) {
	return d.inner.Stats(ctx, id)
}
func (d *delayStopDriver) Logs(ctx context.Context, id runtime.ContainerID, opts runtime.LogOptions) (io.ReadCloser, error) {
	return d.inner.Logs(ctx, id, opts)
}
func (d *delayStopDriver) Exec(ctx context.Context, id runtime.ContainerID, cmd []string) (string, string, int, error) {
	return d.inner.Exec(ctx, id, cmd)
}
func (d *delayStopDriver) CreateNetwork(ctx context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
	return d.inner.CreateNetwork(ctx, spec)
}
func (d *delayStopDriver) RemoveNetwork(ctx context.Context, id runtime.NetworkID) error {
	if d.removeHang != nil {
		select {
		case <-d.removeHang:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return d.inner.RemoveNetwork(ctx, id)
}
func (d *delayStopDriver) InspectNetwork(ctx context.Context, id runtime.NetworkID) (runtime.NetworkInfo, error) {
	return d.inner.InspectNetwork(ctx, id)
}
func (d *delayStopDriver) AttachNetwork(ctx context.Context, c runtime.ContainerID, n runtime.NetworkID) error {
	return d.inner.AttachNetwork(ctx, c, n)
}
func (d *delayStopDriver) DetachNetwork(ctx context.Context, c runtime.ContainerID, n runtime.NetworkID) error {
	return d.inner.DetachNetwork(ctx, c, n)
}
func (d *delayStopDriver) InspectContainerNetworks(ctx context.Context, id runtime.ContainerID) ([]runtime.ContainerNetworkInfo, error) {
	return d.inner.InspectContainerNetworks(ctx, id)
}
func (d *delayStopDriver) InspectContainerIP(ctx context.Context, id runtime.ContainerID, networkID string) (string, error) {
	return d.inner.InspectContainerIP(ctx, id, networkID)
}
func (d *delayStopDriver) ListContainers(ctx context.Context, labelFilters ...string) ([]runtime.ContainerInfo, error) {
	if d.listHang != nil {
		select {
		case <-d.listHang:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.inner.ListContainers(ctx, labelFilters...)
}
func (d *delayStopDriver) ListNetworks(ctx context.Context, labelFilters ...string) ([]runtime.NetworkInfo, error) {
	return d.inner.ListNetworks(ctx, labelFilters...)
}

// TestAdversaryT07b_CleanupIOWindowLeavesCapabilityUsable proves capability is
// NOT cleared when entering STOPPING — only after slow driver I/O returns.
// Round-1 only checked post-cleanup residual with nil driver (no window).
func TestAdversaryT07b_CleanupIOWindowLeavesCapabilityUsable(t *testing.T) {
	const capTok = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	drv := newDelayStopDriver()
	// Pre-register container so Stop/Remove succeed after release.
	drv.inner.mu.Lock()
	cid := runtime.ContainerID("cid-cleanup-window")
	drv.inner.statuses[cid] = runtime.ContainerStatusRunning
	drv.inner.specs[cid] = runtime.ContainerSpec{Labels: map[string]string{}}
	drv.inner.mu.Unlock()

	reg := NewServiceRegistry(drv, nil, nil)
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", capTok, []string{"tool_a"})
	inst.ContainerID = cid
	inst.Generation = 1
	inst.NetworkAlias = "alias-1"
	reg.instances["wf-1/svc-1"] = inst

	done := make(chan error, 1)
	go func() {
		_, err := reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
		done <- err
	}()

	select {
	case <-drv.stopEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup never entered driver.Stop")
	}

	// Mid-I/O observation: capability must already be unusable.
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		close(drv.releaseStop)
		t.Fatalf("Get during cleanup: %v", err)
	}
	if got.Capability == capTok || got.Capability != "" {
		close(drv.releaseStop)
		<-done
		// ADVERSARY BREAK: HIGH - capability remains trusted during cleanup I/O window
		t.Fatalf("ADVERSARY BREAK: HIGH - capability usable during Cleanup STOPPING window: cap=%q state=%s endpoint=%q",
			got.Capability, got.State, got.Endpoint)
	}
	if got.Endpoint != "" || got.NetworkAlias != "" {
		close(drv.releaseStop)
		<-done
		// ADVERSARY BREAK: HIGH - endpoint/alias usable during cleanup I/O
		t.Fatalf("ADVERSARY BREAK: HIGH - endpoint/alias during cleanup I/O: ep=%q alias=%q", got.Endpoint, got.NetworkAlias)
	}

	close(drv.releaseStop)
	if err := <-done; err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

// TestAdversaryT07b_CleanupDoesNotCancelInFlightCalls: Fence cancels via
// CancelTracker; CleanupServiceResources must also revoke in-flight work so
// late tool results cannot commit after teardown.
func TestAdversaryT07b_CleanupDoesNotCancelInFlightCalls(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://x", "cap", []string{"t"})
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callID, err := reg.RegisterCall("wf-1", "svc-1", cancel)
	if err != nil {
		t.Fatalf("RegisterCall: %v", err)
	}
	if callID == "" {
		t.Fatal("empty call id")
	}

	_, err = reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	select {
	case <-ctx.Done():
		// cancelled — good
	default:
		// ADVERSARY BREAK: HIGH - CleanupServiceResources left in-flight call contexts live
		t.Fatalf("ADVERSARY BREAK: HIGH - CleanupServiceResources did not CancelAll in-flight MCP calls (Fence does; cleanup must too)")
	}
}

// TestAdversaryT07b_CleanupGenMismatchDestroysLiveContainer: cleanup snapshots
// containerID, concurrent gen bump keeps same container identity, cleanup
// removes the live container then skips state clear → READY/cap with dead cid
// or STOPPING+cap residual.
func TestAdversaryT07b_CleanupGenMismatchDestroysLiveContainer(t *testing.T) {
	const capTok = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	drv := newDelayStopDriver()
	drv.inner.mu.Lock()
	cid := runtime.ContainerID("cid-shared-gen")
	drv.inner.statuses[cid] = runtime.ContainerStatusRunning
	drv.inner.specs[cid] = runtime.ContainerSpec{Labels: map[string]string{}}
	drv.inner.mu.Unlock()

	reg := NewServiceRegistry(drv, nil, nil)
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", capTok, []string{"t"})
	inst.ContainerID = cid
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	}()

	select {
	case <-drv.stopEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop not entered")
	}

	// Concurrent "restart": bump generation, restore READY, keep same container.
	reg.mu.Lock()
	inst.mu.Lock()
	inst.Generation = 2
	inst.State = StateReady
	inst.Capability = capTok
	inst.ContainerID = cid
	inst.Endpoint = "http://svc:8080"
	inst.mu.Unlock()
	reg.mu.Unlock()

	close(drv.releaseStop)
	<-done

	if !drv.inner.removed(cid) {
		t.Fatal("expected old/live container removed by cleanup")
	}
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// After gen-mismatch skip, instance still claims capability/endpoint for a
	// container that no longer exists — trust/material residual.
	if got.Capability != "" || got.State == StateReady {
		// ADVERSARY BREAK: HIGH - gen-mismatch cleanup destroyed container but left READY+capability
		t.Fatalf("ADVERSARY BREAK: HIGH - cleanup destroyed live container under gen mismatch residual state=%s cap=%q endpoint=%q cid=%v",
			got.State, got.Capability, got.Endpoint, got.ContainerID)
	}
}

// ---------------------------------------------------------------------------
// DiscoverOrphans tracking gaps (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_DiscoverOrphansMissesStoppingAndFailed: STOPPING/FAILED
// instances still own containers but are excluded from the tracked set → false
// orphans (double-stop) or operator confusion.
func TestAdversaryT07b_DiscoverOrphansMissesStoppingAndFailed(t *testing.T) {
	drv := newFakeRuntimeDriver()
	reg := NewServiceRegistry(drv, nil, nil)

	// Seed two containers with MCP labels matching DiscoverOrphans filters.
	seed := func(id, wf, svc string) {
		drv.mu.Lock()
		defer drv.mu.Unlock()
		cid := runtime.ContainerID(id)
		drv.statuses[cid] = runtime.ContainerStatusRunning
		drv.specs[cid] = runtime.ContainerSpec{Labels: map[string]string{
			runtime.LabelManagedBy:    runtime.ManagedByValue,
			runtime.LabelWorkflowID:   wf,
			runtime.LabelResourceType: runtime.ResourceTypeMCP,
			runtime.LabelServiceID:    svc,
		}}
	}
	seed("cid-stopping", "wf-1", "svc-stop")
	seed("cid-failed", "wf-1", "svc-fail")

	instStop := TestServiceInstance("wf-1", "svc-stop", StateStopping, "http://x", "c", nil)
	instStop.ContainerID = runtime.ContainerID("cid-stopping")
	reg.instances["wf-1/svc-stop"] = instStop

	instFail := TestServiceInstance("wf-1", "svc-fail", StateFailed, "http://x", "c", nil)
	instFail.ContainerID = runtime.ContainerID("cid-failed")
	reg.instances["wf-1/svc-fail"] = instFail

	orphans, err := reg.DiscoverOrphans(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("DiscoverOrphans: %v", err)
	}
	orphanSet := map[string]bool{}
	for _, o := range orphans {
		orphanSet[string(o)] = true
	}
	if orphanSet["cid-stopping"] {
		// ADVERSARY BREAK: MEDIUM - STOPPING-owned container reported as orphan (double-cleanup race)
		t.Fatalf("ADVERSARY BREAK: MEDIUM - DiscoverOrphans reported STOPPING-owned container as orphan: %v", orphans)
	}
	if orphanSet["cid-failed"] {
		// ADVERSARY BREAK: MEDIUM - FAILED-owned container reported as orphan
		t.Fatalf("ADVERSARY BREAK: MEDIUM - DiscoverOrphans reported FAILED-owned container as orphan: %v", orphans)
	}
}

// ---------------------------------------------------------------------------
// Terminal status monotonicity / demotion (HIGH)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_TerminalFailedOverwrittenBySucceeded: any non-success
// terminal must not be raised to SUCCEEDED (round-1 covered CANCELLED/UNKNOWN only).
func TestAdversaryT07b_TerminalFailedOverwrittenBySucceeded(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	corr := "corr-failed-terminal"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusFailed,
		Reason:        "upstream 500",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusSucceeded,
		OutputDigest:  "forged",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	final, _ := store.GetCall(corr)
	if final.Status == CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - FAILED evidence rewritten to SUCCEEDED
		t.Fatalf("ADVERSARY BREAK: HIGH - FAILED call evidence overwritten to SUCCEEDED (fabricated commit)")
	}
}

// TestAdversaryT07b_SucceededDemotedToUnknownReopensInFlight: RecordCall accepts
// UNKNOWN after SUCCEEDED, re-enters inFlight, enabling restart mark / late rewrite games.
func TestAdversaryT07b_SucceededDemotedToUnknownReopensInFlight(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	corr := "corr-demote"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusSucceeded,
		OutputDigest:  "real",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	// Adversary demotes committed success.
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusUnknown,
		StartedAt:     time.Now().UTC(),
	})
	rec, ok := store.GetCall(corr)
	if !ok || rec.Status != CallStatusUnknown {
		t.Fatalf("demotion did not stick: ok=%v status=%s", ok, rec.Status)
	}
	// inFlight should not accept demotion of a committed success.
	store.mu.RLock()
	inFlight := store.inFlight[corr]
	store.mu.RUnlock()
	if inFlight {
		// ADVERSARY BREAK: HIGH - committed SUCCEEDED demoted to in-flight UNKNOWN
		t.Fatalf("ADVERSARY BREAK: HIGH - SUCCEEDED demoted to UNKNOWN re-entered inFlight map")
	}
	// Even if not inFlight, demotion itself destroys durable commit evidence.
	if rec.Status != CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - store allows erase of SUCCEEDED commit via UNKNOWN rewrite
		t.Fatalf("ADVERSARY BREAK: HIGH - store allowed demotion of SUCCEEDED → %s", rec.Status)
	}
}

// TestAdversaryT07b_TimeoutOverwrittenBySucceeded same terminal class as FAILED.
func TestAdversaryT07b_TimeoutOverwrittenBySucceeded(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	corr := "corr-timeout"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr, WorkflowID: "wf", Tool: "t", InputDigest: "d",
		Status: CallStatusTimeout, StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	})
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr, WorkflowID: "wf", Tool: "t", InputDigest: "d",
		Status: CallStatusSucceeded, OutputDigest: "x", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
	})
	final, _ := store.GetCall(corr)
	if final.Status == CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - TIMEOUT evidence rewritten to SUCCEEDED
		t.Fatalf("ADVERSARY BREAK: HIGH - TIMEOUT overwritten to SUCCEEDED")
	}
}

// TestAdversaryT07b_MarkInFlightUnknownPreservesForgedOutputDigest: if a racy
// writer stuffed OutputDigest while still UNKNOWN, restart mark must not keep
// a success-looking digest on UNKNOWN/restart records.
func TestAdversaryT07b_MarkInFlightUnknownPreservesForgedOutputDigest(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	corr := "corr-digest-forge"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf-1",
		Tool:          "t",
		InputDigest:   "d",
		OutputDigest:  "forged-success-digest",
		Status:        CallStatusUnknown,
		StartedAt:     time.Now().UTC(),
	})
	if n := store.MarkInFlightUnknown("wf-1"); n != 1 {
		t.Fatalf("MarkInFlightUnknown=%d", n)
	}
	final, _ := store.GetCall(corr)
	if final.Status != CallStatusUnknown {
		t.Fatalf("status=%s", final.Status)
	}
	if final.OutputDigest != "" {
		// ADVERSARY BREAK: MEDIUM - restart UNKNOWN retains forged output_digest (looks committed)
		t.Fatalf("ADVERSARY BREAK: MEDIUM - MarkInFlightUnknown left output_digest=%q on restart UNKNOWN", final.OutputDigest)
	}
}

// ---------------------------------------------------------------------------
// Health key slash injection + cross-tenant empty workflow (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_HealthKeySlashInjection: healthStates keyed by wf+"/"+binding
// collides like lifecycle store (round-1 covered lifecycle only).
func TestAdversaryT07b_HealthKeySlashInjection(t *testing.T) {
	reg := newTestRegistry()
	// Victim instance
	reg.instances["wf-1/svc"] = TestServiceInstance("wf-1", "svc", StateReady, "http://x", "c", nil)
	// Colliding identities sharing composite key wf-1/svc/evil
	reg.instances["wf-1/svc/evil"] = TestServiceInstance("wf-1", "svc/evil", StateReady, "http://x", "c", nil)
	reg.instances["wf-1/svc"] = reg.instances["wf-1/svc"] // keep victim
	// Directly place second identity under colliding key used by RecordHealthFailure
	reg.instances["wf-1/svc/evil"] = TestServiceInstance("wf-1", "svc/evil", StateReady, "http://y", "c", nil)
	// Also register alternate split identity for Get path confusion
	reg.instances["wf-1/svc"] = TestServiceInstance("wf-1", "svc", StateReady, "http://x", "c", nil)

	reg.RecordHealthFailure("wf-1", "svc/evil", ErrCodeTimeout, "injected-a", "t")
	reg.RecordHealthFailure("wf-1/svc", "evil", ErrCodeOverloaded, "injected-b", "t")

	// Both writes target healthStates["wf-1/svc/evil"].
	sumA := reg.HealthSummary("wf-1", "svc/evil")
	sumB := reg.HealthSummary("wf-1/svc", "evil")
	// HealthSummary filters by inst.WorkflowID/ServiceBindingID fields, not map key —
	// so summary may miss failures recorded under colliding keys when instance fields differ.
	var failA, failB int
	if sumA != nil {
		for _, s := range sumA.Services {
			failA += len(s.RecentFailures)
		}
	}
	if sumB != nil {
		for _, s := range sumB.Services {
			failB += len(s.RecentFailures)
		}
	}
	// Probe raw healthStates collision.
	reg.mu.RLock()
	hs := reg.healthStates["wf-1/svc/evil"]
	reg.mu.RUnlock()
	if hs == nil {
		t.Fatal("expected health state at composite key wf-1/svc/evil")
	}
	fails := hs.getFailures()
	if len(fails) >= 2 {
		// ADVERSARY BREAK: MEDIUM - health key slash collision merges distinct identities
		t.Fatalf("ADVERSARY BREAK: MEDIUM - healthStates key collision merged %d failures for distinct (wf,binding) pairs", len(fails))
	}
	_ = failA
	_ = failB
}

// TestAdversaryT07b_RecordHealthFailureEmptyWorkflowCrossTalk: empty workflowID
// must not attach failures onto other tenants if keys collide on "/binding".
func TestAdversaryT07b_RecordHealthFailureEmptyWorkflowCrossTalk(t *testing.T) {
	reg := newTestRegistry()
	reg.instances["/svc"] = TestServiceInstance("", "svc", StateReady, "http://x", "c", nil)
	reg.instances["wf-real/svc"] = TestServiceInstance("wf-real", "svc", StateReady, "http://x", "c", nil)

	reg.RecordHealthFailure("", "svc", ErrCodeTimeout, "empty-wf-fail", "t")

	sum := reg.HealthSummary("wf-real", "svc")
	if sum != nil {
		for _, s := range sum.Services {
			for _, f := range s.RecentFailures {
				if f.Reason == "empty-wf-fail" || strings.Contains(f.Reason, "empty-wf") {
					// ADVERSARY BREAK: HIGH - empty workflow health failure leaked into real workflow
					t.Fatalf("ADVERSARY BREAK: HIGH - empty-workflow health failure visible on wf-real: %+v", f)
				}
			}
		}
	}
	// Control: empty workflow sees its own (if any).
	_ = reg.HealthSummary("", "svc")
}

// ---------------------------------------------------------------------------
// Missing timeouts on cleanup/orphan paths (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_DiscoverOrphansHonorsContextCancel: ListContainers hang must
// be abortable via ctx (no orphan discovery that ignores cancel).
func TestAdversaryT07b_DiscoverOrphansHonorsContextCancel(t *testing.T) {
	drv := newDelayStopDriver()
	drv.listHang = make(chan struct{}) // block forever until test ends
	reg := NewServiceRegistry(drv, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := reg.DiscoverOrphans(ctx, "wf-1")
	elapsed := time.Since(start)

	if err == nil {
		// ADVERSARY BREAK: MEDIUM - DiscoverOrphans ignored ctx cancel/timeout on ListContainers
		t.Fatalf("ADVERSARY BREAK: MEDIUM - DiscoverOrphans returned nil error despite cancelled ctx (elapsed=%s)", elapsed)
	}
	if elapsed > 2*time.Second {
		// ADVERSARY BREAK: MEDIUM - DiscoverOrphans hung far beyond ctx deadline
		t.Fatalf("ADVERSARY BREAK: MEDIUM - DiscoverOrphans hung %s ignoring ctx deadline", elapsed)
	}
	close(drv.listHang)
}

// TestAdversaryT07b_CleanupNetworkRemoveUsesBackgroundContext: empty-network
// cleanup calls RemoveServiceNetwork(context.Background(), ...) — cancelled
// parent ctx cannot abort network remove hang.
func TestAdversaryT07b_CleanupNetworkRemoveUsesBackgroundContext(t *testing.T) {
	drv := newDelayStopDriver()
	drv.removeHang = make(chan struct{})
	// Seed container + empty network path: after remove container, attachments=0 triggers network remove.
	drv.inner.mu.Lock()
	cid := runtime.ContainerID("cid-net")
	drv.inner.statuses[cid] = runtime.ContainerStatusRunning
	drv.inner.specs[cid] = runtime.ContainerSpec{Labels: map[string]string{}}
	netID := runtime.NetworkID("net-1")
	drv.inner.networks[netID] = runtime.NetworkSpec{Name: "svcnet"}
	drv.inner.mu.Unlock()

	reg := NewServiceRegistry(drv, nil, nil)
	inst := TestServiceInstance("wf-net", "svc", StateReady, "http://x", "c", nil)
	inst.ContainerID = cid
	inst.Generation = 1
	reg.instances["wf-net/svc"] = inst
	reg.serviceNetworks["wf-net"] = &serviceNetworkState{
		NetworkID:           netID,
		attachedContainers:  map[runtime.ContainerID]bool{cid: true},
	}

	parent, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		// Release Stop immediately so we reach network cleanup.
		// stopEntered will fire; release promptly.
		go func() {
			<-drv.stopEntered
			close(drv.releaseStop)
		}()
		_, err := reg.CleanupServiceResources(parent, "wf-net", "svc")
		done <- err
	}()

	select {
	case err := <-done:
		// If it returned quickly with error due to parent cancel during Stop — OK-ish.
		// If removeHang was hit with Background, it would not return until we close hang.
		_ = err
		if time.Since(start) < 30*time.Millisecond {
			// finished before parent timeout — network path may have been skipped
			close(drv.removeHang)
			return
		}
	case <-time.After(500 * time.Millisecond):
		// Still hung after parent ctx expired → Background used on network remove.
		close(drv.removeHang)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup never finished after releasing removeHang")
		}
		// ADVERSARY BREAK: MEDIUM - cleanupNetworkIfEmptyLocked uses context.Background(), ignores caller cancel
		t.Fatalf("ADVERSARY BREAK: MEDIUM - network remove ignored parent ctx cancel (hung past deadline; uses context.Background)")
	}
}

// ---------------------------------------------------------------------------
// Evidence field injection (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_EvidenceToolNewlineInjection: tool/binding fields stored raw
// enable log/forensics directive injection when serialized.
func TestAdversaryT07b_EvidenceToolNewlineInjection(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	evilTool := "good\nstatus: SUCCEEDED\ncorrelation_id: forged"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "c-nl",
		WorkflowID:    "wf\ninjected",
		BindingID:     "bind\rX-Injected: 1",
		Tool:          evilTool,
		InputDigest:   "d",
		Status:        CallStatusFailed,
		Reason:        "line1\nline2",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	rec, _ := store.GetCall("c-nl")
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	// JSON encoding escapes newlines — check structural acceptance without validation.
	if rec.Tool != evilTool {
		t.Fatalf("tool not preserved for injection probe")
	}
	if strings.Contains(rec.Tool, "\n") || strings.Contains(rec.WorkflowID, "\n") || strings.Contains(rec.BindingID, "\r") {
		// Acceptance of control chars is the gap when consumers pretty-print without escaping.
		// Flag as MEDIUM contract gap: no validation on evidence identity fields.
		// ADVERSARY BREAK: MEDIUM - evidence identity fields accept CR/LF (log injection)
		t.Fatalf("ADVERSARY BREAK: MEDIUM - evidence identity fields accept CR/LF injection tool=%q wf=%q bind=%q json=%s",
			rec.Tool, rec.WorkflowID, rec.BindingID, raw)
	}
}

// TestAdversaryT07b_NullByteCorrelationIDTruncation: null in correlation ID can
// confuse C bridges / file backends (T08 JSONL) via silent truncate.
func TestAdversaryT07b_NullByteCorrelationIDTruncation(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()

	id := "visible\x00hidden-tail"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: id,
		WorkflowID:    "wf",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusSucceeded,
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	// Second record that collides if null truncates to "visible"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: "visible",
		WorkflowID:    "wf",
		Tool:          "other",
		InputDigest:   "e",
		Status:        CallStatusFailed,
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	a, okA := store.GetCall(id)
	b, okB := store.GetCall("visible")
	if !okA || !okB {
		// Go maps keep null bytes — both exist. Still reject null IDs as contract.
		if !okA {
			t.Fatal("null-bearing id missing")
		}
	}
	if strings.Contains(a.CorrelationID, "\x00") {
		// ADVERSARY BREAK: MEDIUM - correlation IDs with NUL accepted (file/export truncate risk)
		t.Fatalf("ADVERSARY BREAK: MEDIUM - NUL accepted in CorrelationID %q (sibling visible status=%s)", a.CorrelationID, b.Status)
	}
	_ = okB
}

// ---------------------------------------------------------------------------
// Router TOCTOU finalize vs terminal restart (HIGH) — stronger than r1 race
// ---------------------------------------------------------------------------

// TestAdversaryT07b_RecordCallEvidenceIgnoresTerminalRestartStatus: even without
// a concurrent race, recordCallEvidence must refuse to finalize SUCCEEDED when
// store already holds restart UNKNOWN terminal reason.
func TestAdversaryT07b_RecordCallEvidenceIgnoresTerminalRestartStatus(t *testing.T) {
	store := NewInMemoryCallEvidenceStore()
	defer func() { _ = store.Close() }()
	router := NewRouter(NewManager(), nil, nil, nil)

	corr := "corr-term-guard"
	_ = store.RecordCall(MCPCallRecord{
		CorrelationID: corr,
		WorkflowID:    "wf",
		Tool:          "t",
		InputDigest:   "d",
		Status:        CallStatusUnknown,
		Reason:        "daemon restart: call outcome unknown",
		StartedAt:     time.Now().UTC(),
		FinishedAt:    time.Now().UTC(),
	})
	// Ensure not inFlight (restart already finished it).
	store.mu.Lock()
	delete(store.inFlight, corr)
	store.mu.Unlock()

	router.recordCallEvidence(store, corr, CallStatusSucceeded, "", time.Now().UTC().Add(-time.Second), "late-digest")
	final, _ := store.GetCall(corr)
	if final.Status == CallStatusSucceeded {
		// ADVERSARY BREAK: HIGH - recordCallEvidence overwrote terminal restart UNKNOWN with SUCCEEDED
		t.Fatalf("ADVERSARY BREAK: HIGH - recordCallEvidence overwrote restart terminal UNKNOWN with SUCCEEDED digest=%q", final.OutputDigest)
	}
}

// TestAdversaryT07b_StopDoesNotCancelInFlightCalls mirrors cleanup cancel gap on Stop.
func TestAdversaryT07b_StopDoesNotCancelInFlightCalls(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://x", "cap", []string{"t"})
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := reg.RegisterCall("wf-1", "svc-1", cancel); err != nil {
		t.Fatalf("RegisterCall: %v", err)
	}

	if err := reg.Stop(context.Background(), "wf-1", "svc-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-ctx.Done():
	default:
		// ADVERSARY BREAK: HIGH - Stop left in-flight call contexts live (only Fence cancels)
		t.Fatalf("ADVERSARY BREAK: HIGH - Stop did not cancel in-flight MCP calls")
	}
}

// ---------------------------------------------------------------------------
// Homoglyph / alternate secret forms past health redaction (MEDIUM)
// ---------------------------------------------------------------------------

// TestAdversaryT07b_HomoglyphSecretBypassesHealthRedaction: fullwidth or
// zero-width split patterns may bypass naive Index checks used in sanitize.
func TestAdversaryT07b_HomoglyphSecretBypassesHealthRedaction(t *testing.T) {
	reg := newTestRegistry()
	reg.instances["wf-1/svc-1"] = TestServiceInstance("wf-1", "svc-1", StateReady, "http://x", "c", nil)

	// Zero-width joiner inside sk- prefix split.
	zw := "sk\u200b-live-EVILSECRETVALUE999"
	// Fullwidth letters that normalize to sk- in some UIs.
	fw := "ｓｋ-live-EVILFULLWIDTH0001"

	for _, secret := range []string{zw, fw, "ASIAIOSFODNN7EXAMPLE"} {
		reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeTimeout, "upstream "+secret, "t")
	}
	sum := reg.HealthSummary("wf-1", "svc-1")
	if sum == nil || len(sum.Services) != 1 {
		t.Fatalf("summary=%+v", sum)
	}
	for _, f := range sum.Services[0].RecentFailures {
		if strings.Contains(f.Reason, "EVILSECRETVALUE999") || strings.Contains(f.Reason, "EVILFULLWIDTH") {
			// ADVERSARY BREAK: MEDIUM - zwj/homoglyph secret survived health redaction
			t.Fatalf("ADVERSARY BREAK: MEDIUM - homoglyph/zw secret in health reason: %q", f.Reason)
		}
		if strings.Contains(f.Reason, "ASIAIOSFODNN7EXAMPLE") {
			// ADVERSARY BREAK: MEDIUM - ASIA key in health (also r1 sanitize list gap)
			t.Fatalf("ADVERSARY BREAK: MEDIUM - ASIA key not redacted in health: %q", f.Reason)
		}
	}
}

// TestAdversaryT07b_ConcurrentCleanupAndRegisterCallNoDeadlock stresses lock
// order cleanup vs RegisterCall under -race.
func TestAdversaryT07b_ConcurrentCleanupAndRegisterCallNoDeadlock(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://x", "cap", []string{"t"})
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	var wg sync.WaitGroup
	var panics atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() {
				if recover() != nil {
					panics.Add(1)
				}
			}()
			_, _ = reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if recover() != nil {
					panics.Add(1)
				}
			}()
			_, _ = reg.RegisterCall("wf-1", "svc-1", func() {})
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// ADVERSARY BREAK: HIGH - deadlock between CleanupServiceResources and RegisterCall
		t.Fatalf("ADVERSARY BREAK: HIGH - deadlock Cleanup vs RegisterCall")
	}
	if panics.Load() > 0 {
		// ADVERSARY BREAK: HIGH - panic under concurrent cleanup/register
		t.Fatalf("ADVERSARY BREAK: HIGH - panics=%d under concurrent cleanup/register", panics.Load())
	}
}

// TestAdversaryT07b_InFlightTrackerCompleteThenSnapshotRace ensures Snapshot
// never returns a call after Complete under race (memory consistency).
func TestAdversaryT07b_InFlightTrackerCompleteThenSnapshotRace(t *testing.T) {
	tr := NewInFlightCallTracker()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := NewCorrelationID()
			tr.Register(id, "wf", "b", "t")
			tr.Complete(id)
		}(i)
	}
	wg.Wait()
	if tr.Active() != 0 {
		// ADVERSARY BREAK: MEDIUM - tracker leaked completed calls
		t.Fatalf("ADVERSARY BREAK: MEDIUM - InFlightCallTracker Active=%d after complete", tr.Active())
	}
	if snap := tr.Snapshot(); len(snap) != 0 {
		t.Fatalf("ADVERSARY BREAK: MEDIUM - Snapshot non-empty after drains: %d", len(snap))
	}
}

// silence unused import if errors unused in some builds
var _ = errors.New
