package runtime

// B29-T05: Safe wait and wake.
//
// A worker waiting at a safe boundary (between tool calls, not mid-LLM-
// request) has no open client request dependency. WaitForWake returns a
// channel that receives a WakeSignal when an inbox message, approval
// resolution, or timeout lands. The wait is backed by the durable event
// store's subscription channel — there is no polling. The supervisor may
// stop an on-demand sandbox (scale to zero) and later resume it from
// committed state when the wake event arrives: because the wake is a durable
// event, a re-subscribe after restart replays any wake that landed while the
// worker was stopped.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// Sentinel errors for the wake registry.
var (
	// ErrWakeClosed is returned after the wake registry is closed.
	ErrWakeClosed = errors.New("runtime: wake registry closed")
)

// WakeReason explains why a worker was woken.
type WakeReason string

const (
	// WakeReasonMessage: an inbox input message was appended.
	WakeReasonMessage WakeReason = "message"
	// WakeReasonApproval: an approval was resolved.
	WakeReasonApproval WakeReason = "approval"
	// WakeReasonTimeout: the wait timed out.
	WakeReasonTimeout WakeReason = "timeout"
)

// WakeSignal is delivered to a waiting worker. The worker receives exactly
// the reason and the task identifier; it must then List its inbox to read
// the actual (untrusted) content.
type WakeSignal struct {
	RunID   string
	TaskID  string
	Reason  WakeReason
}

// Event types used by the inbox/approval stores to emit wake events. These
// are durable records in the event store; a subscriber filters on them.
const (
	// wakeEventTypeInbox is appended when an inbox message is added.
	wakeEventTypeInbox = "inbox.message"
	// wakeEventTypeApproval is appended when an approval is resolved.
	wakeEventTypeApproval = "inbox.approval"
)

// WakeRegistry maps durable event-store subscriptions to per-task wake
// channels. It depends only on port.EventStore — no polling.
type WakeRegistry struct {
	events port.EventStore

	mu     sync.Mutex
	closed bool
	// subs tracks the cancel funcs for the goroutines we spawn, so Close can
	// stop them deterministically.
	subs []context.CancelFunc
}

// NewWakeRegistry creates a WakeRegistry backed by the given event store.
func NewWakeRegistry(events port.EventStore) *WakeRegistry {
	return &WakeRegistry{events: events}
}

// Close stops all in-flight wait goroutines. It is idempotent.
func (w *WakeRegistry) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	for _, cancel := range w.subs {
		cancel()
	}
	w.subs = nil
}

// WaitForWake returns a channel that receives a WakeSignal when an inbox
// message or approval resolution is appended for (tenantID, runID, taskID).
//
// The wait does NOT hold an open client request: the worker can be stopped
// (on_demand scale-to-zero) and later resumed. On resume, the worker calls
// WaitForWake again; the durable event store replays any wake event that
// landed while the worker was stopped (Subscribe replays from
// afterSequence=0), so the wake is delivered immediately.
//
// No polling: delivery is driven by the event store's subscription channel.
func (w *WakeRegistry) WaitForWake(ctx context.Context, tenantID, runID, taskID string) (<-chan WakeSignal, error) {
	if tenantID == "" || runID == "" || taskID == "" {
		return nil, fmt.Errorf("runtime: WaitForWake requires tenant, run, and task ids")
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, ErrWakeClosed
	}
	w.mu.Unlock()

	// Subscribe from sequence 0 so any wake event that already landed (e.g.
	// while the worker was suspended) is replayed immediately. This is the
	// "no polling" path: we block on the subscription channel, not a ticker.
	subCtx, cancel := context.WithCancel(ctx)
	eventCh, err := w.events.Subscribe(subCtx, tenantID, runID, 0)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("runtime: wake subscribe: %w", err)
	}

	out := make(chan WakeSignal, 1)
	w.mu.Lock()
	w.subs = append(w.subs, cancel)
	w.mu.Unlock()

	go func() {
		defer cancel()
		defer close(out)
		for {
			select {
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				sig, ok := wakeSignalFromEvent(ev, taskID)
				if !ok {
					continue
				}
				select {
				case out <- sig:
				case <-subCtx.Done():
					return
				}
			case <-subCtx.Done():
				return
			}
		}
	}()
	return out, nil
}

// NotifyWake is called by the supervisor when resuming a stopped worker, or
// immediately if the worker is still running. It appends a wake event to the
// durable event store so any WaitForWake subscriber (live or future) is
// notified. This is the "resume from committed state when the event arrives"
// path.
func (w *WakeRegistry) NotifyWake(ctx context.Context, tenantID string, sig WakeSignal) error {
	if tenantID == "" || sig.RunID == "" || sig.TaskID == "" {
		return fmt.Errorf("runtime: NotifyWake requires tenant, run, and task ids")
	}
	payload, _ := json.Marshal(inboxWakePayload{ // best-effort marshal
		TaskID: sig.TaskID,
		Type:   string(sig.Reason),
	})
	_, err := w.events.Append(ctx, port.Event{
		TenantID: tenantID,
		RunID:    sig.RunID,
		Type:     wakeEventTypeFromReason(sig.Reason),
		Payload:  payload,
	})
	return err
}

// wakeSignalFromEvent maps a durable event to a WakeSignal for the given
// task. Returns ok=false if the event is not a wake event for this task.
func wakeSignalFromEvent(ev port.Event, taskID string) (WakeSignal, bool) {
	switch ev.Type {
	case wakeEventTypeInbox:
		var p inboxWakePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return WakeSignal{}, false
		}
		if p.TaskID != taskID {
			return WakeSignal{}, false
		}
		return WakeSignal{RunID: ev.RunID, TaskID: taskID, Reason: WakeReasonMessage}, true
	case wakeEventTypeApproval:
		var p inboxWakePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return WakeSignal{}, false
		}
		if p.TaskID != taskID {
			return WakeSignal{}, false
		}
		return WakeSignal{RunID: ev.RunID, TaskID: taskID, Reason: WakeReasonApproval}, true
	default:
		return WakeSignal{}, false
	}
}

// wakeEventTypeFromReason maps a WakeReason to the durable event type used
// to notify subscribers.
func wakeEventTypeFromReason(r WakeReason) string {
	switch r {
	case WakeReasonApproval:
		return wakeEventTypeApproval
	case WakeReasonTimeout:
		return wakeEventTypeInbox
	default:
		return wakeEventTypeInbox
	}
}
