package dashboard

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

func TestAuditSearch_WithEventType(t *testing.T) {
	indexer, cleanup := newAuditSearchTestIndexer(t, []audit.AuditRecord{
		testAuditRecord("policy.diff", "alice"),
		testAuditRecord("secret_read", "bob"),
		testAuditRecord("policy.diff", "carol"),
	})
	defer cleanup()
	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	var view AuditSearchView
	requestAuditSearchJSON(t, server, "/api/audit/search?event_type=policy.diff", &view)

	if !view.Indexed {
		t.Fatalf("Indexed = false, want true")
	}
	if view.TotalCount != 2 || len(view.Records) != 2 {
		t.Fatalf("records = %d/%d, want 2/2: %#v", view.TotalCount, len(view.Records), view)
	}
	for _, record := range view.Records {
		if record.EventType != "policy.diff" {
			t.Fatalf("EventType = %q, want policy.diff", record.EventType)
		}
	}
}

func TestAuditSearch_LimitAndOffset(t *testing.T) {
	indexer, cleanup := newAuditSearchTestIndexer(t, []audit.AuditRecord{
		testAuditRecord("event", "alice"),
		testAuditRecord("event", "bob"),
		testAuditRecord("event", "carol"),
	})
	defer cleanup()
	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	var view AuditSearchView
	requestAuditSearchJSON(t, server, "/api/audit/search?limit=1&offset=1", &view)

	if view.TotalCount != 3 || len(view.Records) != 1 {
		t.Fatalf("records = %d/%d, want 3/1: %#v", view.TotalCount, len(view.Records), view)
	}
	if view.Records[0].Actor != "bob" {
		t.Fatalf("Actor = %q, want bob", view.Records[0].Actor)
	}
}

func TestAuditSearch_EmptyIndex(t *testing.T) {
	indexer, err := audit.NewSQLiteIndexer(filepath.Join(dashboardTestTempDir(t, "audit-empty-*"), "audit.db"))
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	defer func() { _ = indexer.Close() }()
	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	var view AuditSearchView
	requestAuditSearchJSON(t, server, "/api/audit/search", &view)

	if view.TotalCount != 0 || len(view.Records) != 0 {
		t.Fatalf("records = %d/%d, want 0/0", view.TotalCount, len(view.Records))
	}
	if view.SeqRange != [2]int64{} {
		t.Fatalf("SeqRange = %#v, want zero range", view.SeqRange)
	}
}

func TestAuditSearch_Sanitized(t *testing.T) {
	indexer, cleanup := newAuditSearchTestIndexer(t, []audit.AuditRecord{
		testAuditRecord("event", `<script>alert(1)</script>`),
	})
	defer cleanup()
	server := NewServerWithAudit("", testAPIKey, nil, &MockResourceManager{}, indexer)

	body := requestAuditSearchBody(t, server, "/api/audit/search")

	if strings.Contains(body, "<script>") {
		t.Fatalf("response contains raw script tag: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") && !strings.Contains(body, `\u0026lt;script\u0026gt;`) {
		t.Fatalf("response missing escaped script tag: %s", body)
	}
}

func TestAuditExport_CreatesBundle(t *testing.T) {
	auditPath, checkpointPath, signingKey, pubDER := writeAuditSearchChain(t, []audit.AuditRecord{
		testAuditRecord("event", "alice"),
	})
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	server.SetAuditSigningKey(signingKey, pubDER)
	bundleDir := filepath.Join(dashboardTestTempDir(t, "audit-bundle-*"), "bundle")
	token := fetchCSRFToken(t, server)
	reqBody := bytes.NewBufferString(`{"audit_path":` + strconvQuote(auditPath) + `,"checkpoint_path":` + strconvQuote(checkpointPath) + `,"bundle_dir":` + strconvQuote(bundleDir) + `}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/audit/export", reqBody)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	req.Header.Set("X-CSRF-Token", token)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var manifest audit.BundleManifest
	if err := json.Unmarshal(rr.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.AuditRecordCount != 1 || manifest.AuditHeadSeq != 1 || manifest.PubKeyFingerprint == "" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestAuditVerify_ValidChain(t *testing.T) {
	auditPath, checkpointPath, _, pubDER := writeAuditSearchChain(t, []audit.AuditRecord{
		testAuditRecord("event", "alice"),
	})
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})
	server.SetAuditTrustAnchor(pubDER)

	var view AuditVerifyView
	requestAuditVerifyJSON(t, server, auditPath, checkpointPath, &view)

	if !view.Verified || view.IssuesCount != 0 {
		t.Fatalf("verification failed: %#v", view)
	}
	if view.TrustAnchorFingerprint != sha256Hex(pubDER) {
		t.Fatalf("TrustAnchorFingerprint = %q, want %q", view.TrustAnchorFingerprint, sha256Hex(pubDER))
	}
	if view.VerificationCommand == "" {
		t.Fatalf("VerificationCommand empty")
	}
}

func TestAuditVerify_TamperedChain(t *testing.T) {
	auditPath, checkpointPath, _, _ := writeAuditSearchChain(t, []audit.AuditRecord{
		testAuditRecord("event", "alice"),
	})
	line, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	tampered := strings.Replace(string(line), `"actor":"alice"`, `"actor":"mallory"`, 1)
	if err := os.WriteFile(auditPath, []byte(tampered), 0600); err != nil {
		t.Fatalf("write tampered audit: %v", err)
	}
	server := NewServer("", testAPIKey, nil, &MockResourceManager{})

	var view AuditVerifyView
	requestAuditVerifyJSON(t, server, auditPath, checkpointPath, &view)

	if view.Verified || view.IssuesCount == 0 {
		t.Fatalf("verification unexpectedly passed: %#v", view)
	}
}

func newAuditSearchTestIndexer(t *testing.T, records []audit.AuditRecord) (*audit.SQLiteIndexer, func()) {
	t.Helper()
	auditPath, _, _, _ := writeAuditSearchChain(t, records)
	indexer, err := audit.NewSQLiteIndexer(filepath.Join(dashboardTestTempDir(t, "audit-index-*"), "audit.db"))
	if err != nil {
		t.Fatalf("new indexer: %v", err)
	}
	if err := indexer.Rebuild(auditPath); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
	return indexer, func() { _ = indexer.Close() }
}

func writeAuditSearchChain(t *testing.T, records []audit.AuditRecord) (string, string, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	dir := dashboardTestTempDir(t, "audit-chain-*")
	auditPath := filepath.Join(dir, "audit.jsonl")
	checkpointPath := filepath.Join(dir, "audit.checkpoints")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("new audit writer: %v", err)
	}
	for _, record := range records {
		if err := writer.Append(record); err != nil {
			t.Fatalf("append audit record: %v", err)
		}
	}
	headSeq, headHash := writer.CurrentHead()
	if err := writer.Close(); err != nil {
		t.Fatalf("close audit writer: %v", err)
	}
	keyDER, pubKey, err := audit.GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("generate checkpoint key: %v", err)
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		t.Fatalf("parse checkpoint key: %v", err)
	}
	signingKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("checkpoint key type = %T, want *ecdsa.PrivateKey", parsedKey)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	cp := audit.NewCheckpoint(1, headSeq, headHash, "")
	if err := cp.Sign(signingKey); err != nil {
		t.Fatalf("sign checkpoint: %v", err)
	}
	if err := audit.WriteCheckpointJSONL(checkpointPath, cp); err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	return auditPath, checkpointPath, signingKey, pubDER
}

func testAuditRecord(eventType, actor string) audit.AuditRecord {
	return audit.AuditRecord{
		Timestamp:      time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          actor,
		Payload:        map[string]interface{}{"result": "ok"},
	}
}

func requestAuditSearchJSON(t *testing.T, server *Server, endpoint string, dst interface{}) {
	t.Helper()
	body := requestAuditSearchBody(t, server, endpoint)
	if err := json.Unmarshal([]byte(body), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
}

func requestAuditSearchBody(t *testing.T, server *Server, endpoint string) string {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	return rr.Body.String()
}

func requestAuditVerifyJSON(t *testing.T, server *Server, auditPath, checkpointPath string, dst interface{}) {
	t.Helper()
	endpoint := "/api/audit/verify?audit=" + url.QueryEscape(auditPath) + "&checkpoints=" + url.QueryEscape(checkpointPath)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Authorization", "Bearer "+testAPIKey)

	server.handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
