package runtime

// B29-ARCH-REVIEW WARNING 3: the inbox wake-event append error must not be
// silently swallowed. A durable message existing without a corresponding wake
// event is the worst failure mode of the inbox protocol: a waiting worker
// would never be resumed, and the caller would have no signal that the wake
// was lost.
//
// This test verifies that when the paired event store's Append fails during
// inbox Append, the error is surfaced to the caller (not silently dropped).

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// erroringWakeEventStore is a port.EventStore whose Append always returns err.
type erroringWakeEventStore struct {
	mu        sync.Mutex
	appends   int
	appendErr error
}

func (e *erroringWakeEventStore) Append(_ context.Context, _ port.Event) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.appends++
	return 0, e.appendErr
}

func (e *erroringWakeEventStore) Subscribe(context.Context, string, string, int64) (<-chan port.Event, error) {
	ch := make(chan port.Event)
	close(ch)
	return ch, nil
}

func (e *erroringWakeEventStore) Read(context.Context, string, string, int64, int) ([]port.Event, error) {
	return nil, nil
}

func (e *erroringWakeEventStore) LatestSequence(context.Context, string, string) (int64, error) {
	return 0, nil
}

func (e *erroringWakeEventStore) appendCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.appends
}

var _ port.EventStore = (*erroringWakeEventStore)(nil)

// TestInboxAppend_WakeAppendErrorSurfaced verifies that when the paired event
// store's Append fails, the inbox Append returns an error to the caller
// rather than silently dropping the wake event. The message itself was already
// persisted to the inbox WAL; the returned MessageID is non-empty so the caller
// can reconcile, but the error signals the wake was lost.
func TestInboxAppend_WakeAppendErrorSurfaced(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inboxDir := dir + "/state/inbox"
	wakeErr := errors.New("event store offline: cannot append wake")
	events := &erroringWakeEventStore{appendErr: wakeErr}

	inbox, err := NewDurableInboxStore(inboxDir, events)
	if err != nil {
		t.Fatalf("NewDurableInboxStore: %v", err)
	}
	defer func() { _ = inbox.Close() }()

	ctx := context.Background()
	id, err := inbox.Append(ctx, mkInboxMsg("tenant-w", "run-w", "task-w", "s", InboxTypeInput, []byte("hello")))
	if err == nil {
		t.Fatal("ADVERSARY BREAK: inbox Append returned nil error when wake append failed — wake event silently lost")
	}
	if id == "" {
		t.Fatal("ADVERSARY BREAK: inbox Append returned empty MessageID on wake append failure — caller cannot reconcile")
	}
	// The wake Append must have been attempted (not skipped).
	if got := events.appendCount(); got == 0 {
		t.Fatal("ADVERSARY BREAK: inbox Append did not attempt wake append — wake event silently lost")
	}
	// The message WAS persisted to the inbox WAL (List returns it), so the
	// caller can reconcile by re-emitting the wake. Confirm List sees it.
	msgs, lerr := inbox.List(ctx, "tenant-w", "run-w", "task-w")
	if lerr != nil {
		t.Fatalf("List after wake-append failure: %v", lerr)
	}
	if len(msgs) != 1 {
		t.Fatalf("List = %d messages; want 1 (message persisted despite wake failure)", len(msgs))
	}
	if msgs[0].MessageID != string(id) {
		t.Fatalf("List message id = %q; want %q", msgs[0].MessageID, id)
	}
}
