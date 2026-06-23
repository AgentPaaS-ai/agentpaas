package otel

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 1

// migrate runs database migrations to bring the schema to currentSchemaVersion.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS otel_schema_version (
			version INTEGER NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}

	var version int
	err := db.QueryRow("SELECT version FROM otel_schema_version LIMIT 1").Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("read schema version: %w", err)
	}

	for next := version + 1; next <= currentSchemaVersion; next++ {
		switch next {
		case 1:
			if err := migration1(db); err != nil {
				return fmt.Errorf("migration %d: %w", next, err)
			}
		default:
			return fmt.Errorf("unknown migration version %d", next)
		}

		if _, err := db.Exec("DELETE FROM otel_schema_version"); err != nil {
			return fmt.Errorf("clear schema version: %w", err)
		}
		if _, err := db.Exec("INSERT INTO otel_schema_version (version) VALUES (?)", next); err != nil {
			return fmt.Errorf("write schema version: %w", err)
		}
	}

	return nil
}

// migration1 creates the initial schema.
func migration1(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS otel_spans (
			id INTEGER PRIMARY KEY,
			trace_id TEXT NOT NULL,
			span_id TEXT NOT NULL,
			parent_span_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			attributes TEXT NOT NULL,
			status TEXT NOT NULL,
			status_code TEXT NOT NULL,
			resource TEXT NOT NULL,
			scope TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON otel_spans(trace_id);`,
		`CREATE INDEX IF NOT EXISTS idx_spans_start_time ON otel_spans(start_time);`,
		`CREATE TABLE IF NOT EXISTS otel_logs (
			id INTEGER PRIMARY KEY,
			timestamp INTEGER NOT NULL,
			trace_id TEXT NOT NULL,
			span_id TEXT NOT NULL,
			severity TEXT NOT NULL,
			body TEXT NOT NULL,
			attributes TEXT NOT NULL,
			resource TEXT NOT NULL,
			scope TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON otel_logs(timestamp);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_trace_id ON otel_logs(trace_id);`,
		`CREATE TABLE IF NOT EXISTS otel_metrics (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			value REAL NOT NULL,
			attributes TEXT NOT NULL,
			resource TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_name ON otel_metrics(name);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON otel_metrics(timestamp);`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("exec schema statement: %w", err)
		}
	}
	return nil
}
