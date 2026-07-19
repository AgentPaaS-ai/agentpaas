// Package runtime — performance conformance harness (B29-T07).
//
// PerfHarness runs a fixture under a chosen activation mode and records the
// timing spans and resource metrics mandated by the performance and
// efficiency contract. The harness itself is deterministic: it invokes
// the fixture's Setup/Run/Teardown callbacks, records spans around each
// contract phase using a TimingRecorder, and drives a ResourceCollector in
// parallel. Fixtures supply their own (fake) provider/tool implementations
// so no live LLM is required; the harness only orchestrates timing and
// resource capture.
package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// PerfFixture is a deterministic unit of work to be measured. Setup primes
// any caches/images the fixture needs (e.g. pre-warms a sandbox for the
// warm mode), Run performs the work, and Teardown releases resources. Each
// callback may be nil. Run is invoked exactly once; Setup/Teardown at most
// once. The callbacks must be deterministic: same inputs ⇒ same outputs and
// roughly the same wall-clock time, so the baseline is reproducible.
type PerfFixture struct {
	Name    string
	Setup   func(ctx context.Context, rec *TimingRecorder) error
	Run     func(ctx context.Context, rec *TimingRecorder) error
	Teardown func(ctx context.Context, rec *TimingRecorder) error
}

// PerfReport is the result of running one fixture under one activation
// mode. The mode flags (ColdStart/CachedCold/Warm/Resident) classify which
// baseline regime this run represents, mirroring the contract's
// cold-image / cached-cold / warm / resident modes. Exactly one flag is
// expected to be true per run; the flags are kept explicit so the baseline
// report JSON reads clearly without needing to decode the ActivationMode.
type PerfReport struct {
	FixtureName string         `json:"fixture_name"`
	Mode        port.ActivationMode `json:"mode"`
	Timings     TimingSummary  `json:"timings"`
	Resources   ResourceSummary `json:"resources"`
	ColdStart   bool           `json:"cold_start"`
	CachedCold  bool           `json:"cached_cold"`
	Warm        bool           `json:"warm"`
	Resident    bool           `json:"resident"`
	Duration    time.Duration  `json:"duration_ns"`
	Err         string         `json:"err,omitempty"`
}

// PerfHarness orchestrates timing + resource capture for a fixture run. It
// is safe to reuse across fixtures, but each RunFixture call must complete
// before the next starts (the harness holds a single collector + recorder
// per run). Concurrency between fixtures is the caller's responsibility and
// is not needed for the baseline.
type PerfHarness struct {
	// ResourceInterval is the sampling interval for the ResourceCollector.
	// Defaults to 5ms when zero; tests use a small value to keep runs fast.
	ResourceInterval time.Duration
	// Sampler overrides the default resource sampler. nil uses
	// DefaultSampler. Inject a fake sampler for deterministic tests.
	Sampler Sampler
	// Now, when non-nil, replaces time.Now for the admission span and run
	// duration. Inject a fixed clock for byte-stable baseline reports.
	Now func() time.Time
}

// NewPerfHarness returns a harness with the default sampler and a 5ms
// resource sampling interval.
func NewPerfHarness() *PerfHarness {
	return &PerfHarness{ResourceInterval: 5 * time.Millisecond}
}

func (h *PerfHarness) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// RunFixture runs fixture under mode and returns a PerfReport. The harness:
//  1. Starts a ResourceCollector in the background.
//  2. Records an admission span (AgentPaaS overhead) covering the whole run.
//  3. Calls Setup, Run, Teardown, recording a span around each callback.
//  4. Stops the collector and returns the combined report.
//
// If a callback returns an error, the harness records the error string in
// the report and still runs Teardown so resources are released. The error
// is also returned to the caller.
func (h *PerfHarness) RunFixture(
	ctx context.Context,
	fixture PerfFixture,
	mode port.ActivationMode,
) (PerfReport, error) {
	rec := NewTimingRecorder()
	collector := NewResourceCollector(h.Sampler, h.ResourceInterval)

	report := PerfReport{
		FixtureName: fixture.Name,
		Mode:        mode,
	}
	switch mode {
	case port.ActivationOnDemand:
		report.ColdStart = true
	case port.ActivationWarm:
		report.Warm = true
	case port.ActivationResident:
		report.Resident = true
	default:
		// Unknown mode: treat as cold (on_demand) and surface the issue.
		report.ColdStart = true
	}

	// Cached-cold is a sub-regime of on_demand (image cached, no warm
	// process). Fixtures that want to mark cached-cold set a sentinel via
	// the fixture name prefix; the harness does not infer it. Callers may
	// also flip the flag on the returned report before serialising.

	if err := collector.Start(ctx); err != nil {
		report.Err = err.Error()
		return report, err
	}
	runStart := h.now()
	runEnd := runStart
	defer func() { report.Duration = runEnd.Sub(runStart) }()

	// Admission span covers the entire harness orchestration (overhead).
	admissionStart := runStart
	callErr := h.runPhases(ctx, fixture, rec)
	admissionEnd := h.now()
	runEnd = admissionEnd
	rec.Record(PhaseAdmission, admissionStart, admissionEnd, false)

	res := collector.Stop()
	report.Timings = rec.Summary()
	report.Resources = res

	if callErr != nil {
		report.Err = callErr.Error()
		return report, callErr
	}
	return report, nil
}

// runPhases invokes Setup/Run/Teardown. Fixtures record their own phase
// spans (they know which phases apply to their regime — e.g. a warm
// fixture records no image_pull/sandbox_start); the harness does not
// auto-record phase spans around the callbacks to avoid double-counting.
// An error from Run is returned after Teardown runs; an error from Setup
// is returned immediately (there is nothing to teardown). Teardown errors
// are returned only when Setup/Run succeeded.
func (h *PerfHarness) runPhases(
	ctx context.Context,
	fixture PerfFixture,
	rec *TimingRecorder,
) error {
	if fixture.Setup != nil {
		if err := fixture.Setup(ctx, rec); err != nil {
			return errors.Join(ErrPerfSetupFailed, err)
		}
	}
	if fixture.Run != nil {
		if err := fixture.Run(ctx, rec); err != nil {
			// Still attempt teardown so resources are released.
			_ = h.teardown(ctx, fixture, rec)
			return errors.Join(ErrPerfRunFailed, err)
		}
	}
	if err := h.teardown(ctx, fixture, rec); err != nil {
		return errors.Join(ErrPerfTeardownFailed, err)
	}
	return nil
}

func (h *PerfHarness) teardown(
	ctx context.Context,
	fixture PerfFixture,
	rec *TimingRecorder,
) error {
	if fixture.Teardown == nil {
		return nil
	}
	return fixture.Teardown(ctx, rec)
}

// PerfHarness error sentinels.
var (
	ErrPerfSetupFailed    = errors.New("perf: fixture setup failed")
	ErrPerfRunFailed      = errors.New("perf: fixture run failed")
	ErrPerfTeardownFailed = errors.New("perf: fixture teardown failed")
)

// MarkCachedCold flips a PerfReport's regime flags to cached-cold. It is a
// helper for fixtures that run the on_demand path against a pre-populated
// image cache; the harness does not infer cached-cold from runtime state
// because the contract treats it as a distinct baseline regime.
func MarkCachedCold(r *PerfReport) {
	r.ColdStart = false
	r.CachedCold = true
}

// serialiseMu protects the baseline report writer below from concurrent
// invocations of TestRecordBaseline; tests are hermetic but the guard keeps
// the JSON writer robust under -race if a future test runs in parallel.
var serialiseMu sync.Mutex
