package audit

import (
	"bufio"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

// auditJSONLNewline is a package-level newline used by Append to avoid
// allocating []byte{'\n'} (or converting via fmt.Fprintf) on every write.
var auditJSONLNewline = []byte{'\n'}

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
//
// If the chain is broken (e.g. tampered or corrupted), this function returns
// an error. Use NewAuditWriterRecoverable if you want automatic recovery
// (truncate at the last valid record) instead of failing.
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
		_ = f.Close() // best-effort close
		return nil, fmt.Errorf("replay audit file: %w", err)
	}

	return w, nil
}

// NewAuditWriterRecoverable opens the audit file with automatic corruption
// recovery. If the chain is broken (e.g. from an unclean shutdown mid-write),
// the writer truncates the file at the last valid record and re-replays.
// This prevents the daemon from crash-looping forever on a corrupted tail —
// the valid prefix is preserved, the broken tail is lost.
//
// Use this for daemon startup. Use NewAuditWriter for tests and strict
// integrity checking (where tampering should be detected, not repaired).
func NewAuditWriterRecoverable(path string) (*AuditWriter, error) {
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
		// Chain is broken — attempt recovery by truncating at the last
		// valid record. This is a best-effort recovery: the broken tail
		// (usually a partial write from an unclean shutdown) is discarded,
		// but the valid prefix is preserved and the daemon can start.
		recoverErr := w.recoverFromCorruption(err)
		if recoverErr != nil {
			_ = f.Close() // best-effort close
			return nil, fmt.Errorf("replay audit file: %w (recovery failed: %v)", err, recoverErr)
		}
	}

	return w, nil
}

// NewAuditWriterWithCheckpoints opens the audit JSONL at path and a signed checkpoint
// manager at checkpointPath. cadence is the record interval for automatic checkpoints
// (values <= 0 use DefaultCheckpointCadence). keyDER is the PKCS#8 ECDSA signing key.
func NewAuditWriterWithCheckpoints(path string, checkpointPath string, cadence int64, keyDER []byte) (*AuditWriter, error) {
	w, err := NewAuditWriterRecoverable(path)
	if err != nil {
		return nil, fmt.Errorf("new audit writer with checkpoints: %w", err)
	}
	if cadence <= 0 {
		cadence = DefaultCheckpointCadence
	}
	mgr, err := NewCheckpointManager(checkpointPath, cadence, keyDER)
	if err != nil {
		_ = w.Close() // best-effort close
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

// recoverFromCorruption attempts to repair a broken audit chain by
// truncating the file at the last valid record and re-replaying.
//
// Strategy:
//  1. Seek to the beginning of the file.
//  2. Read line by line, tracking the byte offset of the START of each
//     line and validating the chain as we go.
//  3. When we hit the broken record (the one that caused the replay
//     error), truncate the file at the byte offset of that record —
//     effectively discarding the broken tail.
//  4. Re-replay the truncated file to set the head anchor.
//
// If recovery succeeds, the writer is ready for new Append calls with
// the head set to the last valid record. The corrupted tail is lost.
func (w *AuditWriter) recoverFromCorruption(replayErr error) error {
	if _, err := w.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek for recovery: %w", err)
	}

	scanner := bufio.NewScanner(w.file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var prev AuditRecord
	var hasRecords bool
	var validBytes int64 // byte offset of the end of the last valid line
	var truncateAt int64 // byte offset where the broken line starts
	var lineStart int64  // byte offset of the start of the current line
	foundBreak := false

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := int64(len(line)) + 1 // +1 for the \n
		currentLineStart := lineStart
		lineStart += lineLen

		if line == "" {
			validBytes = lineStart
			continue
		}

		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			truncateAt = currentLineStart
			foundBreak = true
			break
		}

		// Validate seq monotonicity
		if hasRecords {
			if rec.Seq != prev.Seq+1 {
				truncateAt = currentLineStart
				foundBreak = true
				break
			}
		}

		// Genesis check
		if !hasRecords {
			if rec.Seq != 1 {
				// Entire file is corrupt — truncate to empty
				truncateAt = 0
				foundBreak = true
				break
			}
			if rec.PrevHash != "" {
				truncateAt = currentLineStart
				foundBreak = true
				break
			}
		}

		// Verify prev_hash chain
		if hasRecords {
			if rec.PrevHash != prev.RecordHash {
				truncateAt = currentLineStart
				foundBreak = true
				break
			}
		}

		// Recompute and verify record_hash
		computedHash, err := rec.computeRecordHash()
		if err != nil {
			truncateAt = currentLineStart
			foundBreak = true
			break
		}
		if rec.RecordHash != computedHash {
			truncateAt = currentLineStart
			foundBreak = true
			break
		}

		prev = rec
		hasRecords = true
		validBytes = lineStart
	}

	if err := scanner.Err(); err != nil {
		// Scanner error (not a chain error) — try truncating at last valid
		truncateAt = validBytes
		foundBreak = true
	}

	if !foundBreak {
		// We didn't find a break during rescan — the error was transient?
		// Re-try the original replay (shouldn't happen, but be safe).
		return w.replay()
	}

	// Truncate the file at the break point, discarding the corrupted tail.
	if err := w.file.Truncate(truncateAt); err != nil {
		return fmt.Errorf("truncate at %d: %w", truncateAt, err)
	}

	// Seek to end for appending
	if _, err := w.file.Seek(0, 2); err != nil {
		return fmt.Errorf("seek to end after truncate: %w", err)
	}

	// Reset head and re-replay the truncated file
	w.seq = 0
	w.hash = ""
	if err := w.replay(); err != nil {
		return fmt.Errorf("replay after truncate: %w", err)
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

	// Write JSONL directly as bytes (avoid fmt.Fprintf + string(line) allocs on hot path).
	if _, err := w.file.Write(line); err != nil {
		return fmt.Errorf("write audit line: %w", err)
	}
	if _, err := w.file.Write(auditJSONLNewline); err != nil {
		return fmt.Errorf("write audit line newline: %w", err)
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
//
// If a checkpoint manager is configured and there are uncheckpointed records
// (i.e., the audit head has advanced beyond the last checkpoint), a final
// checkpoint is created before closing. This ensures audit chain verification
// passes even when the daemon shuts down before hitting the cadence threshold.
func (w *AuditWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Create a final checkpoint if there are uncheckpointed records.
	if w.checkpointMgr != nil && w.seq > 0 {
		lastCpSeq, _ := w.checkpointMgr.LatestAnchor() // optional value; zero on miss
		if w.seq > lastCpSeq {
			if _, err := w.checkpointMgr.CreateCheckpoint(w.seq, w.hash); err != nil {
				// Log but don't block shutdown — the chain is still valid,
				// just without a final checkpoint.
				log.Printf("audit: failed to create final checkpoint on close (seq %d): %v", w.seq, err)
			}
		}
	}

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
