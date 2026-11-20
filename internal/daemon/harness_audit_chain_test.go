package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/harness"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

func writeHarnessAuditChain(t *testing.T, path string, records []audit.AuditRecord) {
	t.Helper()
	appender, err := harness.NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	for _, rec := range records {
		if err := appender.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func validHarnessChainRecords() []audit.AuditRecord {
	return []audit.AuditRecord{
		{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "egress_denied",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "evil.com"},
		},
		{
			Timestamp: "2026-01-02T03:04:06Z",
			EventType: "egress_allowed",
			Actor:     "harness",
			Payload:   map[string]interface{}{"destination": "api.example.com"},
		},
	}
}

func TestVerifyHarnessChain_ValidChain(t *testing.T) {
	records := validHarnessChainRecords()
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	writeHarnessAuditChain(t, path, records)

	stored, err := readAuditJSONL(path)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	if err := verifyHarnessChain(stored); err != nil {
		t.Fatalf("verifyHarnessChain() = %v, want nil", err)
	}
}

func TestVerifyHarnessChain_TamperedRecord(t *testing.T) {
	records := validHarnessChainRecords()
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	writeHarnessAuditChain(t, path, records)

	stored, err := readAuditJSONL(path)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	stored[1].Payload["destination"] = "tampered.example.com"

	err = verifyHarnessChain(stored)
	if err == nil {
		t.Fatal("verifyHarnessChain() = nil, want record_hash mismatch error")
	}
	if !strings.Contains(err.Error(), "record_hash mismatch") {
		t.Fatalf("verifyHarnessChain() error = %q, want record_hash mismatch", err)
	}
}

func TestVerifyHarnessChain_GenesisNonEmptyPrevHash(t *testing.T) {
	records := validHarnessChainRecords()
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	writeHarnessAuditChain(t, path, records)

	stored, err := readAuditJSONL(path)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	// Tamper: set non-empty prev_hash on genesis record
	stored[0].PrevHash = "deadbeef"
	// Recompute record_hash so the record_hash check passes, isolating the genesis check
	recomputed, err := stored[0].ComputeRecordHash()
	if err != nil {
		t.Fatalf("ComputeRecordHash: %v", err)
	}
	stored[0].RecordHash = recomputed

	err = verifyHarnessChain(stored)
	if err == nil {
		t.Fatal("verifyHarnessChain() = nil, want genesis prev_hash error")
	}
	if !strings.Contains(err.Error(), "genesis record must have empty prev_hash") {
		t.Fatalf("verifyHarnessChain() error = %q, want genesis prev_hash error", err)
	}
}

func TestVerifyHarnessChain_BrokenLink(t *testing.T) {
	records := validHarnessChainRecords()
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	writeHarnessAuditChain(t, path, records)

	stored, err := readAuditJSONL(path)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	stored[1].PrevHash = "deadbeef"
	recomputed, err := stored[1].ComputeRecordHash()
	if err != nil {
		t.Fatalf("ComputeRecordHash: %v", err)
	}
	stored[1].RecordHash = recomputed

	err = verifyHarnessChain(stored)
	if err == nil {
		t.Fatal("verifyHarnessChain() = nil, want prev_hash mismatch error")
	}
	if !strings.Contains(err.Error(), "prev_hash mismatch") {
		t.Fatalf("verifyHarnessChain() error = %q, want prev_hash mismatch", err)
	}
}

func TestIngestHarnessAudit_RefusesTamperedChain(t *testing.T) {
	dir := t.TempDir()
	hp := home.NewHomePaths(dir)
	if err := home.Ensure(hp); err != nil {
		t.Fatalf("home.Ensure: %v", err)
	}

	auditPath := filepath.Join(hp.State, "audit.jsonl")
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })

	runID := "run-tamper-test"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())

	records, err := readAuditJSONL(harnessAuditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	records[0].Payload["destination"] = "tampered.example.com"
	if err := rewriteHarnessAuditJSONL(harnessAuditPath, records); err != nil {
		t.Fatalf("rewriteHarnessAuditJSONL: %v", err)
	}

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}
	server.ingestHarnessAudit(runID, auditDir)

	_ = w.Close()
	var stderr bytes.Buffer
	_, _ = stderr.ReadFrom(r)
	stderrText := stderr.String()
	if !strings.Contains(stderrText, "chain verification failed") {
		t.Fatalf("stderr = %q, want chain verification failed", stderrText)
	}

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}
	if len(daemonRecords) != 1 {
		t.Fatalf("daemon record count = %d, want 1 (tamper event only)", len(daemonRecords))
	}
	if daemonRecords[0].EventType != "harness_audit_chain_broken" {
		t.Fatalf("daemon event type = %q, want harness_audit_chain_broken", daemonRecords[0].EventType)
	}
	if got := auditString(daemonRecords[0].Payload, "run_id"); got != runID {
		t.Fatalf("tamper event run_id = %q, want %q", got, runID)
	}
	if got := auditString(daemonRecords[0].Payload, "action"); got != "audit_ingestion_refused" {
		t.Fatalf("tamper event action = %q, want audit_ingestion_refused", got)
	}

	for _, rec := range daemonRecords {
		if rec.EventType == "egress_denied" || rec.EventType == "egress_allowed" {
			t.Fatalf("tampered harness records were ingested: %#v", rec)
		}
	}
}

func rewriteHarnessAuditJSONL(path string, records []audit.AuditRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	for _, rec := range records {
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}