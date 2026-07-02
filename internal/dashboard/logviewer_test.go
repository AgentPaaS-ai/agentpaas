package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestLogViewer_PlantedScriptEscaped(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(1).String()
	ingestLogViewerLog(t, ctx, store, runID, "<script>alert(1)</script>", nil, nil)
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	if strings.Contains(body, "<script>") {
		t.Fatalf("response contains raw script tag: %s", body)
	}
	if !strings.Contains(body, `\u0026lt;script\u0026gt;`) && !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("response does not contain escaped script tag: %s", body)
	}
}

func TestLogViewer_SentinelSecretRedacted(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(2).String()
	secret := "sk-SECRETKEY123456789"
	ingestLogViewerLog(t, ctx, store, runID, secret, nil, nil)
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	if strings.Contains(body, secret) {
		t.Fatalf("response contains raw secret: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("response does not contain redaction marker: %s", body)
	}
}

func TestLogViewer_BinaryCharsEscaped(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(3).String()
	ingestLogViewerLog(t, ctx, store, runID, "prefix\x00\x01\x7fsuffix", nil, nil)
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	if strings.Contains(body, "\x00") || strings.Contains(body, "\x01") || strings.Contains(body, "\x7f") {
		t.Fatalf("response contains raw control chars: %q", body)
	}
	for _, want := range []string{`\\x00`, `\\x01`, `\\x7f`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing escaped control char %s: %s", want, body)
		}
	}
}

func TestLogViewer_HugeValueTruncated(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(4).String()
	ingestLogViewerLog(t, ctx, store, runID, strings.Repeat("value: ", 20*1024), nil, nil)
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	var entries []LogViewEntry
	requestLogViewerJSON(t, server, "/api/runs/"+runID+"/logs", &entries)

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if len(entries[0].Body) != maxLogBodyLen+len("...") {
		t.Fatalf("body length = %d, want %d", len(entries[0].Body), maxLogBodyLen+len("..."))
	}
	if !strings.HasSuffix(entries[0].Body, "...") || !entries[0].Truncated {
		t.Fatalf("body not marked truncated: len=%d truncated=%v", len(entries[0].Body), entries[0].Truncated)
	}
}

func TestLogViewer_AttributesRedacted(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(5).String()
	secret := "sk-ATTRSECRET123456789"
	ingestLogViewerLog(t, ctx, store, runID, "body", map[string]string{"token": secret}, nil)
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	if strings.Contains(body, secret) {
		t.Fatalf("response contains raw attribute secret: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("response does not contain redaction marker: %s", body)
	}
}

func TestLogViewer_ResourceRedacted(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(6).String()
	secret := "sk-RESOURCESECRET123456789"
	ingestLogViewerLog(t, ctx, store, runID, "body", nil, map[string]string{"token": secret})
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	if strings.Contains(body, secret) {
		t.Fatalf("response contains raw resource secret: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("response does not contain redaction marker: %s", body)
	}
}

func TestLogViewer_SpansRedacted(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(12).String()
	secret := "sk-SPANSECRET123456789"
	ingestTimelineSpan(t, ctx, store, runID, logViewerSpanID(2), "span "+secret, map[string]any{"token": secret})
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/spans")

	if strings.Contains(body, secret) {
		t.Fatalf("response contains raw span secret: %s", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("response does not contain redaction marker: %s", body)
	}
}

func TestLogViewer_DockerArtifacts_Sanitized(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(7).String()
	secret := "sk-DOCKERSECRET123456789"
	provider := &MockDockerArtifactProvider{Artifacts: []DockerArtifact{{
		ContainerID: "<script>" + secret + "</script>",
		ImageDigest: "sha256:" + secret,
		Labels:      map[string]string{"token": secret, "unsafe": "<script>"},
		Network:     "net\x00work",
		Health:      "green",
		Exists:      true,
	}}}
	server := newLogViewerTestServer(store, provider)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/artifacts")

	if strings.Contains(body, secret) || strings.Contains(body, "<script>") || strings.Contains(body, "\x00") {
		t.Fatalf("response contains unsanitized artifact data: %q", body)
	}
	if !strings.Contains(body, "[REDACTED]") || !strings.Contains(body, `\\x00`) {
		t.Fatalf("response missing sanitized artifact data: %s", body)
	}
}

func TestLogViewer_DockerArtifacts_StaleReconciled(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(8).String()
	provider := &MockDockerArtifactProvider{Artifacts: []DockerArtifact{{
		ContainerID: "abc123",
		ImageDigest: "sha256:abc",
		Health:      "green",
		Exists:      false,
	}}}
	server := newLogViewerTestServer(store, provider)
	defer func() { server.Close() }()

	var artifacts []DockerArtifactView
	requestLogViewerJSON(t, server, "/api/runs/"+runID+"/artifacts", &artifacts)

	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	if artifacts[0].State != "reconciled" {
		t.Fatalf("state = %q, want reconciled", artifacts[0].State)
	}
	if artifacts[0].Health == "green" {
		t.Fatalf("stale artifact kept green health: %#v", artifacts[0])
	}
}

func TestLogViewer_InvalidRunID_Rejected(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/runs/run%2Fbad/logs", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestLogViewer_RequiresAuth(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	resp, err := http.Get(server.URL + "/api/runs/run-auth/logs")
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestLogViewer_AppUsesTextContent(t *testing.T) {
	source, err := os.ReadFile("dist/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "function mountLogViewer")
	end := strings.Index(text, "function timelineRunIDFromRoute")
	if start == -1 || end == -1 || end <= start {
		t.Fatal("log viewer renderer not found")
	}
	renderer := text[start:end]
	if strings.Contains(renderer, ".innerHTML") {
		t.Fatalf("log viewer renderer must not use innerHTML: %s", renderer)
	}
	if !strings.Contains(renderer, ".textContent") && !strings.Contains(renderer, "createTextNode") {
		t.Fatalf("log viewer renderer should render text with textContent/createTextNode: %s", renderer)
	}
}

func newLogViewerTestServer(store *otel.Store, provider DockerArtifactProvider) *httptest.Server {
	s := NewServer("", testAPIKey, store, &MockResourceManager{})
	if provider != nil {
		s.logViewer.artifactProvider = provider
	}
	return httptest.NewServer(s.handler)
}

func openLogViewerTestStore(t *testing.T, ctx context.Context) *otel.Store {
	t.Helper()
	store, err := otel.NewStore(ctx, t.TempDir()+"/otel.db")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func requestLogViewerEndpoint(t *testing.T, server *httptest.Server, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}

func requestLogViewerJSON(t *testing.T, server *httptest.Server, path string, target any) {
	t.Helper()
	body := requestLogViewerEndpoint(t, server, path)
	if err := json.Unmarshal([]byte(body), target); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, body)
	}
}

func ingestLogViewerLog(
	t *testing.T,
	ctx context.Context,
	store *otel.Store,
	runID string,
	body string,
	attrs map[string]string,
	resourceAttrs map[string]string,
) {
	t.Helper()
	tid, err := parseTimelineTraceID(runID)
	if err != nil {
		t.Fatalf("parse run trace id: %v", err)
	}
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	for key, value := range resourceAttrs {
		resourceLogs.Resource().Attributes().PutStr(key, value)
	}
	record := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetTraceID(tid)
	record.SetSpanID(logViewerSpanID(1))
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Now().UTC()))
	record.SetSeverityText("INFO")
	record.Body().SetStr(body)
	for key, value := range attrs {
		record.Attributes().PutStr(key, value)
	}
	if err := store.IngestLogs(ctx, logs); err != nil {
		t.Fatalf("ingest logs: %v", err)
	}
}

func logViewerTraceID(seed byte) pcommon.TraceID {
	return pcommon.TraceID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, seed}
}

func logViewerSpanID(seed byte) pcommon.SpanID {
	return pcommon.SpanID{0, 0, 0, 0, 0, 0, 1, seed}
}
