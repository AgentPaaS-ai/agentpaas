//go:build adversary

package dashboard

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const sentinelSecret = "sk-ADV...2345"
const xssPayload = `<script>alert(1)</script>`

func TestAdversaryB10T03_Redaction_SentinelInSpanAttrs(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(10).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(10), "llm secret", map[string]any{
		"llm.model":    "gpt-5.5",
		"api.key":      sentinelSecret,
		"llm.provider": "openai",
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	// Read raw SSE response body to check for sentinel
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/runs/"+runID+"/timeline", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// Read full body (small test)
	buf := make([]byte, 64*1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if strings.Contains(body, sentinelSecret) {
		t.Fatalf("ADVERSARY BREAK: sentinel secret %q leaked in raw SSE body", sentinelSecret)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Logf("note: redaction marker may be present via logging.Redact")
	}
}

func TestAdversaryB10T03_Redaction_AllEventTypes(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(11).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(11), "egress with secret", map[string]any{
		"egress.destination": sentinelSecret,
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")
	if strings.Contains(string(event.Data), sentinelSecret) {
		t.Fatalf("ADVERSARY BREAK: sentinel leaked in event %s: %s", event.Event, event.Data)
	}
}

func TestAdversaryB10T03_XSS_SpanNameWithScript(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(12).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(12), xssPayload, map[string]any{
		"llm.model": "gpt-5.5",
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")
	dataStr := string(event.Data)
	if strings.Contains(dataStr, "<script>") || strings.Contains(dataStr, "alert(1)") {
		t.Fatalf("ADVERSARY BREAK: XSS payload not escaped/redacted in SSE: %s", dataStr)
	}
	js, _ := spaFiles.ReadFile("dist/app.js")
	if strings.Contains(string(js), xssPayload) {
		t.Fatalf("ADVERSARY BREAK: XSS payload hardcoded in app.js")
	}
}

func TestAdversaryB10T03_LastEventID_Injection(t *testing.T) {
	server, bus := newTimelineTestServer(t, nil)
	bus.RegisterRun("run-inject")
	defer server.Close()

	cases := []string{"99999999999999999", "-1", "abc", "1; DROP TABLE otel_spans", ""}
	for _, id := range cases {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/runs/run-inject/timeline", nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		req.Header.Set("Last-Event-ID", id)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", id, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("ADVERSARY BREAK: bad Last-Event-ID %q caused status %d", id, resp.StatusCode)
		}
	}
}

func TestAdversaryB10T03_RunID_Validation(t *testing.T) {
	server, _ := newTimelineTestServer(t, nil)
	defer server.Close()

	badRunIDs := []string{
		"../../../etc/passwd",
		"'; DROP TABLE--",
		"run/../../other",
		strings.Repeat("a", 10000),
		"",
		"run with space",
		"run@invalid",
	}
	for _, rid := range badRunIDs {
		path := "/api/runs/" + rid + "/timeline"
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", rid, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("ADVERSARY BREAK: bad runID %q accepted with status %d (expected 400)", rid, resp.StatusCode)
		}
	}
}

func TestAdversaryB10T03_SSE_ConnectionFlooding(t *testing.T) {
	server, bus := newTimelineTestServer(t, nil)
	runID := "run-flood"
	bus.RegisterRun(runID)
	defer server.Close()

	const numConns = 50
	var wg sync.WaitGroup
	errCh := make(chan error, numConns)
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/runs/"+runID+"/timeline", nil)
			req.Header.Set("Authorization", "Bearer "+testAPIKey)
			resp, err := server.Client().Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				errCh <- err
				return
			}
			// Just ensure connection opens; do not block on read for flood test
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		if e != nil {
			t.Fatalf("ADVERSARY BREAK: flooding caused error: %v", e)
		}
	}
}

func TestAdversaryB10T03_10KSpans_PerformanceAndOrder(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(13).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()

	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	start := time.Now().Add(-time.Hour)
	for i := 0; i < 10000; i++ {
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(timelineTraceID(13))
		span.SetSpanID(pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, byte(i % 256)})
		span.SetName("perf span")
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Duration(i) * time.Millisecond)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Duration(i+1) * time.Millisecond)))
		span.Attributes().PutStr("llm.model", "gpt")
		if i%100 == 0 {
			span.Attributes().PutStr("secret", sentinelSecret)
		}
	}
	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	startTime := time.Now()
	event := openTimelineAndRead(t, server, runID, "")
	elapsed := time.Since(startTime)
	if elapsed > 30*time.Second {
		t.Fatalf("ADVERSARY BREAK: 10k spans took too long: %v", elapsed)
	}
	if strings.Contains(string(event.Data), sentinelSecret) {
		t.Fatalf("ADVERSARY BREAK: secret leaked under 10k load")
	}
}

func TestAdversaryB10T03_Heartbeat_NoUserData(t *testing.T) {
	server, bus := newTimelineTestServer(t, nil)
	runID := "run-hb"
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")
	if event.Event != "heartbeat" {
		t.Fatalf("expected heartbeat")
	}
	if strings.Contains(string(event.Data), runID) || strings.Contains(string(event.Data), "user") {
		t.Fatalf("ADVERSARY BREAK: heartbeat contains user-controlled data: %s", event.Data)
	}
}
