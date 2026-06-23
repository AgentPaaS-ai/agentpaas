package otel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	_ "modernc.org/sqlite"
)

// Store is an in-process OTLP collector that persists traces, logs, and
// metrics to a SQLite WAL database. It is safe for concurrent use.
// Audit JSONL is NEVER stored here - only OTel telemetry data.
type Store struct {
	db   *sql.DB
	mu   sync.RWMutex
	path string
}

// SpanRecord is a flattened trace span stored in the otel_spans table.
type SpanRecord struct {
	ID           int64     `json:"id"`
	TraceID      string    `json:"trace_id"`
	SpanID       string    `json:"span_id"`
	ParentSpanID string    `json:"parent_span_id,omitempty"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Attributes   string    `json:"attributes"` // JSON, redacted
	Status       string    `json:"status"`
	StatusCode   string    `json:"status_code"`
	Resource     string    `json:"resource"` // JSON, redacted
	Scope        string    `json:"scope"`    // JSON
}

// LogRecord is a flattened OTel log stored in the otel_logs table.
type LogRecord struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	TraceID    string    `json:"trace_id,omitempty"`
	SpanID     string    `json:"span_id,omitempty"`
	Severity   string    `json:"severity"`
	Body       string    `json:"body"`       // redacted
	Attributes string    `json:"attributes"` // JSON, redacted
	Resource   string    `json:"resource"`   // JSON, redacted
	Scope      string    `json:"scope"`      // JSON
}

// MetricRecord is a flattened OTel metric data point stored in otel_metrics.
type MetricRecord struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"` // gauge, sum, histogram
	Timestamp  time.Time `json:"timestamp"`
	Value      float64   `json:"value"`
	Attributes string    `json:"attributes"` // JSON, redacted
	Resource   string    `json:"resource"`   // JSON, redacted
}

// NewStore opens (or creates) the OTel SQLite store at dbPath.
// It enables WAL mode and sets busy_timeout for concurrent reader access.
func NewStore(ctx context.Context, dbPath string) (*Store, error) {
	db, err := openSQLiteDB(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	return &Store{db: db, path: dbPath}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	db := s.db
	s.db = nil
	return db.Close()
}

// IngestTraces stores OTLP trace data (ptrace.Traces) into the database.
// All attribute values are redacted via logging.Redact before storage.
func (s *Store) IngestTraces(ctx context.Context, traces ptrace.Traces) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin trace tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_spans (
			trace_id, span_id, parent_span_id, name, kind, start_time, end_time,
			attributes, status, status_code, resource, scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare span insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	resourceSpans := traces.ResourceSpans()
	for i := 0; i < resourceSpans.Len(); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rs := resourceSpans.At(i)
		resource, err := mapJSON(rs.Resource().Attributes())
		if err != nil {
			return fmt.Errorf("marshal span resource: %w", err)
		}
		scopeSpans := rs.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			ss := scopeSpans.At(j)
			scope, err := scopeJSON(ss.Scope())
			if err != nil {
				return fmt.Errorf("marshal span scope: %w", err)
			}
			spans := ss.Spans()
			for k := 0; k < spans.Len(); k++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				span := spans.At(k)
				attributes, err := mapJSON(span.Attributes())
				if err != nil {
					return fmt.Errorf("marshal span attributes: %w", err)
				}
				if _, err := stmt.ExecContext(
					ctx,
					traceIDString(span.TraceID()),
					spanIDString(span.SpanID()),
					spanIDString(span.ParentSpanID()),
					span.Name(),
					span.Kind().String(),
					timeToUnixNano(timestampToTime(span.StartTimestamp())),
					timeToUnixNano(timestampToTime(span.EndTimestamp())),
					attributes,
					redactString(span.Status().Message()),
					span.Status().Code().String(),
					resource,
					scope,
				); err != nil {
					return fmt.Errorf("insert span: %w", err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trace tx: %w", err)
	}
	return nil
}

// IngestLogs stores OTLP log data (plog.Logs) into the database.
// All body and attribute values are redacted via logging.Redact before storage.
func (s *Store) IngestLogs(ctx context.Context, logs plog.Logs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin log tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_logs (
			timestamp, trace_id, span_id, severity, body, attributes, resource, scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare log insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	resourceLogs := logs.ResourceLogs()
	for i := 0; i < resourceLogs.Len(); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rl := resourceLogs.At(i)
		resource, err := mapJSON(rl.Resource().Attributes())
		if err != nil {
			return fmt.Errorf("marshal log resource: %w", err)
		}
		scopeLogs := rl.ScopeLogs()
		for j := 0; j < scopeLogs.Len(); j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			sl := scopeLogs.At(j)
			scope, err := scopeJSON(sl.Scope())
			if err != nil {
				return fmt.Errorf("marshal log scope: %w", err)
			}
			records := sl.LogRecords()
			for k := 0; k < records.Len(); k++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				record := records.At(k)
				attributes, err := mapJSON(record.Attributes())
				if err != nil {
					return fmt.Errorf("marshal log attributes: %w", err)
				}
				if _, err := stmt.ExecContext(
					ctx,
					timeToUnixNano(logTimestamp(record)),
					traceIDString(record.TraceID()),
					spanIDString(record.SpanID()),
					logSeverity(record),
					logBody(record.Body()),
					attributes,
					resource,
					scope,
				); err != nil {
					return fmt.Errorf("insert log: %w", err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit log tx: %w", err)
	}
	return nil
}

// IngestMetrics stores OTLP metric data (pmetric.Metrics) into the database.
// All attribute values are redacted via logging.Redact before storage.
func (s *Store) IngestMetrics(ctx context.Context, metrics pmetric.Metrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metric tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_metrics (name, type, timestamp, value, attributes, resource)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare metric insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	resourceMetrics := metrics.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rm := resourceMetrics.At(i)
		resource, err := mapJSON(rm.Resource().Attributes())
		if err != nil {
			return fmt.Errorf("marshal metric resource: %w", err)
		}
		scopeMetrics := rm.ScopeMetrics()
		for j := 0; j < scopeMetrics.Len(); j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			ms := scopeMetrics.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				if err := ingestMetric(ctx, stmt, ms.At(k), resource); err != nil {
					return err
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metric tx: %w", err)
	}
	return nil
}

// QuerySpans returns spans matching the filter, ordered by start_time.
// limit <= 0 means no limit.
func (s *Store) QuerySpans(ctx context.Context, runID string, limit int) ([]SpanRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, trace_id, span_id, parent_span_id, name, kind, start_time, end_time,
			attributes, status, status_code, resource, scope
		FROM otel_spans
	`
	args := make([]any, 0, 2)
	if runID != "" {
		query += " WHERE trace_id = ?"
		args = append(args, runID)
	}
	query += " ORDER BY start_time"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query spans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []SpanRecord
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var record SpanRecord
		var startTime, endTime int64
		if err := rows.Scan(
			&record.ID,
			&record.TraceID,
			&record.SpanID,
			&record.ParentSpanID,
			&record.Name,
			&record.Kind,
			&startTime,
			&endTime,
			&record.Attributes,
			&record.Status,
			&record.StatusCode,
			&record.Resource,
			&record.Scope,
		); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		record.StartTime = unixNanoToTime(startTime)
		record.EndTime = unixNanoToTime(endTime)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate spans: %w", err)
	}
	return records, nil
}

// QueryLogs returns logs matching the filter, ordered by timestamp.
func (s *Store) QueryLogs(ctx context.Context, runID string, limit int) ([]LogRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, timestamp, trace_id, span_id, severity, body, attributes, resource, scope
		FROM otel_logs
	`
	args := make([]any, 0, 2)
	if runID != "" {
		query += " WHERE trace_id = ?"
		args = append(args, runID)
	}
	query += " ORDER BY timestamp"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []LogRecord
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var record LogRecord
		var timestamp int64
		if err := rows.Scan(
			&record.ID,
			&timestamp,
			&record.TraceID,
			&record.SpanID,
			&record.Severity,
			&record.Body,
			&record.Attributes,
			&record.Resource,
			&record.Scope,
		); err != nil {
			return nil, fmt.Errorf("scan log: %w", err)
		}
		record.Timestamp = unixNanoToTime(timestamp)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate logs: %w", err)
	}
	return records, nil
}

// Prune deletes OTel data older than the retention period.
// This ONLY prunes OTel tables - audit JSONL is NEVER pruned by this method.
func (s *Store) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := time.Now().Add(-retention).UTC().UnixNano()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin prune tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var deleted int64
	for _, statement := range []string{
		"DELETE FROM otel_spans WHERE start_time < ?",
		"DELETE FROM otel_logs WHERE timestamp < ?",
		"DELETE FROM otel_metrics WHERE timestamp < ?",
	} {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, statement, cutoff)
		if err != nil {
			return 0, fmt.Errorf("prune otel table: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("read prune rows affected: %w", err)
		}
		deleted += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit prune tx: %w", err)
	}
	return deleted, nil
}

// Checkpoint forces a WAL checkpoint (PASSIVE mode).
func (s *Store) Checkpoint(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint wal: %w", err)
	}
	return nil
}

// RecoverFromCorruption attempts to recover a corrupted database by
// creating a fresh database. It returns the number of records recovered
// (0 if the DB was unrecoverable and recreated empty).
func (s *Store) RecoverFromCorruption(ctx context.Context) (recovered int64, err error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	db := s.db
	openedForCheck := false
	if db == nil {
		db, err = sql.Open("sqlite", s.path)
		if err != nil {
			return 0, fmt.Errorf("open sqlite for integrity check: %w", err)
		}
		openedForCheck = true
	}

	var result string
	checkErr := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result)
	if checkErr == nil && result == "ok" {
		if openedForCheck {
			s.db = db
			if err := migrate(s.db); err != nil {
				_ = db.Close()
				s.db = nil
				return 0, fmt.Errorf("migrate healthy db: %w", err)
			}
		}
		total, err := countTelemetryRecords(ctx, db)
		if err != nil {
			return 0, err
		}
		return total, nil
	}

	if err := db.Close(); err != nil {
		return 0, fmt.Errorf("close corrupt db: %w", err)
	}
	if s.db == db {
		s.db = nil
	}

	if err := renameCorruptFiles(s.path); err != nil {
		return 0, err
	}

	fresh, err := openSQLiteDB(ctx, s.path)
	if err != nil {
		return 0, fmt.Errorf("create fresh sqlite: %w", err)
	}
	s.db = fresh
	return 0, nil
}

func timeToUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}

func timestampToTime(ts pcommon.Timestamp) time.Time {
	if ts == 0 {
		return time.Time{}
	}
	return ts.AsTime().UTC()
}

func unixNanoToTime(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func openSQLiteDB(ctx context.Context, dbPath string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func mapJSON(attrs pcommon.Map) (string, error) {
	return redactJSON(attrs.AsRaw())
}

func scopeJSON(scope pcommon.InstrumentationScope) (string, error) {
	return redactJSON(map[string]any{
		"name":       scope.Name(),
		"version":    scope.Version(),
		"attributes": scope.Attributes().AsRaw(),
	})
}

func traceIDString(traceID pcommon.TraceID) string {
	if traceID == (pcommon.TraceID{}) {
		return ""
	}
	return traceID.String()
}

func spanIDString(spanID pcommon.SpanID) string {
	if spanID == (pcommon.SpanID{}) {
		return ""
	}
	return spanID.String()
}

func logSeverity(record plog.LogRecord) string {
	if record.SeverityText() != "" {
		return redactString(record.SeverityText())
	}
	return record.SeverityNumber().String()
}

func logBody(body pcommon.Value) string {
	if body.Type() == pcommon.ValueTypeStr {
		return redactString(body.Str())
	}
	redacted, err := redactJSON(body.AsRaw())
	if err != nil {
		return redactString(body.AsString())
	}
	return redacted
}

func logTimestamp(record plog.LogRecord) time.Time {
	timestamp := timestampToTime(record.Timestamp())
	if !timestamp.IsZero() {
		return timestamp
	}
	observed := timestampToTime(record.ObservedTimestamp())
	if !observed.IsZero() {
		return observed
	}
	return time.Now().UTC()
}

func ingestMetric(ctx context.Context, stmt *sql.Stmt, metric pmetric.Metric, resource string) error {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		return ingestNumberDataPoints(ctx, stmt, metric.Name(), "gauge", metric.Gauge().DataPoints(), resource)
	case pmetric.MetricTypeSum:
		return ingestNumberDataPoints(ctx, stmt, metric.Name(), "sum", metric.Sum().DataPoints(), resource)
	case pmetric.MetricTypeHistogram:
		points := metric.Histogram().DataPoints()
		for i := 0; i < points.Len(); i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			point := points.At(i)
			attributes, err := mapJSON(point.Attributes())
			if err != nil {
				return fmt.Errorf("marshal histogram attributes: %w", err)
			}
			if _, err := stmt.ExecContext(
				ctx,
				metric.Name(),
				"histogram",
				timeToUnixNano(metricTimestamp(point.Timestamp(), point.StartTimestamp())),
				histogramValue(point),
				attributes,
				resource,
			); err != nil {
				return fmt.Errorf("insert histogram metric: %w", err)
			}
		}
		return nil
	default:
		return nil
	}
}

func ingestNumberDataPoints(
	ctx context.Context,
	stmt *sql.Stmt,
	name string,
	metricType string,
	points pmetric.NumberDataPointSlice,
	resource string,
) error {
	for i := 0; i < points.Len(); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		point := points.At(i)
		attributes, err := mapJSON(point.Attributes())
		if err != nil {
			return fmt.Errorf("marshal metric attributes: %w", err)
		}
		if _, err := stmt.ExecContext(
			ctx,
			name,
			metricType,
			timeToUnixNano(metricTimestamp(point.Timestamp(), point.StartTimestamp())),
			numberValue(point),
			attributes,
			resource,
		); err != nil {
			return fmt.Errorf("insert number metric: %w", err)
		}
	}
	return nil
}

func numberValue(point pmetric.NumberDataPoint) float64 {
	if point.ValueType() == pmetric.NumberDataPointValueTypeInt {
		return float64(point.IntValue())
	}
	return point.DoubleValue()
}

func histogramValue(point pmetric.HistogramDataPoint) float64 {
	if point.HasSum() {
		return point.Sum()
	}
	return float64(point.Count())
}

func metricTimestamp(timestamp pcommon.Timestamp, start pcommon.Timestamp) time.Time {
	t := timestampToTime(timestamp)
	if !t.IsZero() {
		return t
	}
	t = timestampToTime(start)
	if !t.IsZero() {
		return t
	}
	return time.Now().UTC()
}

func countTelemetryRecords(ctx context.Context, db *sql.DB) (int64, error) {
	var total int64
	for _, query := range []string{
		"SELECT COUNT(*) FROM otel_spans",
		"SELECT COUNT(*) FROM otel_logs",
		"SELECT COUNT(*) FROM otel_metrics",
	} {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		var count int64
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return 0, fmt.Errorf("count telemetry records: %w", err)
		}
		total += count
	}
	return total, nil
}

func renameCorruptFiles(path string) error {
	timestamp := time.Now().UTC().Format("20060102150405")
	corruptPath := fmt.Sprintf("%s.corrupt.%s", path, timestamp)
	if err := renameIfExists(path, corruptPath); err != nil {
		return fmt.Errorf("rename corrupt db: %w", err)
	}
	if err := renameIfExists(path+"-wal", corruptSidecarPath(corruptPath, "wal")); err != nil {
		return fmt.Errorf("rename corrupt wal: %w", err)
	}
	if err := renameIfExists(path+"-shm", corruptSidecarPath(corruptPath, "shm")); err != nil {
		return fmt.Errorf("rename corrupt shm: %w", err)
	}
	return nil
}

func corruptSidecarPath(corruptPath string, suffix string) string {
	return filepath.Clean(corruptPath + "." + suffix)
}

func renameIfExists(oldPath string, newPath string) error {
	if _, err := os.Lstat(oldPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.Rename(oldPath, newPath)
}
