package audit

import (
	"bufio"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"os"
)

// VerificationResult holds the outcome of verifying an audit chain against
// its checkpoints. A result passes when Issues is empty.
type VerificationResult struct {
	AuditRecordCount int64             `json:"audit_record_count"`
	AuditHeadSeq     int64             `json:"audit_head_seq"`
	AuditHeadHash    string            `json:"audit_head_hash"`
	CheckpointCount  int               `json:"checkpoint_count"`
	LatestAnchorSeq  int64             `json:"latest_anchor_seq"`
	LatestAnchorHash string            `json:"latest_anchor_hash"`
	Issues           []*CheckpointError `json:"issues,omitempty"`

	// Replayed records if the caller wants them (nil if chain is broken).
	Records []AuditRecord `json:"-"`
}

// VerifyAuditChain reads the audit JSONL file and checkpoints JSONL file
// and verifies the integrity of the audit chain against the latest checkpoint.
//
// If pubKey is non-nil, checkpoint signatures are also verified.
//
// Returns a VerificationResult with Issues populated if any integrity
// violations are found. An error is returned only for I/O failures.
func VerifyAuditChain(auditPath, checkpointsPath string, pubKey *ecdsa.PublicKey) (*VerificationResult, error) {
	result := &VerificationResult{}

	// Phase 1: Read and verify audit chain integrity
	records, err := readAuditChain(auditPath)
	if err != nil {
		// If we get a chain integrity error, it means middle tamper or reorder
		// was detected during replay. We still need the records we can get.
		result.Issues = append(result.Issues, &CheckpointError{
			Type:    classifyReplayError(err),
			Message: err.Error(),
		})
	} else {
		result.Records = records
		if len(records) > 0 {
			last := records[len(records)-1]
			result.AuditRecordCount = int64(len(records))
			result.AuditHeadSeq = last.Seq
			result.AuditHeadHash = last.RecordHash
		}
	}

	// Phase 2: Read checkpoints
	checkpoints, err := readCheckpoints(checkpointsPath)
	if err != nil {
		// If no checkpoints file exists, no verification is possible
		if os.IsNotExist(err) {
			result.Issues = append(result.Issues, &CheckpointError{
				Type:    ErrTypeMissingCheckpoint,
				Message: "no checkpoints file found",
			})
			return result, nil
		}
		return nil, fmt.Errorf("read checkpoints: %w", err)
	}

	if len(checkpoints) == 0 {
		result.Issues = append(result.Issues, &CheckpointError{
			Type:    ErrTypeMissingCheckpoint,
			Message: "no checkpoints found in checkpoints file",
		})
		return result, nil
	}

	result.CheckpointCount = len(checkpoints)

	// Phase 3: Verify checkpoint chain integrity
	for i, cp := range checkpoints {
		// Verify checkpoint-to-checkpoint hash chain
		if i > 0 {
			if cp.PrevCheckpointHash != checkpoints[i-1].CheckpointHash {
				result.Issues = append(result.Issues, &CheckpointError{
					Type:    ErrTypeCheckpointChain,
					Message: fmt.Sprintf("checkpoint seq=%d: prev_checkpoint_hash mismatch at checkpoint line %d", cp.Seq, i),
					Seq:     cp.Seq,
					Line:    i + 1,
				})
			}
		} else {
			// First checkpoint should have empty prev_checkpoint_hash
			if cp.PrevCheckpointHash != "" {
				result.Issues = append(result.Issues, &CheckpointError{
					Type:    ErrTypeCheckpointChain,
					Message: fmt.Sprintf("genesis checkpoint seq=%d: expected empty prev_checkpoint_hash", cp.Seq),
					Seq:     cp.Seq,
					Line:    1,
				})
			}
		}

		// Verify checkpoint signature
		if pubKey != nil {
			if !cp.VerifySignature(pubKey) {
				result.Issues = append(result.Issues, &CheckpointError{
					Type:    ErrTypeSignature,
					Message: fmt.Sprintf("checkpoint seq=%d: invalid signature at checkpoint line %d", cp.Seq, i+1),
					Seq:     cp.Seq,
					Line:    i + 1,
				})
			}
		}

		// Verify the checkpoint's self-hash is correct
		computedHash, err := cp.computeCheckpointHash()
		if err == nil && computedHash != cp.CheckpointHash {
			result.Issues = append(result.Issues, &CheckpointError{
				Type:    ErrTypeCheckpointChain,
				Message: fmt.Sprintf("checkpoint seq=%d: self-hash mismatch at checkpoint line %d", cp.Seq, i+1),
				Seq:     cp.Seq,
				Line:    i + 1,
			})
		}
	}

	// Phase 4: Verify checkpoint head anchors match the audit chain
	if len(records) > 0 {
		latestCp := checkpoints[len(checkpoints)-1]
		result.LatestAnchorSeq = latestCp.HeadAnchorSeq
		result.LatestAnchorHash = latestCp.HeadAnchorHash

		// Check that the latest checkpoint's head is reachable in the audit chain
		if latestCp.HeadAnchorSeq > 0 && latestCp.HeadAnchorSeq <= result.AuditHeadSeq {
			// Find the record at that seq
			idx := int(latestCp.HeadAnchorSeq - 1)
			if idx < len(records) && records[idx].Seq == latestCp.HeadAnchorSeq {
				if records[idx].RecordHash != latestCp.HeadAnchorHash {
					result.Issues = append(result.Issues, &CheckpointError{
						Type:    ErrTypeTamperMiddle,
						Message: fmt.Sprintf("middle tamper at line %d (seq=%d): record_hash %q does not match checkpoint anchor %q",
							idx+1, records[idx].Seq, records[idx].RecordHash, latestCp.HeadAnchorHash),
						Line: idx + 1,
						Seq:  records[idx].Seq,
					})
				}
			} else {
				// Seq exists in checkpoint but can't find matching record
				result.Issues = append(result.Issues, &CheckpointError{
					Type:    ErrTypeTamperMiddle,
					Message: fmt.Sprintf("checkpoint seq=%d anchors seq=%d but audit chain has no matching record at that position",
						latestCp.Seq, latestCp.HeadAnchorSeq),
					Seq: latestCp.HeadAnchorSeq,
				})
			}
		} else if latestCp.HeadAnchorSeq > result.AuditHeadSeq {
			// Checkpoint refers to a seq beyond what's in the file = tail truncation
			result.Issues = append(result.Issues, &CheckpointError{
				Type:    ErrTypeTailTruncation,
				Message: fmt.Sprintf("tail truncation: checkpoint seq=%d anchors head anchor seq=%d but audit chain only has %d records (last seq=%d)",
					latestCp.Seq, latestCp.HeadAnchorSeq, len(records), result.AuditHeadSeq),
				Seq: latestCp.HeadAnchorSeq,
			})
		}
	}

	// Phase 5: Check for gaps in checkpoint seq (missing checkpoints)
	for i := 1; i < len(checkpoints); i++ {
		if checkpoints[i].Seq != checkpoints[i-1].Seq+1 {
			result.Issues = append(result.Issues, &CheckpointError{
				Type:    ErrTypeMissingCheckpoint,
				Message: fmt.Sprintf("missing checkpoint: seq gap between checkpoint %d (seq=%d) and checkpoint %d (seq=%d)",
					i, checkpoints[i-1].Seq, i+1, checkpoints[i].Seq),
				Seq: checkpoints[i].Seq,
			})
		}
	}

	return result, nil
}

// readAuditChain reads an audit JSONL file and validates the chain integrity.
// Returns the valid records. If the chain is broken, returns the records up to
// the break point and a descriptive error.
func readAuditChain(path string) ([]AuditRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audit file: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close

	var records []AuditRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		lineNum++

		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return records, fmt.Errorf("malformed JSON at line %d: %w", lineNum, err)
		}

		// Genesis check
		if len(records) == 0 {
			if rec.Seq != 1 {
				return records, fmt.Errorf("chain integrity broken at line %d (seq=%d): first record must have seq=1", lineNum, rec.Seq)
			}
			if rec.PrevHash != "" {
				return records, fmt.Errorf("chain integrity broken at line %d (seq=1): expected empty prev_hash", lineNum)
			}
		} else {
			prev := records[len(records)-1]
			// Monotonicity
			if rec.Seq != prev.Seq+1 {
				return records, fmt.Errorf("chain integrity broken at line %d (seq=%d): expected seq=%d, got %d (gap or duplicate)",
					lineNum, rec.Seq, prev.Seq+1, rec.Seq)
			}
			// Prev hash chain
			if rec.PrevHash != prev.RecordHash {
				return records, fmt.Errorf("chain integrity broken at line %d (seq=%d): prev_hash mismatch",
					lineNum, rec.Seq)
			}
		}

		// Record hash verification
		computedHash, err := rec.computeRecordHash()
		if err != nil {
			return records, fmt.Errorf("chain integrity broken at line %d (seq=%d): failed to compute record_hash: %w", lineNum, rec.Seq, err)
		}
		if rec.RecordHash != computedHash {
			return records, fmt.Errorf("chain integrity broken at line %d (seq=%d): record_hash mismatch: stored %q, recomputed %q",
				lineNum, rec.Seq, rec.RecordHash, computedHash)
		}

		records = append(records, rec)
	}

	if err := scanner.Err(); err != nil {
		return records, fmt.Errorf("scan error: %w", err)
	}

	return records, nil
}

// readCheckpoints reads all checkpoint records from a JSONL file.
func readCheckpoints(path string) ([]*CheckpointRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // best-effort close

	var checkpoints []*CheckpointRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var cp CheckpointRecord
		if err := json.Unmarshal([]byte(line), &cp); err != nil {
			return checkpoints, fmt.Errorf("malformed checkpoint: %w", err)
		}
		checkpoints = append(checkpoints, &cp)
	}

	if err := scanner.Err(); err != nil {
		return checkpoints, fmt.Errorf("scan checkpoints: %w", err)
	}

	return checkpoints, nil
}

// classifyReplayError examines the replay error message and returns the
// appropriate CheckpointErrorType.
func classifyReplayError(err error) CheckpointErrorType {
	msg := err.Error()
	switch {
	case contains(msg, "record_hash mismatch"):
		return ErrTypeTamperMiddle
	case contains(msg, "prev_hash mismatch"):
		if contains(msg, "reorder") {
			return ErrTypeReorder
		}
		// prev_hash mismatch can also indicate middle tamper or reorder
		return ErrTypeReorder
	case contains(msg, "gap or duplicate"), contains(msg, "gap or duplicate"):
		return ErrTypeMissingCheckpoint
	default:
		return ErrTypeTamperMiddle
	}
}

// contains reports whether substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

// searchString is a simple substring search without importing strings.
func searchString(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// WriteCheckpointJSONL serializes a checkpoint record as a single JSONL line
// and appends it to the given file path. The file is created if it does not
// exist.
func WriteCheckpointJSONL(path string, cp *CheckpointRecord) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open checkpoints file: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close

	line, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if _, err := fmt.Fprintf(f, "%s\n", string(line)); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}

	return f.Sync()
}
