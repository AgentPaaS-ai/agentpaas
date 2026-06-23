//go:build adversary

package trigger

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type adversaryAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (a *adversaryAudit) Append(record audit.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return nil
}

func (a *adversaryAudit) hasEvent(ev string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.records {
		if r.EventType == ev {
			return true
		}
	}
	return false
}

func (a *adversaryAudit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.records)
}

func newAdversaryService() (*TriggerService, *adversaryAudit, *EventBus) {
	auditLog := &adversaryAudit{}
	bus := NewEventBus()
	service := NewTriggerService(auditLog, DefaultMaxPayload, bus)
	service.cancelGracePeriod = 20 * time.Millisecond
	return service, auditLog, bus
}

func TestAdversaryB9T08_IdempotentCancel_DoubleTriple(t *testing.T) {
	service, _, _ := newAdversaryService()
	service.runStore.Register("run-idem", "agent")

	for i := 0; i < 3; i++ {
		run, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-idem"})
		if err != nil {
			t.Fatalf("CancelRun %d error = %v", i, err)
		}
		if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
			t.Fatalf("status after %d = %s", i, run.GetStatus())
		}
	}
	t.Log("Tested: idempotent double/triple cancel on pending (good)")
}

func TestAdversaryB9T08_TerminalRejection_AllStatuses(t *testing.T) {
	terminals := []triggerv1.RunStatus{
		triggerv1.RunStatus_RUN_STATUS_SUCCEEDED,
		triggerv1.RunStatus_RUN_STATUS_FAILED,
		triggerv1.RunStatus_RUN_STATUS_BUDGET_EXCEEDED,
	}
	for _, st := range terminals {
		service, _, _ := newAdversaryService()
		rid := "run-term-" + st.String()
		service.runStore.Register(rid, "agent")
		service.runStore.MarkFinished(rid, st)
		_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: rid})
		if status.Code(err) != codes.FailedPrecondition {
			t.Errorf("SECURITY BREAK: terminal %s did not return FailedPrecondition: %v", st, err)
			t.Fail()
		}
	}
	t.Log("Tested: terminal rejection for SUCCEEDED/FAILED/BUDGET_EXCEEDED (good)")
}

func TestAdversaryB9T08_PendingImmediateNoWait(t *testing.T) {
	service, _, _ := newAdversaryService()
	service.runStore.Register("run-pend", "agent")
	start := time.Now()
	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-pend"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("SECURITY BREAK: pending cancel took >100ms, possible wait")
		t.Fail()
	}
	t.Log("Tested: pending immediate cancel (no 30s wait) (good)")
}

func TestAdversaryB9T08_GracePeriodEnforced_AndForced(t *testing.T) {
	service, auditLog, _ := newAdversaryService()
	service.runStore.Register("run-grace", "agent")
	service.runStore.MarkStarted("run-grace")
	// No cancel func, so graceful never happens, forces after timeout
	start := time.Now()
	run, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-grace"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 15*time.Millisecond || elapsed > 100*time.Millisecond {
		t.Errorf("SECURITY BREAK: grace period not enforced, elapsed=%v", elapsed)
		t.Fail()
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("status=%s", run.GetStatus())
	}
	if !auditLog.hasEvent(EventCancelTimeout) || !auditLog.hasEvent(EventCancelForced) {
		t.Errorf("SECURITY BREAK: missing timeout/forced audit events")
		t.Fail()
	}
	t.Log("Tested: graceful period enforcement + forced cancel after timeout (good)")
}

func TestAdversaryB9T08_ForcedCancelAuditEvent(t *testing.T) {
	service, auditLog, _ := newAdversaryService()
	service.runStore.Register("run-force", "agent")
	service.runStore.MarkStarted("run-force")
	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-force", Reason: "force test"})
	if err != nil {
		t.Fatalf("error=%v", err)
	}
	if !auditLog.hasEvent(EventCancelForced) {
		t.Errorf("SECURITY BREAK: EventCancelForced not emitted")
		t.Fail()
	}
	t.Log("Tested: EventCancelForced emitted on forced path (good)")
}

func TestAdversaryB9T08_EmptyWhitespaceLongRunID(t *testing.T) {
	service, _, _ := newAdversaryService()
	cases := []string{"", "   ", strings.Repeat("x", 1000)}
	for _, rid := range cases {
		_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: rid})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("SECURITY BREAK: run_id=%q did not return InvalidArgument: %v", rid, err)
			t.Fail()
		}
	}
	t.Log("Tested: empty/whitespace/long run_id -> InvalidArgument (good)")
}

func TestAdversaryB9T08_UnknownRunID_NotFound(t *testing.T) {
	service, _, _ := newAdversaryService()
	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "does-not-exist"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("SECURITY BREAK: unknown run_id did not return NotFound: %v", err)
		t.Fail()
	}
	t.Log("Tested: unknown run_id -> NotFound (good)")
}

func TestAdversaryB9T08_AuditCompleteness_AllPaths(t *testing.T) {
	// Pending path
	service, auditLog, _ := newAdversaryService()
	service.runStore.Register("run-audit-pend", "agent")
	_, _ = service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-audit-pend", Reason: "a"})
	if !auditLog.hasEvent(EventCancelRequested) || !auditLog.hasEvent(EventCancelGraceful) {
		t.Errorf("SECURITY BREAK: pending missing requested/graceful")
		t.Fail()
	}

	// Running forced path (short grace)
	service2, auditLog2, _ := newAdversaryService()
	service2.runStore.Register("run-audit-force", "agent")
	service2.runStore.MarkStarted("run-audit-force")
	_, _ = service2.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-audit-force"})
	if !auditLog2.hasEvent(EventCancelRequested) || !auditLog2.hasEvent(EventCancelForced) {
		t.Errorf("SECURITY BREAK: forced missing requested/forced")
		t.Fail()
	}
	t.Log("Tested: audit completeness (requested + graceful/forced) on all paths (good)")
}

func TestAdversaryB9T08_EventBusPublish(t *testing.T) {
	service, _, bus := newAdversaryService()
	service.runStore.Register("run-bus", "agent")
	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-bus"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	evs := bus.GetEvents("run-bus")
	if len(evs) != 1 || evs[0].Type != EventRunCancelled {
		t.Errorf("SECURITY BREAK: EventRunCancelled not published")
		t.Fail()
	}
	t.Log("Tested: EventBus EventRunCancelled published (good)")
}

func TestAdversaryB9T08_GetRunAfterCancel(t *testing.T) {
	service, _, _ := newAdversaryService()
	service.runStore.Register("run-get", "agent")
	_, _ = service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-get"})
	run, err := service.GetRun(context.Background(), &triggerv1.GetRunRequest{RunId: "run-get"})
	if err != nil || run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED || run.GetFinishedAt() == nil {
		t.Errorf("SECURITY BREAK: GetRun after cancel incorrect: err=%v status=%s", err, run.GetStatus())
		t.Fail()
	}
	t.Log("Tested: GetRun returns correct cancelled run+timestamps (good)")
}

func TestAdversaryB9T08_ConcurrentCancel_Race(t *testing.T) {
	service, _, _ := newAdversaryService()
	service.runStore.Register("run-conc", "agent")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-conc"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Errorf("SECURITY BREAK: concurrent cancel error: %v", e)
			t.Fail()
		}
	}
	// Verify final state
	run, _ := service.GetRun(context.Background(), &triggerv1.GetRunRequest{RunId: "run-conc"})
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Errorf("SECURITY BREAK: concurrent did not end CANCELLED")
		t.Fail()
	}
	t.Log("Tested: concurrent cancel (idempotent, no race/panic) (good)")
}

func TestAdversaryB9T08_ContextCancellation_DuringWait(t *testing.T) {
	service, _, _ := newAdversaryService()
	service.runStore.Register("run-ctx", "agent")
	service.runStore.MarkStarted("run-ctx")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := service.CancelRun(ctx, &triggerv1.CancelRunRequest{RunId: "run-ctx"})
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Errorf("SECURITY BREAK: context cancel during grace did not propagate: %v", err)
		t.Fail()
	}
	t.Log("Tested: context cancellation during 30s wait returns ctx err (good)")
}