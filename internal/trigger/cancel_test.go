package trigger

import (
	"context"
	"sync"
	"testing"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cancelTestAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (a *cancelTestAudit) Append(record audit.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return nil
}

func (a *cancelTestAudit) eventTypes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	eventTypes := make([]string, 0, len(a.records))
	for _, record := range a.records {
		eventTypes = append(eventTypes, record.EventType)
	}
	return eventTypes
}

func newCancelTestService() (*TriggerService, *cancelTestAudit, *EventBus) {
	auditLog := &cancelTestAudit{}
	bus := NewEventBus()
	service := NewTriggerService(auditLog, DefaultMaxPayload, bus)
	service.cancelGracePeriod = 20 * time.Millisecond
	return service, auditLog, bus
}

func TestCancelRun_Pending(t *testing.T) {
	service, _, _ := newCancelTestService()
	service.runStore.Register("run-pending", "agent")

	run, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{
		RunId:  "run-pending",
		Reason: "user requested",
	})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("CancelRun() status = %s, want CANCELLED", run.GetStatus())
	}
	if run.GetFinishedAt() == nil {
		t.Fatal("CancelRun() FinishedAt is nil")
	}
}

func TestCancelRun_Running_Graceful(t *testing.T) {
	service, auditLog, _ := newCancelTestService()
	service.runStore.Register("run-running", "agent")
	service.runStore.MarkStarted("run-running")
	runCtx, cancel := context.WithCancel(context.Background())
	service.runStore.SetCancelFunc("run-running", cancel)
	defer cancel()
	go func() {
		<-runCtx.Done()
		service.runStore.MarkFinished("run-running", triggerv1.RunStatus_RUN_STATUS_CANCELLED)
	}()

	run, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{
		RunId:  "run-running",
		Reason: "user requested",
	})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("CancelRun() status = %s, want CANCELLED", run.GetStatus())
	}
	assertAuditContains(t, auditLog, EventCancelGraceful)
}

func TestCancelRun_Running_Forced(t *testing.T) {
	service, auditLog, _ := newCancelTestService()
	service.runStore.Register("run-forced", "agent")
	service.runStore.MarkStarted("run-forced")

	run, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{
		RunId:  "run-forced",
		Reason: "user requested",
	})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("CancelRun() status = %s, want CANCELLED", run.GetStatus())
	}
	assertAuditContains(t, auditLog, EventCancelTimeout)
	assertAuditContains(t, auditLog, EventCancelForced)
}

func TestCancelRun_AlreadyCancelled(t *testing.T) {
	service, _, _ := newCancelTestService()
	service.runStore.Register("run-cancelled", "agent")

	first, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-cancelled"})
	if err != nil {
		t.Fatalf("first CancelRun() error = %v", err)
	}
	second, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-cancelled"})
	if err != nil {
		t.Fatalf("second CancelRun() error = %v", err)
	}
	if second.GetRunId() != first.GetRunId() || second.GetStatus() != first.GetStatus() {
		t.Fatalf("second CancelRun() = (%q, %s), want (%q, %s)", second.GetRunId(), second.GetStatus(), first.GetRunId(), first.GetStatus())
	}
}

func TestCancelRun_Terminal(t *testing.T) {
	service, _, _ := newCancelTestService()
	service.runStore.Register("run-terminal", "agent")
	service.runStore.MarkFinished("run-terminal", triggerv1.RunStatus_RUN_STATUS_SUCCEEDED)

	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-terminal"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CancelRun() code = %s, want FailedPrecondition", status.Code(err))
	}
}

func TestCancelRun_NotFound(t *testing.T) {
	service, _, _ := newCancelTestService()

	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("CancelRun() code = %s, want NotFound", status.Code(err))
	}
}

func TestCancelRun_EmptyRunID(t *testing.T) {
	service, _, _ := newCancelTestService()

	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CancelRun() code = %s, want InvalidArgument", status.Code(err))
	}
}

func TestCancelRun_AuditEvents(t *testing.T) {
	service, auditLog, _ := newCancelTestService()
	service.runStore.Register("run-audit", "agent")

	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{
		RunId:  "run-audit",
		Reason: "audit",
	})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	assertAuditContains(t, auditLog, EventCancelRequested)
	assertAuditContains(t, auditLog, EventCancelGraceful)
}

func TestCancelRun_EventBusPublish(t *testing.T) {
	service, _, bus := newCancelTestService()
	service.runStore.Register("run-event", "agent")

	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{
		RunId:  "run-event",
		Reason: "event",
	})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	events := bus.GetEvents("run-event")
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Type != EventRunCancelled {
		t.Fatalf("event type = %s, want %s", events[0].Type, EventRunCancelled)
	}
}

func TestGetRun_Cancelled(t *testing.T) {
	service, _, _ := newCancelTestService()
	service.runStore.Register("run-get", "agent")
	_, err := service.CancelRun(context.Background(), &triggerv1.CancelRunRequest{RunId: "run-get"})
	if err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	run, err := service.GetRun(context.Background(), &triggerv1.GetRunRequest{RunId: "run-get"})
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if run.GetStatus() != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("GetRun() status = %s, want CANCELLED", run.GetStatus())
	}
}

func assertAuditContains(t *testing.T, auditLog *cancelTestAudit, eventType string) {
	t.Helper()
	for _, got := range auditLog.eventTypes() {
		if got == eventType {
			return
		}
	}
	t.Fatalf("audit events = %v, want %s", auditLog.eventTypes(), eventType)
}
