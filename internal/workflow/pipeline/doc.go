// Package pipeline provides B34 pipeline and handoff conformance validation.
//
// This package validates declarative pipeline YAML and handoff envelopes
// against the B34 locked rules. It does NOT implement the durable scheduler,
// container launch, or SDK workflow_input/commit_handoff (those are T03–T04).
//
// Validation is pure: it operates on parsed types and bytes without side effects.
// Runtime pipeline admission remains fail-closed (PIPELINE_NOT_ENABLED) until
// later B34 tasks flip it.
package pipeline
