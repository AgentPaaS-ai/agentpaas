package trigger

import (
	"sync"
	"time"
)

// EventType is the type of run event.
type EventType string

const (
	EventRunCreated   EventType = "run_created"
	EventRunStarted   EventType = "run_started"
	EventRunProgress  EventType = "run_progress"
	EventRunSucceeded EventType = "run_succeeded"
	EventRunFailed    EventType = "run_failed"
	EventRunCancelled EventType = "run_cancelled"
	EventHeartbeat    EventType = "heartbeat"
)

// RunEvent is a single event in a run's lifecycle.
type RunEvent struct {
	EventID   int64     `json:"event_id"`
	RunID     string    `json:"run_id"`
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// IsTerminal returns true if this event ends the run lifecycle.
func (e *RunEvent) IsTerminal() bool {
	return e.Type == EventRunSucceeded || e.Type == EventRunFailed || e.Type == EventRunCancelled
}

// EventBus manages event subscriptions for runs.
type EventBus struct {
	mu      sync.RWMutex
	buffers map[string]*runBuffer
}

type runBuffer struct {
	mu          sync.Mutex
	events      []RunEvent
	nextID      int64
	subscribers map[int64]chan RunEvent
	nextSubID   int64
	closed      bool
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		buffers: make(map[string]*runBuffer),
	}
}

// RegisterRun creates a buffer for a new run.
func (b *EventBus) RegisterRun(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.buffers[runID]; ok {
		return
	}
	b.buffers[runID] = &runBuffer{
		subscribers: make(map[int64]chan RunEvent),
	}
}

// Publish publishes an event for a run. Non-blocking if there are no subscribers.
func (b *EventBus) Publish(runID string, eventType EventType, data any) *RunEvent {
	b.mu.RLock()
	buf, ok := b.buffers[runID]
	b.mu.RUnlock()
	if !ok {
		b.RegisterRun(runID)
		b.mu.RLock()
		buf = b.buffers[runID]
		b.mu.RUnlock()
	}

	buf.mu.Lock()
	defer buf.mu.Unlock()

	if buf.closed {
		return nil
	}

	buf.nextID++
	event := RunEvent{
		EventID:   buf.nextID,
		RunID:     runID,
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
	buf.events = append(buf.events, event)

	// Fan-out under the buffer lock but never block: a stalled subscriber
	// must not stall the publisher or other subscribers.
	for _, ch := range buf.subscribers {
		select {
		case ch <- event:
		default:
		}
	}

	if event.IsTerminal() {
		buf.closed = true
		for _, ch := range buf.subscribers {
			close(ch)
		}
		buf.subscribers = make(map[int64]chan RunEvent)
	}

	return &event
}

// Subscribe subscribes to events for a run, replaying events after fromEventID.
// The returned cancel function unregisters the subscriber and closes its
// channel so the consumer cannot hang after disconnect (SSE client gone, etc.).
// cancel is idempotent and safe after a terminal event has already closed the
// channel.
func (b *EventBus) Subscribe(runID string, fromEventID int64) (<-chan RunEvent, func()) {
	b.mu.RLock()
	buf, ok := b.buffers[runID]
	b.mu.RUnlock()
	if !ok {
		ch := make(chan RunEvent)
		close(ch)
		return ch, func() {}
	}

	buf.mu.Lock()
	defer buf.mu.Unlock()

	subID := buf.nextSubID
	buf.nextSubID++
	ch := make(chan RunEvent, 64)

	// Non-blocking replay under the lock: never hold buf.mu while blocked on
	// a full channel (deadlock risk if the consumer is not yet reading).
	for _, event := range buf.events {
		if event.EventID > fromEventID {
			select {
			case ch <- event:
			default:
			}
		}
	}

	if buf.closed {
		close(ch)
		return ch, func() {}
	}

	buf.subscribers[subID] = ch

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			buf.mu.Lock()
			defer buf.mu.Unlock()
			if existing, ok := buf.subscribers[subID]; ok {
				delete(buf.subscribers, subID)
				close(existing)
			}
		})
	}

	return ch, cancel
}

// GetEvents returns all events for a run.
func (b *EventBus) GetEvents(runID string) []RunEvent {
	b.mu.RLock()
	buf, ok := b.buffers[runID]
	b.mu.RUnlock()
	if !ok {
		return nil
	}

	buf.mu.Lock()
	defer buf.mu.Unlock()

	result := make([]RunEvent, len(buf.events))
	copy(result, buf.events)
	return result
}

// UnregisterRun removes a run's buffer.
func (b *EventBus) UnregisterRun(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.buffers, runID)
}
