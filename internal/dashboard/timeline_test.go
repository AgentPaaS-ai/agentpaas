package dashboard

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func TestTimeline_SSE_ServesExistingSpans(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(1).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(1), "llm completion", map[string]any{
		"llm.model":         "gpt-5.5",
		"llm.provider":      "openai",
		"llm.input_tokens":  int64(21),
		"llm.output_tokens": int64(34),
		"llm.cost":          0.042,
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if event.Event != "llm_call" {
		t.Fatalf("event = %q, want llm_call; data=%s", event.Event, event.Data)
	}
	var timeline TimelineEvent
	if err := json.Unmarshal(event.Data, &timeline); err != nil {
		t.Fatalf("decode timeline event: %v", err)
	}
	var row LLMTimelineRow
	if err := json.Unmarshal(timeline.Data, &row); err != nil {
		t.Fatalf("decode llm row: %v", err)
	}
	if row.Model != "gpt-5.5" || row.Provider != "openai" || row.InputTokens != 21 || row.OutputTokens != 34 {
		t.Fatalf("unexpected llm row: %#v", row)
	}
}

func TestTimeline_SSE_LiveEvents(t *testing.T) {
	runID := "run-live"
	server, bus := newTimelineTestServer(t, nil)
	bus.RegisterRun(runID)
	bus.Publish(runID, trigger.EventRunProgress, map[string]string{"phase": "tooling"})
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if event.ID != "1" || event.Event != "run_event" {
		t.Fatalf("unexpected event id=%q event=%q data=%s", event.ID, event.Event, event.Data)
	}
	var timeline TimelineEvent
	if err := json.Unmarshal(event.Data, &timeline); err != nil {
		t.Fatalf("decode timeline event: %v", err)
	}
	if timeline.Type != "run_event" || timeline.RunID != runID {
		t.Fatalf("unexpected timeline event: %#v", timeline)
	}
}

func TestTimeline_SSE_LastEventID_Reconnect(t *testing.T) {
	runID := "run-reconnect"
	server, bus := newTimelineTestServer(t, nil)
	bus.RegisterRun(runID)
	bus.Publish(runID, trigger.EventRunCreated, nil)
	bus.Publish(runID, trigger.EventRunStarted, nil)
	bus.Publish(runID, trigger.EventRunSucceeded, nil)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "2")

	if event.ID != "3" || event.Event != "run_event" {
		t.Fatalf("expected only new event 3, got id=%q event=%q data=%s", event.ID, event.Event, event.Data)
	}
	if strings.Contains(string(event.Data), "run_started") {
		t.Fatalf("reconnect included already-seen data: %s", event.Data)
	}
}

func TestTimeline_SSE_Heartbeat(t *testing.T) {
	previous := timelineHeartbeatInterval
	timelineHeartbeatInterval = 10 * time.Millisecond
	defer func() { timelineHeartbeatInterval = previous }()

	runID := "run-heartbeat"
	server, bus := newTimelineTestServer(t, nil)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if event.Event != "heartbeat" {
		t.Fatalf("event = %q, want heartbeat; data=%s", event.Event, event.Data)
	}
	if strings.Contains(string(event.Data), runID) || strings.Contains(string(event.Data), "user") || strings.Contains(string(event.Data), "secret") {
		t.Fatalf("heartbeat leaked user-controlled data: %s", event.Data)
	}
}

func TestTimeline_RunIDValidationRejectsUnsafePaths(t *testing.T) {
	server, _ := newTimelineTestServer(t, nil)
	defer server.Close()

	badRunIDs := []string{
		"../../../etc/passwd",
		"run/../../other",
		"run%2Fother",
		"run%5Cother",
		"run%252Fother",
		"run.with.dot",
		"run:with:colon",
		strings.Repeat("a", maxTimelineRunIDLength+1),
	}
	for _, runID := range badRunIDs {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/api/runs/"+runID+"/timeline", nil)
		if err != nil {
			t.Fatalf("new request for %q: %v", runID, err)
		}
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("do request for %q: %v", runID, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("runID %q status = %d, want %d", runID, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

func TestTimeline_SSE_DeniedEgressRow(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(2).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(2), "egress denied", map[string]any{
		"egress.destination": "https://blocked.example",
		"http.method":        "POST",
		"egress.allowed":     false,
		"egress.deny_reason": "policy denied",
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if event.Event != "egress_denied" {
		t.Fatalf("event = %q, want egress_denied; data=%s", event.Event, event.Data)
	}
	var timeline TimelineEvent
	if err := json.Unmarshal(event.Data, &timeline); err != nil {
		t.Fatalf("decode timeline event: %v", err)
	}
	var row EgressTimelineRow
	if err := json.Unmarshal(timeline.Data, &row); err != nil {
		t.Fatalf("decode egress row: %v", err)
	}
	if row.Allowed || row.DenyReason != "policy denied" {
		t.Fatalf("unexpected denied egress row: %#v", row)
	}
}

func TestTimeline_SSE_RedactsSensitiveData(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(3).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(3), "llm secret span", map[string]any{
		"llm.model":    "gpt-5.5",
		"api.token":    "sk-sentinel-secret-1234567890",
		"llm.provider": "openai",
	})
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if strings.Contains(string(event.Data), "sk-sentinel-secret-1234567890") {
		t.Fatalf("SSE data contains raw secret: %s", event.Data)
	}
	if !strings.Contains(string(event.Data), "[REDACTED]") {
		t.Fatalf("SSE data does not contain redaction marker: %s", event.Data)
	}
}

func TestTimeline_SSE_10KSpanVirtualization(t *testing.T) {
	ctx := context.Background()
	runID := timelineTraceID(4).String()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	ss := rs.ScopeSpans().AppendEmpty()
	start := time.Now().Add(-time.Hour)
	for i := 0; i < 10_000; i++ {
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(timelineTraceID(4))
		span.SetSpanID(pcommon.SpanID{
			byte(i >> 56), byte(i >> 48), byte(i >> 40), byte(i >> 32),
			byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i),
		})
		span.SetName(fmt.Sprintf("llm span %05d", i))
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Duration(i) * time.Millisecond)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(start.Add(time.Duration(i+1) * time.Millisecond)))
		span.Attributes().PutStr("llm.model", "gpt-5.5")
		span.Attributes().PutInt("llm.input_tokens", int64(i))
	}
	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest traces: %v", err)
	}
	server, bus := newTimelineTestServer(t, store)
	bus.RegisterRun(runID)
	defer server.Close()

	event := openTimelineAndRead(t, server, runID, "")

	if event.Event != "span_batch" {
		t.Fatalf("event = %q, want span_batch", event.Event)
	}
	var batch struct {
		Events []TimelineEvent `json:"events"`
	}
	if err := json.Unmarshal(event.Data, &batch); err != nil {
		t.Fatalf("decode span batch: %v", err)
	}
	if len(batch.Events) != 100 {
		t.Fatalf("batch size = %d, want 100", len(batch.Events))
	}
	if batch.Events[0].Type != "llm_call" || batch.Events[99].Type != "llm_call" {
		t.Fatalf("unexpected batch event types: first=%q last=%q", batch.Events[0].Type, batch.Events[99].Type)
	}
}

func TestTimeline_SSE_RequiresAuth(t *testing.T) {
	server, bus := newTimelineTestServer(t, nil)
	bus.RegisterRun("run-auth")
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/runs/run-auth/timeline", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestTimeline_SPA_RendersEventDataWithTextContent(t *testing.T) {
	js, err := spaFiles.ReadFile("dist/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	source := string(js)
	start := strings.Index(source, "function createTimelineRow")
	end := strings.Index(source, "function timelineIcon")
	if start == -1 || end == -1 || end <= start {
		t.Fatal("timeline row renderer not found")
	}
	renderer := source[start:end]
	if strings.Contains(renderer, "innerHTML") {
		t.Fatalf("timeline row renderer must not use innerHTML: %s", renderer)
	}
	if strings.Count(renderer, "textContent") < 4 {
		t.Fatalf("timeline row renderer should render event data with textContent: %s", renderer)
	}
}

type sseMessage struct {
	ID    string
	Event string
	Data  json.RawMessage
}

func openTimelineAndRead(t *testing.T, server *httptest.Server, runID string, lastEventID string) sseMessage {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/runs/"+runID+"/timeline", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	reader := bufio.NewReader(resp.Body)
	return readSSEMessage(t, reader)
}

func readSSEMessage(t *testing.T, reader *bufio.Reader) sseMessage {
	t.Helper()
	type result struct {
		message sseMessage
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := readSSEMessageBlocking(reader)
		ch <- result{message: msg, err: err}
	}()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read SSE: %v", got.err)
		}
		return got.message
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE message")
		return sseMessage{}
	}
}

func readSSEMessageBlocking(reader *bufio.Reader) (sseMessage, error) {
	var msg sseMessage
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return msg, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			msg.Data = json.RawMessage(data.String())
			return msg, nil
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			msg.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			msg.Event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
}

func newTimelineTestServer(t *testing.T, store *otel.Store) (*httptest.Server, *trigger.EventBus) {
	t.Helper()
	bus := trigger.NewEventBus()
	s := NewServerWithTimeline("", testAPIKey, store, &MockResourceManager{}, bus)
	return httptest.NewServer(s.handler), bus
}

func openTimelineTestStore(t *testing.T, ctx context.Context) *otel.Store {
	t.Helper()
	store, err := otel.NewStore(ctx, t.TempDir()+"/otel.db")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func ingestTimelineSpan(
	t *testing.T,
	ctx context.Context,
	store *otel.Store,
	runID string,
	sid pcommon.SpanID,
	name string,
	attrs map[string]any,
) {
	t.Helper()
	tid, err := parseTimelineTraceID(runID)
	if err != nil {
		t.Fatalf("parse run trace id: %v", err)
	}
	traces := ptrace.NewTraces()
	span := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(tid)
	span.SetSpanID(sid)
	span.SetName(name)
	now := time.Now().UTC()
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(25 * time.Millisecond)))
	putTimelineAttrs(span.Attributes(), attrs)
	if err := store.IngestTraces(ctx, traces); err != nil {
		t.Fatalf("ingest span: %v", err)
	}
}

func putTimelineAttrs(target pcommon.Map, attrs map[string]any) {
	for key, value := range attrs {
		switch v := value.(type) {
		case string:
			target.PutStr(key, v)
		case bool:
			target.PutBool(key, v)
		case int:
			target.PutInt(key, int64(v))
		case int64:
			target.PutInt(key, v)
		case float64:
			target.PutDouble(key, v)
		}
	}
}

func timelineTraceID(seed byte) pcommon.TraceID {
	return pcommon.TraceID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, seed}
}

func timelineSpanID(seed byte) pcommon.SpanID {
	return pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, seed}
}

func parseTimelineTraceID(raw string) (pcommon.TraceID, error) {
	var tid pcommon.TraceID
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return tid, err
	}
	if len(decoded) != len(tid) {
		return tid, fmt.Errorf("trace id length = %d, want %d", len(decoded), len(tid))
	}
	copy(tid[:], decoded)
	return tid, nil
}
