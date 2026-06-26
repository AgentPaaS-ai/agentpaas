package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

func readHarnessAuditJSONL(t *testing.T, path string) []audit.AuditRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file: %v", err)
	}
	defer func() { _ = f.Close() }()

	var records []audit.AuditRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse audit line: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit file: %v", err)
	}
	return records
}

func TestFileAuditAppender_HashChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	appender, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	defer func() { _ = appender.Close() }()

	events := []string{"egress_denied", "egress_allowed", "mcp_call"}
	for _, eventType := range events {
		if err := appender.Append(audit.AuditRecord{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: eventType,
			Actor:     "harness",
			Payload:   map[string]interface{}{"event": eventType},
		}); err != nil {
			t.Fatalf("Append(%s): %v", eventType, err)
		}
	}

	records := readHarnessAuditJSONL(t, path)
	if len(records) != 3 {
		t.Fatalf("record count = %d, want 3", len(records))
	}
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			t.Fatalf("record[%d].PrevHash = %q, want %q", i, records[i].PrevHash, records[i-1].RecordHash)
		}
	}
}

func TestFileAuditAppender_FirstRecordEmptyPrevHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	appender, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	defer func() { _ = appender.Close() }()

	if err := appender.Append(audit.AuditRecord{
		Timestamp: "2026-01-02T03:04:05Z",
		EventType: "egress_denied",
		Actor:     "harness",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records := readHarnessAuditJSONL(t, path)
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	if records[0].PrevHash != "" {
		t.Fatalf("first record PrevHash = %q, want empty", records[0].PrevHash)
	}
}

func TestFileAuditAppender_RecordHashMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	appender, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	defer func() { _ = appender.Close() }()

	if err := appender.Append(audit.AuditRecord{
		Timestamp: "2026-01-02T03:04:05Z",
		EventType: "egress_denied",
		Actor:     "harness",
		Payload:   map[string]interface{}{"destination": "evil.com"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := appender.Append(audit.AuditRecord{
		Timestamp: "2026-01-02T03:04:06Z",
		EventType: "egress_allowed",
		Actor:     "harness",
		Payload:   map[string]interface{}{"destination": "api.example.com"},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records := readHarnessAuditJSONL(t, path)
	for i, rec := range records {
		computed, err := rec.ComputeRecordHash()
		if err != nil {
			t.Fatalf("record[%d] ComputeRecordHash: %v", i, err)
		}
		if rec.RecordHash != computed {
			t.Fatalf("record[%d].RecordHash = %q, want %q", i, rec.RecordHash, computed)
		}
	}
}

func TestFileAuditAppender_ConcurrentAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
	appender, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	defer func() { _ = appender.Close() }()

	const goroutines = 8
	const perGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = appender.Append(audit.AuditRecord{
					EventType: "concurrent_event",
					Actor:     "harness",
					Payload: map[string]interface{}{
						"goroutine": id,
						"iteration": i,
					},
				})
			}
		}(g)
	}
	wg.Wait()

	records := readHarnessAuditJSONL(t, path)
	wantCount := goroutines * perGoroutine
	if len(records) != wantCount {
		t.Fatalf("record count = %d, want %d", len(records), wantCount)
	}

	seenPrev := make(map[string]int)
	for i, rec := range records {
		computed, err := rec.ComputeRecordHash()
		if err != nil {
			t.Fatalf("record[%d] ComputeRecordHash: %v", i, err)
		}
		if rec.RecordHash != computed {
			t.Fatalf("record[%d].RecordHash mismatch", i)
		}
		if i == 0 {
			if rec.PrevHash != "" {
				t.Fatalf("first record PrevHash = %q, want empty", rec.PrevHash)
			}
		} else if rec.PrevHash != records[i-1].RecordHash {
			t.Fatalf("record[%d] broken chain: prev_hash %q != predecessor %q",
				i, rec.PrevHash, records[i-1].RecordHash)
		}
		seenPrev[rec.PrevHash]++
	}
	if seenPrev[""] != 1 {
		t.Fatalf("genesis prev_hash seen %d times, want 1", seenPrev[""])
	}
}

func TestFileAuditAppender_SeedsPrevHashOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "harness-audit.jsonl")

	appender1, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := appender1.Append(audit.AuditRecord{
			Timestamp: "2026-01-02T03:04:05Z",
			EventType: "event",
			Actor:     "harness",
			Payload:   map[string]interface{}{"n": i},
		}); err != nil {
			t.Fatalf("Append record %d: %v", i, err)
		}
	}
	if err := appender1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recordsBefore := readHarnessAuditJSONL(t, path)
	if len(recordsBefore) != 3 {
		t.Fatalf("record count before reopen = %d, want 3", len(recordsBefore))
	}
	thirdHash := recordsBefore[2].RecordHash
	if thirdHash == "" {
		t.Fatal("third record RecordHash is empty")
	}

	appender2, err := NewFileAuditAppender(path)
	if err != nil {
		t.Fatalf("NewFileAuditAppender reopen: %v", err)
	}
	defer func() { _ = appender2.Close() }()

	if err := appender2.Append(audit.AuditRecord{
		Timestamp: "2026-01-02T03:04:06Z",
		EventType: "event",
		Actor:     "harness",
		Payload:   map[string]interface{}{"n": 3},
	}); err != nil {
		t.Fatalf("Append 4th record: %v", err)
	}

	records := readHarnessAuditJSONL(t, path)
	if len(records) != 4 {
		t.Fatalf("record count = %d, want 4", len(records))
	}
	if records[3].PrevHash != thirdHash {
		t.Fatalf("4th record PrevHash = %q, want %q (3rd record_hash)", records[3].PrevHash, thirdHash)
	}
	if records[3].PrevHash == "" {
		t.Fatal("4th record PrevHash must not be empty after reopen")
	}
}