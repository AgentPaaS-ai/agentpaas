package k8s

import (
	"context"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"sync"
	"time"
)

// K8sEventStore adapts EventBus and keeps an ordered local event journal.
type K8sEventStore struct {
	bus    *trigger.EventBus
	mu     sync.Mutex
	events map[string][]port.Event
}

var _ port.EventStore = (*K8sEventStore)(nil)

func (e *K8sEventStore) Append(_ context.Context, v port.Event) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := v.TenantID + "/" + v.RunID
	v.Sequence = int64(len(e.events[k]) + 1)
	if v.Timestamp.IsZero() {
		v.Timestamp = time.Now()
	}
	e.events[k] = append(e.events[k], v)
	if e.bus != nil {
		e.bus.RegisterRun(v.RunID)
	}
	return v.Sequence, nil
}
func (e *K8sEventStore) Subscribe(ctx context.Context, t, r string, from int64) (<-chan port.Event, error) {
	ch := make(chan port.Event, 64)
	e.mu.Lock()
	for _, v := range e.events[t+"/"+r] {
		if v.Sequence > from {
			ch <- v
		}
	}
	e.mu.Unlock()
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}
func (e *K8sEventStore) Read(_ context.Context, t, r string, from int64, n int) ([]port.Event, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	all := e.events[t+"/"+r]
	var out []port.Event
	for _, v := range all {
		if v.Sequence > from && (n <= 0 || len(out) < n) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (e *K8sEventStore) LatestSequence(_ context.Context, t, r string) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return int64(len(e.events[t+"/"+r])), nil
}
