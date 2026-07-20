package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
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

// lastRecordHashFromFile reads the record_hash from the last non-empty JSONL line.
// Returns empty string if the file is empty, unreadable, or the last line is invalid.
func lastRecordHashFromFile(path string) string {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return ""
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: open %s: %v\n", path, err)
		return ""
	}
	defer func() { _ = f.Close() }() // best-effort close

	const tailRead = 4096
	readSize := int64(tailRead)
	if fi.Size() < readSize {
		readSize = fi.Size()
	}
	if _, err := f.Seek(-readSize, io.SeekEnd); err != nil {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: seek %s: %v\n", path, err)
		return ""
	}
	buf := make([]byte, readSize)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: read %s: %v\n", path, err)
		return ""
	}
	buf = buf[:n]

	lines := bytes.Split(buf, []byte{'\n'})
	var lastLine []byte
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) > 0 {
			lastLine = line
			break
		}
	}
	if len(lastLine) == 0 {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: no non-empty line in %s\n", path)
		return ""
	}

	var tail struct {
		RecordHash string `json:"record_hash"`
	}
	if err := json.Unmarshal(lastLine, &tail); err != nil {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: parse last line in %s: %v\n", path, err)
		return ""
	}
	if tail.RecordHash == "" {
		fmt.Fprintf(os.Stderr, "harness: seed prevHash: missing record_hash in last line of %s\n", path)
		return ""
	}
	return tail.RecordHash
}

// NewFileAuditAppender opens (or creates) the file at path for appending.
// The file is created with mode 0600. If the file already contains records,
// prevHash is seeded from the last line's record_hash so the hash chain continues.
func NewFileAuditAppender(path string) (*FileAuditAppender, error) {
	if path == "" {
		return nil, fmt.Errorf("harness: audit path is empty")
	}
	prevHash := lastRecordHashFromFile(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("harness: open audit file %s: %w", path, err)
	}
	return &FileAuditAppender{file: f, prevHash: prevHash}, nil
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
		return fmt.Errorf("file audit appender append: %w", err)
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
