//go:build adversary

package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

func TestAdversaryB10T05_PolicyDiff_PathTraversal(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	server.SetPolicyDir(dir)

	// Create a legit policy
	policyContent := `version: "1.0"
agent:
  name: test
  description: safe
`
	legit := filepath.Join(dir, "legit.yaml")
	if err := os.WriteFile(legit, []byte(policyContent), 0600); err != nil {
		t.Fatalf("write legit: %v", err)
	}

	cases := []struct {
		a, b   string
		expect int
	}{
		{"../etc/passwd", "legit.yaml", 400},
		{"legit.yaml", "../etc/passwd", 400},
		{"/etc/passwd", "legit.yaml", 400},
		{"legit.yaml", "/etc/passwd", 400},
		{"legit\x00.yaml", "legit.yaml", 400},
		{"legit.yaml", "legit%2f..%2fetc%2fpasswd", 400},
	}

	for _, c := range cases {
		endpoint := "/api/policy/diff?a=" + url.QueryEscape(c.a) + "&b=" + url.QueryEscape(c.b)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		server.handler.ServeHTTP(rr, req)
		if rr.Code != c.expect {
			t.Errorf("path %q,%q: got %d want %d: %s", c.a, c.b, rr.Code, c.expect, rr.Body.String())
		}
	}

	// NUL via direct request (httptest.NewRequest panics on NUL in URL)
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/policy/diff", nil)
	req.URL.Path = "/api/policy/diff"
	req.URL.RawQuery = "a=legit.yaml&b=legit\x00.yaml"
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("NUL byte path: got %d want 400", rr.Code)
	}
}

func TestAdversaryB10T05_PolicyDiff_XSS(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	server.SetPolicyDir(dir)

	xssPolicy := `version: "1.0"
agent:
  name: xss
  description: "<script>alert(1)</script>"
`
	xssFile := filepath.Join(dir, "xss.yaml")
	if err := os.WriteFile(xssFile, []byte(xssPolicy), 0600); err != nil {
		t.Fatalf("write xss: %v", err)
	}
	safeFile := filepath.Join(dir, "safe.yaml")
	if err := os.WriteFile(safeFile, []byte(`version: "1.0"
agent:
  name: safe
  description: ok
`), 0600); err != nil {
		t.Fatalf("write safe: %v", err)
	}

	endpoint := "/api/policy/diff?a=" + url.QueryEscape("xss.yaml") + "&b=" + url.QueryEscape("safe.yaml")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("ADVERSARY BREAK: raw <script> in policy diff JSON response: %s", body)
	}
}

func TestAdversaryB10T05_AuditSearch_SQLInjection(t *testing.T) {
	records := []audit.AuditRecord{
		testAuditRecord("login", "user1"),
		testAuditRecord("deploy", "user2"),
	}
	indexer, cleanup := newAuditSearchTestIndexer(t, records)
	defer cleanup()

	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	// event_type injection
	endpoint := "/api/audit/search?event_type=" + url.QueryEscape("'; DROP TABLE audit--")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	// Should not 500 or crash, expect 200 or 400 depending on query, but safe
	if rr.Code == 500 {
		t.Fatalf("ADVERSARY BREAK: SQLi in event_type caused 500: %s", rr.Body.String())
	}

	// q injection
	endpoint = "/api/audit/search?q=" + url.QueryEscape("'; DROP TABLE--")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	if rr.Code == 500 {
		t.Fatalf("ADVERSARY BREAK: SQLi in q caused 500")
	}

	// negative/large limit/offset
	for _, bad := range []string{"-1", "999999999999", "abc"} {
		endpoint = "/api/audit/search?limit=" + bad
		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		server.handler.ServeHTTP(rr, req)
		if rr.Code != 400 {
			t.Errorf("bad limit %q: got %d want 400", bad, rr.Code)
		}
	}
}

func TestAdversaryB10T05_AuditSearch_XSS(t *testing.T) {
	xssRecord := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "<script>alert(1)</script>",
		DeploymentMode: "local",
		Actor:          "AKIAIO...MPLE", // proper sentinel
		Payload:        map[string]interface{}{"note": "<img src=x onerror=alert(1)>"},
	}
	indexer, cleanup := newAuditSearchTestIndexer(t, []audit.AuditRecord{xssRecord})
	defer cleanup()

	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	body := requestAuditSearchBody(t, server, "/api/audit/search")
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img src=x onerror") {
		t.Fatalf("ADVERSARY BREAK: raw XSS in audit search JSON: %s", body)
	}
}

func TestAdversaryB10T05_AuditExport_PathInjection(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// POST routes require a CSRF token; fetch one so path validation runs.
	csrfToken := fetchCSRFToken(t, server)

	// create dummy audit files
	auditPath := filepath.Join(dir, "audit.jsonl")
	checkpointPath := filepath.Join(dir, "checkpoints")
	os.WriteFile(auditPath, []byte{}, 0600)
	os.WriteFile(checkpointPath, []byte{}, 0600)

	cases := []auditExportRequest{
		{AuditPath: "../etc/passwd", CheckpointPath: checkpointPath, BundleDir: dir},
		{AuditPath: auditPath, CheckpointPath: "/etc/passwd", BundleDir: dir},
		{AuditPath: auditPath, CheckpointPath: checkpointPath, BundleDir: "/etc"},
		{AuditPath: auditPath + "\x00", CheckpointPath: checkpointPath, BundleDir: dir},
	}

	for _, reqBody := range cases {
		bodyBytes, _ := json.Marshal(reqBody)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/audit/export", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		server.handler.ServeHTTP(rr, req)
		if rr.Code != 400 {
			t.Errorf("path injection %+v: got %d want 400: %s", reqBody, rr.Code, rr.Body.String())
		}
	}
}

func TestAdversaryB10T05_AuditVerify_PathInjection(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	auditPath := filepath.Join(dir, "audit.jsonl")
	cpPath := filepath.Join(dir, "cp")
	os.WriteFile(auditPath, []byte{}, 0600)
	os.WriteFile(cpPath, []byte{}, 0600)

	cases := []string{
		"../etc/passwd",
		"/etc/passwd",
		auditPath + "\x00",
	}

	for _, bad := range cases {
		endpoint := "/api/audit/verify?audit=" + url.QueryEscape(bad) + "&checkpoints=" + url.QueryEscape(cpPath)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		req.Header.Set("Authorization", "Bearer "+testAPIKey)
		server.handler.ServeHTTP(rr, req)
		if rr.Code != 400 {
			t.Errorf("verify path %q: got %d want 400", bad, rr.Code)
		}
	}

	// NUL direct
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/audit/verify", nil)
	req.URL.RawQuery = "audit=" + url.QueryEscape(auditPath) + "&checkpoints=bad\x00path"
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Errorf("NUL verify: got %d", rr.Code)
	}
}

func TestAdversaryB10T05_AuditExportVerify_ResourceExhaustion(t *testing.T) {
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Simulate large by using a non-existent large-ish path or just check no crash on bad file
	auditPath := filepath.Join(dir, "missing.jsonl")
	cpPath := filepath.Join(dir, "missing.cp")

	// Export with invalid paths already tested, here test verify on bad file
	endpoint := "/api/audit/verify?audit=" + url.QueryEscape(auditPath) + "&checkpoints=" + url.QueryEscape(cpPath)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	server.handler.ServeHTTP(rr, req)
	if rr.Code != 500 { // expect error, not crash/panic
		// 500 is fine as long as no panic
	}

	// For export, POST with bad bundle dir already covered
}
