// Package runtime — B29-T07 baseline report writer.
//
// TestRecordBaseline runs all four conformance fixtures (cold-image,
// cached-cold, warm, resident) under the performance harness and writes a
// committed baseline report to internal/runtime/perf_baseline.json. The
// report contains p50/p95/p99 for each phase, separated by activation mode,
// with AgentPaaS overhead separated from provider/tool time.
//
// This is a RECORDED baseline, not a pass/fail gate. SLOs are approved only
// after the baseline exists. The test rewrites the file on every run so the
// committed numbers stay current with the harness; reviewers should diff the
// JSON to spot regressions, not assert specific values.
package runtime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// baselineReport is the JSON shape written to perf_baseline.json. The top
// level is a map from activation regime to its phase timings + resource
// summary; regimes are cold_image, cached_cold, warm, resident.
type baselineReport struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Fixtures    map[string]baselineRun `json:"fixtures"`
}

type baselineRun struct {
	Mode      port.ActivationMode `json:"mode"`
	Timings   TimingSummary       `json:"timings"`
	Resources ResourceSummary     `json:"resources"`
	Regime    string              `json:"regime"`
}

func TestRecordBaseline(t *testing.T) {
	t.Parallel()
	// This test writes a committed artifact; guard against parallel CI
	// collisions via the package-level serialiseMu.
	serialiseMu.Lock()
	defer serialiseMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fixed clock + per-fixture fresh sampler so the baseline JSON is
	// byte-stable across runs (no time.Now, no shared sampler state).
	harness := &PerfHarness{
		ResourceInterval: time.Millisecond,
		Now:              func() time.Time { return perfBase() },
	}

	fixtures := []struct {
		regime  string
		fixture PerfFixture
		mode    port.ActivationMode
	}{
		{"cold_image", coldImageFixture(&fakeProvider{modelLat: time.Millisecond, toolLat: time.Millisecond}), port.ActivationOnDemand},
		{"cached_cold", cachedColdFixture(&fakeProvider{modelLat: time.Millisecond, toolLat: time.Millisecond}), port.ActivationOnDemand},
		{"warm", warmFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationWarm},
		{"resident", residentFixture(&fakeProvider{modelLat: time.Millisecond}), port.ActivationResident},
	}

	report := baselineReport{
		GeneratedAt: perfBase(),
		Fixtures:    make(map[string]baselineRun, len(fixtures)),
	}
	for _, fx := range fixtures {
		f := fx.fixture
		sampler := &fakeSampler{}
		harness.Sampler = sampler.sample
		rep, err := harness.RunFixture(ctx, f, fx.mode)
		if err != nil {
			t.Fatalf("RunFixture %s failed: %v", fx.regime, err)
		}
		// cached_cold regime flag is set explicitly (harness does not infer).
		if fx.regime == "cached_cold" {
			MarkCachedCold(&rep)
		}
		report.Fixtures[fx.regime] = baselineRun{
			Mode:      fx.mode,
			Timings:   rep.Timings,
			Resources: rep.Resources,
			Regime:    fx.regime,
		}
	}

	// Validate the report has the contract-required phases for at least
	// the cold_image regime (the superset). SLOs are NOT asserted.
	cold := report.Fixtures["cold_image"]
	requiredPhases := []Phase{PhaseAdmission, PhaseSandboxStart, PhaseFirstModelToken, PhaseModelCall}
	for _, ph := range requiredPhases {
		if _, ok := cold.Timings[ph]; !ok {
			t.Fatalf("cold_image baseline missing phase %s", ph)
		}
	}
	// Overhead vs provider separation: model_call must have provider samples.
	if cold.Timings[PhaseModelCall].Provider.Count == 0 {
		t.Fatal("cold_image baseline missing provider samples for model_call")
	}
	if cold.Timings[PhaseFirstModelToken].Provider.Count == 0 {
		t.Fatal("cold_image baseline missing provider samples for first_model_token")
	}

	// Write the committed baseline JSON next to this test file. The test's
	// working directory is the package directory (internal/runtime), so a
	// plain filename lands in the right place.
	outPath := "perf_baseline.json"
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	// Ensure trailing newline.
	data = append(data, '\n')
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}
	t.Logf("wrote baseline report to %s (%d bytes)", outPath, len(data))
}
