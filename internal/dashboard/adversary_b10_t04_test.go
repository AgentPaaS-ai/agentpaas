//go:build adversary

package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAdversaryB10T04_ScriptTagEscaping(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(10).String()

	// Body with script
	ingestLogViewerLog(t, ctx, store, runID, "<script>alert('XSS')</script>", nil, nil)
	// img onerror
	ingestLogViewerLog(t, ctx, store, runID, `<img src=x onerror=alert(1)>`, nil, nil)
	// attribute escape attempt
	ingestLogViewerLog(t, ctx, store, runID, `"><script>alert(1)</script>`, map[string]string{"attr": `"><script>alert(1)</script>`}, nil)
	// svg onload
	ingestLogViewerLog(t, ctx, store, runID, `<svg/onload=alert(1)>`, nil, map[string]string{"res": `<svg/onload=alert(1)>`})

	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	// Must not contain raw <script> etc in response
	for _, bad := range []string{"<script>", "<img src=x onerror", `"><script>`, "<svg/onload"} {
		if strings.Contains(body, bad) {
			t.Fatalf("ADVERSARY BREAK: response contains raw XSS payload %q: %s", bad, body)
		}
	}
	// JSON encoding escapes HTML metacharacters by default (& -> \u0026, < -> \u003c).
	// The XSS defense is confirmed by absence of raw <script> in the body.
}

func TestAdversaryB10T04_SentinelSecretsRedacted(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(11).String()

	secrets := []string{
		"sk-advtest1234567890abcdef",
		"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.abc123def456",
		"AKIAIOSFODNN7EXAMPLE",
		"ghp_1234567890abcdefghijklmnopqrstuvwxyz",
	}
	for _, s := range secrets {
		ingestLogViewerLog(t, ctx, store, runID, s, map[string]string{"auth": s}, map[string]string{"cred": s})
	}

	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	for _, secret := range secrets {
		if strings.Contains(body, secret) {
			t.Fatalf("ADVERSARY BREAK: raw secret leaked in response: %s", secret)
		}
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Fatalf("ADVERSARY BREAK: no redaction marker present")
	}
}

func TestAdversaryB10T04_BinaryControlCharInjection(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(12).String()

	// Null, BEL, ESC, DEL, C1, invalid UTF8
	ingestLogViewerLog(t, ctx, store, runID, "body\x00\x07", map[string]string{"a": "\x1b"}, map[string]string{"r": "\x7f\x80\x9f"})
	ingestLogViewerLog(t, ctx, store, runID, "\xff\xfe\xc0\x80", nil, nil)

	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/logs")

	rawBad := []string{"\x00", "\x07", "\x1b", "\x7f", "\x80", "\xff", "\xfe"}
	for _, b := range rawBad {
		if strings.Contains(body, b) {
			t.Fatalf("ADVERSARY BREAK: raw control/invalid byte passed through: %q", b)
		}
	}
	// Should see escapes like \x00
	if !strings.Contains(body, `\x00`) {
		t.Fatalf("ADVERSARY BREAK: missing control escape in response")
	}
}

func TestAdversaryB10T04_TruncationAfterRedaction(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(13).String()

	// 100KB body with secret before the truncation point.
	longPrefix := strings.Repeat(".", 9*1024)
	secret := "sk-trunctest1234567890abcdef"
	fullBody := longPrefix + secret + ":" + strings.Repeat(":", 80*1024)
	ingestLogViewerLog(t, ctx, store, runID, fullBody, nil, nil)

	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	var entries []LogViewEntry
	requestLogViewerJSON(t, server, "/api/runs/"+runID+"/logs", &entries)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	e := entries[0]
	if !e.Truncated {
		t.Fatalf("ADVERSARY BREAK: not marked truncated")
	}
	if len(e.Body) > maxLogBodyLen+len("...") {
		t.Fatalf("ADVERSARY BREAK: body not truncated to limit")
	}
	if strings.Contains(e.Body, secret) {
		t.Fatalf("ADVERSARY BREAK: secret at 11KB leaked after truncation")
	}
	if !strings.Contains(e.Body, "[REDACTED]") {
		t.Fatalf("ADVERSARY BREAK: secret not redacted before truncate")
	}
	// Check no half UTF8 (simple ascii here)
}

func TestAdversaryB10T04_HugeAttributesBounded(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(14).String()

	attrs := make(map[string]string, 1000)
	for i := 0; i < 1000; i++ {
		attrs["k"+string(rune(i%26+'a'))] = strings.Repeat("v", 4*1024)
	}
	ingestLogViewerLog(t, ctx, store, runID, "body", attrs, nil)

	server := newLogViewerTestServer(store, nil)
	defer func() { server.Close() }()

	var entries []LogViewEntry
	requestLogViewerJSON(t, server, "/api/runs/"+runID+"/logs", &entries)

	if len(entries) != 1 {
		t.Fatalf("expected 1")
	}
	totalAttrLen := 0
	for _, v := range entries[0].Attributes {
		if len(v) > maxAttributeValueLen+len("...") { // allow for possible ...
			t.Fatalf("ADVERSARY BREAK: attr value not truncated per field")
		}
		totalAttrLen += len(v)
	}
	// Total response bounded reasonably (not full 4MB raw)
	if totalAttrLen > 1000*maxAttributeValueLen {
		t.Fatalf("ADVERSARY BREAK: total attr data unbounded")
	}
}

func TestAdversaryB10T04_DockerArtifactsSanitized(t *testing.T) {
	ctx := context.Background()
	store := openLogViewerTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := logViewerTraceID(15).String()

	provider := &MockDockerArtifactProvider{Artifacts: []DockerArtifact{
		{
			ContainerID: "<script>evil</script>",
			ImageDigest: "sha256:AKIAIOSFODNN7EXAMPLE",
			Labels:      map[string]string{"token": "ghp_1234567890abcdefghijklmnopqrstuvwxyz"},
			Network:     "net\x00",
			Health:      "green",
			Exists:      true,
		},
	}}
	server := newLogViewerTestServer(store, provider)
	defer func() { server.Close() }()

	body := requestLogViewerEndpoint(t, server, "/api/runs/"+runID+"/artifacts")

	bads := []string{"<script>", "AKIAIOSFODNN7EXAMPLE", "ghp_1234567890abcdefghijklmnopqrstuvwxyz", "\x00"}
	for _, b := range bads {
		if strings.Contains(body, b) {
			t.Fatalf("ADVERSARY BREAK: unsanitized docker artifact: %q", b)
		}
	}
}

func TestAdversaryB10T04_RunIDInjectionRejected(t *testing.T) {
	badRunIDs := []string{
		"../etc/passwd",
		"run\x00id",
		"run%2Fid",
		"run\\id",
		"run..id",
		"",
		strings.Repeat("a", 300),
	}
	for _, rid := range badRunIDs {
		rr := httptest.NewRecorder()
		req := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/api/runs/" + rid + "/logs"},
			Header: http.Header{},
		}
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		// Use the package level handler construction for isolation
		s := NewServer("", testAPIKey, nil, &MockResourceManager{})
		s.handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("ADVERSARY BREAK: bad runID %q accepted with status %d", rid, rr.Code)
		}
	}
}
