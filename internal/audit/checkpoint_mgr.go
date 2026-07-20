package audit

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// CheckpointManager creates, stores, and retrieves signed checkpoint records
// for the audit chain. It supports both fixed-cadence checkpointing (every N
// records) and on-demand export-time checkpointing.
//
// The manager maintains a separate JSONL file for checkpoints, each signed
// with the daemon's audit signing key. The checkpoint file path is typically
// named <audit_path>.checkpoints.
type CheckpointManager struct {
	mu             sync.Mutex
	file           *os.File
	path           string
	cadence        int64
	cpSeq          int64
	prevHash       string
	lastAnchorSeq  int64
	lastAnchorHash string
	signer         *ecdsa.PrivateKey
}

// NewCheckpointManager opens or creates a checkpoint file at the given path.
// cadence is the number of audit records between automatic checkpoints
// (0 means no automatic checkpointing). If keyDER is non-nil, it is parsed
// as a PKCS#8 ECDSA private key for signing.
func NewCheckpointManager(path string, cadence int64, keyDER []byte) (*CheckpointManager, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open checkpoint file: %w", err)
	}

	m := &CheckpointManager{
		file:    f,
		path:    path,
		cadence: cadence,
	}

	// Replay existing checkpoints to reconstruct chain state
	if err := m.replay(); err != nil {
		_ = f.Close() // best-effort close
		return nil, fmt.Errorf("replay checkpoints: %w", err)
	}

	// Parse signing key if provided
	if len(keyDER) > 0 {
		key, err := x509.ParsePKCS8PrivateKey(keyDER)
		if err != nil {
			_ = f.Close() // best-effort close
			return nil, fmt.Errorf("parse checkpoint signing key: %w", err)
		}
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			_ = f.Close() // best-effort close
			return nil, fmt.Errorf("checkpoint signing key is not ECDSA")
		}
		m.signer = ecKey
	}

	return m, nil
}

// replay reads existing checkpoints from the file to reconstruct seq and prev
// hash for the next checkpoint.
func (m *CheckpointManager) replay() error {
	if _, err := m.file.Seek(0, 0); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	scanner := bufio.NewScanner(m.file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var cp CheckpointRecord
		if err := json.Unmarshal([]byte(line), &cp); err != nil {
			return fmt.Errorf("malformed checkpoint line: %w", err)
		}

		m.cpSeq = cp.Seq
		m.prevHash = cp.CheckpointHash
		m.lastAnchorSeq = cp.HeadAnchorSeq
		m.lastAnchorHash = cp.HeadAnchorHash
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	return nil
}

// ShouldCheckpoint returns true if a checkpoint should be created based on the
// current audit head seq and the configured cadence.
func (m *CheckpointManager) ShouldCheckpoint(auditHeadSeq int64) bool {
	if m.cadence <= 0 {
		return false
	}
	return auditHeadSeq >= m.lastAnchorSeq+m.cadence
}

// CreateCheckpoint creates a signed checkpoint for the given head anchor
// (seq and record_hash) and appends it to the checkpoint file. If a signer
// is configured, the checkpoint is signed; otherwise it is stored unsigned
// (useful for testing).
//
// Returns the created checkpoint record.
func (m *CheckpointManager) CreateCheckpoint(auditHeadSeq int64, auditHeadHash string) (*CheckpointRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Sanity: audit head seq must be >= the last checkpointed seq
	if auditHeadSeq < m.lastAnchorSeq {
		return nil, fmt.Errorf("audit head seq %d is behind last checkpointed seq %d", auditHeadSeq, m.lastAnchorSeq)
	}

	cp := &CheckpointRecord{
		Seq:                m.cpSeq + 1,
		HeadAnchorSeq:      auditHeadSeq,
		HeadAnchorHash:     auditHeadHash,
		PrevCheckpointHash: m.prevHash,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
	}

	// Sign if we have a key
	if m.signer != nil {
		if err := cp.Sign(m.signer); err != nil {
			return nil, fmt.Errorf("sign checkpoint: %w", err)
		}
	} else {
		// Compute hash without signature
		h, err := cp.computeCheckpointHash()
		if err != nil {
			return nil, fmt.Errorf("compute checkpoint hash: %w", err)
		}
		cp.CheckpointHash = h
	}

	// Write to file
	line, err := json.Marshal(cp)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint: %w", err)
	}
	if _, err := fmt.Fprintf(m.file, "%s\n", string(line)); err != nil {
		return nil, fmt.Errorf("write checkpoint: %w", err)
	}
	if err := m.file.Sync(); err != nil {
		return nil, fmt.Errorf("fsync checkpoint: %w", err)
	}

	// Update state
	m.cpSeq = cp.Seq
	m.prevHash = cp.CheckpointHash
	m.lastAnchorSeq = auditHeadSeq
	m.lastAnchorHash = auditHeadHash

	return cp, nil
}

// LatestAnchor returns the latest checkpoint's head anchor (seq and head anchor hash).
// Returns (0, "") if no checkpoints have been created.
func (m *CheckpointManager) LatestAnchor() (int64, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cpSeq == 0 {
		return 0, ""
	}
	return m.lastAnchorSeq, m.lastAnchorHash
}

// CheckpointSeq returns the current checkpoint sequence number.
func (m *CheckpointManager) CheckpointSeq() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cpSeq
}

// Close closes the underlying checkpoint file.
func (m *CheckpointManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	return err
}