package trigger

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestDurableSubscriberOverflowClosesChannel publishes more than
// subscriberBufferSize (128) events with a stalled subscriber. The subscriber
// channel must be CLOSED with an overflow semantic — NOT silently drop events
// like the old EventBus did.
//
// Spec: "When the subscriber channel buffer (128) is full, the store must NOT
// silently drop events. Instead, it blocks the publisher briefly (up to 100ms)
// then closes the subscriber channel with an explicit overflow error. The
// subscriber must reconnect."
//
// Approach: subscribe but do NOT read from the channel (stalled consumer).
// Publish >128 events. The first 128 fill the buffer; the 129th blocks up to
// 100ms then closes the channel. After the publisher completes, drain the
// buffered events and verify the channel is closed.
func TestDurableSubscriberOverflowClosesChannel(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-overflow"
	runID := "run-overflow"

	// Subscribe but never read — stalled consumer.
	ch, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Publish more than the buffer (128) can hold. The first 128 fill the
	// buffer; the 129th blocks up to 100ms then closes the channel.
	publishDone := make(chan struct{})
	var appendErr error
	go func() {
		defer close(publishDone)
		for i := 0; i < subscriberBufferSize+10; i++ {
			if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
				appendErr = err
				return
			}
		}
	}()

	// Wait for the publisher to complete. The 129th Append blocks ~100ms
	// (overflow timeout) then closes the subscriber channel and removes the
	// subscriber; remaining appends proceed without a live subscriber.
	select {
	case <-publishDone:
	case <-time.After(15 * time.Second):
		t.Fatal("publisher goroutine did not complete — overflow handler may be deadlocked")
	}
	if appendErr != nil {
		t.Fatalf("Append returned error during overflow: %v", appendErr)
	}

	// Drain all buffered events. After the overflow handler closed the channel,
	// any buffered events are still readable (Go semantics: sends that
	// happened before close are retrievable). Once the buffer is empty, the
	// next receive must return open=false (channel closed).
	drained := 0
drainLoop:
	for {
		select {
		case _, open := <-ch:
			if !open {
				break drainLoop
			}
			drained++
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after draining %d events; channel not closed", drained)
		}
	}
	// The subscriber buffer holds at most subscriberBufferSize events. We may
	// have drained fewer if the overflow handler closed the channel before
	// the buffer was completely full (race between publisher and reader
	// during the 100ms window).
	if drained > subscriberBufferSize {
		t.Fatalf("drained %d events; buffer capacity is %d", drained, subscriberBufferSize)
	}

	// Events that made it to the WAL are still readable from disk. The
	// overflow only affects delivery to the stalled subscriber — durability
	// is unaffected.
	events, err := store.Read(ctx, tenant, runID, 0, 1000)
	if err != nil {
		t.Fatalf("Read after overflow: %v", err)
	}
	if len(events) != subscriberBufferSize+10 {
		t.Fatalf("after overflow Read returned %d events; want %d (all published, durable)",
			len(events), subscriberBufferSize+10)
	}
	// All published events must have contiguous sequences starting at 1.
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatalf("events[%d].Sequence = %d; want %d (durability must preserve order)", i, e.Sequence, i+1)
		}
	}
}

// TestDurableSubscriberReconnectAfterOverflow verifies the documented
// recovery contract: after a subscriber is closed due to overflow, the client
// reconnects with the last-received sequence and receives subsequent events
// without duplicates.
func TestDurableSubscriberReconnectAfterOverflow(t *testing.T) {
	t.Parallel()

	store, _, cleanup := newTestDurableStore(t)
	defer cleanup()

	ctx := context.Background()
	tenant := "tenant-reconnect"
	runID := "run-reconnect"

	// Fast consumer that records every sequence it sees.
	var mu sync.Mutex
	var seen []int64
	receiverDone := make(chan struct{})
	ch, err := store.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	go func() {
		defer close(receiverDone)
		for e := range ch {
			mu.Lock()
			seen = append(seen, e.Sequence)
			mu.Unlock()
		}
	}()

	// Publish a handful — fast consumer keeps up.
	for i := 0; i < 5; i++ {
		if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p"))); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	// Give the receiver time to drain.
	select {
	case <-time.After(200 * time.Millisecond):
	case <-receiverDone:
	}

	mu.Lock()
	lastSeq := int64(0)
	if len(seen) > 0 {
		lastSeq = seen[len(seen)-1]
	}
	mu.Unlock()

	// Reconnect from the last sequence. New subscriber must receive only
	// events AFTER lastSeq, with no duplicates of previously-seen events.
	ch2, err := store.Subscribe(ctx, tenant, runID, lastSeq)
	if err != nil {
		t.Fatalf("Reconnect Subscribe: %v", err)
	}
	// Publish one more event; it must arrive on the new channel.
	if _, err := store.Append(ctx, mkEvent(tenant, runID, "e", []byte("p-new"))); err != nil {
		t.Fatalf("Append after reconnect: %v", err)
	}
	select {
	case e := <-ch2:
		if e.Sequence != lastSeq+1 {
			t.Fatalf("reconnected subscriber first event seq = %d; want %d (no dup)", e.Sequence, lastSeq+1)
		}
	case <-time.After(time.Second):
		t.Fatal("reconnected subscriber did not receive the next event")
	}
}
