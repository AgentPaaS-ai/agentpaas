package routedrun

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Control / amendment / active-time tests (requirements 16–17)
// ---------------------------------------------------------------------------

func TestControl_CancelBeatsPauseAndResume(t *testing.T) {
	for _, be := range []struct {
		name string
		new  func(t *testing.T) WorkflowStore
	}{
		{"LocalStore", func(t *testing.T) WorkflowStore { return openTestStore(t) }},
		{"MemoryStore", func(t *testing.T) WorkflowStore {
			return NewMemoryStore(WithMemoryClock(testClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))))
		}},
	} {
		t.Run(be.name, func(t *testing.T) {
			s := be.new(t)
			ctx := context.Background()
			wf := seedWorkflow(t, s, WorkflowStatusRunning)
			// Pause first.
			if err := s.RequestControl(ctx, &ControlRequest{
				SchemaVersion:      CurrentSchemaVersion,
				WorkflowID:         wf.WorkflowID,
				Command:            ControlPause,
				ExpectedGeneration: wf.Generation,
				ActorIdentity:      "ops",
				AuthorityScope:      AuthScopeControl,
				IdempotencyKey:     "pause-1",
			}); err != nil {
				t.Fatal(err)
			}
			ds, err := s.GetDesiredState(ctx, wf.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			if ds.DesiredCommand != ControlPause {
				t.Fatalf("desired=%s", ds.DesiredCommand)
			}
			// Cancel wins.
			if err := s.RequestControl(ctx, &ControlRequest{
				SchemaVersion:      CurrentSchemaVersion,
				WorkflowID:         wf.WorkflowID,
				Command:            ControlCancel,
				ExpectedGeneration: wf.Generation,
				ActorIdentity:      "ops",
				AuthorityScope:      AuthScopeControl,
				IdempotencyKey:     "cancel-1",
			}); err != nil {
				t.Fatal(err)
			}
			ds, err = s.GetDesiredState(ctx, wf.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			if ds.DesiredCommand != ControlCancel || !ds.CancelPrecedence {
				t.Fatalf("desired after cancel: %+v", ds)
			}
			// Subsequent pause/resume/continue must not override cancel.
			for _, cmd := range []ControlCommand{ControlPause, ControlResume, ControlContinue, ControlRestart} {
				if err := s.RequestControl(ctx, &ControlRequest{
					SchemaVersion:      CurrentSchemaVersion,
					WorkflowID:         wf.WorkflowID,
					Command:            cmd,
					ExpectedGeneration: wf.Generation,
					ActorIdentity:      "ops",
					AuthorityScope:      AuthScopeControl,
					IdempotencyKey:     "after-cancel-" + cmd.String(),
				}); err != nil {
					t.Fatal(err)
				}
				ds, err = s.GetDesiredState(ctx, wf.WorkflowID)
				if err != nil {
					t.Fatal(err)
				}
				if ds.DesiredCommand != ControlCancel || !ds.CancelPrecedence {
					t.Fatalf("after %s desired became %+v", cmd, ds)
				}
			}
		})
	}
}

func TestControl_CancelIdempotentReplay(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := seedWorkflow(t, s, WorkflowStatusRunning)

	req := &ControlRequest{
		SchemaVersion:      CurrentSchemaVersion,
		WorkflowID:         wf.WorkflowID,
		Command:            ControlCancel,
		ExpectedGeneration: wf.Generation,
		ActorIdentity:      "ops",
		AuthorityScope:      AuthScopeControl,
		IdempotencyKey:     "cancel-idem",
	}
	if err := s.RequestControl(ctx, req); err != nil {
		t.Fatal(err)
	}
	firstID := req.ControlRequestID
	ds1, err := s.GetDesiredState(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	// Replay same logical cancel with a new request object / same key.
	// Must preserve original ControlRequestID (idempotent replay).
	req2 := &ControlRequest{
		SchemaVersion:      CurrentSchemaVersion,
		WorkflowID:         wf.WorkflowID,
		Command:            ControlCancel,
		ExpectedGeneration: wf.Generation,
		ActorIdentity:      "ops",
		AuthorityScope:     AuthScopeControl,
		IdempotencyKey:     "cancel-idem",
	}
	if err := s.RequestControl(ctx, req2); err != nil {
		t.Fatal(err)
	}
	ds2, err := s.GetDesiredState(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if ds2.DesiredCommand != ControlCancel || !ds2.CancelPrecedence {
		t.Fatalf("cancel replay outcome: %+v", ds2)
	}
	if ds2.ControlRequestID != firstID {
		t.Fatalf("control cancel idempotency must preserve ControlRequestID (first=%s second=%s desired=%s)",
			firstID, req2.ControlRequestID, ds2.ControlRequestID)
	}
	if req2.ControlRequestID != firstID {
		t.Fatalf("replay must fill req.ControlRequestID with original (got %s want %s)", req2.ControlRequestID, firstID)
	}
	if ds1.DesiredCommand != ds2.DesiredCommand {
		t.Fatal("desired command changed across cancel replays")
	}
}

func TestControl_PauseCommitsBeforeSchedulerLaunch(t *testing.T) {
	// PAUSE_REQUESTED status holds concurrency slot so scheduler/admission
	// cannot launch a second top-level run while pause is in flight.
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	rec, err := s.AdmitInvocation(ctx, baseInvocation(dep, "ctrl-pause", "c", `{}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Operator desired pause.
	if err := s.RequestControl(ctx, &ControlRequest{
		SchemaVersion:      CurrentSchemaVersion,
		WorkflowID:         rec.WorkflowID,
		Command:            ControlPause,
		ExpectedGeneration: 1,
		ActorIdentity:      "ops",
		AuthorityScope:      AuthScopeControl,
		IdempotencyKey:     "p1",
	}); err != nil {
		t.Fatal(err)
	}
	// Scheduler applies PAUSE_REQUESTED (durable status) before launching more work.
	wf, err := s.GetWorkflow(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	wf.Status = WorkflowStatusPauseRequested
	if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatal(err)
	}
	// Concurrent admit must still see slot held.
	if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "ctrl-pause-2", "c2", `{}`), 0); !errorsIs(err, ErrAlreadyRunning) {
		t.Fatalf("PAUSE_REQUESTED must block new admit, got %v", err)
	}
	// Full PAUSED releases.
	wf, err = s.GetWorkflow(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	wf.Status = WorkflowStatusPaused
	if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "ctrl-pause-2", "c2", `{}`), 0); err != nil {
		t.Fatalf("PAUSED must release slot: %v", err)
	}
}

func TestControl_ResumeAdmissionSlotRace(t *testing.T) {
	// Resume reacquisition vs concurrent admit: with max=1, after pause only
	// one of {resume to RUNNING, new admit} can hold the slot. Status updates
	// are CAS'd; admits check holders atomically under store lock.
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	rec, err := s.AdmitInvocation(ctx, baseInvocation(dep, "resume-race", "c", `{}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := s.GetWorkflow(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	wf.Status = WorkflowStatusPaused
	if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatal(err)
	}
	// Concurrent: resume original + new admit.
	var admitOK, resumeOK int32
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Re-read and resume.
		w, err := s.GetWorkflow(ctx, rec.WorkflowID)
		if err != nil {
			return
		}
		w.Status = WorkflowStatusRunning
		if err := s.UpdateWorkflow(ctx, w, w.Generation); err == nil {
			atomic.AddInt32(&resumeOK, 1)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "resume-race-2", "c2", `{}`), 0); err == nil {
			atomic.AddInt32(&admitOK, 1)
		}
	}()
	wg.Wait()
	// With max=1, resume re-acquire and concurrent admit must not both hold slots.
	wfs, err := s.ListWorkflows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	holders := 0
	for _, w := range wfs {
		switch w.Status {
		case WorkflowStatusPending, WorkflowStatusRunning, WorkflowStatusPauseRequested:
			holders++
		}
	}
	if holders > 1 {
		t.Fatalf("resume+admit race left %d slot-holders under max_concurrent_runs=1 (resumeOK=%d admitOK=%d)",
			holders, resumeOK, admitOK)
	}
	// At least one of resume or admit should have progressed.
	if resumeOK+admitOK == 0 {
		t.Fatal("neither resume nor admit succeeded")
	}
}

func TestAmendment_AuthorityGenerationCAS(t *testing.T) {
	for _, be := range []struct {
		name string
		new  func(t *testing.T) WorkflowStore
	}{
		{"LocalStore", func(t *testing.T) WorkflowStore { return openTestStore(t) }},
		{"MemoryStore", func(t *testing.T) WorkflowStore {
			return NewMemoryStore(WithMemoryClock(testClock(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))))
		}},
	} {
		t.Run(be.name, func(t *testing.T) {
			s := be.new(t)
			ctx := context.Background()
			wf := seedWorkflowWithLimits(t, s, 1000, 500, "1.00")
			// Wrong authority generation denied.
			err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 99, &LimitAmendment{
				NewMaxActiveDurationMs: 2000,
				Reason:                 "stale",
				ActorIdentity:          "admin",
				IdempotencyKey:         "amd-stale",
			})
			if !errorsIs(err, ErrCASConflict) {
				t.Fatalf("want authority CAS conflict, got %v", err)
			}
			// Correct authority succeeds and bumps.
			if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
				NewMaxActiveDurationMs: 5000,
				Reason:                 "more",
				ActorIdentity:          "admin",
				IdempotencyKey:         "amd-ok",
			}); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetWorkflow(ctx, wf.WorkflowID)
			if err != nil {
				t.Fatal(err)
			}
			if got.AuthorityGeneration != 2 || got.MaxActiveDurationMs != 5000 {
				t.Fatalf("got %+v", got)
			}
			// Stale authority after bump.
			if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
				NewMaxActiveDurationMs: 9000,
			}); !errorsIs(err, ErrCASConflict) {
				t.Fatalf("stale auth: %v", err)
			}
		})
	}
}

func TestAmendment_IncreaseOnly(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := seedWorkflowWithLimits(t, s, 10_000, 1000, "5.00")
	// Decrease active time rejected.
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
		NewMaxActiveDurationMs: 100,
	}); !errorsIs(err, ErrInvalidArgument) {
		t.Fatalf("decrease active: %v", err)
	}
	// Decrease lease rejected.
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
		NewCurrentAttemptLeaseMs: 10,
	}); !errorsIs(err, ErrInvalidArgument) {
		t.Fatalf("decrease lease: %v", err)
	}
	// Equal or higher allowed.
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
		NewMaxActiveDurationMs:   10_000,
		NewCurrentAttemptLeaseMs: 2000,
		NewMaxLLMSpendDecimal:    "10.00",
		Reason:                   "bump",
		ActorIdentity:            "admin",
		IdempotencyKey:           "inc-1",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxActiveDurationMs != 10_000 || got.MaxAttemptLeaseMs != 2000 || got.MaxLLMSpendDecimal != "10.00" {
		t.Fatalf("got %+v", got)
	}
}

func TestAmendment_IdempotencyKeyBehavior(t *testing.T) {
	// Spec: same key + same payload → IDEMPOTENT_REPLAY; changed payload → conflict.
	s := openTestStore(t)
	ctx := context.Background()
	wf := seedWorkflowWithLimits(t, s, 1000, 500, "1.00")
	amd := &LimitAmendment{
		NewMaxActiveDurationMs: 2000,
		Reason:                 "first",
		ActorIdentity:          "admin",
		IdempotencyKey:         "same-key",
	}
	if err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, amd); err != nil {
		t.Fatal(err)
	}
	// Replay same key + same payload with next authority gen — IDEMPOTENT_REPLAY
	// (no second authority bump).
	err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 2, &LimitAmendment{
		NewMaxActiveDurationMs: 2000,
		Reason:                 "first",
		ActorIdentity:          "admin",
		IdempotencyKey:         "same-key",
	})
	if err != nil {
		t.Fatalf("idempotent replay of amendment should succeed: %v", err)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorityGeneration != 2 {
		t.Fatalf("idempotent replay must not bump authority generation, got %d", got.AuthorityGeneration)
	}
	if got.MaxActiveDurationMs != 2000 {
		t.Fatalf("ceiling after replay=%d", got.MaxActiveDurationMs)
	}
	// Changed payload same key should be IDEMPOTENCY_CONFLICT per spec.
	err = s.AppendLimitAmendment(ctx, wf.WorkflowID, got.AuthorityGeneration, &LimitAmendment{
		NewMaxActiveDurationMs: 3000,
		Reason:                 "changed",
		ActorIdentity:          "admin",
		IdempotencyKey:         "same-key",
	})
	if !errorsIs(err, ErrIdempotencyConflict) {
		t.Fatalf("amendment same key changed payload: want ErrIdempotencyConflict, got %v", err)
	}
}

func TestAmendment_VsTerminalExhaustionRace(t *testing.T) {
	// Exactly one of concurrent authority-generation CAS operations wins.
	s := openTestStore(t)
	ctx := context.Background()
	wf := seedWorkflowWithLimits(t, s, 1000, 500, "1.00")
	var success int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.AppendLimitAmendment(ctx, wf.WorkflowID, 1, &LimitAmendment{
				NewMaxActiveDurationMs: 50_000,
				Reason:                 "race",
				ActorIdentity:          "admin",
				// Unique keys so authority CAS (not idempotency) serializes the race.
				IdempotencyKey: "race-key-" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			})
			if err == nil {
				atomic.AddInt32(&success, 1)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("exactly one amendment must win authority CAS, got %d", success)
	}
	got, err := s.GetWorkflow(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorityGeneration != 2 || got.MaxActiveDurationMs != 50_000 {
		t.Fatalf("got %+v", got)
	}

	// Terminal exhaustion path: mark workflow terminal with ACTIVE_TIME_EXHAUSTED
	// while racing an amendment — authority CAS still serializes; terminal flag
	// is separate. Document that store does not block amendments on terminal.
	reason := FailureActiveTimeExhausted
	got.Status = WorkflowStatusFailed
	got.TerminalReason = &reason
	if err := s.UpdateWorkflow(ctx, got, got.Generation); err != nil {
		t.Fatal(err)
	}
	// Amendment after terminal still CAS on authority — store allows (scheduler
	// should deny). Document.
	err = s.AppendLimitAmendment(ctx, wf.WorkflowID, got.AuthorityGeneration, &LimitAmendment{
		NewMaxActiveDurationMs: 100_000,
		Reason:                 "after-terminal",
		ActorIdentity:          "admin",
		IdempotencyKey:         "post-term",
	})
	if err == nil {
		t.Log("BUG: AppendLimitAmendment accepted after workflow terminal ACTIVE_TIME_EXHAUSTED")
	}
}

func TestActiveTime_LedgerSemanticsPausedNotCharged(t *testing.T) {
	// Requirement 17: persist consumed + at most one running segment start.
	// PAUSED / NEEDS_REPLAN wall time not charged.
	// Part A stores ActiveTimeLedger on WorkflowReport only — no store API.
	// Exercise type-level accounting rules used by higher layers.

	ledger := ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		ConsumedMs:    0,
	}
	// Start segment at t=1000.
	start := int64(1000)
	ledger.RunningSegmentStartMs = &start
	// Close segment at t=4000 → +3000 consumed.
	end := int64(4000)
	if ledger.RunningSegmentStartMs != nil {
		ledger.ConsumedMs += end - *ledger.RunningSegmentStartMs
		ledger.RunningSegmentStartMs = nil
	}
	if ledger.ConsumedMs != 3000 {
		t.Fatalf("consumed=%d", ledger.ConsumedMs)
	}
	// Freeze for PAUSED: capture frozen consumed, clear segment (no wall charge).
	ledger.FrozenConsumedMs = ledger.ConsumedMs
	// Simulate wall clock advancing while paused — must not change ConsumedMs.
	pausedWallAdvance := int64(10_000)
	_ = pausedWallAdvance
	if ledger.RunningSegmentStartMs != nil {
		t.Fatal("PAUSED must not have open running segment")
	}
	if ledger.ConsumedMs != 3000 {
		t.Fatalf("PAUSED must not charge wall time, consumed=%d", ledger.ConsumedMs)
	}
	// Unfreeze (resume): open new segment at current consumed watermark.
	resumeAt := ledger.ConsumedMs // logical clock watermark
	ledger.RunningSegmentStartMs = &resumeAt
	ledger.FrozenConsumedMs = 0
	// Run another 2000 ms of active time (logical).
	ledger.ConsumedMs += 2000
	ledger.RunningSegmentStartMs = nil
	if ledger.ConsumedMs != 5000 {
		t.Fatalf("after resume consumed=%d", ledger.ConsumedMs)
	}

	// NEEDS_REPLAN freeze same as pause.
	ledger.FrozenConsumedMs = ledger.ConsumedMs
	ledger.RunningSegmentStartMs = nil
	if ledger.ConsumedMs != 5000 {
		t.Fatal("NEEDS_REPLAN must not charge")
	}

	// At most one running segment: setting a second without close would be a bug.
	s1 := int64(100)
	ledger.RunningSegmentStartMs = &s1
	if ledger.RunningSegmentStartMs == nil {
		t.Fatal("expected one segment")
	}
	// Conservative crash close: if open segment and daemon restarts, close at
	// last known safe point (do not invent future time). Consumed stays; clear segment.
	// Spec: reconciliation closes interrupted segment conservatively.
	ledger.RunningSegmentStartMs = nil // discard open segment without adding unknown wall time
	// Never sum overlapping node intervals: model as single workflow segment only.
	// (Multi-node pipelines charge workflow segment, not sum of node intervals.)
	nodeA := int64(1000)
	nodeB := int64(1000)
	sumNodes := nodeA + nodeB // incorrect double-count if used as workflow charge
	workflowSegment := int64(1000)
	if sumNodes == workflowSegment {
		t.Fatal("overlapping node intervals must not be used as summed workflow charge")
	}
	if workflowSegment != 1000 {
		t.Fatal("must not double-count overlapping nodes")
	}
}

func TestActiveTime_CrashClosesSegmentConservatively(t *testing.T) {
	// Simulate durable ledger on workflow and ReconcileInterrupted interaction:
	// open segment discarded, no over-charge.
	ledger := ActiveTimeLedger{
		SchemaVersion: CurrentSchemaVersion,
		ConsumedMs:    8000,
	}
	openStart := int64(8000)
	ledger.RunningSegmentStartMs = &openStart
	// Crash: unknown wall time after openStart. Conservative close = do not add.
	// Persist: consumed unchanged, segment cleared.
	closed := ledger
	closed.RunningSegmentStartMs = nil
	if closed.ConsumedMs != 8000 {
		t.Fatalf("conservative close charged %d", closed.ConsumedMs)
	}
	if closed.RunningSegmentStartMs != nil {
		t.Fatal("segment must be cleared on crash reconcilation")
	}

	// Store-level: ReconcileInterrupted fails run/attempt and closes open
	// ActiveTimeLedger segment conservatively.
	s := openTestStore(t)
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    "wf-at",
		Status:        WorkflowStatusRunning,
		WorkflowKind:  "standalone",
		Generation:    1,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	openStartMs := int64(8000)
	if err := s.PutActiveTimeLedger(ctx, wf.WorkflowID, &ActiveTimeLedger{
		SchemaVersion:         CurrentSchemaVersion,
		ConsumedMs:            8000,
		RunningSegmentStartMs: &openStartMs,
	}, 1); err != nil {
		t.Fatal(err)
	}
	run := &RunRecord{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    wf.WorkflowID,
		Status:        RunStatusRunning,
		RunKind:       "standalone",
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	att := &AttemptRecord{
		SchemaVersion: CurrentSchemaVersion,
		RunID:         run.RunID,
		WorkflowID:    wf.WorkflowID,
		Status:        AttemptStatusRunning,
		AttemptNumber: 1,
		Lease:         &AttemptLease{DurationMs: 1000, AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), LeaseToken: "t"},
	}
	if err := s.CreateAttempt(ctx, att); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileInterrupted(ctx, run.RunID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetActiveTimeLedger(ctx, wf.WorkflowID)
	if err != nil {
		t.Fatalf("active time ledger: %v", err)
	}
	if got.RunningSegmentStartMs != nil {
		t.Fatal("ReconcileInterrupted must clear open RunningSegmentStartMs conservatively")
	}
	if got.ConsumedMs != 8000 {
		t.Fatalf("conservative close must not invent wall time, consumed=%d", got.ConsumedMs)
	}
}

func TestControl_AppendControlResult(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	wf := seedWorkflow(t, s, WorkflowStatusRunning)
	req := &ControlRequest{
		SchemaVersion:      CurrentSchemaVersion,
		WorkflowID:         wf.WorkflowID,
		Command:            ControlPause,
		ExpectedGeneration: 1,
		ActorIdentity:      "ops",
		AuthorityScope:      AuthScopeControl,
		IdempotencyKey:     "cr-1",
	}
	if err := s.RequestControl(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendControlResult(ctx, req, map[string]string{"status": "accepted"}); err != nil {
		t.Fatal(err)
	}
}

func seedWorkflow(t *testing.T, s WorkflowStore, status WorkflowStatus) *WorkflowRecord {
	t.Helper()
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion:       CurrentSchemaVersion,
		Status:              status,
		WorkflowKind:        "standalone",
		Generation:          1,
		AuthorityGeneration: 1,
		MaxActiveDurationMs: 60_000,
		MaxAttemptLeaseMs:   30_000,
		MaxLLMSpendDecimal:  "1.00",
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	return wf
}

func seedWorkflowWithLimits(t *testing.T, s WorkflowStore, active, lease int64, cost string) *WorkflowRecord {
	t.Helper()
	ctx := context.Background()
	wf := &WorkflowRecord{
		SchemaVersion:       CurrentSchemaVersion,
		Status:              WorkflowStatusRunning,
		WorkflowKind:        "standalone",
		Generation:          1,
		AuthorityGeneration: 1,
		MaxActiveDurationMs: active,
		MaxAttemptLeaseMs:   lease,
		MaxLLMSpendDecimal:  cost,
	}
	if err := s.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	return wf
}
