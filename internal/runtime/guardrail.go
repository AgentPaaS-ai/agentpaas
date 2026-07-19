package runtime

// GuardrailMode selects how a governed model stream releases output to
// observers. The mode is chosen by SelectGuardrail from the configured set of
// response filters; a strict whole-response filter forces buffered_release.
type GuardrailMode string

const (
	// GuardrailBufferedRelease holds the entire response until it is
	// committed, then emits a single response_completed event. No output_delta
	// events are emitted. This is the default for any non-stream-safe filter
	// set and for strict whole-response guardrails.
	GuardrailBufferedRelease GuardrailMode = "buffered_release"
	// GuardrailIncrementalRelease emits output_delta events as tokens arrive,
	// but partial output is marked uncommitted until response_completed. Only
	// permitted when every configured response filter declares stream-safe
	// incremental semantics.
	GuardrailIncrementalRelease GuardrailMode = "incremental_release"
)

// ResponseFilter is a configured response filter. A filter that can be
// applied incrementally to a partial response (without buffering the whole
// response) declares stream-safe semantics via IsStreamSafe. A strict
// whole-response filter (e.g. one that must see the entire output before it
// can decide) returns false.
type ResponseFilter interface {
	// IsStreamSafe reports whether the filter can be applied incrementally to
	// partial output without buffering the whole response. Filters that must
	// inspect the whole response return false.
	IsStreamSafe() bool
	// FilterName returns a stable, human-readable name for the filter, used
	// in diagnostics and audit. Must be non-empty.
	FilterName() string
}

// SelectGuardrail chooses the guardrail mode for a configured set of response
// filters. If ANY filter does not declare stream-safe incremental semantics
// (IsStreamSafe() == false), the stream must buffer the whole response
// (buffered_release). Only if ALL filters are stream-safe is
// incremental_release permitted.
//
// A nil or empty filter set is treated as "no incremental filters configured"
// and selects buffered_release: the absence of an explicit stream-safe
// declaration never defaults to incremental release (fail closed).
func SelectGuardrail(filters []ResponseFilter) GuardrailMode {
	if len(filters) == 0 {
		// No filters configured: no explicit stream-safe declaration, fail
		// closed to buffered release.
		return GuardrailBufferedRelease
	}
	for _, f := range filters {
		if f == nil {
			// A nil filter cannot declare stream-safe semantics; fail closed.
			return GuardrailBufferedRelease
		}
		if !f.IsStreamSafe() {
			return GuardrailBufferedRelease
		}
	}
	// Every filter declared stream-safe incremental semantics.
	return GuardrailIncrementalRelease
}
