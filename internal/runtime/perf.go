// Package runtime — performance conformance harness (B29-T07).
//
// This file implements TimingSpan / TimingRecorder: a lightweight recorder
// for the per-invocation timing spans mandated by the performance and
// efficiency contract. Each span is tagged with its phase and whether it
// represents provider/tool time (billable, external) or AgentPaaS overhead
// (internal). Summary computes p50/p95/p99 distributions per phase, separating
// the two classes so that provider/tool time is reported separately from
// AgentPaaS overhead, as required by the contract.
//
// The recorder is deterministic: it does not call any live provider, read
// the clock beyond what callers pass in, or perform I/O. All spans are
// supplied by the caller (the harness/fixtures), which keeps the unit tests
// hermetic and reproducible.
package runtime

import (
	"sort"
	"sync"
	"time"
)

// Phase is a named timing phase of a single invocation. The set of phases
// matches the performance and efficiency contract: authentication/policy/
// admission; queue/claim; image pull or cache hit; network/gateway
// preparation; sandbox start and harness readiness; time to first progress
// and first model token; each model/tool call and artifact transfer;
// wait/wakeup, child/stage transition where applicable; terminal-event
// publication and cleanup.
type Phase string

// Canonical phase identifiers. Callers may use these constants when
// recording spans; arbitrary strings are also accepted (the summary is a
// map keyed by Phase) but using the constants keeps reports comparable
// across runs and modes.
const (
	PhaseAdmission        Phase = "admission"
	PhaseQueueClaim       Phase = "queue_claim"
	PhaseImagePull        Phase = "image_pull"
	PhaseGatewayPrep      Phase = "gateway_prep"
	PhaseSandboxStart     Phase = "sandbox_start"
	PhaseFirstProgress    Phase = "first_progress"
	PhaseFirstModelToken  Phase = "first_model_token"
	PhaseModelCall        Phase = "model_call"
	PhaseToolCall         Phase = "tool_call"
	PhaseArtifactTransfer Phase = "artifact_transfer"
	PhaseWaitWakeup       Phase = "wait_wakeup"
	PhaseChildTransition  Phase = "child_transition"
	PhaseTerminalEvent    Phase = "terminal_event"
	PhaseCleanup          Phase = "cleanup"
)

// TimingSpan is a single measured interval for one phase of one invocation.
// ProviderTime is true when the interval represents provider/tool time
// (billable, external — e.g. a model call's server-side latency) and false
// when it represents AgentPaaS overhead. The two classes are aggregated
// separately by Summary so the baseline report can attribute cost.
type TimingSpan struct {
	Phase        Phase
	Start        time.Time
	End          time.Time
	ProviderTime bool
}

// Duration returns End - Start. A span with a zero End is treated as
// zero-duration so callers can record "instant" events safely.
func (s TimingSpan) Duration() time.Duration {
	if s.End.IsZero() || s.Start.IsZero() {
		return 0
	}
	return s.End.Sub(s.Start)
}

// DistStats summarises a set of durations with p50/p95/p99 and the max.
// All fields are in nanoseconds-as-duration. Count is the number of
// observations aggregated into this distribution.
type DistStats struct {
	Count int           `json:"count"`
	P50   time.Duration `json:"p50_ns"`
	P95   time.Duration `json:"p95_ns"`
	P99   time.Duration `json:"p99_ns"`
	Max   time.Duration `json:"max_ns"`
}

// PhaseStats holds the separated overhead and provider/tool distributions
// for a single phase. Overhead aggregates spans with ProviderTime=false;
// Provider aggregates spans with ProviderTime=true.
type PhaseStats struct {
	Overhead DistStats `json:"overhead"`
	Provider DistStats `json:"provider"`
}

// TimingSummary is the per-phase summary returned by TimingRecorder.Summary.
// Keys are Phase values; each value separates AgentPaaS overhead from
// provider/tool time.
type TimingSummary map[Phase]PhaseStats

// TimingRecorder collects timing spans for a single run and computes
// p50/p95/p99 distributions per phase, separating AgentPaaS overhead from
// provider/tool time. It is safe for concurrent use by the recording
// goroutine and the Summary reader (the harness records from a single
// goroutine, but the mutex keeps the API safe).
type TimingRecorder struct {
	mu    sync.Mutex
	spans []TimingSpan
}

// NewTimingRecorder returns an empty recorder.
func NewTimingRecorder() *TimingRecorder {
	return &TimingRecorder{}
}

// Record appends a timing span. start and end are the wall-clock bounds of
// the span; providerTime is true if the span measures provider/tool time
// rather than AgentPaaS overhead. A span with end before start is clamped
// to zero duration (and recorded as such) so a caller bug cannot produce
// negative durations in the summary.
func (r *TimingRecorder) Record(phase Phase, start, end time.Time, providerTime bool) {
	if end.Before(start) {
		end = start
	}
	r.mu.Lock()
	r.spans = append(r.spans, TimingSpan{
		Phase:        phase,
		Start:        start,
		End:          end,
		ProviderTime: providerTime,
	})
	r.mu.Unlock()
}

// Summary computes per-phase p50/p95/p99 distributions, separating AgentPaaS
// overhead (ProviderTime=false) from provider/tool time (ProviderTime=true).
// Phases with no recorded spans are omitted from the map; callers that need
// a stable key set should range over the canonical Phase constants.
func (r *TimingRecorder) Summary() TimingSummary {
	r.mu.Lock()
	spansCopy := append([]TimingSpan(nil), r.spans...)
	r.mu.Unlock()

	// Bucket durations per phase and per class.
	type key struct {
		phase        Phase
		providerTime bool
	}
	buckets := make(map[key][]time.Duration)
	for _, s := range spansCopy {
		k := key{phase: s.Phase, providerTime: s.ProviderTime}
		buckets[k] = append(buckets[k], s.Duration())
	}

	summary := make(TimingSummary, len(buckets))
	for k, durs := range buckets {
		stats := distStats(durs)
		ps, ok := summary[k.phase]
		if !ok {
			ps = PhaseStats{}
		}
		if k.providerTime {
			ps.Provider = stats
		} else {
			ps.Overhead = stats
		}
		summary[k.phase] = ps
	}
	return summary
}

// distStats computes p50/p95/p99/max for a slice of durations using the
// nearest-rank method. An empty input returns a zero DistStats.
func distStats(durs []time.Duration) DistStats {
	if len(durs) == 0 {
		return DistStats{}
	}
	sorted := append([]time.Duration(nil), durs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	max := sorted[len(sorted)-1]
	return DistStats{
		Count: len(sorted),
		P50:   percentileRank(sorted, 50),
		P95:   percentileRank(sorted, 95),
		P99:   percentileRank(sorted, 99),
		Max:   max,
	}
}

// percentileRank returns the nearest-rank percentile p (1..100) from a
// pre-sorted ascending slice. It uses the standard formula:
// rank = ceil(p/100 * n), index = rank-1, clamped to [0, n-1].
func percentileRank(sorted []time.Duration, p int) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := (p*n + 99) / 100 // ceil(p/100 * n) via integer math
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}
