package daemon

import (
	"context"
	"testing"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestB30T03PartB_ClientQueryTimeoutLeavesRunActive verifies the b30-summary.md
// :367-368 contract: "A waiting status/result query has its own short client
// timeout and never cancels the run."
//
// A client issuing GetRunStatus / GetRunResult with a short (100ms) client-side
// context timeout MUST NOT cause the durable run to be cancelled or failed.
// The query may return (possibly with a DeadlineExceeded from the gRPC layer if
// the store blocked), but the run record's status in the store is unchanged —
// still PENDING/READY/RUNNING, never CANCELLED or FAILED as a side effect of
// the query timing out.
//
// This is the durable-path contract that distinguishes a QUERY (read-only,
// client-cancellable) from a CONTROL operation (CancelRun / lifecycle). The
// run's lifetime is governed exclusively by its TimeEnvelope / ceilings and
// explicit control RPCs — never by a reader's context deadline.
func TestB30T03PartB_ClientQueryTimeoutLeavesRunActive(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depID := seedActiveDepForInvoke(t, s)

	// Admit a run via the durable InvokeDeployment path.
	ir, err := s.InvokeDeployment(ctx, invokeReq(depID, "idem-query-timeout", "tester", `{}`))
	if err != nil {
		t.Fatalf("InvokeDeployment: %v", err)
	}
	runID := ir.GetRunId()

	// Snapshot the run's status immediately after admission. Admission creates
	// a PENDING run (READY launch intent, no attempt yet).
	preRun, err := s.runStore.GetRun(ctx, routedrun.RunID(runID))
	if err != nil {
		t.Fatalf("pre-query GetRun: %v", err)
	}
	preStatus := preRun.Status
	if preStatus != routedrun.RunStatusPending {
		t.Fatalf("pre-query status=%s want PENDING", preStatus)
	}

	// Issue GetRunStatus with a short (100ms) client-side context timeout.
	// The call may return successfully (the store's GetRun is fast and ignores
	// ctx) OR return a DeadlineExceeded. Either outcome is acceptable; what
	// matters is that the run is NOT cancelled/failed as a side effect.
	queryCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer func() { cancel() }()
	_, qerr := s.GetRunStatus(queryCtx, &controlv1.GetRunStatusRequest{RunId: runID})
	if qerr != nil {
		// A DeadlineExceeded (or Unavailable from the gRPC layer) is an
		// acceptable outcome for a client-cancelled query. Any other error
		// is a bug: the query must not surface a typed control error.
		if code := status.Code(qerr); code != codes.DeadlineExceeded && code != codes.Unavailable {
			t.Fatalf("GetRunStatus returned unexpected error: %v (code=%v)", qerr, code)
		}
	}

	// Issue GetRunResult with a short (100ms) client-side context timeout.
	// Same contract: the run must not be cancelled/failed.
	resultCtx, cancel2 := context.WithTimeout(ctx, 100*time.Millisecond)
	defer func() { cancel2() }()
	_, rerr := s.GetRunResult(resultCtx, &controlv1.GetRunResultRequest{RunId: runID})
	if rerr != nil {
		if code := status.Code(rerr); code != codes.DeadlineExceeded && code != codes.Unavailable {
			t.Fatalf("GetRunResult returned unexpected error: %v (code=%v)", rerr, code)
		}
	}

	// Re-read the run record from the store. Its status MUST be unchanged
	// (still PENDING) — the query timeouts did not cancel or fail the run.
	// We poll briefly to flush any asynchronous mutation the handler might
	// have scheduled (defensive: there is none today, but a future regression
	// could add one). After the poll window the status must still be active.
	deadline := time.Now().Add(500 * time.Millisecond)
	var postRun *routedrun.RunRecord
	for {
		postRun, err = s.runStore.GetRun(ctx, routedrun.RunID(runID))
		if err != nil {
			t.Fatalf("post-query GetRun: %v", err)
		}
		if postRun.Status != preStatus {
			break // observed a transition — investigate below
		}
		if time.Now().After(deadline) {
			break // no transition within the window — good
		}
		time.Sleep(20 * time.Millisecond)
	}

	if postRun.Status != preStatus {
		t.Fatalf("query timeout mutated run status: pre=%s post=%s (run was cancelled/failed "+
			"as a side effect of a client query timeout — queries must never cancel the run)",
			preStatus, postRun.Status)
	}
	// The run must NOT be in a terminal-failed state due to the query.
	if postRun.Status == routedrun.RunStatusCancelled || postRun.Status == routedrun.RunStatusFailed {
		t.Fatalf("run status=%s after client query timeout — query must not cancel/fail the run",
			postRun.Status)
	}
	// The run must not have a TerminatedAt timestamp set by the query path.
	if postRun.TerminatedAt != nil && !postRun.TerminatedAt.IsZero() {
		t.Fatalf("run terminated_at=%s set by client query timeout — queries must not terminate the run",
			postRun.TerminatedAt)
	}
}
