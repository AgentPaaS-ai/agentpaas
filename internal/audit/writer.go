package audit

import (
	"bufio"
	"crypto/ecdsa"
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

	checkpointMgr    *CheckpointManager
	checkpointKeyDER []byte
	checkpointPath   string
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

// NewAuditWriterWithCheckpoints opens the audit JSONL at path and a signed checkpoint
// manager at checkpointPath. cadence is the record interval for automatic checkpoints
// (values <= 0 use DefaultCheckpointCadence). keyDER is the PKCS#8 ECDSA signing key.
func NewAuditWriterWithCheckpoints(path string, checkpointPath string, cadence int64, keyDER []byte) (*AuditWriter, error) {
	w, err := NewAuditWriter(path)
	if err != nil {
		return nil, err
	}
	if cadence <= 0 {
		cadence = DefaultCheckpointCadence
	}
	mgr, err := NewCheckpointManager(checkpointPath, cadence, keyDER)
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("checkpoint manager: %w", err)
	}
	w.checkpointMgr = mgr
	w.checkpointPath = checkpointPath
	if len(keyDER) > 0 {
		w.checkpointKeyDER = append([]byte(nil), keyDER...)
	}
	return w, nil
}

// CheckpointPublicKey returns the ECDSA public key for verifying signed checkpoints.
func (w *AuditWriter) CheckpointPublicKey() (*ecdsa.PublicKey, error) {
	if len(w.checkpointKeyDER) == 0 {
		return nil, fmt.Errorf("audit writer: no checkpoint signing key configured")
	}
	return PublicKeyFromCheckpointKeyDER(w.checkpointKeyDER)
}

// CheckpointsPath returns the checkpoint JSONL path when checkpoints are enabled.
func (w *AuditWriter) CheckpointsPath() string {
	return w.checkpointPath
}

// replay reads all existing lines from the JSONL file and validates the full
// hash chain integrity on startup. It verifies:
//  1. Each record is parseable JSON.
//  2. seq=1 (genesis) has prev_hash == "".
//  3. Every subsequent record's prev_hash matches the previous record's record_hash.
//  4. Every record's stored record_hash matches a recomputed hash from canonical JSON.
//  5. seq is monotonically increasing by 1 (no gaps or duplicates).
//
// If any check fails, a descriptive error is returned. Only if the entire chain
// is valid is the head set to the last record's seq and record_hash.
func (w *AuditWriter) replay() error {
	// Seek to beginning of file
	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	scanner := bufio.NewScanner(w.file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB buffer
	var prev AuditRecord
	lineNum := 0
	hasRecords := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lineNum++

		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return fmt.Errorf("chain integrity broken at line %d: malformed JSON: %w", lineNum, err)
		}

		// Verify seq monotonicity (no gaps, no duplicates)
		if hasRecords {
			if rec.Seq != prev.Seq+1 {
				return fmt.Errorf("chain integrity broken at line %d (seq=%d): expected seq=%d, got seq=%d (gap or duplicate)",
					lineNum, rec.Seq, prev.Seq+1, rec.Seq)
			}
		}

		// Genesis check: first record must be seq=1 with empty prev_hash
		if !hasRecords {
			if rec.Seq != 1 {
				return fmt.Errorf("chain integrity broken at line %d: first record must have seq=1, got seq=%d", lineNum, rec.Seq)
			}
			if rec.PrevHash != "" {
				return fmt.Errorf("chain integrity broken at line %d (seq=1): expected prev_hash=\"\", got %q", lineNum, rec.PrevHash)
			}
		}

		// Verify prev_hash chain
		if hasRecords {
			if rec.PrevHash != prev.RecordHash {
				return fmt.Errorf("chain integrity broken at line %d (seq=%d): prev_hash mismatch: got %q, expected %q",
					lineNum, rec.Seq, rec.PrevHash, prev.RecordHash)
			}
		}

		// Recompute record_hash from canonical JSON and verify it matches the stored hash
		computedHash, err := rec.computeRecordHash()
		if err != nil {
			return fmt.Errorf("chain integrity broken at line %d (seq=%d): failed to compute record_hash: %w", lineNum, rec.Seq, err)
		}
		if rec.RecordHash != computedHash {
			return fmt.Errorf("chain integrity broken at line %d (seq=%d): record_hash mismatch: stored %q, recomputed %q",
				lineNum, rec.Seq, rec.RecordHash, computedHash)
		}

		prev = rec
		hasRecords = true
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan audit file: %w", err)
	}

	// Set head only if the entire chain is valid
	if hasRecords {
		w.seq = prev.Seq
		w.hash = prev.RecordHash
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

	if w.checkpointMgr != nil && w.checkpointMgr.ShouldCheckpoint(w.seq) {
		if _, err := w.checkpointMgr.CreateCheckpoint(w.seq, w.hash); err != nil {
			return fmt.Errorf("create checkpoint at seq %d: %w", w.seq, err)
		}
	}

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
	var err error
	if w.checkpointMgr != nil {
		err = w.checkpointMgr.Close()
		w.checkpointMgr = nil
	}
	if w.file == nil {
		return err
	}
	if closeErr := w.file.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	w.file = nil
	return err
}