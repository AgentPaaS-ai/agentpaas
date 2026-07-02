// Package otel provides the in-process OpenTelemetry collector and SQLite
// WAL store for AgentPaaS observability data.
//
// The store accepts OTLP trace, log, and metric data and persists it to a
// SQLite database in WAL mode for concurrent read access. All attribute
// values are redacted via internal/logging before storage. Audit JSONL
// records are NEVER stored here - they remain in the hash-chained audit
// trail managed by internal/audit.
package otel
