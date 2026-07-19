package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Adversary: concurrent progress calls to the journal writer.
// Ensures 10 concurrent appends produce exactly 10 records with
// monotonic sequences 1-10 (no duplicates, no gaps) and no data races.
func TestAdversary_B27_ConcurrentProgress(t *testing.T) {
	dir := t.TempDir()
	journalPath := dir + "/j.jsonl"
	key := []byte("test-key-32-bytes-long-enough!!")
	w, err := newProgressJournalWriter(journalPath, key, progressIdentity{RunID: "r", AttemptID: "a"})
	if err != nil {
		t.Fatalf("newProgressJournalWriter: %v", err)
	}
	defer func() { _ = w.close() }()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_, err := w.append(
				fmt.Sprintf("evt-%d", n), "p", nil, nil, nil, "", false, "", "",
			)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent append %d: %v", i, err)
		}
	}

	// Verify all 10 records were written.
	data, _ := os.ReadFile(journalPath)
	lines := bytes.Split(data, []byte("\n"))
	count := 0
	for _, l := range lines {
		if len(l) > 0 {
			count++
		}
	}
	if count != 10 {
		t.Fatalf("expected 10 journal records, got %d", count)
	}

	// Verify sequences are 1-10 (no duplicates, no gaps).
	seqs := make(map[int64]bool)
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		var rec progressJournalRecord
		if err := json.Unmarshal(l, &rec); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if seqs[rec.Sequence] {
			t.Fatal("ADVERSARY BREAK: duplicate sequence number")
		}
		seqs[rec.Sequence] = true
	}
	for i := int64(1); i <= 10; i++ {
		if !seqs[i] {
			t.Fatalf("ADVERSARY BREAK: missing sequence %d", i)
		}
	}
}
