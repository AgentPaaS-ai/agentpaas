// Package runtime — B29-T07 unit tests for TimingRecorder and ResourceCollector.
//
// These tests pin the core invariants the harness relies on:
//   - TimingRecorder records spans and computes p50/p95/p99.
//   - AgentPaaS overhead is separated from provider/tool time in the summary.
//   - ResourceCollector samples CPU/memory/PIDs at intervals.
package runtime

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTimingRecorderRecordsSpansAndPercentiles(t *testing.T) {
	t.Parallel()
	r := NewTimingRecorder()
	base := time.Now()
	// Five spans of 1,2,3,4,5 ns-durations for phase X (overhead).
	for i := 1; i <= 5; i++ {
		r.Record(PhaseQueueClaim, base, base.Add(time.Duration(i)), false)
	}
	s := r.Summary()
	ps, ok := s[PhaseQueueClaim]
	if !ok {
		t.Fatal("expected summary for PhaseQueueClaim")
	}
	if ps.Overhead.Count != 5 {
		t.Fatalf("expected 5 overhead samples, got %d", ps.Overhead.Count)
	}
	// nearest-rank p50 of {1,2,3,4,5}: rank=ceil(0.5*5)=3 → 3
	if ps.Overhead.P50 != 3 {
		t.Fatalf("p50 = %d, want 3", ps.Overhead.P50)
	}
	// p95: ceil(0.95*5)=5 → 5
	if ps.Overhead.P95 != 5 {
		t.Fatalf("p95 = %d, want 5", ps.Overhead.P95)
	}
	// p99: ceil(0.99*5)=5 → 5
	if ps.Overhead.P99 != 5 {
		t.Fatalf("p99 = %d, want 5", ps.Overhead.P99)
	}
	if ps.Overhead.Max != 5 {
		t.Fatalf("max = %d, want 5", ps.Overhead.Max)
	}
}

func TestTimingRecorderSeparatesOverheadFromProvider(t *testing.T) {
	t.Parallel()
	r := NewTimingRecorder()
	base := time.Now()
	// 3 overhead spans of 10ns + 2 provider spans of 100ns for model_call.
	r.Record(PhaseModelCall, base, base.Add(10), false)
	r.Record(PhaseModelCall, base, base.Add(10), false)
	r.Record(PhaseModelCall, base, base.Add(10), false)
	r.Record(PhaseModelCall, base, base.Add(100), true)
	r.Record(PhaseModelCall, base, base.Add(100), true)
	s := r.Summary()
	ps := s[PhaseModelCall]
	if ps.Overhead.Count != 3 {
		t.Fatalf("overhead count = %d, want 3", ps.Overhead.Count)
	}
	if ps.Provider.Count != 2 {
		t.Fatalf("provider count = %d, want 2", ps.Provider.Count)
	}
	if ps.Overhead.P50 != 10 {
		t.Fatalf("overhead p50 = %d, want 10", ps.Overhead.P50)
	}
	if ps.Provider.P50 != 100 {
		t.Fatalf("provider p50 = %d, want 100", ps.Provider.P50)
	}
	// Phases with only one class should have zero Count in the other.
	if ps2, ok := s[PhaseQueueClaim]; ok {
		if ps2.Overhead.Count != 0 || ps2.Provider.Count != 0 {
			t.Fatalf("unexpected PhaseQueueClaim entry: %+v", ps2)
		}
	}
}

func TestTimingRecorderClampsNegativeSpan(t *testing.T) {
	t.Parallel()
	r := NewTimingRecorder()
	base := time.Now()
	// end before start → clamped to zero duration.
	r.Record(PhaseCleanup, base.Add(10), base, false)
	s := r.Summary()
	if s[PhaseCleanup].Overhead.P50 != 0 {
		t.Fatalf("expected clamped zero duration, got %d", s[PhaseCleanup].Overhead.P50)
	}
	if s[PhaseCleanup].Overhead.Max != 0 {
		t.Fatalf("expected clamped zero max, got %d", s[PhaseCleanup].Overhead.Max)
	}
}

func TestTimingRecorderConcurrentSafe(t *testing.T) {
	t.Parallel()
	r := NewTimingRecorder()
	base := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Record(PhaseModelCall, base, base.Add(time.Duration(i)), i%2 == 0)
			}
		}()
	}
	wg.Wait()
	s := r.Summary()
	total := s[PhaseModelCall].Overhead.Count + s[PhaseModelCall].Provider.Count
	if total != 800 {
		t.Fatalf("total spans = %d, want 800", total)
	}
}

func TestResourceCollectorSamplesAtIntervals(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	count := 0
	sampler := func(_ context.Context) (ResourceSample, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return ResourceSample{
			Timestamp: time.Now().UTC(),
			CPUPercent: float64(count),
			MemoryMB:   int64(count),
			PIDs:       int64(count),
		}, nil
	}
	c := NewResourceCollector(sampler, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Let the ticker fire a few times.
	time.Sleep(20 * time.Millisecond)
	// Calling Start again WHILE RUNNING should error.
	if err := c.Start(ctx); err == nil {
		t.Fatal("expected error when starting an already-running collector")
	}
	summary := c.Stop()
	if summary.Count == 0 {
		t.Fatal("expected at least one sample, got 0")
	}
	if summary.CPUPercent.Max < 1 {
		t.Fatalf("expected max cpu >= 1, got %f", summary.CPUPercent.Max)
	}
	if summary.PIDs.Max < 1 {
		t.Fatalf("expected max pids >= 1, got %f", summary.PIDs.Max)
	}
	// Start after Stop is valid (fresh run).
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start after Stop should succeed: %v", err)
	}
	_ = c.Stop()
}

func TestResourceCollectorDirectSample(t *testing.T) {
	t.Parallel()
	c := NewResourceCollector(DefaultSampler, time.Millisecond)
	ctx := context.Background()
	s, err := c.Sample(ctx)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if s.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
	// Stop without Start returns summary of direct samples.
	summary := c.Stop()
	if summary.Count != 1 {
		t.Fatalf("expected 1 sample, got %d", summary.Count)
	}
}

func TestResourceCollectorStopIdempotent(t *testing.T) {
	t.Parallel()
	sampler := func(_ context.Context) (ResourceSample, error) {
		return ResourceSample{Timestamp: time.Now().UTC()}, nil
	}
	c := NewResourceCollector(sampler, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = c.Start(ctx)
	time.Sleep(5 * time.Millisecond)
	_ = c.Stop()
	// Second Stop is a no-op (returns empty summary, no panic).
	summary2 := c.Stop()
	if summary2.Count != 0 {
		t.Fatalf("second Stop should return empty summary, got %d", summary2.Count)
	}
}

func TestResourceCollectorSamplerError(t *testing.T) {
	t.Parallel()
	sampler := func(_ context.Context) (ResourceSample, error) {
		return ResourceSample{}, errSamplerFails
	}
	c := NewResourceCollector(sampler, time.Millisecond)
	_, err := c.Sample(context.Background())
	if err == nil {
		t.Fatal("expected sampler error to propagate")
	}
}

var errSamplerFails = errString("sampler fails")
