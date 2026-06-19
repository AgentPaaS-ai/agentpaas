package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// AuditWriter is a daemon-owned append-only writer for the audit JSONL file.
// It serializes all writes under a mutex, fsyncs after each line, and
// maintains a current head anchor (seq + record_hash) for chaining.
//
// The writer is fail-closed: any write or fsync error is propagated to the
// caller so the guarded operation can be aborted.
type AuditWriter struct {
	mu   sync.Mutex
	file *os.File
	path string
	seq  int64
	hash string
}

// NewAuditWriter opens or creates the JSONL file at path and reconstructs the
// head anchor by replaying all existing records. If the file does not exist,
// it is created. The writer is ready for Append calls immediately.
func NewAuditWriter(path string) (*AuditWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}

	w := &AuditWriter{
		file: f,
		path: path,
		seq:  0,
		hash: "",
	}

	// Reconstruct head by replaying all existing records.
	if err := w.replay(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("replay audit file: %w", err)
	}

	return w, nil
}

// replay reads all existing lines from the JSONL file and reconstructs the
// head anchor (last seq and record_hash). It also validates chain integrity
// but does not fail on broken chains — it simply takes the last record as head.
func (w *AuditWriter) replay() error {
	// Seek to beginning of file
	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	scanner := bufio.NewScanner(w.file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Skip malformed lines rather than failing; the head is best-effort.
			continue
		}
		w.seq = rec.Seq
		w.hash = rec.RecordHash
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan audit file: %w", err)
	}

	return nil
}

// Append serializes a new record to the JSONL file under a mutex. It sets the
// record's Seq, PrevHash, and RecordHash fields, writes the canonical JSON
// line, fsyncs, and updates the head anchor.
//
// The record's Seq is set to the current head seq + 1, and PrevHash is set to
// the current head hash. The caller-provided fields (Timestamp, EventType,
// DeploymentMode, Actor, Payload, HostedContext) are preserved.
//
// If the writer has been closed, or if the write or fsync fails, an error is
// returned and the head anchor is NOT updated (fail-closed).
func (w *AuditWriter) Append(record AuditRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return fmt.Errorf("audit writer: append on closed writer")
	}

	// Assign chain fields
	record.Seq = w.seq + 1
	record.PrevHash = w.hash

	// Compute record hash from canonical JSON (without record_hash field)
	recordHash, err := record.computeRecordHash()
	if err != nil {
		return fmt.Errorf("compute record hash: %w", err)
	}
	record.RecordHash = recordHash

	// Marshal the full record (with record_hash this time) for the JSONL line
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	// Write the JSONL line (append, newline-terminated)
	if _, err := fmt.Fprintf(w.file, "%s\n", string(line)); err != nil {
		return fmt.Errorf("write audit line: %w", err)
	}

	// fsync for durability
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("fsync audit file: %w", err)
	}

	// Update head anchor only after successful write + fsync
	w.seq = record.Seq
	w.hash = record.RecordHash

	return nil
}

// CurrentHead returns the current head anchor (seq and record_hash) of the
// audit chain. Returns (0, "") if no records have been written.
func (w *AuditWriter) CurrentHead() (seq int64, hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq, w.hash
}

// Close flushes and closes the underlying file. After Close, the writer must
// not be used. It is safe to call Close multiple times (the second call is a
// no-op after the file is closed).
func (w *AuditWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}