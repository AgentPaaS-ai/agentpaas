package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

// FileAuditAppender writes hash-chained JSONL audit records to a file.
// Each record includes prev_hash and record_hash so the daemon can verify
// the chain before re-chaining into its own audit trail.
//
// This is used by the harness binary running inside the container to
// record egress decisions, MCP calls, and other audit-worthy events.
// The daemon reads the file after (or during) the run, verifies the
// harness-side chain, and ingests the records into its own hash-chained
// audit trail.
type FileAuditAppender struct {
	mu       sync.Mutex
	file     *os.File
	prevHash string // hash of the last written record
}

// NewFileAuditAppender opens (or creates) the file at path for appending.
// The file is created with mode 0600.
func NewFileAuditAppender(path string) (*FileAuditAppender, error) {
	if path == "" {
		return nil, fmt.Errorf("harness: audit path is empty")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("harness: open audit file %s: %w", path, err)
	}
	return &FileAuditAppender{file: f}, nil
}

// Append writes a single hash-chained audit record as a JSON line.
// It sets the timestamp if empty and maintains prev_hash/record_hash.
func (a *FileAuditAppender) Append(record audit.AuditRecord) error {
	if a == nil || a.file == nil {
		return nil
	}
	if record.Timestamp == "" {
		record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if record.DeploymentMode == "" {
		record.DeploymentMode = "local"
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	record.PrevHash = a.prevHash

	recordHash, err := record.ComputeRecordHash()
	if err != nil {
		return fmt.Errorf("harness: compute record hash: %w", err)
	}
	record.RecordHash = recordHash

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("harness: marshal audit record: %w", err)
	}
	data = append(data, '\n')

	if _, err = a.file.Write(data); err != nil {
		return err
	}

	a.prevHash = recordHash
	return nil
}

// Close closes the underlying file.
func (a *FileAuditAppender) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.file.Close()
	a.file = nil
	return err
}