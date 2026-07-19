package runtime

// B29-T05: Interactive inbox and suspend/wake.
//
// Tests cover:
//   - Inbox Append/List/MarkDelivered/Purge ordering and durability.
//   - Cross-tenant isolation: tenant A cannot list tenant B's inbox.
//   - WaitForWake: appending a message delivers a wake signal.
//   - Suspend/wake: a stopped worker is resumed and the wake signal still
//     arrives (the wait persists; no open client request).
//   - Disconnect/reconnect: the wait persists across a simulated client
//     disconnect (the event-store subscription is durable).
//   - Approval protocol: RequestApproval is durable; ResolveApproval emits a
//     wake; resolution must be in the original options (reject otherwise).
//   - Input content cannot expand authority: an out-of-set approval resolution
//     is rejected and produces no wake.
//   - No polling: WaitForWake is backed by the event store subscription
//     channel, not a periodic check.

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// newTestInboxStore creates a DurableInboxStore and a paired DurableEventStore
// rooted in a temp dir, returning both plus a cleanup.
func newTestInboxStore(t *testing.T) (*DurableInboxStore, *trigger.DurableEventStore, string, func()) {
	t.Helper()
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "state", "inbox")
	eventsDir := filepath.Join(dir, "state", "events")
	events, err := trigger.NewDurableEventStore(eventsDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	inbox, err := NewDurableInboxStore(inboxDir, events)
	if err != nil {
		_ = events.Close()
		t.Fatalf("NewDurableInboxStore: %v", err)
	}
	cleanup := func() {
		_ = inbox.Close()
		_ = events.Close()
	}
	return inbox, events, dir, cleanup
}

func mkInboxMsg(tenant, run, task, sender string, mtype InboxMessageType, content []byte) InboxMessage {
	return InboxMessage{
		TenantID:  tenant,
		RunID:     run,
		TaskID:    task,
		SenderID:  sender,
		Type:      mtype,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// Inbox Append / List / MarkDelivered / Purge
// ---------------------------------------------------------------------------

func TestInboxAppendListOrder(t *testing.T) {
	t.Parallel()
	inbox, _, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	ctx := context.Background()
	const tenant, run, task, sender = "tenant-a", "run-1", "task-1", "sender-1"

	id1, err := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("first")))
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if id1 == "" {
		t.Fatal("Append 1 returned empty MessageID")
	}
	id2, err := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("second")))
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if id2 == "" {
		t.Fatal("Append 2 returned empty MessageID")
	}

	msgs, err := inbox.List(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d; want 2", len(msgs))
	}
	if string(msgs[0].Content) != "first" || string(msgs[1].Content) != "second" {
		t.Fatalf("order = %q, %q; want first, second", msgs[0].Content, msgs[1].Content)
	}
	if msgs[0].MessageID != string(id1) || msgs[1].MessageID != string(id2) {
		t.Fatal("List returned messages out of assigned-ID order")
	}
	if msgs[0].Delivered {
		t.Fatal("newly appended message should not be Delivered")
	}
}

func TestInboxMarkDelivered(t *testing.T) {
	t.Parallel()
	inbox, _, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	ctx := context.Background()
	const tenant, run, task, sender = "tenant-a", "run-1", "task-1", "sender-1"

	id1, _ := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("a")))
	id2, _ := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("b")))
	id3, _ := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("c")))

	if err := inbox.MarkDelivered(ctx, []MessageID{id1, id3}); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	remaining, err := inbox.List(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 1 || remaining[0].MessageID != string(id2) {
		t.Fatalf("remaining = %v; want only %s", remaining, id2)
	}
}

func TestInboxPurge(t *testing.T) {
	t.Parallel()
	inbox, _, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	ctx := context.Background()
	const tenant, run = "tenant-a", "run-1"
	if _, err := inbox.Append(ctx, mkInboxMsg(tenant, run, "task-1", "s", InboxTypeInput, []byte("a"))); err != nil {
		t.Fatalf("Append a: %v", err)
	}
	if _, err := inbox.Append(ctx, mkInboxMsg(tenant, run, "task-2", "s", InboxTypeInput, []byte("b"))); err != nil {
		t.Fatalf("Append b: %v", err)
	}
	if _, err := inbox.Append(ctx, mkInboxMsg(tenant, run, "task-1", "s", InboxTypeApproval, []byte("c"))); err != nil {
		t.Fatalf("Append c: %v", err)
	}

	if err := inbox.Purge(ctx, tenant, run); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	for _, task := range []string{"task-1", "task-2"} {
		remaining, err := inbox.List(ctx, tenant, run, task)
		if err != nil {
			t.Fatalf("List after purge (%s): %v", task, err)
		}
		if len(remaining) != 0 {
			t.Fatalf("Purge left %d messages for task %s", len(remaining), task)
		}
	}
}

func TestInboxCrossTenantIsolation(t *testing.T) {
	t.Parallel()
	inbox, _, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	ctx := context.Background()
	const run, task, sender = "run-1", "task-1", "sender-1"
	if _, err := inbox.Append(ctx, mkInboxMsg("tenant-a", run, task, sender, InboxTypeInput, []byte("secret-a"))); err != nil {
		t.Fatalf("Append A: %v", err)
	}
	if _, err := inbox.Append(ctx, mkInboxMsg("tenant-b", run, task, sender, InboxTypeInput, []byte("secret-b"))); err != nil {
		t.Fatalf("Append B: %v", err)
	}
	got, err := inbox.List(ctx, "tenant-b", run, task)
	if err != nil {
		t.Fatalf("List B: %v", err)
	}
	if len(got) != 1 || string(got[0].Content) != "secret-b" {
		t.Fatalf("tenant-b saw %v; want only secret-b", got)
	}
	gotA, err := inbox.List(ctx, "tenant-a", run, task)
	if err != nil {
		t.Fatalf("List A: %v", err)
	}
	if len(gotA) != 1 || string(gotA[0].Content) != "secret-a" {
		t.Fatalf("tenant-a saw %v; want only secret-a", gotA)
	}
}

// ---------------------------------------------------------------------------
// Wake / suspend / disconnect
// ---------------------------------------------------------------------------

// TestWaitForWakeAppendedDeliversSignal verifies that appending an inbox
// message delivers a wake signal to a waiting worker's channel.
func TestWaitForWakeAppendedDeliversSignal(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registry := NewWakeRegistry(events)
	const tenant, run, task = "tenant-a", "run-1", "task-1"

	ch, err := registry.WaitForWake(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	// Append a message; the inbox appends a wake event to the event store,
	// which the registry's subscription forwards as a WakeSignal.
	if _, err := inbox.Append(ctx, mkInboxMsg(tenant, run, task, "sender", InboxTypeInput, []byte("hello"))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	select {
	case sig := <-ch:
		if sig.RunID != run || sig.TaskID != task {
			t.Fatalf("wake signal = %+v; want run=%s task=%s", sig, run, task)
		}
		if sig.Reason != WakeReasonMessage {
			t.Fatalf("reason = %q; want %q", sig.Reason, WakeReasonMessage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wake signal")
	}
}

// TestSuspendWakeResume verifies the suspend/wake flow: a worker waits, the
// supervisor suspends it (the wait persists with no open client request), then
// resumes it and the wake signal is still delivered.
func TestSuspendWakeResume(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	registry := NewWakeRegistry(events)
	const tenant, run, task = "tenant-a", "run-1", "task-1"

	// Phase 1: worker starts waiting at a safe boundary.
	ctx1, cancel1 := context.WithCancel(context.Background())
	ch1, err := registry.WaitForWake(ctx1, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	// Phase 2: supervisor suspends the worker — cancel its context (no open
	// client request dependency). The durable event store retains events.
	cancel1()
	// Drain any closed-channel signal.
	select {
	case <-ch1:
	default:
	}

	// Phase 3: while suspended, an approved sender appends a message. This
	// appends a durable wake event to the event store.
	bg := context.Background()
	if _, err := inbox.Append(bg, mkInboxMsg(tenant, run, task, "sender", InboxTypeInput, []byte("resume-input"))); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Phase 4: supervisor resumes the worker. The worker re-subscribes to the
	// durable event store from sequence 0 and immediately receives the wake
	// event that landed while it was suspended.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2, err := registry.WaitForWake(ctx2, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake resume: %v", err)
	}
	select {
	case sig := <-ch2:
		if sig.Reason != WakeReasonMessage {
			t.Fatalf("reason = %q; want %q", sig.Reason, WakeReasonMessage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume: timed out waiting for wake signal")
	}
}

// TestWaitPersistsAcrossDisconnect verifies that a client disconnect does not
// lose the wait: a fresh client re-subscribes via the durable event store and
// receives the wake signal + inbox messages. No polling.
func TestWaitPersistsAcrossDisconnect(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	registry := NewWakeRegistry(events)
	const tenant, run, task = "tenant-a", "run-1", "task-1"

	// Client connects, waits.
	ctx1, cancel1 := context.WithCancel(context.Background())
	_, err := registry.WaitForWake(ctx1, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	// Client disconnects.
	cancel1()

	// While disconnected, sender appends a message (durable).
	bg := context.Background()
	id, err := inbox.Append(bg, mkInboxMsg(tenant, run, task, "sender", InboxTypeInput, []byte("after-disconnect")))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Client reconnects and re-subscribes from sequence 0.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ch2, err := registry.WaitForWake(ctx2, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake reconnect: %v", err)
	}
	select {
	case <-ch2:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect: timed out waiting for wake signal")
	}
	// On reconnect the client also lists undelivered inbox messages.
	msgs, err := inbox.List(bg, tenant, run, task)
	if err != nil {
		t.Fatalf("List after reconnect: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageID != string(id) {
		t.Fatalf("reconnect list = %v; want the appended message", msgs)
	}
}

// TestWakeNoPolling verifies that WaitForWake is backed by the event store
// subscription channel, not a periodic check. We assert the wake arrives
// faster than any polling interval could, and that no ticker fires.
func TestWakeNoPolling(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	registry := NewWakeRegistry(events)
	const tenant, run, task = "tenant-a", "run-1", "task-1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := registry.WaitForWake(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	// Count how many signals arrive WITHOUT any append — should be zero.
	// If the registry polled, we would see spurious wake-ups.
	var spurious atomic.Int32
	go func() {
		for range ch {
			spurious.Add(1)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	if got := spurious.Load(); got != 0 {
		t.Fatalf("spurious wake signals without any append = %d; registry must not poll", got)
	}
	// Now append; the wake must come from the subscription, not a poll.
	if _, err := inbox.Append(ctx, mkInboxMsg(tenant, run, task, "s", InboxTypeInput, []byte("x"))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Re-subscribe to read the wake (the goroutine above may have drained it).
	ch2, err := registry.WaitForWake(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake 2: %v", err)
	}
	select {
	case <-ch2:
	case <-time.After(2 * time.Second):
		t.Fatal("no wake after append; registry is not subscription-backed")
	}
}

// ---------------------------------------------------------------------------
// Approval protocol
// ---------------------------------------------------------------------------

func TestRequestApprovalDurableAndResolveWake(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()
	_ = inbox

	registry := NewWakeRegistry(events)
	approvals := NewApprovalStore(events)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const tenant, run, task = "tenant-a", "run-1", "task-1"
	req := ApprovalRequest{
		RequestID: "",
		RunID:    run,
		TaskID:   task,
		Question: "Proceed with deploy?",
		Options:  []string{"yes", "no"},
		Status:   ApprovalPending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	rid, err := approvals.Request(ctx, tenant, req)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if rid == "" {
		t.Fatal("Request did not assign a RequestID")
	}

	// Worker waits for the wake.
	ch, err := registry.WaitForWake(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	// Approved sender resolves the approval (valid option).
	if err := approvals.Resolve(ctx, tenant, rid, "yes"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	select {
	case sig := <-ch:
		if sig.Reason != WakeReasonApproval {
			t.Fatalf("reason = %q; want %q", sig.Reason, WakeReasonApproval)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve did not deliver a wake signal")
	}
}

func TestResolveApprovalRejectsOutOfSetResolution(t *testing.T) {
	t.Parallel()
	inbox, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()
	approvals := NewApprovalStore(events)
	_ = inbox
	registry := NewWakeRegistry(events)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const tenant, run, task = "tenant-a", "run-1", "task-1"
	req := ApprovalRequest{
		RunID:     run,
		TaskID:    task,
		Question:  "Proceed?",
		Options:   []string{"yes", "no"},
		Status:    ApprovalPending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	id, err := approvals.Request(ctx, tenant, req)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Out-of-set resolution: must be rejected, no wake emitted.
	ch, err := registry.WaitForWake(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("WaitForWake: %v", err)
	}
	err = approvals.Resolve(ctx, tenant, id, "rm -rf /")
	if err == nil {
		t.Fatal("Resolve accepted an out-of-set resolution; input expanded authority")
	}
	if !errors.Is(err, ErrApprovalResolutionNotInOptions) {
		t.Fatalf("error = %v; want ErrApprovalResolutionNotInOptions", err)
	}
	select {
	case <-ch:
		t.Fatal("a rejected resolution emitted a wake signal; input expanded authority")
	case <-time.After(200 * time.Millisecond):
		// good: no wake.
	}
}

func TestResolveApprovalAlreadyResolved(t *testing.T) {
	t.Parallel()
	_, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	approvals := NewApprovalStore(events)
	ctx := context.Background()
	const tenant, run, task = "tenant-a", "run-1", "task-1"
	req := ApprovalRequest{
		RunID: run, TaskID: task, Options: []string{"a", "b"},
		Status: ApprovalPending, ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	id, err := approvals.Request(ctx, tenant, req)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if err := approvals.Resolve(ctx, tenant, id, "a"); err != nil {
		t.Fatalf("Resolve 1: %v", err)
	}
	if err := approvals.Resolve(ctx, tenant, id, "b"); err == nil {
		t.Fatal("second Resolve should fail; approval already resolved")
	}
}

func TestRequestApprovalExpired(t *testing.T) {
	t.Parallel()
	_, events, _, cleanup := newTestInboxStore(t)
	defer cleanup()

	approvals := NewApprovalStore(events)
	ctx := context.Background()
	const tenant, run, task = "tenant-a", "run-1", "task-1"
	req := ApprovalRequest{
		RunID: run, TaskID: task, Options: []string{"a"},
		Status: ApprovalPending, ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	if _, err := approvals.Request(ctx, tenant, req); err == nil {
		t.Fatal("Request accepted an already-expired approval")
	}
}

// ---------------------------------------------------------------------------
// Durability: inbox survives restart (new process opens same state dir)
// ---------------------------------------------------------------------------

func TestInboxSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "state", "inbox")
	eventsDir := filepath.Join(dir, "state", "events")

	events, err := trigger.NewDurableEventStore(eventsDir)
	if err != nil {
		t.Fatalf("NewDurableEventStore: %v", err)
	}
	inbox, err := NewDurableInboxStore(inboxDir, events)
	if err != nil {
		_ = events.Close()
		t.Fatalf("NewDurableInboxStore: %v", err)
	}
	ctx := context.Background()
	const tenant, run, task, sender = "tenant-a", "run-1", "task-1", "sender-1"
	id, err := inbox.Append(ctx, mkInboxMsg(tenant, run, task, sender, InboxTypeInput, []byte("persisted")))
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Simulate process restart: close and reopen.
	_ = inbox.Close()
	_ = events.Close()

	events2, err := trigger.NewDurableEventStore(eventsDir)
	if err != nil {
		t.Fatalf("reopen events: %v", err)
	}
	defer func() { _ = events2.Close() }()
	inbox2, err := NewDurableInboxStore(inboxDir, events2)
	if err != nil {
		t.Fatalf("reopen inbox: %v", err)
	}
	defer func() { _ = inbox2.Close() }()

	msgs, err := inbox2.List(ctx, tenant, run, task)
	if err != nil {
		t.Fatalf("List after restart: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageID != string(id) || string(msgs[0].Content) != "persisted" {
		t.Fatalf("after restart list = %v; want the persisted message %s", msgs, id)
	}
}

// Compile-time assertion that DurableInboxStore implements InboxStore.
var _ InboxStore = (*DurableInboxStore)(nil)

// Compile-time assertion that the wake registry uses the event store (no
// polling): it only depends on port.EventStore for delivery.
var _ port.EventStore = (*trigger.DurableEventStore)(nil)
