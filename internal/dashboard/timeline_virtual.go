package dashboard

import "time"

// SpanBatcher collects spans and sends them in batches to avoid flooding
// the SSE connection. When the number of pending spans exceeds maxBatchSize,
// a batch event is sent containing multiple spans.
type SpanBatcher struct {
	maxBatchSize int
	flushTimeout time.Duration
	events       []TimelineEvent
	lastFlush    time.Time
}

// NewSpanBatcher creates a batcher for virtualizing large span sets.
func NewSpanBatcher(maxBatchSize int, flushTimeout time.Duration) *SpanBatcher {
	if maxBatchSize <= 0 {
		maxBatchSize = 100
	}
	return &SpanBatcher{
		maxBatchSize: maxBatchSize,
		flushTimeout: flushTimeout,
		events:       make([]TimelineEvent, 0, maxBatchSize),
		lastFlush:    time.Now(),
	}
}

// Add adds a span to the batcher. Returns true if a flush is needed.
func (b *SpanBatcher) Add(event TimelineEvent) bool {
	b.events = append(b.events, event)
	if len(b.events) >= b.maxBatchSize {
		return true
	}
	return b.flushTimeout > 0 && time.Since(b.lastFlush) >= b.flushTimeout
}

// Flush returns all pending events and resets the batcher.
func (b *SpanBatcher) Flush() []TimelineEvent {
	if len(b.events) == 0 {
		return nil
	}
	events := make([]TimelineEvent, len(b.events))
	copy(events, b.events)
	b.events = b.events[:0]
	b.lastFlush = time.Now()
	return events
}

// Pending returns the number of pending events.
func (b *SpanBatcher) Pending() int {
	return len(b.events)
}
