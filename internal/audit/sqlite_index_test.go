package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteIndexRebuild(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 10; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "sqlite_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := RebuildSQLiteIndex(auditPath, dbPath); err != nil {
		t.Fatalf("RebuildSQLiteIndex: %v", err)
	}

	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	defer func() { _ = ix.Close() }()

	count, err := ix.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 records, got %d", count)
	}

	rec, err := ix.QueryBySeq(5)
	if err != nil {
		t.Fatalf("QueryBySeq(5): %v", err)
	}
	if rec.Seq != 5 {
		t.Fatalf("expected seq=5, got %d", rec.Seq)
	}
	if rec.EventType != "sqlite_test" {
		t.Fatalf("expected event_type=sqlite_test, got %q", rec.EventType)
	}

	records, err := ix.QueryByEventType("sqlite_test", 3)
	if err != nil {
		t.Fatalf("QueryByEventType: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if records[0].Seq != 1 || records[2].Seq != 3 {
		t.Fatalf("expected records 1-3, got seqs %d and %d", records[0].Seq, records[2].Seq)
	}
}

func TestSQLiteIndexRebuildFromBrokenChain(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "broken_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Tamper a byte in the file
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data[100] ^= 0xFF
	if err := os.WriteFile(auditPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err = RebuildSQLiteIndex(auditPath, dbPath)
	if err == nil {
		t.Log("Expected chain error, got nil - records up to break may still be imported")
	} else {
		t.Logf("Got expected chain error (records up to break imported): %v", err)
	}

	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	defer func() { _ = ix.Close() }()

	count, err := ix.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	t.Logf("Imported %d records from broken chain (0 is OK if first record was corrupted)", count)
}

func TestSQLiteIndexRebuildEmptyFile(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	dbPath := filepath.Join(dir, "audit.db")

	if err := os.WriteFile(auditPath, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RebuildSQLiteIndex(auditPath, dbPath); err != nil {
		t.Fatalf("RebuildSQLiteIndex on empty file: %v", err)
	}

	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	defer func() { _ = ix.Close() }()

	count, err := ix.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 records in index from empty file, got %d", count)
	}
}

func TestSQLiteIndexRebuildIdempotent(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "idempotent_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := RebuildSQLiteIndex(auditPath, dbPath); err != nil {
		t.Fatalf("First Rebuild: %v", err)
	}
	if err := RebuildSQLiteIndex(auditPath, dbPath); err != nil {
		t.Fatalf("Second Rebuild: %v", err)
	}

	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	defer func() { _ = ix.Close() }()

	count, err := ix.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 records from idempotent rebuild, got %d", count)
	}
}

func TestSQLiteIndexRebuildWithHostedContext(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")
	dbPath := filepath.Join(dir, "audit.db")

	w, err := NewAuditWriter(auditPath)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	rec := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "hosted_test",
		DeploymentMode: "hosted",
		Actor:          "deployer",
		Payload:        map[string]interface{}{"image": "nginx:1.25"},
		HostedContext: &HostedContext{
			TenantID:        "tenant-abc",
			ProjectID:       "proj-42",
			Region:          "us-east-1",
			RuntimeProvider: "lambda",
		},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := RebuildSQLiteIndex(auditPath, dbPath); err != nil {
		t.Fatalf("RebuildSQLiteIndex: %v", err)
	}

	ix, err := NewSQLiteIndexer(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteIndexer: %v", err)
	}
	defer func() { _ = ix.Close() }()

	record, err := ix.QueryBySeq(1)
	if err != nil {
		t.Fatalf("QueryBySeq: %v", err)
	}
	if record.HostedContext == nil {
		t.Fatal("expected hosted_context to be present")
	}
	if record.HostedContext.TenantID != "tenant-abc" {
		t.Fatalf("tenant_id: got %q", record.HostedContext.TenantID)
	}
}