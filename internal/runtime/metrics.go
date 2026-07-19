// Package runtime — resource metrics collection (B29-T07).
//
// ResourceCollector samples the resource and throughput counters mandated by
// the performance and efficiency contract: idle/active CPU, memory, PIDs,
// container-seconds, gateway overhead, bytes, stored bytes, token/cache use,
// and cost per completed task. The collector is pluggable via a Sampler
// function so tests can inject deterministic samples without a real
// container runtime; the default sampler reads from runtime.Metrics (or a
// stub) and never blocks. Start/Stop run a periodic ticker on a background
// goroutine that the Stop call joins, so tests are race-free.
package runtime

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"time"
)

// ResourceSample is a single point-in-time resource reading. All fields are
// instantaneous values (not cumulative) except ContainerSeconds and the
// byte counters, which are cumulative where the underlying source exposes
// them cumulatively; the summary treats them as point values for the
// avg/p95/max rollups.
type ResourceSample struct {
	Timestamp            time.Time `json:"timestamp"`
	CPUPercent           float64   `json:"cpu_percent"`
	MemoryMB             int64     `json:"memory_mb"`
	PIDs                 int64     `json:"pids"`
	ContainerSeconds     float64   `json:"container_seconds"`
	GatewayOverheadBytes int64     `json:"gateway_overhead_bytes"`
	BytesIn              int64     `json:"bytes_in"`
	BytesOut             int64     `json:"bytes_out"`
	StoredBytes          int64     `json:"stored_bytes"`
	TokensUsed           int64     `json:"tokens_used"`
	CacheHit             bool      `json:"cache_hit"`
}

// Sampler returns one ResourceSample. Implementations must not block for
// appreciable time and must be safe for concurrent use. The default sampler
// reads Go runtime memory stats and the goroutine count as a deterministic
// proxy; production deployments inject a sampler that reads cgroup / Docker
// stats. The error is non-nil only when sampling is fundamentally not
// possible (e.g. the source is gone); transient gaps should return a sample
// with zeroed counters rather than an error.
type Sampler func(ctx context.Context) (ResourceSample, error)

// ResourceSummary is the avg/p95/max rollup of sampled resources over a run.
// Each scalar field has three corresponding summaries (Avg/P95/Max) so the
// baseline report can express both typical and tail behaviour, matching the
// contract's p50/p95/p99 framing for timings.
type ResourceSummary struct {
	Count            int           `json:"count"`
	CPUPercent       Rollup        `json:"cpu_percent"`
	MemoryMB         Rollup        `json:"memory_mb"`
	PIDs             Rollup        `json:"pids"`
	ContainerSeconds Rollup        `json:"container_seconds"`
	GatewayOverhead  Rollup        `json:"gateway_overhead_bytes"`
	BytesIn          Rollup        `json:"bytes_in"`
	BytesOut         Rollup        `json:"bytes_out"`
	StoredBytes      Rollup        `json:"stored_bytes"`
	TokensUsed       Rollup        `json:"tokens_used"`
	CacheHitCount    int           `json:"cache_hit_count"`
	CacheMissCount   int           `json:"cache_miss_count"`
	SampledFrom      time.Time     `json:"sampled_from"`
	SampledTo        time.Time     `json:"sampled_to"`
	Interval         time.Duration `json:"interval_ns"`
}

// Rollup is the avg/p95/max of a scalar resource counter.
type Rollup struct {
	Avg float64 `json:"avg"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

// ResourceCollector samples resources at a fixed interval during a run.
// The zero value is not usable; use NewResourceCollector. Sampling runs on
// a background goroutine started by Start and joined by Stop, so the
// collector is safe to use from the harness without extra synchronisation
// beyond calling Stop before reading the summary.
type ResourceCollector struct {
	mu        sync.Mutex
	sampler   Sampler
	interval  time.Duration
	samples   []ResourceSample
	done      chan struct{}
	stopped   chan struct{}
	startTime time.Time
	running   bool
}

// NewResourceCollector returns a collector that will call sampler every
// interval once started. interval must be > 0; sampler must be non-nil.
func NewResourceCollector(sampler Sampler, interval time.Duration) *ResourceCollector {
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	if sampler == nil {
		sampler = DefaultSampler
	}
	return &ResourceCollector{
		sampler:  sampler,
		interval: interval,
	}
}

// Sample collects a single sample synchronously and stores it. It is safe
// to call Sample directly when periodic sampling is not desired.
func (c *ResourceCollector) Sample(ctx context.Context) (ResourceSample, error) {
	s, err := c.sampler(ctx)
	if err != nil {
		return s, err
	}
	if s.Timestamp.IsZero() {
		s.Timestamp = time.Now().UTC()
	}
	c.mu.Lock()
	c.samples = append(c.samples, s)
	c.mu.Unlock()
	return s, nil
}

// Start begins periodic sampling on a background goroutine. The goroutine
// exits when ctx is cancelled or Stop is called. Start is idempotent for a
// fresh collector; calling Start twice without an intervening Stop returns
// an error rather than spawning a second ticker.
func (c *ResourceCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errCollectorRunning
	}
	c.running = true
	c.done = make(chan struct{})
	c.stopped = make(chan struct{})
	c.startTime = time.Now().UTC()
	c.mu.Unlock()

	go c.loop(ctx)
	return nil
}

// loop is the background sampling loop. It exits on ctx.Done() or when
// done is closed by Stop, and closes stopped to signal completion.
func (c *ResourceCollector) loop(ctx context.Context) {
	defer close(c.stopped)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-t.C:
			// Sample is best-effort here: an error just skips this tick.
			_, _ = c.Sample(ctx)
		}
	}
}

// Stop halts periodic sampling and returns a summary of everything
// collected so far. Stop joins the background goroutine and clears the
// sample buffer, so a subsequent Stop returns an empty summary. It is safe
// to call Stop without Start (returns a summary of any direct Sample
// calls). Calling Stop twice is safe; the second call returns an empty
// summary.
func (c *ResourceCollector) Stop() ResourceSummary {
	c.mu.Lock()
	if !c.running {
		// Not started: summarise any direct samples, then clear.
		summary := summarise(c.samples, c.interval)
		c.samples = nil
		c.mu.Unlock()
		return summary
	}
	c.running = false
	done := c.done
	stopped := c.stopped
	samples := append([]ResourceSample(nil), c.samples...)
	c.samples = nil
	c.mu.Unlock()

	close(done)
	<-stopped

	c.mu.Lock()
	// Pick up any final samples appended between the snapshot and the
	// goroutine actually exiting.
	samples = append(samples, c.samples...)
	c.samples = nil
	c.mu.Unlock()
	return summarise(samples, c.interval)
}

// errCollectorRunning is returned by Start when the collector is already
// sampling. It is a sentinel so callers can distinguish a logic bug from a
// transport error.
var errCollectorRunning = errString("resource collector already running")

// errString is a minimal error type so we avoid importing errors just for
// one sentinel; package runtime already uses errors elsewhere but this
// keeps the resource metrics file self-contained.
type errString string

func (e errString) Error() string { return string(e) }

// DefaultSampler is the default Sampler used when none is supplied. It
// reads the Go runtime memory stats and goroutine count as a deterministic,
// dependency-free proxy. Production code injects a cgroup/Docker sampler.
func DefaultSampler(_ context.Context) (ResourceSample, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ResourceSample{
		Timestamp:            time.Now().UTC(),
		CPUPercent:           0, // not available without a cgroup reader
		MemoryMB:             int64(ms.Alloc / 1024 / 1024),
		PIDs:                 int64(runtime.NumGoroutine()),
		ContainerSeconds:     0,
		GatewayOverheadBytes: 0,
		BytesIn:              0,
		BytesOut:             0,
		StoredBytes:          int64(ms.HeapInuse),
		TokensUsed:           0,
		CacheHit:             false,
	}, nil
}

// summarise computes the avg/p95/max rollups for a slice of samples.
func summarise(samples []ResourceSample, interval time.Duration) ResourceSummary {
	s := ResourceSummary{
		Count:    len(samples),
		Interval: interval,
	}
	if len(samples) == 0 {
		return s
	}
	s.SampledFrom = samples[0].Timestamp
	s.SampledTo = samples[len(samples)-1].Timestamp

	cpuVals := make([]float64, 0, len(samples))
	memVals := make([]float64, 0, len(samples))
	pidVals := make([]float64, 0, len(samples))
	csVals := make([]float64, 0, len(samples))
	gwVals := make([]float64, 0, len(samples))
	inVals := make([]float64, 0, len(samples))
	outVals := make([]float64, 0, len(samples))
	storedVals := make([]float64, 0, len(samples))
	tokVals := make([]float64, 0, len(samples))
	for _, x := range samples {
		cpuVals = append(cpuVals, x.CPUPercent)
		memVals = append(memVals, float64(x.MemoryMB))
		pidVals = append(pidVals, float64(x.PIDs))
		csVals = append(csVals, x.ContainerSeconds)
		gwVals = append(gwVals, float64(x.GatewayOverheadBytes))
		inVals = append(inVals, float64(x.BytesIn))
		outVals = append(outVals, float64(x.BytesOut))
		storedVals = append(storedVals, float64(x.StoredBytes))
		tokVals = append(tokVals, float64(x.TokensUsed))
		if x.CacheHit {
			s.CacheHitCount++
		} else {
			s.CacheMissCount++
		}
	}
	s.CPUPercent = rollup(cpuVals)
	s.MemoryMB = rollup(memVals)
	s.PIDs = rollup(pidVals)
	s.ContainerSeconds = rollup(csVals)
	s.GatewayOverhead = rollup(gwVals)
	s.BytesIn = rollup(inVals)
	s.BytesOut = rollup(outVals)
	s.StoredBytes = rollup(storedVals)
	s.TokensUsed = rollup(tokVals)
	return s
}

// rollup computes avg/p95/max of a float64 slice. p95 uses the same
// nearest-rank method as the timing distributions.
func rollup(vals []float64) Rollup {
	if len(vals) == 0 {
		return Rollup{}
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range vals {
		sum += v
	}
	n := len(sorted)
	rank := (95*n + 99) / 100
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return Rollup{
		Avg: sum / float64(n),
		P95: sorted[rank-1],
		Max: sorted[n-1],
	}
}
