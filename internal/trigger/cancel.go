package trigger

import (
	"context"
	"fmt"
	"sync"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	EventCancelRequested = "cancel_requested"
	EventCancelGraceful  = "cancel_graceful"
	EventCancelForced    = "cancel_forced"
	EventCancelTimeout   = "cancel_timeout"
)

const CancelGracePeriod = 30 * time.Second

type RunEntry struct {
	RunID      string
	AgentName  string
	Status     triggerv1.RunStatus
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	CancelCtx  context.CancelFunc

	cancelRequested       bool
	cancelForced          bool
	cancelGracefulAudited bool
	mu                    sync.Mutex
}

type RunStore struct {
	mu   sync.Mutex
	runs map[string]*RunEntry
}

func NewRunStore() *RunStore {
	return &RunStore{runs: make(map[string]*RunEntry)}
}

func (rs *RunStore) Register(runID, agentName string) *RunEntry {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if entry, ok := rs.runs[runID]; ok {
		return entry
	}
	entry := &RunEntry{
		RunID:     runID,
		AgentName: agentName,
		Status:    triggerv1.RunStatus_RUN_STATUS_PENDING,
		CreatedAt: time.Now().UTC(),
	}
	rs.runs[runID] = entry
	return entry
}

func (rs *RunStore) Get(runID string) (*RunEntry, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	entry, ok := rs.runs[runID]
	return entry, ok
}

func (rs *RunStore) List() []*RunEntry {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	entries := make([]*RunEntry, 0, len(rs.runs))
	for _, entry := range rs.runs {
		entries = append(entries, entry)
	}
	return entries
}

func (rs *RunStore) MarkStarted(runID string) {
	entry, ok := rs.Get(runID)
	if !ok {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_PENDING {
		return
	}
	entry.Status = triggerv1.RunStatus_RUN_STATUS_RUNNING
	entry.StartedAt = time.Now().UTC()
}

func (rs *RunStore) MarkFinished(runID string, runStatus triggerv1.RunStatus) {
	entry, ok := rs.Get(runID)
	if !ok {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.Status == triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		return
	}
	entry.Status = runStatus
	entry.FinishedAt = time.Now().UTC()
}

func (rs *RunStore) SetCancelFunc(runID string, cancel context.CancelFunc) {
	entry, ok := rs.Get(runID)
	if !ok {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.CancelCtx = cancel
}

func (s *TriggerService) CancelRun(ctx context.Context, req *triggerv1.CancelRunRequest) (*triggerv1.Run, error) {
	if req.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	caller, _ := CallerFromContext(ctx)
	if err := s.appendCancelAudit(EventCancelRequested, caller, map[string]interface{}{
		"run_id": req.GetRunId(),
		"reason": req.GetReason(),
	}); err != nil {
		return nil, err
	}

	entry, ok := s.runStore.Get(req.GetRunId())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "run %s not found", req.GetRunId())
	}

	entry.mu.Lock()
	runStatus := entry.Status
	switch {
	case runStatus == triggerv1.RunStatus_RUN_STATUS_CANCELLED:
		entry.mu.Unlock()
		return entry.toRun(), nil
	case isTerminalRunStatus(runStatus):
		entry.mu.Unlock()
		return nil, status.Errorf(codes.FailedPrecondition, "run %s already terminal: %s", req.GetRunId(), runStatus.String())
	case runStatus == triggerv1.RunStatus_RUN_STATUS_PENDING:
		entry.Status = triggerv1.RunStatus_RUN_STATUS_CANCELLED
		entry.FinishedAt = time.Now().UTC()
		entry.cancelGracefulAudited = true
		entry.mu.Unlock()

		s.eventBus.Publish(req.GetRunId(), EventRunCancelled, map[string]string{"reason": req.GetReason()})
		if err := s.appendCancelAudit(EventCancelGraceful, caller, map[string]interface{}{
			"run_id": req.GetRunId(),
			"method": "immediate_pending",
		}); err != nil {
			return nil, err
		}
		return entry.toRun(), nil
	case runStatus != triggerv1.RunStatus_RUN_STATUS_RUNNING:
		entry.mu.Unlock()
		return nil, status.Errorf(codes.FailedPrecondition, "run %s cannot be cancelled from status: %s", req.GetRunId(), runStatus.String())
	}

	cancel := entry.CancelCtx
	firstRequest := !entry.cancelRequested
	entry.cancelRequested = true
	entry.mu.Unlock()

	if firstRequest && cancel != nil {
		cancel()
	}

	timer := time.NewTimer(s.cancelGracePeriod)
	defer timer.Stop()
	ticker := time.NewTicker(cancelPollInterval(s.cancelGracePeriod))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			if s.forceCancel(entry) {
				s.eventBus.Publish(req.GetRunId(), EventRunCancelled, map[string]string{"reason": "forced: " + req.GetReason()})
				if err := s.appendCancelAudit(EventCancelTimeout, caller, map[string]interface{}{
					"run_id":  req.GetRunId(),
					"timeout": s.cancelGracePeriod.String(),
				}); err != nil {
					return nil, err
				}
				if err := s.appendCancelAudit(EventCancelForced, caller, map[string]interface{}{
					"run_id": req.GetRunId(),
				}); err != nil {
					return nil, err
				}
			}
			return entry.toRun(), nil
		case <-ticker.C:
			graceful, done := entry.markGracefulCancelAudited()
			if !done {
				continue
			}
			if graceful {
				if err := s.appendCancelAudit(EventCancelGraceful, caller, map[string]interface{}{
					"run_id": req.GetRunId(),
					"method": "graceful",
				}); err != nil {
					return nil, err
				}
			}
			return entry.toRun(), nil
		}
	}
}

func (s *TriggerService) appendCancelAudit(eventType string, caller CallerID, payload map[string]interface{}) error {
	if s.audit == nil {
		return nil
	}
	if err := s.audit.Append(audit.AuditRecord{
		EventType: eventType,
		Actor:     string(caller),
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return status.Errorf(codes.Internal, "append audit %s: %v", eventType, err)
	}
	return nil
}

func (s *TriggerService) forceCancel(entry *RunEntry) bool {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.Status == triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		return false
	}
	entry.Status = triggerv1.RunStatus_RUN_STATUS_CANCELLED
	entry.FinishedAt = time.Now().UTC()
	entry.cancelForced = true
	return true
}

func (e *RunEntry) markGracefulCancelAudited() (bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.Status != triggerv1.RunStatus_RUN_STATUS_CANCELLED {
		return false, false
	}
	if e.cancelForced || e.cancelGracefulAudited {
		return false, true
	}
	e.cancelGracefulAudited = true
	return true, true
}

func (e *RunEntry) toRun() *triggerv1.Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	run := &triggerv1.Run{
		RunId:     e.RunID,
		AgentName: e.AgentName,
		Status:    e.Status,
	}
	if !e.CreatedAt.IsZero() {
		run.CreatedAt = timestamppb.New(e.CreatedAt)
	}
	if !e.StartedAt.IsZero() {
		run.StartedAt = timestamppb.New(e.StartedAt)
	}
	if !e.FinishedAt.IsZero() {
		run.FinishedAt = timestamppb.New(e.FinishedAt)
	}
	return run
}

func isTerminalRunStatus(runStatus triggerv1.RunStatus) bool {
	return runStatus == triggerv1.RunStatus_RUN_STATUS_SUCCEEDED ||
		runStatus == triggerv1.RunStatus_RUN_STATUS_FAILED ||
		runStatus == triggerv1.RunStatus_RUN_STATUS_BUDGET_EXCEEDED
}

func cancelPollInterval(gracePeriod time.Duration) time.Duration {
	interval := gracePeriod / 10
	if interval < time.Millisecond {
		return time.Millisecond
	}
	if interval > 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return interval
}

func runSortKey(entry *RunEntry) string {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return fmt.Sprintf("%s/%s", entry.CreatedAt.Format(time.RFC3339Nano), entry.RunID)
}
