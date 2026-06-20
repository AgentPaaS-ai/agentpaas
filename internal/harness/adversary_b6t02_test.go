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
)

func TestAdversary_B6T02_WallClockTimerEvasion(t *testing.T) {
	// Attempt: sleep during import (should be under startup timeout, not run budget)
	// or manipulate time (but use fixed now?); here test that budget starts at invoke, not import
	srv := newReadyServerWithConfig(t, Config{
		ImportTimeout: 2 * time.Second,
	}, `import time
def invoke(payload):
    time.sleep(2)
    return {}
`)
	defer func() { _ = srv.Close() }()

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-evade","invoke_id":"inv-evade","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	var got ErrorResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status == StatusBudgetExceeded {
		t.Logf("SAFE: wall clock budget enforced from invoke start, elapsed=%v", elapsed)
	} else if elapsed > 3*time.Second {
		t.Logf("// ADVERSARY BREAK: import sleep allowed evasion or budget start wrong; status=%s elapsed=%v", got.Status, elapsed)
		t.Fail()
	}
}

func TestAdversary_B6T02_IterationRaceOffByOne(t *testing.T) {
	recorder := &recordingAuditAppender{}
	enforcer := newBudgetEnforcer(BudgetConfig{MaxIterations: 1}, "run-iter", "inv-iter", recorder, time.Now)
	enforcer.Start()

	// Race: concurrent RecordIteration calls
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = enforcer.RecordIteration()
		}(i)
	}
	wg.Wait()

	exceededCount := 0
	for _, e := range errs {
		if errors.Is(e, ErrBudgetExceeded) {
			exceededCount++
		}
	}
	if exceededCount < 1 {
		t.Logf("// ADVERSARY BREAK: iteration budget race allowed exceeding cap; errs=%v", errs)
		t.Fail()
	} else {
		t.Logf("SAFE: iteration budget prevented overage under concurrency")
	}
}

func TestAdversary_B6T02_TokenNegativeOrBypass(t *testing.T) {
	enforcer := NewBudgetEnforcer(BudgetConfig{MaxTokens: 10})
	enforcer.Start()

	err := enforcer.RecordTokens(-5)
	if err == nil {
		t.Logf("// ADVERSARY BREAK: negative token count accepted without error")
		t.Fail()
	} else {
		t.Logf("SAFE: negative tokens rejected: %v", err)
	}

	// Bypass? RecordTokens is the hook; if not called, no enforcement, but caller controls
	// Test overflow path
	err = enforcer.RecordTokens(1<<60)
	if errors.Is(err, ErrBudgetExceeded) {
		t.Logf("SAFE: large token count triggers exceed")
	} else {
		t.Logf("// ADVERSARY BREAK: overflow or large count bypass? err=%v", err)
		t.Fail()
	}
}

func TestAdversary_B6T02_SIGTERMGraceAndChildSurvive(t *testing.T) {
	// Test if child process can be forked to survive parent SIGTERM (python sets RLIMIT_NPROC=0, hard to fork)
	// Check grace timer starts on wall exceed
	srv := newReadyServer(t, `import time, os, signal
def invoke(payload):
    # try to trap signals and fork (will fail due to nproc=0)
    try:
        pid = os.fork()
        if pid == 0:
            time.sleep(30)
            os._exit(0)
    except:
        pass
    time.sleep(30)
    return {}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-sig","invoke_id":"inv-sig","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	start := time.Now()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	var got ErrorResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status == StatusBudgetExceeded && elapsed < 15*time.Second {
		t.Logf("SAFE: SIGTERM grace + kill worked, no child survival visible, elapsed=%v", elapsed)
	} else {
		t.Logf("// ADVERSARY BREAK: SIGTERM trap or child survive or grace wrong; status=%s elapsed=%v", got.Status, elapsed)
		t.Fail()
	}
}

func TestAdversary_B6T02_BudgetExceededStatusAfterKill(t *testing.T) {
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{Audit: recorder, TerminateGrace: 50 * time.Millisecond}, `import time
def invoke(payload):
    time.sleep(30)
    return {}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-status","invoke_id":"inv-status","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got ErrorResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != StatusBudgetExceeded {
		t.Logf("// ADVERSARY BREAK: killed run reported non-BUDGET_EXCEEDED status %s", got.Status)
		t.Fail()
	} else {
		t.Logf("SAFE: BUDGET_EXCEEDED status correctly set on kill")
	}
}

func TestAdversary_B6T02_AuditOverageHiddenOrSpoofed(t *testing.T) {
	recorder := &recordingAuditAppender{}
	enforcer := newBudgetEnforcer(BudgetConfig{MaxTokens: 5}, "run-audit", "inv-audit", recorder, time.Now)
	enforcer.Start()
	enforcer.RecordTokens(10) // force exceed

	event := recorder.lastEvent(t)
	observed := event.Payload["observed"]
	if observed == int64(10) && event.Payload["category"] == tokenBudgetCategory {
		t.Logf("SAFE: audit shows observed >= limit, category correct")
	} else {
		t.Logf("// ADVERSARY BREAK: audit overage hidden or spoofed: observed=%v cat=%v", observed, event.Payload["category"])
		t.Fail()
	}
}

func TestAdversary_B6T02_PostHocOverageRace(t *testing.T) {
	// Wall clock Mark uses Elapsed() at kill time; check if observed reflects actual or limit
	recorder := &recordingAuditAppender{}
	srv := newReadyServerWithConfig(t, Config{Audit: recorder, TerminateGrace: 100 * time.Millisecond}, `import time
def invoke(payload):
    time.sleep(5)
    return {}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-post","invoke_id":"inv-post","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	event := recorder.lastEvent(t)
	observed, _ := event.Payload["observed"].(int64)
	limit := int64(1000)
	if observed >= limit {
		t.Logf("SAFE: post-hoc observed %d >= limit reflects kill time", observed)
	} else {
		t.Logf("// ADVERSARY BREAK: post-hoc observed < limit, race or wrong value: %d", observed)
		t.Fail()
	}
}

func TestAdversary_B6T02_StartupVsRunBudget(t *testing.T) {
	// Import time under ImportTimeout, run budget separate; test long import doesn't count to wall budget
	srv := newReadyServerWithConfig(t, Config{ImportTimeout: 3 * time.Second}, `import time
time.sleep(2)  # import sleep
def invoke(payload):
    return {}
`)
	defer func() { _ = srv.Close() }()

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-startup","invoke_id":"inv-startup","budget":{"wall_clock_seconds":1}}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed < 3*time.Second {
		t.Logf("SAFE: import time excluded from run budget (startup timeout separate)")
	} else {
		t.Logf("// ADVERSARY BREAK: import time leaked into run budget or wrong cap")
		t.Fail()
	}
}

func TestAdversary_B6T02_ConcurrencyBudgetLeak(t *testing.T) {
	// Multiple concurrent invokes; check per-run budget isolation (invokeMu serializes but budgets per-invoke)
	srv := newReadyServer(t, `import time
def invoke(payload):
    time.sleep(2)
    return {"ok": True}
`)
	defer func() { _ = srv.Close() }()

	var wg sync.WaitGroup
	results := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"run_id":"run-conc","invoke_id":"inv-conc-`+string(rune('0'+idx))+`","budget":{"wall_clock_seconds":1}}`))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			var got ErrorResponse
			json.Unmarshal(rec.Body.Bytes(), &got)
			results[idx] = got.Status
		}(i)
	}
	wg.Wait()

	if results[0] == StatusBudgetExceeded && results[1] == StatusBudgetExceeded {
		t.Logf("SAFE: per-run budgets isolated under concurrency")
	} else {
		t.Logf("// ADVERSARY BREAK: budget leak across concurrent runs: %v", results)
		t.Fail()
	}
}
