// Package runtime — B29-T07 deterministic conformance fixtures.
//
// These fixtures exercise the performance harness under cold-image,
// cached-cold, warm, and resident activation modes using a fake provider
// (no live LLM). Each fixture records timing spans via the TimingRecorder
// and reads a deterministic fake resource sampler, so the baseline numbers
// are reproducible. The tests RECORD a baseline (p50/p95/p99) rather than
// asserting SLOs; SLOs are approved only after the baseline exists.
package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// fakeProvider simulates one deterministic model/tool call. It records a
// provider-time span with a FIXED duration (no real sleep) so the baseline
// JSON is byte-stable across runs. No network, no LLM.
type fakeProvider struct {
	modelLat    time.Duration
	toolLat     time.Duration
	calledModel int
	calledTool  int
}

func (f *fakeProvider) modelCall(rec *TimingRecorder, base time.Time) error {
	f.calledModel++
	end := base.Add(f.modelLat)
	rec.Record(PhaseModelCall, base, end, true)
	rec.Record(PhaseFirstModelToken, base, end, true)
	return nil
}

func (f *fakeProvider) toolCall(rec *TimingRecorder, base time.Time) error {
	f.calledTool++
	end := base.Add(f.toolLat)
	rec.Record(PhaseToolCall, base, end, true)
	return nil
}

// fakeSampler returns deterministic resource samples so the baseline JSON
// is byte-stable across runs. The timestamp is anchored to perfEpoch and
// each call increments the counters by a fixed step so summary rollups
// have non-trivial, reproducible values to report.
type fakeSampler struct {
	step int
}

func (s *fakeSampler) sample(_ context.Context) (ResourceSample, error) {
	s.step++
	return ResourceSample{
		Timestamp:            perfBase(),
		CPUPercent:           1.0 + float64(s.step),
		MemoryMB:             16 + int64(s.step),
		PIDs:                 2 + int64(s.step),
		ContainerSeconds:     float64(s.step) * 0.5,
		GatewayOverheadBytes: int64(s.step) * 64,
		BytesIn:              int64(s.step) * 1024,
		BytesOut:             int64(s.step) * 512,
		StoredBytes:          int64(s.step) * 2048,
		TokensUsed:           100 + int64(s.step),
		CacheHit:             s.step > 1,
	}, nil
}

// perfEpoch is the fixed base time used by all fixtures so recorded span
// durations are byte-stable across runs (the baseline JSON must not churn
// on every test run). Using a single epoch also makes the timing samples
// deterministic: every span's start/end is epoch+offset.
const perfEpoch = 1_700_000_000_000_000_000 // 2023-11-14T22:13:20Z ns

func perfBase() time.Time { return time.Unix(0, perfEpoch) }

// coldImageFixture builds a cold-image fixture: Setup simulates a real
// image pull (cache miss), Run starts the sandbox and calls the fake
// provider once. All spans are anchored to perfEpoch so durations are
// deterministic.
func coldImageFixture(fp *fakeProvider) PerfFixture {
	return PerfFixture{
		Name: "cold-image",
		Setup: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseImagePull, base, base.Add(2*time.Millisecond), false)
			return nil
		},
		Run: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseSandboxStart, base, base.Add(time.Millisecond), false)
			rec.Record(PhaseFirstProgress, base, base, false)
			if err := fp.modelCall(rec, base); err != nil {
				return err
			}
			return fp.toolCall(rec, base)
		},
		Teardown: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseCleanup, base, base, false)
			return nil
		},
	}
}

// cachedColdFixture reuses a cached image (Setup is a no-op cache hit),
// then runs the same sandbox+provider path.
func cachedColdFixture(fp *fakeProvider) PerfFixture {
	f := coldImageFixture(fp)
	f.Name = "cached-cold"
	f.Setup = func(_ context.Context, rec *TimingRecorder) error {
		base := perfBase()
		rec.Record(PhaseImagePull, base, base, true) // cache hit (instant)
		return nil
	}
	return f
}

// warmFixture starts from a warm sandbox (no image pull, sandbox already
// up): Setup is a no-op, Run skips sandbox_start and goes straight to the
// provider call.
func warmFixture(fp *fakeProvider) PerfFixture {
	return PerfFixture{
		Name: "warm",
		Run: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseFirstProgress, base, base, false)
			return fp.modelCall(rec, base)
		},
		Teardown: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseCleanup, base, base, false)
			return nil
		},
	}
}

// residentFixture simulates a resident sandbox (always-on): like warm but
// also records a gateway-prep span.
func residentFixture(fp *fakeProvider) PerfFixture {
	return PerfFixture{
		Name: "resident",
		Run: func(_ context.Context, rec *TimingRecorder) error {
			base := perfBase()
			rec.Record(PhaseGatewayPrep, base, base, false)
			rec.Record(PhaseFirstProgress, base, base, false)
			return fp.modelCall(rec, base)
		},
	}
}

func newHarness(s Sampler) *PerfHarness {
	return &PerfHarness{
		ResourceInterval: time.Millisecond,
		Sampler:          s,
		Now:              func() time.Time { return perfBase() },
	}
}

// runBaseline runs a fixture under a mode and returns the report. It
// asserts the run itself succeeded; baseline assertions on the numbers are
// left to the caller (this is a recorded baseline, not an SLO gate).
func runBaseline(t *testing.T, fixture PerfFixture, mode port.ActivationMode) PerfReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sampler := &fakeSampler{}
	h := newHarness(sampler.sample)
	rep, err := h.RunFixture(ctx, fixture, mode)
	if err != nil {
		t.Fatalf("RunFixture %s/%s failed: %v", fixture.Name, mode, err)
	}
	return rep
}

func TestColdImageBaseline(t *testing.T) {
	t.Parallel()
	rep := runBaseline(t, coldImageFixture(&fakeProvider{modelLat: time.Millisecond, toolLat: time.Millisecond}), port.ActivationOnDemand)
	if !rep.ColdStart {
		t.Fatal("cold-image run should set ColdStart")
	}
	ts := rep.Timings[PhaseSandboxStart]
	if ts.Overhead.Count == 0 {
		t.Fatalf("expected sandbox_start overhead samples, got %d", ts.Overhead.Count)
	}
	fm := rep.Timings[PhaseFirstModelToken]
	if fm.Provider.Count == 0 {
		t.Fatalf("expected first_model_token provider samples, got %d", fm.Provider.Count)
	}
	t.Logf("cold-image sandbox_start p50=%s p95=%s p99=%s",
		ts.Overhead.P50, ts.Overhead.P95, ts.Overhead.P99)
	t.Logf("cold-image first_model_token p50=%s p95=%s p99=%s (provider)",
		fm.Provider.P50, fm.Provider.P95, fm.Provider.P99)
}

func TestCachedColdBaseline(t *testing.T) {
	t.Parallel()
	rep := runBaseline(t, cachedColdFixture(&fakeProvider{modelLat: time.Millisecond, toolLat: time.Millisecond}), port.ActivationOnDemand)
	// harness doesn't infer cached-cold; mark it explicitly for the report.
	MarkCachedCold(&rep)
	if !rep.CachedCold {
		t.Fatal("cached-cold run should set CachedCold")
	}
	ip := rep.Timings[PhaseImagePull]
	if ip.Provider.Count == 0 {
		t.Fatalf("expected image_pull provider (cache hit) samples, got %d", ip.Provider.Count)
	}
	t.Logf("cached-cold image_pull p50=%s p95=%s p99=%s (provider/cache hit)",
		ip.Provider.P50, ip.Provider.P95, ip.Provider.P99)
}

func TestWarmBaseline(t *testing.T) {
	t.Parallel()
	rep := runBaseline(t, warmFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationWarm)
	if !rep.Warm {
		t.Fatal("warm run should set Warm")
	}
	if ss, ok := rep.Timings[PhaseSandboxStart]; ok {
		t.Fatalf("warm fixture should not record sandbox_start, got %d", ss.Overhead.Count)
	}
	fm := rep.Timings[PhaseFirstModelToken]
	if fm.Provider.Count == 0 {
		t.Fatalf("expected first_model_token provider samples, got %d", fm.Provider.Count)
	}
	t.Logf("warm first_model_token p50=%s p95=%s p99=%s (provider)",
		fm.Provider.P50, fm.Provider.P95, fm.Provider.P99)
}

func TestResidentBaseline(t *testing.T) {
	t.Parallel()
	rep := runBaseline(t, residentFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationResident)
	if !rep.Resident {
		t.Fatal("resident run should set Resident")
	}
	gp := rep.Timings[PhaseGatewayPrep]
	if gp.Overhead.Count == 0 {
		t.Fatalf("expected gateway_prep overhead samples, got %d", gp.Overhead.Count)
	}
	t.Logf("resident gateway_prep p50=%s p95=%s p99=%s",
		gp.Overhead.P50, gp.Overhead.P95, gp.Overhead.P99)
}

func TestPerfHarnessDeterministic(t *testing.T) {
	t.Parallel()
	// Two runs of the same fixture should produce the same phase set and
	// the same provider/overhead classification.
	r1 := runBaseline(t, warmFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationWarm)
	r2 := runBaseline(t, warmFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationWarm)
	if len(r1.Timings) != len(r2.Timings) {
		t.Fatalf("non-deterministic phase set: %d vs %d", len(r1.Timings), len(r2.Timings))
	}
	for ph, a := range r1.Timings {
		b, ok := r2.Timings[ph]
		if !ok {
			t.Fatalf("phase %s missing in second run", ph)
		}
		if a.Overhead.Count != b.Overhead.Count {
			t.Fatalf("phase %s overhead count differs: %d vs %d", ph, a.Overhead.Count, b.Overhead.Count)
		}
		if a.Provider.Count != b.Provider.Count {
			t.Fatalf("phase %s provider count differs: %d vs %d", ph, a.Provider.Count, b.Provider.Count)
		}
	}
}

func TestPerfHarnessRunErrorRecorded(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h := newHarness((&fakeSampler{}).sample)
	f := PerfFixture{
		Name: "fails",
		Run: func(ctx context.Context, rec *TimingRecorder) error {
			return errors.New("boom")
		},
	}
	rep, err := h.RunFixture(ctx, f, port.ActivationOnDemand)
	if err == nil {
		t.Fatal("expected error from RunFixture when Run fails")
	}
	if rep.Err == "" {
		t.Fatal("expected report.Err to record the failure")
	}
}
