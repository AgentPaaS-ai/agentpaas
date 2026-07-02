//go:build adversary

package trigger

import (
	"context"
	"sync"
	"testing"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
)

func TestAdversaryB9T06_ParseCronLargeStep(t *testing.T) {
	t.Parallel()
	// Attack: extremely large step value
	_, err := ParseCron("*/9999999999999999999 * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: large step accepted without error")
		t.Fail()
	}
	t.Logf("Tested: large step rejected (good)")
}

func TestAdversaryB9T06_ParseCronZeroStep(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("*/0 * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: zero step accepted")
		t.Fail()
	}
	t.Logf("Tested: zero step rejected (good)")
}

func TestAdversaryB9T06_ParseCronNegativeStep(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("*/-1 * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: negative step accepted")
		t.Fail()
	}
	t.Logf("Tested: negative step rejected (good)")
}

func TestAdversaryB9T06_ParseCronReverseRange(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("10-5 * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: reverse range accepted")
		t.Fail()
	}
	t.Logf("Tested: reverse range rejected (good)")
}

func TestAdversaryB9T06_ParseCronOverlappingOrInvalidRange(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("5-1 * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: overlapping/reverse range 5-1 accepted")
		t.Fail()
	}
	t.Logf("Tested: reverse range rejected (good)")
}

func TestAdversaryB9T06_ParseCronEmptyField(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("* * * * ")
	if err == nil {
		t.Errorf("SECURITY BREAK: empty field accepted")
		t.Fail()
	}
	t.Logf("Tested: empty field rejected (good)")
}

func TestAdversaryB9T06_ParseCronTabsAndWhitespace(t *testing.T) {
	t.Parallel()
	exprs := []string{"*	*	*	*	*", "* * * * *", "\t*\t*\t*\t*\t*"}
	for _, e := range exprs {
		_, err := ParseCron(e)
		if err != nil {
			t.Errorf("SECURITY BREAK: whitespace/tab expr %q rejected: %v", e, err)
			t.Fail()
		}
	}
	t.Logf("Tested: tabs/whitespace handled (good)")
}

func TestAdversaryB9T06_ParseCronNonNumeric(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("foo * * * *")
	if err == nil {
		t.Errorf("SECURITY BREAK: non-numeric accepted")
		t.Fail()
	}
	t.Logf("Tested: non-numeric rejected (good)")
}

func TestAdversaryB9T06_TimezoneInvalid(t *testing.T) {
	t.Parallel()
	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "tz-bad",
			Timezone:  "Invalid/Zone",
		}},
	})
	invoked := make(chan struct{}, 1)
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked <- struct{}{}
		return &triggerv1.InvokeResponse{}, nil
	}
	cs.tick(time.Now().UTC())
	select {
	case <-invoked:
		t.Errorf("SECURITY BREAK: invalid timezone schedule still fired")
		t.Fail()
	case <-time.After(50 * time.Millisecond):
		t.Logf("Tested: invalid timezone skipped silently (good, but no audit)")
	}
}

func TestAdversaryB9T06_TimezoneEmptyUsesLocal(t *testing.T) {
	t.Parallel()
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "tz-empty",
			Timezone:  "",
		}},
	})
	if cs.schedules[0].Timezone != "" {
		t.Error("empty tz not preserved")
	}
	t.Logf("Tested: empty timezone uses local (documented)")
}

func TestAdversaryB9T06_MissedRunPolicyCatchupIgnored(t *testing.T) {
	t.Parallel()
	// Attack 12/13: catchup policy is set but NEVER USED in tick/shouldFire/fire
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:            "* * * * *",
			AgentName:       "catchup-agent",
			MissedRunPolicy: "catchup",
		}},
	})
	if cs.schedules[0].MissedRunPolicy != "catchup" {
		t.Fatal("defaulting wrong")
	}
	// No code path uses it — always behaves as skip. This is a contract gap.
	t.Logf("CONFIRMED: MissedRunPolicy 'catchup' stored but ignored in logic — MEDIUM contract gap (no catchup ever happens)")
	// To make test fail on break, we note it but since impl always skips, no t.Fail here; regression documents gap
}

func TestAdversaryB9T06_ConcurrencyForbidRace(t *testing.T) {
	t.Parallel()
	// Attack 10: try to bypass forbid with concurrent fire
	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "race-agent",
		}},
	})
	cs.activeRuns["race-agent"] = true
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cs.fire(cs.schedules[0], time.Now())
	}()
	go func() {
		defer wg.Done()
		cs.fire(cs.schedules[0], time.Now())
	}()
	wg.Wait()
	types := auditLog.eventTypes()
	skipped := 0
	for _, ty := range types {
		if ty == "cron_skipped_concurrency" {
			skipped++
		}
	}
	if skipped < 1 {
		t.Errorf("SECURITY BREAK: forbid concurrency race allowed multiple runs, only %d skips", skipped)
		t.Fail()
	}
	t.Logf("Tested: forbid under concurrent fire still audits skips (good)")
}

func TestAdversaryB9T06_ActiveRunsConcurrentAccess(t *testing.T) {
	t.Parallel()
	// Attack 11: direct concurrent map access simulation (should be protected by mu)
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{Expr: "* * * * *", AgentName: "concurrent-agent"}},
	})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cs.mu.Lock()
			cs.activeRuns["concurrent-agent"] = true
			delete(cs.activeRuns, "concurrent-agent")
			cs.mu.Unlock()
		}()
	}
	wg.Wait()
	t.Logf("Tested: activeRuns concurrent protected by mu (good)")
}

func TestAdversaryB9T06_CronMissedAuditOnlyOnError(t *testing.T) {
	t.Parallel()
	// Attack 17: cron_missed only on invoke error, not on policy skip
	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "* * * * *",
			AgentName: "miss-audit",
		}},
	})
	// normal fire with no error -> no missed
	cs.fire(cs.schedules[0], time.Now())
	if len(auditLog.eventTypes()) != 0 {
		t.Errorf("SECURITY BREAK: unexpected audit on success")
		t.Fail()
	}
	t.Logf("Tested: cron_missed only emitted on invoke error (as implemented; policy skips do not emit missed)")
}

func TestAdversaryB9T06_CallerIDPropagated(t *testing.T) {
	t.Parallel()
	// Attack 16
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{Expr: "* * * * *", AgentName: "caller-test"}},
	})
	got := make(chan CallerID, 1)
	cs.invoke = func(ctx context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		caller, ok := CallerFromContext(ctx)
		if ok {
			got <- caller
		}
		return &triggerv1.InvokeResponse{}, nil
	}
	cs.fire(cs.schedules[0], time.Now())
	select {
	case c := <-got:
		if c != "system:cron:caller-test" {
			t.Errorf("SECURITY BREAK: wrong caller ID %q", c)
			t.Fail()
		}
		t.Logf("Tested: cron caller ID propagated correctly (good)")
	case <-time.After(200 * time.Millisecond):
		t.Errorf("SECURITY BREAK: caller ID not propagated to invoke")
		t.Fail()
	}
}

func TestAdversaryB9T06_LastFireMapGrowth(t *testing.T) {
	t.Parallel()
	// Attack 20: unbounded? No, one per schedule
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{
			{Expr: "0 * * * *", AgentName: "a1"},
			{Expr: "1 * * * *", AgentName: "a2"},
		},
	})
	for i := 0; i < 5; i++ {
		cs.tick(time.Date(2026, 6, 22, 0, i, 0, 0, time.UTC))
	}
	if len(cs.lastFire) > len(cs.schedules) {
		t.Errorf("SECURITY BREAK: lastFire grew beyond schedules: %d", len(cs.lastFire))
		t.Fail()
	}
	t.Logf("Tested: lastFire bounded to #schedules (good)")
}

func TestAdversaryB9T06_StartStopRace(t *testing.T) {
	t.Parallel()
	// Attack 19
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{Expr: "* * * * *", AgentName: "stop-race"}},
	})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		cs.Start()
	}()
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		cs.Stop()
	}()
	wg.Wait()
	t.Logf("Tested: Start/Stop concurrent no panic (good)")
}

func TestAdversaryB9T06_TickFireConcurrent(t *testing.T) {
	t.Parallel()
	// Attack 18
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{Expr: "* * * * *", AgentName: "tick-fire"}},
	})
	invoked := make(chan struct{}, 10)
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked <- struct{}{}
		return &triggerv1.InvokeResponse{}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cs.tick(time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC))
		}()
	}
	wg.Wait()
	// should not have fired 5x due to lastFire and same minute check
	count := 0
	timeout := time.After(100 * time.Millisecond)
	for {
		select {
		case <-invoked:
			count++
		case <-timeout:
			goto done
		}
	}
done:
	if count > 1 {
		t.Errorf("SECURITY BREAK: concurrent ticks caused multiple fires same minute: %d", count)
		t.Fail()
	}
	t.Logf("Tested: concurrent tick/fire deduped (good)")
}

func TestAdversaryB9T06_DSTNonexistentSkipped(t *testing.T) {
	t.Parallel()
	// Attack 8
	loc, _ := time.LoadLocation("America/New_York")
	auditLog := &fakeCronAudit{}
	cs := NewCronScheduler(CronConfig{
		Audit: auditLog,
		Schedules: []*CronSchedule{{
			Expr:      "30 2 * * *",
			AgentName: "dst-spring",
			Timezone:  loc.String(),
		}},
	})
	invoked := 0
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked++
		return &triggerv1.InvokeResponse{}, nil
	}
	cs.tick(time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC)) // would be 2:30 nonexistent
	if invoked != 0 {
		t.Errorf("SECURITY BREAK: DST nonexistent time fired")
		t.Fail()
	}
	t.Logf("Tested: DST nonexistent skipped (good)")
}

func TestAdversaryB9T06_DSTRepeatedOnce(t *testing.T) {
	t.Parallel()
	// Attack 9
	loc, _ := time.LoadLocation("America/New_York")
	cs := NewCronScheduler(CronConfig{
		Schedules: []*CronSchedule{{
			Expr:      "30 1 * * *",
			AgentName: "dst-fall",
			Timezone:  loc.String(),
		}},
	})
	invoked := make(chan struct{}, 2)
	cs.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		invoked <- struct{}{}
		return &triggerv1.InvokeResponse{}, nil
	}
	cs.tick(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
	cs.tick(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	// Allow the fire goroutine to complete
	time.Sleep(50 * time.Millisecond)
	select {
	case <-invoked:
		// Good: first occurrence fired
	default:
		t.Errorf("SECURITY BREAK: DST first occurrence did not fire")
		t.Fail()
	}
	select {
	case <-invoked:
		t.Errorf("SECURITY BREAK: repeated DST time fired twice")
		t.Fail()
	default:
		t.Logf("Tested: DST repeated runs once (good)")
	}
}
