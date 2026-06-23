//go:build adversary

package otel

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const adversarySentinel = "sk-ADV...cdef"

func TestAdversaryB10T01_Redaction_SpanNameNotRedacted(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test")
	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(traceID(1))
	span.SetSpanID(spanID(1))
	span.SetName("secret-span-" + adversarySentinel)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now().Add(time.Millisecond)))

	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest traces: %v", err)
	}

	spans, err := store.QuerySpans(ctx, traceID(1).String(), 10)
	if err != nil {
		t.Fatalf("query spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if strings.Contains(spans[0].Name, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: span name stored without redaction, contains raw secret: %s", spans[0].Name)
	}
}

func TestAdversaryB10T01_Redaction_MetricNameNotRedacted(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "test")
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("secret-metric-" + adversarySentinel)
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	dp.SetDoubleValue(42)

	if err := store.IngestMetrics(ctx, metrics); err != nil {
		t.Fatalf("ingest metrics: %v", err)
	}

	// Query via direct DB since no QueryMetrics exposed, check raw name
	var name string
	err := store.db.QueryRowContext(ctx, "SELECT name FROM otel_metrics LIMIT 1").Scan(&name)
	if err != nil {
		t.Fatalf("query metric name: %v", err)
	}
	if strings.Contains(name, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: metric name stored without redaction, contains raw secret: %s", name)
	}
}

func TestAdversaryB10T01_Redaction_AllAttributePathsRedacted(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	// Traces: resource + span attrs
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("token", adversarySentinel)
	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(traceID(1))
	span.SetSpanID(spanID(1))
	span.SetName("test")
	span.Attributes().PutStr("secret", adversarySentinel)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	spans, _ := store.QuerySpans(ctx, "", 1)
	if strings.Contains(spans[0].Resource, adversarySentinel) || strings.Contains(spans[0].Attributes, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: trace resource/attrs not redacted")
	}

	// Logs: resource + attrs + body
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("token", adversarySentinel)
	sl := rl.ScopeLogs().AppendEmpty()
	rec := sl.LogRecords().AppendEmpty()
	rec.Body().SetStr("body-" + adversarySentinel)
	rec.Attributes().PutStr("secret", adversarySentinel)
	if err := store.IngestLogs(ctx, logs); err != nil {
		t.Fatalf("ingest logs: %v", err)
	}
	logrecs, _ := store.QueryLogs(ctx, "", 1)
	if strings.Contains(logrecs[0].Resource, adversarySentinel) || strings.Contains(logrecs[0].Attributes, adversarySentinel) || strings.Contains(logrecs[0].Body, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: log resource/attrs/body not redacted")
	}

	// Metrics: resource + attrs
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("token", adversarySentinel)
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("m")
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.Attributes().PutStr("secret", adversarySentinel)
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	dp.SetDoubleValue(1)
	if err := store.IngestMetrics(ctx, metrics); err != nil {
		t.Fatalf("ingest metrics: %v", err)
	}
	var res, attrs string
	if err := store.db.QueryRowContext(ctx, "SELECT resource, attributes FROM otel_metrics LIMIT 1").Scan(&res, &attrs); err != nil {
		t.Fatalf("query metric resource/attrs: %v", err)
	}
	if strings.Contains(res, adversarySentinel) || strings.Contains(attrs, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: metric resource/attrs not redacted")
	}
}

func TestAdversaryB10T01_Prune_DoesNotTouchAuditJSONL(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	content := []byte(`{"event_type":"secret","token":"` + adversarySentinel + `"}` + "\n")
	if err := os.WriteFile(auditPath, content, 0o600); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	if _, err := store.Prune(ctx, 24*time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}

	got, err := os.ReadFile(auditPath)
	if err != nil || string(got) != string(content) {
		t.Fatalf("ADVERSARY BREAK: prune affected audit JSONL or secret visible outside DB")
	}
}

func TestAdversaryB10T01_SQLInjection_TraceID(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	malicious := "'; DROP TABLE otel_spans; -- " + adversarySentinel
	// test via Query with malicious runID (parameterized so safe)
	if _, err := store.QuerySpans(ctx, malicious, 10); err != nil {
		t.Fatalf("query spans with malicious trace ID: %v", err)
	}
	// verify table still exists
	var cnt int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM otel_spans").Scan(&cnt); err != nil {
		t.Fatalf("ADVERSARY BREAK: SQL injection succeeded, table dropped or error: %v", err)
	}
}

func TestAdversaryB10T01_Concurrent_IngestQuery_NoDeadlockOrRace(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr := singleSpanTrace(traceID(byte(i%10+1)), spanID(byte(i+1)), time.Now())
			if err := store.IngestTraces(ctx, tr); err != nil {
				errCh <- err
			}
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.QuerySpans(ctx, "", 5); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("ADVERSARY BREAK: concurrent deadlock or error: %v", err)
	}
}

func TestAdversaryB10T01_CorruptionRecovery_FutureSchemaVersion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "otel.db")
	store, err := NewStore(ctx, path)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// bump to future version
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE otel_schema_version SET version = 99"); err != nil {
		t.Fatalf("bump schema version: %v", err)
	}

	store2, err := NewStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen future schema: %v", err)
	}
	defer func() { _ = store2.Close() }()
	// if no panic and opens, pass (handles gracefully)
}

func TestAdversaryB10T01_CorruptionRecovery_TruncateAndGarbage(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	path := store.path
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// truncate
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("truncate db: %v", err)
	}
	store = &Store{path: path}
	rec, err := store.RecoverFromCorruption(ctx)
	if err != nil || rec != 0 {
		t.Fatalf("ADVERSARY BREAK: truncate recovery failed: %v %d", err, rec)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close recovered store: %v", err)
	}

	// garbage
	if err := os.WriteFile(path, []byte("garbage not sqlite "+adversarySentinel), 0o600); err != nil {
		t.Fatalf("write garbage db: %v", err)
	}
	store = &Store{path: path}
	rec, err = store.RecoverFromCorruption(ctx)
	if err != nil || rec != 0 {
		t.Fatalf("ADVERSARY BREAK: garbage recovery failed: %v %d", err, rec)
	}
	defer func() { _ = store.Close() }()
}

func TestAdversaryB10T01_Redaction_ScopeAndResourceAlwaysRedacted(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("token", adversarySentinel)
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("scope-" + adversarySentinel)
	ss.Scope().Attributes().PutStr("s", adversarySentinel)
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(traceID(1))
	span.SetSpanID(spanID(1))
	span.SetName("n")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest traces: %v", err)
	}
	spans, err := store.QuerySpans(ctx, "", 1)
	if err != nil {
		t.Fatalf("query spans: %v", err)
	}
	if strings.Contains(spans[0].Resource, adversarySentinel) || strings.Contains(spans[0].Scope, adversarySentinel) {
		t.Fatalf("ADVERSARY BREAK: scope or resource not redacted")
	}
}
