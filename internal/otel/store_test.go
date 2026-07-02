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

func TestNewStore_CreatesSchema(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	for _, table := range []string{"otel_schema_version", "otel_spans", "otel_logs", "otel_metrics"} {
		var name string
		err := store.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestIngestTraces_RedactsAttributes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	traces := ptrace.NewTraces()
	span := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(traceID(1))
	span.SetSpanID(spanID(1))
	span.SetName("secret span")
	span.Attributes().PutStr("token", "sk-testsecret123456789")

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
	if strings.Contains(spans[0].Attributes, "sk-testsecret123456789") {
		t.Fatalf("attributes contain raw secret: %s", spans[0].Attributes)
	}
	if !strings.Contains(spans[0].Attributes, "[REDACTED]") {
		t.Fatalf("attributes were not redacted: %s", spans[0].Attributes)
	}
}

func TestIngestLogs_RedactsBody(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	record.Body().SetStr("token sk-logsecret123456789")

	if err := store.IngestLogs(ctx, logs); err != nil {
		t.Fatalf("ingest logs: %v", err)
	}

	records, err := store.QueryLogs(ctx, "", 10)
	if err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 log, got %d", len(records))
	}
	if strings.Contains(records[0].Body, "sk-logsecret123456789") {
		t.Fatalf("body contains raw secret: %s", records[0].Body)
	}
	if !strings.Contains(records[0].Body, "[REDACTED]") {
		t.Fatalf("body was not redacted: %s", records[0].Body)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			traces := singleSpanTrace(traceID(1), spanID(byte(i+1)), time.Now())
			if err := store.IngestTraces(ctx, traces); err != nil {
				errCh <- err
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			if _, err := store.QuerySpans(ctx, "", 10); err != nil {
				errCh <- err
				return
			}
		}
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent read/write: %v", err)
	}
}

func TestPrune_DeletesOldRecords(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	oldTime := time.Now().Add(-48 * time.Hour)
	if err := store.IngestTraces(ctx, singleSpanTrace(traceID(1), spanID(1), oldTime)); err != nil {
		t.Fatalf("ingest old trace: %v", err)
	}
	if err := store.IngestLogs(ctx, singleLog(oldTime)); err != nil {
		t.Fatalf("ingest old log: %v", err)
	}
	if err := store.IngestMetrics(ctx, singleMetric(oldTime)); err != nil {
		t.Fatalf("ingest old metric: %v", err)
	}

	deleted, err := store.Prune(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 deleted records, got %d", deleted)
	}
	assertCount(t, store.db, "otel_spans", 0)
	assertCount(t, store.db, "otel_logs", 0)
	assertCount(t, store.db, "otel_metrics", 0)
}

func TestPrune_DoesNotAffectAuditJSONL(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	content := []byte(`{"event_type":"secret_read"}` + "\n")
	if err := os.WriteFile(auditPath, content, 0o600); err != nil {
		t.Fatalf("write audit file: %v", err)
	}

	if _, err := store.Prune(ctx, 24*time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}

	got, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("audit file changed: got %q want %q", got, content)
	}
}

func TestCheckpoint_ForcesWALCheckpoint(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	if err := store.IngestTraces(ctx, singleSpanTrace(traceID(1), spanID(1), time.Now())); err != nil {
		t.Fatalf("ingest trace: %v", err)
	}
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	info, err := os.Stat(store.path + "-wal")
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat wal: %v", err)
	}
	if err == nil && info.Size() > 0 {
		t.Fatalf("expected checkpoint to truncate WAL, size=%d", info.Size())
	}
}

func TestRecoverFromCorruption_RecoversOrRecreates(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	path := store.path
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if err := os.WriteFile(path, []byte("corrupt sqlite"), 0o600); err != nil {
		t.Fatalf("corrupt db: %v", err)
	}

	store = &Store{path: path}
	recovered, err := store.RecoverFromCorruption(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	defer func() { _ = store.Close() }()
	if recovered != 0 {
		t.Fatalf("expected recreated empty db, recovered %d", recovered)
	}
	if err := store.db.PingContext(ctx); err != nil {
		t.Fatalf("fresh db ping: %v", err)
	}
	assertCount(t, store.db, "otel_spans", 0)
}

func TestSchemaMigration_Idempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "otel.db")
	store, err := NewStore(ctx, path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	store, err = NewStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() { _ = store.Close() }()
}

func TestQuerySpans_ByTraceID(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	if err := store.IngestTraces(ctx, singleSpanTrace(traceID(1), spanID(1), time.Now())); err != nil {
		t.Fatalf("ingest trace 1: %v", err)
	}
	if err := store.IngestTraces(ctx, singleSpanTrace(traceID(2), spanID(2), time.Now())); err != nil {
		t.Fatalf("ingest trace 2: %v", err)
	}

	spans, err := store.QuerySpans(ctx, traceID(1).String(), 10)
	if err != nil {
		t.Fatalf("query spans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].TraceID != traceID(1).String() {
		t.Fatalf("unexpected trace id: %s", spans[0].TraceID)
	}
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	store, err := NewStore(ctx, filepath.Join(t.TempDir(), "otel.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func singleSpanTrace(tid pcommon.TraceID, sid pcommon.SpanID, at time.Time) ptrace.Traces {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "test")
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("test-scope")
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(tid)
	span.SetSpanID(sid)
	span.SetName("test-span")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(at))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(at.Add(time.Millisecond)))
	return traces
}

func singleLog(at time.Time) plog.Logs {
	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(at))
	record.Body().SetStr("test log")
	return logs
}

func singleMetric(at time.Time) pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("test.metric")
	dp := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetTimestamp(pcommon.NewTimestampFromTime(at))
	dp.SetDoubleValue(42)
	return metrics
}

func traceID(seed byte) pcommon.TraceID {
	return pcommon.TraceID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, seed}
}

func spanID(seed byte) pcommon.SpanID {
	return pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, seed}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
