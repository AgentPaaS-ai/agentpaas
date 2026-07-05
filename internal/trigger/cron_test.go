package trigger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

type fakeCronAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (f *fakeCronAudit) Append(record audit.AuditRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

func (f *fakeCronAudit) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, 0, len(f.records))
	for _, record := range f.records {
		types = append(types, record.EventType)
	}
	return types
}

func TestParseCronValidExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
	}{
		{name: "wildcard", expr: "* * * * *"},
		{name: "daily", expr: "0 9 * * *"},
		{name: "step", expr: "*/5 * * * *"},
		{name: "range", expr: "0-15 * * * *"},
		{name: "list", expr: "1,3,5 * * * *"},
		{name: "range step", expr: "0-15/2 * * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseCron(tt.expr); err != nil {
				t.Fatalf("ParseCron(%q) returned error: %v", tt.expr, err)
			}
		})
	}
}

func TestParseCronInvalidExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
	}{
		{name: "four fields", expr: "* * * *"},
		{name: "six fields", expr: "* * * * * *"},
		{name: "out of range", expr: "60 * * * *"},
		{name: "invalid step", expr: "*/0 * * * *"},
		{name: "invalid range", expr: "10-1 * * * *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseCron(tt.expr); err == nil {
				t.Fatalf("ParseCron(%q) returned nil error", tt.expr)
			}
		})
	}
}

func TestCronExprMatch(t *testing.T) {
	t.Parallel()

	expr, err := ParseCron("30 9 15 6 1")
	if err != nil {
		t.Fatalf("ParseCron returned error: %v", err)
	}

	matching := time.Date(2026, time.June, 15, 9, 30, 0, 0, time.UTC)
	if !expr.Match(matching) {
		t.Fatalf("expected %s to match", matching)
	}

	nonMatching := time.Date(2026, time.June, 15, 9, 31, 0, 0, time.UTC)
	if expr.Match(nonMatching) {
		t.Fatalf("expected %s not to match", nonMatching)
	}
}

func TestCronFieldStepListAndRangeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		matches []int
		misses  []int
	}{
		{name: "step five", expr: "*/5 * * * *", matches: []int{0, 5, 55}, misses: []int{1, 56}},
		{name: "step fifteen", expr: "*/15 * * * *", matches: []int{0, 15, 45}, misses: []int{14, 46}},
		{name: "list", expr: "1,3,5 * * * *", matches: []int{1, 3, 5}, misses: []int{2, 6}},
		{name: "range", expr: "0-15 * * * *", matches: []int{0, 10, 15}, misses: []int{16, 59}},
		{name: "range step", expr: "0-15/2 * * * *", matches: []int{0, 2, 14}, misses: []int{1, 15}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseCron(tt.expr)
			if err != nil {
				t.Fatalf("ParseCron returned error: %v", err)
			}
			for _, minute := range tt.matches {
				if !expr.Minute.Match(minute) {
					t.Fatalf("expected minute %d to match %q", minute, tt.expr)
				}
			}
			for _, minute := range tt.misses {
				if expr.Minute.Match(minute) {
					t.Fatalf("expected minute %d not to match %q", minute, tt.expr)
				}
			}
		})
	}
}

func TestCronSchedulerSkipsDSTNonexistentLocalTime(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation returned error: %v", err)
	}
	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "30 2 * * *",
			AgentName: "spring-agent",
			Timezone:  loc.String(),
		}},
	})
	var invocations int
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invocations++
		return &triggerv1.InvokeResponse{}, nil
	}

	cs.tick(time.Date(2026, time.March, 8, 6, 30, 0, 0, time.UTC))
	cs.tick(time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC))

	if invocations != 0 {
		t.Fatalf("expected nonexistent 02:30 local time to be skipped, got %d invocations", invocations)
	}
}

func TestCronSchedulerRunsDSTRepeatedLocalTimeOnce(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation returned error: %v", err)
	}
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:      "30 1 * * *",
			AgentName: "fall-agent",
			Timezone:  loc.String(),
		}},
	})
	invoked := make(chan struct{}, 2)
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked <- struct{}{}
		return &triggerv1.InvokeResponse{}, nil
	}

	cs.tick(time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC))
	waitForInvokes(t, invoked, 1)
	cs.tick(time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC))

	select {
	case <-invoked:
		t.Fatal("expected repeated 01:30 local time to run once")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCronSchedulerForbidConcurrencySkipsAndAudits(t *testing.T) {
	t.Parallel()

	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "busy-agent",
		}},
	})
	cs.activeRuns["busy-agent"] = true

	cs.fire(cs.schedules[0], time.Date(2026, time.June, 22, 9, 0, 0, 0, time.UTC))

	types := auditLog.eventTypes()
	if len(types) != 1 || types[0] != "cron_skipped_concurrency" {
		t.Fatalf("expected cron_skipped_concurrency audit, got %#v", types)
	}
}

func TestCronSchedulerDefaultPolicies(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{Expr: "* * * * *", AgentName: "policy-agent"}},
	})

	if got := cs.schedules[0].MissedRunPolicy; got != "skip" {
		t.Fatalf("expected default missed-run policy skip, got %q", got)
	}
	if got := cs.schedules[0].ConcurrencyPolicy; got != "forbid" {
		t.Fatalf("expected default concurrency policy forbid, got %q", got)
	}
}

func TestCronSchedulerCronMissedAuditOnInvokeError(t *testing.T) {
	t.Parallel()

	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "error-agent",
		}},
	})
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		return nil, errors.New("boom")
	}

	cs.fire(cs.schedules[0], time.Date(2026, time.June, 22, 9, 0, 0, 0, time.UTC))

	deadline := time.After(500 * time.Millisecond)
	for {
		if types := auditLog.eventTypes(); len(types) == 1 && types[0] == "cron_missed" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected cron_missed audit, got %#v", auditLog.eventTypes())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestCronSchedulerUsesCronCallerID(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "caller-agent",
		}},
	})
	gotCaller := make(chan CallerID, 1)
	cs.invoke = func(ctx context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		caller, ok := CallerFromContext(ctx)
		if !ok {
			t.Fatal("expected caller in context")
		}
		gotCaller <- caller
		return &triggerv1.InvokeResponse{}, nil
	}

	cs.fire(cs.schedules[0], time.Date(2026, time.June, 22, 9, 0, 0, 0, time.UTC))

	select {
	case caller := <-gotCaller:
		if caller != CallerID("system:cron:caller-agent") {
			t.Fatalf("unexpected caller ID %q", caller)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for invoke")
	}
}

func TestCronSchedulerFiresAtCorrectMinuteAndPreventsDuplicates(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:      "15 9 * * *",
			AgentName: "minute-agent",
		}},
	})
	invoked := make(chan struct{}, 2)
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked <- struct{}{}
		return &triggerv1.InvokeResponse{}, nil
	}

	cs.tick(time.Date(2026, time.June, 22, 9, 14, 0, 0, time.Local))
	assertNoInvoke(t, invoked)

	cs.tick(time.Date(2026, time.June, 22, 9, 15, 0, 0, time.Local))
	waitForInvokes(t, invoked, 1)

	cs.tick(time.Date(2026, time.June, 22, 9, 15, 30, 0, time.Local))
	assertNoInvoke(t, invoked)
}

func TestCronSchedulerInvokesMultipleSchedulesForDifferentAgents(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{
			{Expr: "* * * * *", AgentName: "agent-a"},
			{Expr: "* * * * *", AgentName: "agent-b"},
		},
	})
	gotAgents := make(chan string, 2)
	cs.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		gotAgents <- req.GetAgentName()
		return &triggerv1.InvokeResponse{}, nil
	}

	cs.tick(time.Date(2026, time.June, 22, 9, 0, 0, 0, time.UTC))

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case agent := <-gotAgents:
			seen[agent] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for agent invocations, saw %#v", seen)
		}
	}
	if !seen["agent-a"] || !seen["agent-b"] {
		t.Fatalf("expected both agents to be invoked, got %#v", seen)
	}
}

func assertNoInvoke(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("unexpected invoke")
	case <-time.After(50 * time.Millisecond):
	}
}

func waitForInvokes(t *testing.T, ch <-chan struct{}, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-ch:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for invoke %d", i+1)
		}
	}
}
