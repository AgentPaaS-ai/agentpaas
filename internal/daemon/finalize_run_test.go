package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// TestFinalizeRun_IngestsValidHarnessAudit verifies that harness audit records
// (egress_denied, egress_allowed) are ingested into the daemon audit chain
// when finalizeRun is called after a successful run.
func TestFinalizeRun_IngestsValidHarnessAudit(t *testing.T) {
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

	runID := "run-finalize-ingest"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())

	indexPath := filepath.Join(hp.State, "audit.db")
	idx, err := audit.NewSQLiteIndexer(indexPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  idx,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	server.finalizeRun(t.Context(), runID, tr)

	// Verify harness audit records appear in daemon audit.
	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// We expect: genesis (from writer init? No, empty writer starts clean)
	// + runtime_start or similar? Actually NewAuditWriter with empty file
	// means seq starts at 0. The ingested records will be seq=1,2 and
	// run_finalized will be seq=3.
	// Let's verify at least the harness records are present.
	hasEgressDenied := false
	hasEgressAllowed := false
	for _, rec := range daemonRecords {
		switch rec.EventType {
		case "egress_denied":
			hasEgressDenied = true
		case "egress_allowed":
			hasEgressAllowed = true
		}
		if auditString(rec.Payload, "run_id") != runID && rec.EventType != "run_finalized" {
			// Harness records should have run_id injected.
			// But run_finalized also has run_id.
			continue
		}
	}
	if !hasEgressDenied {
		t.Fatal("daemon audit missing egress_denied record")
	}
	if !hasEgressAllowed {
		t.Fatal("daemon audit missing egress_allowed record")
	}

	// Verify run_finalized record exists.
	hasRunFinalized := false
	for _, rec := range daemonRecords {
		if rec.EventType == "run_finalized" {
			hasRunFinalized = true
			if got := auditString(rec.Payload, "status"); got != "succeeded" {
				t.Fatalf("run_finalized status = %q, want succeeded", got)
			}
			break
		}
	}
	if !hasRunFinalized {
		t.Fatal("daemon audit missing run_finalized record")
	}
}

// TestFinalizeRun_Idempotent verifies that calling finalizeRun twice on the
// same run ingests harness audit records exactly once (sync.Once idempotency).
func TestFinalizeRun_Idempotent(t *testing.T) {
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

	runID := "run-idempotent"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	// Call finalizeRun twice — second call must be a no-op.
	server.finalizeRun(t.Context(), runID, tr)
	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// Count egress_denied and egress_allowed — each should appear exactly once.
	deniedCount := 0
	allowedCount := 0
	finalizedCount := 0
	for _, rec := range daemonRecords {
		switch rec.EventType {
		case "egress_denied":
			deniedCount++
		case "egress_allowed":
			allowedCount++
		case "run_finalized":
			finalizedCount++
		}
	}
	if deniedCount != 1 {
		t.Fatalf("egress_denied count = %d, want 1", deniedCount)
	}
	if allowedCount != 1 {
		t.Fatalf("egress_allowed count = %d, want 1", allowedCount)
	}
	if finalizedCount != 1 {
		t.Fatalf("run_finalized count = %d, want 1", finalizedCount)
	}
}

// TestFinalizeRun_CorruptedHarnessChain verifies that a corrupted harness
// audit chain produces exactly one harness_audit_chain_broken daemon audit
// record and refuses ingestion.
func TestFinalizeRun_CorruptedHarnessChain(t *testing.T) {
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

	runID := "run-corrupted"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")

	// Write a valid chain, then tamper with it.
	writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())
	records, err := readAuditJSONL(harnessAuditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}
	records[0].Payload["destination"] = "tampered.example.com"
	if err := rewriteHarnessAuditJSONL(harnessAuditPath, records); err != nil {
		t.Fatalf("rewriteHarnessAuditJSONL: %v", err)
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded", // not yet failed
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// Expect exactly one harness_audit_chain_broken event + run_finalized.
	brokenCount := 0
	finalizedCount := 0
	for _, rec := range daemonRecords {
		switch rec.EventType {
		case "harness_audit_chain_broken":
			brokenCount++
			if got := auditString(rec.Payload, "run_id"); got != runID {
				t.Fatalf("harness_audit_chain_broken run_id = %q, want %q", got, runID)
			}
			if got := auditString(rec.Payload, "action"); got != "audit_ingestion_refused" {
				t.Fatalf("harness_audit_chain_broken action = %q, want audit_ingestion_refused", got)
			}
		case "run_finalized":
			finalizedCount++
		}
		// No harness records should have been ingested.
		if rec.EventType == "egress_denied" || rec.EventType == "egress_allowed" {
			t.Fatalf("tampered harness records were ingested: event_type=%s", rec.EventType)
		}
	}
	if brokenCount != 1 {
		t.Fatalf("harness_audit_chain_broken count = %d, want 1", brokenCount)
	}
	if finalizedCount != 1 {
		t.Fatalf("run_finalized count = %d, want 1", finalizedCount)
	}
}

// TestFinalizeRun_TerminalStatusRecords verifies that finalizeRun records the
// terminal run status (succeeded/failed/stopped) in the daemon audit chain.
func TestFinalizeRun_TerminalStatusRecorded(t *testing.T) {
	tests := []struct {
		name   string
		status string
	}{
		{"succeeded", "succeeded"},
		{"failed", "failed"},
		{"cancelled", "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			runID := "run-status-" + tt.name
			auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
			if err := os.MkdirAll(auditDir, 0o700); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}

			harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
			writeHarnessAuditChain(t, harnessAuditPath, validHarnessChainRecords())

			server := &controlServer{
				homePaths:   hp,
				auditWriter: writer,
			}

			tr := &trackedRun{
				AuditDir: auditDir,
				Status:   tt.status,
			}

			server.finalizeRun(t.Context(), runID, tr)

			daemonRecords, err := readAuditJSONL(auditPath)
			if err != nil {
				t.Fatalf("readAuditJSONL: %v", err)
			}

			found := false
			for _, rec := range daemonRecords {
				if rec.EventType == "run_finalized" {
					found = true
					if got := auditString(rec.Payload, "status"); got != tt.status {
						t.Errorf("run_finalized status = %q, want %q", got, tt.status)
					}
					if got := auditString(rec.Payload, "run_id"); got != runID {
						t.Errorf("run_finalized run_id = %q, want %q", got, runID)
					}
				}
			}
			if !found {
				t.Fatal("run_finalized record not found in daemon audit")
			}
		})
	}
}

// TestFinalizeRun_NoHarnessAuditFile verifies that finalizeRun handles the
// case where harness-audit.jsonl does not exist (e.g., harness never started).
func TestFinalizeRun_NoHarnessAuditFile(t *testing.T) {
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

	runID := "run-no-audit-file"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	// Intentionally do NOT create harness-audit.jsonl

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "failed",
	}

	// Should not panic or error — missing file is graceful.
	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	// Should have exactly run_finalized, no harness records.
	if len(daemonRecords) != 1 {
		t.Fatalf("daemon record count = %d, want 1 (run_finalized only)", len(daemonRecords))
	}
	if daemonRecords[0].EventType != "run_finalized" {
		t.Fatalf("daemon event type = %q, want run_finalized", daemonRecords[0].EventType)
	}
}

// TestFinalizeRun_SQLiteIndexUpdated verifies that SQLite audit index includes
// ingested harness records after run finalization.
func TestFinalizeRun_SQLiteIndexUpdated(t *testing.T) {
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

	indexPath := filepath.Join(hp.State, "audit.db")
	idx, err := audit.NewSQLiteIndexer(indexPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })

	runID := "run-sqlite-index"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write harness audit with 3 records: egress_denied, egress_allowed, worker_start
	records := []audit.AuditRecord{
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
		{
			Timestamp: "2026-01-02T03:04:07Z",
			EventType: "worker_start",
			Actor:     "harness",
			Payload:   map[string]interface{}{"worker_id": "worker-1"},
		},
	}

	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	writeHarnessAuditChain(t, harnessAuditPath, records)

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
		auditIndex:  idx,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	server.finalizeRun(t.Context(), runID, tr)

	// Rebuild index from JSONL to verify.
	if err := idx.Rebuild(auditPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// Query by event type through the index.
	deniedRecords, err := idx.QueryByEventType("egress_denied", 0)
	if err != nil {
		t.Fatalf("QueryByEventType egress_denied: %v", err)
	}
	if len(deniedRecords) != 1 {
		t.Fatalf("egress_denied count in index = %d, want 1", len(deniedRecords))
	}

	allowedRecords, err := idx.QueryByEventType("egress_allowed", 0)
	if err != nil {
		t.Fatalf("QueryByEventType egress_allowed: %v", err)
	}
	if len(allowedRecords) != 1 {
		t.Fatalf("egress_allowed count in index = %d, want 1", len(allowedRecords))
	}

	workerRecords, err := idx.QueryByEventType("worker_start", 0)
	if err != nil {
		t.Fatalf("QueryByEventType worker_start: %v", err)
	}
	if len(workerRecords) != 1 {
		t.Fatalf("worker_start count in index = %d, want 1", len(workerRecords))
	}

	finalizedRecords, err := idx.QueryByEventType("run_finalized", 0)
	if err != nil {
		t.Fatalf("QueryByEventType run_finalized: %v", err)
	}
	if len(finalizedRecords) != 1 {
		t.Fatalf("run_finalized count in index = %d, want 1", len(finalizedRecords))
	}

	// Total record count should be 5: 3 harness + 1 run_finalized + 1 ?
	// Actually, finalizeRun calls recordAudit("run_finalized",...) which also
	// calls Rebuild. And ingestHarnessAudit also calls Rebuild. So the index
	// gets rebuilt multiple times but the end result is the same.
	count, err := idx.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if count != 4 {
		t.Fatalf("total record count in index = %d, want 4 (3 harness + 1 run_finalized)", count)
	}
}

// TestFinalizeRun_EmptyHarnessAuditFile verifies that finalizeRun handles an
// empty harness-audit.jsonl gracefully.
func TestFinalizeRun_EmptyHarnessAuditFile(t *testing.T) {
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

	runID := "run-empty-audit"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create an empty harness-audit.jsonl file.
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	if err := os.WriteFile(harnessAuditPath, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "succeeded",
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// Should have only run_finalized — no harness records ingested.
	if len(daemonRecords) != 1 {
		t.Fatalf("daemon record count = %d, want 1 (run_finalized only)", len(daemonRecords))
	}
	if daemonRecords[0].EventType != "run_finalized" {
		t.Fatalf("daemon event type = %q, want run_finalized", daemonRecords[0].EventType)
	}
}

// TestFinalizeRun_AuditDirNotExist verifies graceful handling when the audit
// directory does not exist (e.g., Run failed before creating it).
func TestFinalizeRun_AuditDirNotExist(t *testing.T) {
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

	runID := "run-missing-audit-dir"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	// Do NOT create the directory.

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "failed",
	}

	// Should not panic.
	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL: %v", err)
	}

	if len(daemonRecords) != 1 {
		t.Fatalf("daemon record count = %d, want 1 (run_finalized only)", len(daemonRecords))
	}
	if daemonRecords[0].EventType != "run_finalized" {
		t.Fatalf("daemon event type = %q, want run_finalized", daemonRecords[0].EventType)
	}
}

// TestFinalizeRun_CorruptedMalformedJSON verifies that a harness audit file
// with malformed JSON (not just hash mismatch) produces a
// harness_audit_chain_broken event and does NOT crash.
func TestFinalizeRun_CorruptedMalformedJSON(t *testing.T) {
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

	runID := "run-malformed"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write corrupted JSONL.
	corrupted := []byte(`{"not":"valid"}` + "\n" + `this is not json at all` + "\n")
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	if err := os.WriteFile(harnessAuditPath, corrupted, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "running",
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// Should have harness_audit_chain_broken + run_finalized.
	brokenCount := 0
	for _, rec := range daemonRecords {
		if rec.EventType == "harness_audit_chain_broken" {
			brokenCount++
		}
	}
	if brokenCount != 1 {
		t.Fatalf("harness_audit_chain_broken count = %d, want 1", brokenCount)
	}

	// No harness records ingested.
	for _, rec := range daemonRecords {
		if strings.HasPrefix(rec.EventType, "egress_") {
			t.Fatalf("malformed harness records were ingested: event_type=%s", rec.EventType)
		}
	}
}

// TestFinalizeRun_TruncatedJSONL verifies that a truncated JSONL file
// (partial write) produces harness_audit_chain_broken.
func TestFinalizeRun_TruncatedJSONL(t *testing.T) {
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

	runID := "run-truncated"
	auditDir := filepath.Join(hp.State, "runs", runID, "harness-audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Write a partial record.
	truncated := []byte(`{"seq": 1, "prev_hash": "", "record_hash": "abc", "timestamp": "2026-01-` + "\n")
	harnessAuditPath := filepath.Join(auditDir, "harness-audit.jsonl")
	if err := os.WriteFile(harnessAuditPath, truncated, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	server := &controlServer{
		homePaths:   hp,
		auditWriter: writer,
	}

	tr := &trackedRun{
		AuditDir: auditDir,
		Status:   "running",
	}

	server.finalizeRun(t.Context(), runID, tr)

	daemonRecords, err := readAuditJSONL(auditPath)
	if err != nil {
		t.Fatalf("readAuditJSONL daemon audit: %v", err)
	}

	// parse audit line error from readAuditJSONL should trigger harness_audit_chain_broken
	// in ingestHarnessAudit.
	brokenCount := 0
	for _, rec := range daemonRecords {
		if rec.EventType == "harness_audit_chain_broken" {
			brokenCount++
		}
	}
	if brokenCount != 1 {
		for _, r := range daemonRecords {
			t.Logf("record: seq=%d event=%s payload=%v", r.Seq, r.EventType, r.Payload)
		}
		t.Fatalf("harness_audit_chain_broken count = %d, want 1", brokenCount)
	}
}

// internal helpers for tests

// harnessAuditRecord is a minimal record used for writing test harness audit chains.
type harnessAuditRecord struct {
	Seq        int64  `json:"seq"`
	PrevHash   string `json:"prev_hash"`
	RecordHash string `json:"record_hash"`
	Timestamp  string `json:"timestamp"`
	EventType  string `json:"event_type"`
	Actor      string `json:"actor"`
	Payload    map[string]interface{} `json:"payload"`
}

// writeRawHarnessRecords writes raw audit records to a JSONL file.
// Used for malformed/truncated test data.
func writeRawHarnessRecords(t *testing.T, path string, records []json.RawMessage) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, rec := range records {
		if _, err := f.Write(append(rec, '\n')); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
}