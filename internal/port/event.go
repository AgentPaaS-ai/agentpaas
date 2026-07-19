package port

import (
	"context"
	"time"
)

// EventStore persists ordered tenant/run events.
type EventStore interface {
	Append(context.Context, Event) (int64, error)
	Subscribe(context.Context, string, string, int64) (<-chan Event, error)
	Read(context.Context, string, string, int64, int) ([]Event, error)
	LatestSequence(context.Context, string, string) (int64, error)
}

// Event is an ordered durable run event.
type Event struct {
	TenantID  string
	RunID     string
	Sequence  int64
	Type      string
	Payload   []byte
	Timestamp time.Time
}
