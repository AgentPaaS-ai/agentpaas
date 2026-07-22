package delegation

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemoryStore is an in-memory implementation of Store for unit tests.
type MemoryStore struct {
	mu     sync.RWMutex
	cond   *sync.Cond
	now    func() time.Time

	tasks       map[TaskID]*Task
	// idempotency: callerIdentity\x00idempotencyKey -> TaskID
	idempotency map[string]TaskID
	messages    map[TaskID][]Message
	results     map[TaskID]*Result
	events      map[TaskID][]TaskEvent

	// eventIDs tracks EventIDs per task for duplicate detection.
	eventIDs map[TaskID]map[EventID]bool

	// subscribers tracks channel subscribers per task.
	subscribers map[TaskID][]*eventSubscriber

	// terminalSignaled tracks tasks that have had a terminal event appended.
	terminalSignaled map[TaskID]bool
}

// eventSubscriber is a single subscriber channel for a task.
type eventSubscriber struct {
	ch       chan TaskEvent
	canceled bool
	closeOnce sync.Once
}

func (s *eventSubscriber) close() {
	s.closeOnce.Do(func() {
		s.canceled = true
		close(s.ch)
	})
}

// MemoryStoreOption configures a MemoryStore.
type MemoryStoreOption func(*MemoryStore)

// WithMemoryClock injects a fake clock.
func WithMemoryClock(now func() time.Time) MemoryStoreOption {
	return func(s *MemoryStore) {
		if now != nil {
			s.now = now
		}
	}
}

// NewMemoryStore constructs an empty in-memory store.
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		now:              func() time.Time { return time.Now().UTC() },
		tasks:            make(map[TaskID]*Task),
		idempotency:      make(map[string]TaskID),
		messages:         make(map[TaskID][]Message),
		results:          make(map[TaskID]*Result),
		events:           make(map[TaskID][]TaskEvent),
		eventIDs:         make(map[TaskID]map[EventID]bool),
		subscribers:      make(map[TaskID][]*eventSubscriber),
		terminalSignaled: make(map[TaskID]bool),
	}
	s.cond = sync.NewCond(&s.mu)
	for _, o := range opts {
		o(s)
	}
	return s
}

// idempotencyKey builds the idempotency lookup key.
func idempotencyKey(callerIdentity, idempotencyKey string) string {
	return callerIdentity + "\x00" + idempotencyKey
}

// CreateTask persists a new task. Idempotent on caller_identity+idempotency_key.
func (s *MemoryStore) CreateTask(ctx context.Context, t Task) error {
	_ = ctx // interface compliance

	s.mu.Lock()
	defer s.mu.Unlock()

	key := idempotencyKey(t.CallerIdentity, t.IdempotencyKey)
	if existingID, ok := s.idempotency[key]; ok {
		existing, ok := s.tasks[existingID]
		if !ok {
			return fmt.Errorf("delegation: idempotency record %q exists but task %q not found", key, existingID)
		}
		// Idempotent replay: same task ID, same body? Allow.
		// Compare key fields.
		if existing.TaskID == t.TaskID &&
			existing.WorkflowID == t.WorkflowID &&
			existing.TenantID == t.TenantID &&
			existing.Caller.DeploymentID == t.Caller.DeploymentID &&
			existing.Caller.RunID == t.Caller.RunID &&
			existing.Callee.PackageName == t.Callee.PackageName &&
			existing.BindingID == t.BindingID &&
			existing.Capability == t.Capability {
			// Same body — idempotent replay, return success.
			return nil
		}
		return &ValidationError{
			Field:   "idempotency_key",
			Message: ErrIdempotencyConflict,
		}
	}

	// New task.
	cp := t
	s.idempotency[key] = t.TaskID
	s.tasks[t.TaskID] = &cp
	s.messages[t.TaskID] = nil
	s.events[t.TaskID] = nil
	s.eventIDs[t.TaskID] = make(map[EventID]bool)
	return nil
}

// GetTask returns the task by ID.
func (s *MemoryStore) GetTask(ctx context.Context, taskID TaskID) (*Task, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("delegation: task %q not found", taskID)
	}
	cp := *t
	return &cp, nil
}

// CASTask updates a task atomically using compare-and-swap on generation.
func (s *MemoryStore) CASTask(ctx context.Context, t Task, expectedGen int64) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.tasks[t.TaskID]
	if !ok {
		return fmt.Errorf("delegation: task %q not found", t.TaskID)
	}
	if existing.Generation != expectedGen {
		return fmt.Errorf("delegation: CAS conflict for task %q: expected gen %d, got %d",
			t.TaskID, expectedGen, existing.Generation)
	}
	cp := t
	cp.Generation = expectedGen + 1
	s.tasks[t.TaskID] = &cp
	return nil
}

// AppendMessage appends a message to a task, enforcing contiguous sequence.
func (s *MemoryStore) AppendMessage(ctx context.Context, msg Message) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[msg.TaskID]; !ok {
		return fmt.Errorf("delegation: task %q not found", msg.TaskID)
	}

	msgs := s.messages[msg.TaskID]
	lastSeq := int64(0)
	if len(msgs) > 0 {
		lastSeq = msgs[len(msgs)-1].Sequence
	}

	if msg.Sequence != lastSeq+1 {
		return &ValidationError{
			Field:   "sequence",
			Message: ErrSequenceGap,
		}
	}

	cp := msg
	s.messages[msg.TaskID] = append(msgs, cp)
	return nil
}

// ListMessages returns messages for a task with sequence > afterSeq.
func (s *MemoryStore) ListMessages(ctx context.Context, taskID TaskID, afterSeq int64) ([]Message, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[taskID]
	var result []Message
	for _, m := range msgs {
		if m.Sequence > afterSeq {
			cp := m
			result = append(result, cp)
		}
	}
	return result, nil
}

// PutResult stores the terminal result for a task.
func (s *MemoryStore) PutResult(ctx context.Context, r Result) error {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[r.TaskID]
	if !ok {
		return fmt.Errorf("delegation: task %q not found", r.TaskID)
	}

	if !task.Status.IsTerminal() {
		return fmt.Errorf("delegation: task %q is not terminal (status=%s)", r.TaskID, task.Status)
	}

	if task.Status != r.Status {
		return fmt.Errorf("delegation: result status %s does not match task status %s", r.Status, task.Status)
	}

	if existing, ok := s.results[r.TaskID]; ok {
		// Second different result fails.
		if existing.ResultID != r.ResultID {
			return fmt.Errorf("delegation: result already exists for task %q with different result_id", r.TaskID)
		}
		// Same result — idempotent.
		return nil
	}

	cp := r
	s.results[r.TaskID] = &cp
	return nil
}

// GetResult returns the result for a task.
func (s *MemoryStore) GetResult(ctx context.Context, taskID TaskID) (*Result, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.results[taskID]
	if !ok {
		return nil, fmt.Errorf("delegation: result for task %q not found", taskID)
	}
	cp := *r
	return &cp, nil
}

// AppendEvent appends an event to a task's event stream.
func (s *MemoryStore) AppendEvent(ctx context.Context, ev TaskEvent) (int64, error) {
	_ = ctx

	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.cond.Broadcast() // Wake all waiters after append.

	if _, ok := s.tasks[ev.TaskID]; !ok {
		return 0, fmt.Errorf("delegation: task %q not found", ev.TaskID)
	}

	// Duplicate EventID detection.
	if ids, ok := s.eventIDs[ev.TaskID]; ok {
		if ids[ev.EventID] {
			return 0, fmt.Errorf("delegation: duplicate event_id %q for task %q", ev.EventID, ev.TaskID)
		}
	}

	existing := s.events[ev.TaskID]
	nextSeq := int64(1)
	if len(existing) > 0 {
		nextSeq = existing[len(existing)-1].Sequence + 1
	}

	cp := ev
	cp.Sequence = nextSeq
	s.events[ev.TaskID] = append(existing, cp)
	if s.eventIDs[ev.TaskID] == nil {
		s.eventIDs[ev.TaskID] = make(map[EventID]bool)
	}
	s.eventIDs[ev.TaskID][ev.EventID] = true

	// Mark terminal if this is a terminal event.
	if isTerminalEventType(ev.Type) {
		s.terminalSignaled[ev.TaskID] = true
	}

	// Deliver to subscribers (non-blocking).
	subs := s.subscribers[ev.TaskID]
	for _, sub := range subs {
		select {
		case sub.ch <- cp:
		default:
			// Slow consumer — drop. The subscriber channel is buffered.
		}
	}

	// If terminal, close subscriber channels and remove them.
	if isTerminalEventType(ev.Type) {
		for _, sub := range subs {
			sub.close()
		}
		s.subscribers[ev.TaskID] = nil
	}

	return nextSeq, nil
}

// ListEvents returns events for a task with sequence > afterSeq.
func (s *MemoryStore) ListEvents(ctx context.Context, taskID TaskID, afterSeq int64) ([]TaskEvent, error) {
	_ = ctx

	s.mu.RLock()
	defer s.mu.RUnlock()

	evts := s.events[taskID]
	var result []TaskEvent
	for _, e := range evts {
		if e.Sequence > afterSeq {
			cp := e
			result = append(result, cp)
		}
	}
	return result, nil
}

// SubscribeEvents returns a channel of events for a task, replaying existing
// events with sequence > afterSeq. The channel is closed when the context is
// cancelled or the task's event stream reaches a terminal event.
func (s *MemoryStore) SubscribeEvents(ctx context.Context, taskID TaskID, afterSeq int64) (<-chan TaskEvent, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If task doesn't exist, return a closed channel.
	if _, ok := s.tasks[taskID]; !ok {
		ch := make(chan TaskEvent)
		close(ch)
		return ch, func() {}, nil
	}

	// Replay existing events > afterSeq.
	existing := make([]TaskEvent, 0)
	for _, e := range s.events[taskID] {
		if e.Sequence > afterSeq {
			existing = append(existing, e)
		}
	}

	// Buffer enough for replay + some extra.
	bufSize := len(existing) + 64
	if bufSize < 64 {
		bufSize = 64
	}
	ch := make(chan TaskEvent, bufSize)

	// Replay into channel (non-blocking; buffer is sized for it).
	for _, e := range existing {
		ch <- e
	}

	// If terminal already signaled, close the channel.
	if s.terminalSignaled[taskID] {
		close(ch)
		return ch, func() {}, nil
	}

	// Register subscriber.
	sub := &eventSubscriber{ch: ch}
	s.subscribers[taskID] = append(s.subscribers[taskID], sub)

	// Start a goroutine that waits for context cancel or new events.
	cancel := func() {
		sub.close()
		s.mu.Lock()
		defer s.mu.Unlock()
		// Remove from subscribers list.
		subs := s.subscribers[taskID]
		for i, s2 := range subs {
			if s2 == sub {
				s.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}

	// Context watcher goroutine.
	go func() {
		<-ctx.Done()
		cancel()
	}()

	return ch, cancel, nil
}

// Compile-time interface check.
var _ Store = (*MemoryStore)(nil)
