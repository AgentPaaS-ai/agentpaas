package harness

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

func TestWallClockBudgetKillsFromInvokeStartAndAuditsOverage(t *testing.T) {
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{
		Audit:          recorder,
		TerminateGrace: 100 * time.Millisecond,
	}, `import time

def invoke(payload):
    time.sleep(30)
    return {}
`)

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-wall","invoke_id":"invoke-wall","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal invoke response: %v", err)
	}
	if got.Status != StatusBudgetExceeded || got.Reason != "wall_clock_budget_exceeded" {
		t.Fatalf("invoke response = %#v, want wall-clock budget exceeded", got)
	}
	if elapsed < time.Second || elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want death within 1s +/- 1s from invoke start", elapsed)
	}

	event := recorder.lastEvent(t)
	assertBudgetEvent(t, event, wallClockBudgetCategory, int64(1000), "run-wall")
	observed, ok := event.Payload["observed"].(int64)
	if !ok {
		t.Fatalf("observed payload type = %T, want int64", event.Payload["observed"])
	}
	if observed < 1000 {
		t.Fatalf("observed = %d, want post-hoc observed >= limit", observed)
	}
	overage, ok := event.Payload["overage_ms"].(int64)
	if !ok {
		t.Fatalf("overage_ms payload type = %T, want int64", event.Payload["overage_ms"])
	}
	if overage != observed-1000 {
		t.Fatalf("overage_ms = %d, want %d", overage, observed-1000)
	}
}

func TestTokenBudgetHookBlocksFutureBilledOperationsAndAudits(t *testing.T) {
	recorder := &recordingAuditAppender{}
	enforcer := newBudgetEnforcer(BudgetConfig{MaxTokens: 10}, "run-token", "invoke-token", recorder, time.Now)
	enforcer.Start()

	if err := enforcer.RecordTokens(7); err != nil {
		t.Fatalf("record tokens below cap: %v", err)
	}
	err := enforcer.RecordTokens(4)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("record tokens over cap error = %v, want ErrBudgetExceeded", err)
	}
	err = enforcer.RecordTokens(1)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("record tokens after cap error = %v, want blocked ErrBudgetExceeded", err)
	}

	event := recorder.lastEvent(t)
	assertBudgetEvent(t, event, tokenBudgetCategory, int64(10), "run-token")
	if observed := event.Payload["observed"]; observed != int64(11) {
		t.Fatalf("observed = %#v, want 11", observed)
	}
}

func TestIterationBudgetHookBlocksFutureIterationsAndAudits(t *testing.T) {
	recorder := &recordingAuditAppender{}
	enforcer := newBudgetEnforcer(BudgetConfig{MaxIterations: 2}, "run-iteration", "invoke-iteration", recorder, time.Now)
	enforcer.Start()

	if err := enforcer.RecordIteration(); err != nil {
		t.Fatalf("record first iteration: %v", err)
	}
	if err := enforcer.RecordIteration(); err != nil {
		t.Fatalf("record second iteration: %v", err)
	}
	err := enforcer.RecordIteration()
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("record third iteration error = %v, want ErrBudgetExceeded", err)
	}
	err = enforcer.RecordIteration()
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("record iteration after cap error = %v, want blocked ErrBudgetExceeded", err)
	}

	event := recorder.lastEvent(t)
	assertBudgetEvent(t, event, iterationBudgetCategory, int64(2), "run-iteration")
	if observed := event.Payload["observed"]; observed != int64(3) {
		t.Fatalf("observed = %#v, want 3", observed)
	}
}

func TestWallClockBudgetSendsSigkillAfterGraceWhenSigtermIgnored(t *testing.T) {
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 150 * time.Millisecond}, `import signal
import time

def ignore_term(signum, frame):
    time.sleep(30)

signal.signal(signal.SIGTERM, ignore_term)

def invoke(payload):
    while True:
        time.sleep(0.05)
`)

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status = %d, want %d; body %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal invoke response: %v", err)
	}
	if got.Status != StatusBudgetExceeded {
		t.Fatalf("status = %q, want %q", got.Status, StatusBudgetExceeded)
	}
	if elapsed < time.Second+150*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want budget plus short SIGTERM grace before SIGKILL", elapsed)
	}
}

type recordingAuditAppender struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (r *recordingAuditAppender) Append(record audit.AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func (r *recordingAuditAppender) lastEvent(t *testing.T) audit.AuditRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) == 0 {
		t.Fatal("no audit records emitted")
	}
	return r.records[len(r.records)-1]
}

func assertBudgetEvent(t *testing.T, event audit.AuditRecord, category string, limit int64, runID string) {
	t.Helper()
	if event.EventType != "budget_exceeded" {
		t.Fatalf("event type = %q, want budget_exceeded", event.EventType)
	}
	if event.Payload["category"] != category {
		t.Fatalf("category = %#v, want %q", event.Payload["category"], category)
	}
	if event.Payload["limit"] != limit {
		t.Fatalf("limit = %#v, want %d", event.Payload["limit"], limit)
	}
	if event.Payload["run_id"] != runID {
		t.Fatalf("run_id = %#v, want %q", event.Payload["run_id"], runID)
	}
}

func newReadyServerWithConfig(t *testing.T, cfg Config, source string) *Server {
	t.Helper()
	cfg.AgentPath = writeAgent(t, source)
	srv := NewServer(cfg)
	t.Cleanup(func() { _ = srv.Close() })

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			return srv
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
	return nil
}
