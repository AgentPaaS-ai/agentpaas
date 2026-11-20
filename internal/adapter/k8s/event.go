package k8s

import (
	"context"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// K8sEventStore adapts EventBus and keeps an ordered local event journal.
type K8sEventStore struct {
	bus    *trigger.EventBus
	mu     sync.Mutex
	events map[string][]port.Event
}

var _ port.EventStore = (*K8sEventStore)(nil)

// K8sEventStore.Append appends k8s event store.
//
// It returns an error if the operation fails or inputs are invalid.
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

// K8sEventStore.Subscribe subscribes k8s event store.
//
// It returns an error if the operation fails or inputs are invalid.
//
// Historical events are snapshotted under the mutex and delivered outside it
// so a full subscriber buffer cannot deadlock the store. Delivery also
// respects ctx cancellation to avoid blocking forever or racing close.
func (e *K8sEventStore) Subscribe(ctx context.Context, t, r string, from int64) (<-chan port.Event, error) {
	e.mu.Lock()
	src := e.events[t+"/"+r]
	snap := make([]port.Event, 0, len(src))
	for _, v := range src {
		if v.Sequence > from {
			snap = append(snap, v)
		}
	}
	e.mu.Unlock()

	// Buffer at least the snapshot so the deliver goroutine cannot block on
	// a slow consumer while still holding no locks; extra room for fairness.
	buf := 64
	if len(snap) > buf {
		buf = len(snap)
	}
	ch := make(chan port.Event, buf)
	go func() {
		defer close(ch)
		for _, v := range snap {
			select {
			case ch <- v:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done()
	}()
	return ch, nil
}

// K8sEventStore.Read reads k8s event store.
//
// It returns an error if the operation fails or inputs are invalid.
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

// K8sEventStore.LatestSequence latest sequence.
//
// It returns an error if the operation fails or inputs are invalid.
func (e *K8sEventStore) LatestSequence(_ context.Context, t, r string) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return int64(len(e.events[t+"/"+r])), nil
}
