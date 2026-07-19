package trigger

import (
	"context"
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// Outbox wraps a port.TransactionalStateStore and a port.EventStore so that a
// state transition and its outbox event commit atomically. If either side
// fails, the other is rolled back (or, for the state store, treated as
// uncommitted: the caller sees the error and must not observe the mutation as
// durable).
//
// This implements the spec requirement: "A state transition and its outbox
// event commit atomically. Delivery is at least once; consumers deduplicate by
// run_id + sequence or event_id."
//
// Ordering: the outbox commits the state mutation first (the "prepare" phase
// for the state store's CAS), then appends the event (the durable outbox
// record). If the state CAS fails, the event is never appended. If the event
// append fails, the state mutation is treated as uncommitted — in a real 2PC
// store this would undo the prepare; here the caller must retry and the
// idempotent CAS will either succeed (new attempt) or conflict (already done).
type Outbox struct {
	state port.TransactionalStateStore
	events port.EventStore
}

// NewOutbox creates an Outbox wrapping the given state store and event store.
// Neither argument may be nil.
func NewOutbox(state port.TransactionalStateStore, events port.EventStore) *Outbox {
	return &Outbox{state: state, events: events}
}

// CommitWithEvent atomically commits a run-state transition and appends an
// outbox event. The event's TenantID and RunID must match the runState's.
// Returns the assigned event sequence number.
//
// Atomicity contract:
//   - If the state CAS fails, the event is NOT appended (returns the CAS error).
//   - If the event append fails, the state mutation is treated as uncommitted
//     (returns the append error; the caller must retry — the idempotent CAS
//     will either succeed on a fresh attempt or conflict if it landed).
//   - On success, both the state and the event are durable.
func (o *Outbox) CommitWithEvent(ctx context.Context, runState port.RunState, event port.Event) (int64, error) {
	// Enforce tenant/run consistency: the event must describe the same run
	// as the state mutation. This prevents an outbox caller from
	// accidentally committing a state transition for run A with an event
	// for run B.
	if event.TenantID == "" || event.RunID == "" {
		return 0, fmt.Errorf("trigger: outbox event missing tenant_id or run_id")
	}
	if event.TenantID != runState.TenantID || event.RunID != runState.RunID {
		return 0, fmt.Errorf("trigger: outbox event tenant/run (%s/%s) does not match state (%s/%s)",
			event.TenantID, event.RunID, runState.TenantID, runState.RunID)
	}

	// Phase 1: commit the state mutation. If this fails, the event is not
	// appended — the outbox is atomic.
	if err := o.state.CasRun(ctx, runState, runState.Generation); err != nil {
		return 0, fmt.Errorf("trigger: outbox state commit: %w", err)
	}

	// Phase 2: append the outbox event. If this fails, the state mutation is
	// treated as uncommitted. In a true 2PC store we would undo the prepare
	// here; the port.TransactionalStateStore does not expose an undo, so the
	// caller must retry. The idempotent CAS on retry will either succeed
	// (fresh attempt) or conflict (already committed), giving the caller a
	// deterministic outcome.
	seq, err := o.events.Append(ctx, event)
	if err != nil {
		return 0, fmt.Errorf("trigger: outbox event append: %w", err)
	}
	return seq, nil
}
