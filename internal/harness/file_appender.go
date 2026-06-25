package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

// FileAuditAppender is a simple audit appender that writes JSONL records
// to a file. Unlike audit.AuditWriter, it does NOT maintain a hash chain
// — it writes flat records that the daemon can ingest later.
//
// This is used by the harness binary running inside the container to
// record egress decisions, MCP calls, and other audit-worthy events.
// The daemon reads the file after (or during) the run and ingests the
// records into its own hash-chained audit trail.
type FileAuditAppender struct {
	mu   sync.Mutex
	file *os.File
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

// Append writes a single audit record as a JSON line.
// It sets the timestamp if empty and marshals the record.
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
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("harness: marshal audit record: %w", err)
	}
	data = append(data, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err = a.file.Write(data)
	return err
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
