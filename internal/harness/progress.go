package harness

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// progressJournalRecord is a single authenticated line in the JSONL journal.
type progressJournalRecord struct {
	SchemaVersion     string          `json:"schema_version"`
	WorkflowID        string          `json:"workflow_id"`
	NodeID            string          `json:"node_id"`
	RunID             string          `json:"run_id"`
	AttemptID         string          `json:"attempt_id"`
	LeaseID           string          `json:"lease_id"`
	Sequence          int64           `json:"sequence"`
	Timestamp         string          `json:"timestamp"`
	EventID           string          `json:"event_id"`
	Phase             string          `json:"phase"`
	CompletedWork     []string        `json:"completed_work"`
	RemainingWork     []string        `json:"remaining_work"`
	ArtifactRefs      []string        `json:"artifact_references"`
	LastCommittedAction string        `json:"last_committed_action,omitempty"`
	SafeToResume      bool            `json:"safe_to_resume"`
	CheckpointDigest  string          `json:"checkpoint_digest,omitempty"`
	ArtifactMetaDigest string         `json:"artifact_meta_digest,omitempty"`
	HMAC              string          `json:"hmac"`
}

// progressResponse is what the harness returns to the SDK progress() call.
type progressResponse struct {
	Recorded         bool           `json:"recorded"`
	WorkflowID       string         `json:"workflow_id"`
	NodeID           string         `json:"node_id"`
	RunID            string         `json:"run_id"`
	AttemptID        string         `json:"attempt_id"`
	CheckpointID     *string        `json:"checkpoint_id"`
	LeaseExpiresAt   string         `json:"lease_expires_at"`
	ResumeCheckpoint map[string]any `json:"resume_checkpoint,omitempty"`
	ResumeReason     string         `json:"resume_reason,omitempty"`
}

// progressJournalWriter appends authenticated records to a JSONL file.
type progressJournalWriter struct {
	mu       sync.Mutex
	file     *os.File
	key      []byte
	journalPath string
	sequence int64
	seenEventIDs map[string]bool
	identity  progressIdentity
}

// progressIdentity holds run/attempt/lease metadata for journal records.
type progressIdentity struct {
	WorkflowID  string
	NodeID      string
	RunID       string
	AttemptID   string
	LeaseID     string
	LeaseExpiry time.Time
}

// ---------------------------------------------------------------------------
// Journal writer
// ---------------------------------------------------------------------------

// newProgressJournalWriter creates a writer for the given journal path and
// identity. The key is used for HMAC-SHA-256 authentication of records.
func newProgressJournalWriter(journalPath string, key []byte, identity progressIdentity) (*progressJournalWriter, error) {
	dir := filepath.Dir(journalPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	f, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open journal: %w", err)
	}
	return &progressJournalWriter{
		file:        f,
		key:         key,
		journalPath: journalPath,
		seenEventIDs: make(map[string]bool),
		identity:    identity,
	}, nil
}

// append writes a new progress record to the journal with HMAC authentication.
// Returns the checkpoint ID (if safe_to_resume) and an error.
// If eventID was already seen, returns ( "", nil ) — idempotent dedupe.
func (w *progressJournalWriter) append(
	eventID, phase string,
	completedWork, remainingWork, artifactRefs []string,
	lastCommittedAction string,
	safeToResume bool,
	checkpointDigest, artifactMetaDigest string,
) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Idempotent dedupe by event ID.
	if w.seenEventIDs[eventID] {
		return "", nil
	}
	w.seenEventIDs[eventID] = true

	w.sequence++
	rec := progressJournalRecord{
		SchemaVersion:       "1.0",
		WorkflowID:          w.identity.WorkflowID,
		NodeID:               w.identity.NodeID,
		RunID:                w.identity.RunID,
		AttemptID:            w.identity.AttemptID,
		LeaseID:              w.identity.LeaseID,
		Sequence:             w.sequence,
		Timestamp:           time.Now().UTC().Format(time.RFC3339Nano),
		EventID:             eventID,
		Phase:               phase,
		CompletedWork:       completedWork,
		RemainingWork:       remainingWork,
		ArtifactRefs:        artifactRefs,
		LastCommittedAction: lastCommittedAction,
		SafeToResume:        safeToResume,
		CheckpointDigest:    checkpointDigest,
		ArtifactMetaDigest:  artifactMetaDigest,
	}

	// Compute HMAC over canonical JSON (excluding the HMAC field itself).
	rec.HMAC = w.computeHMAC(rec)

	line, err := json.Marshal(rec)
	if err != nil {
		return "", fmt.Errorf("marshal journal record: %w", err)
	}
	line = append(line, '\n')

	// Write the record and fsync for ALL events, not just safe_to_resume.
	// B27-3: performance impact is minimal (journal writes are already O_APPEND).
	// A non-fsynced heartbeat followed by a crash could allow a false checkpoint
	// to be inferred from a stale journal line; fsync eliminates that window.
	if _, err := w.file.Write(line); err != nil {
		return "", fmt.Errorf("write journal: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return "", fmt.Errorf("fsync journal: %w", err)
	}
	if safeToResume {
		return fmt.Sprintf("cp-%s-%d", w.identity.AttemptID, w.sequence), nil
	}

	// Heartbeat-only: no checkpoint ID.
	return "", nil
}

// computeHMAC returns hex-encoded HMAC-SHA-256 over the canonical form of rec
// (all fields except HMAC).
func (w *progressJournalWriter) computeHMAC(rec progressJournalRecord) string {
	// Marshal without HMAC, then HMAC the bytes.
	rec.HMAC = ""
	canonical, _ := json.Marshal(rec)
	mac := hmac.New(sha256.New, w.key)
	mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

// close closes the journal file.
func (w *progressJournalWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// JournalPath returns the journal file path.
func (w *progressJournalWriter) JournalPath() string {
	return w.journalPath
}

// ---------------------------------------------------------------------------
// Journal key generation
// ---------------------------------------------------------------------------

// generateJournalKey creates a random 32-byte HMAC key.
func generateJournalKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate journal key: %w", err)
	}
	return key, nil
}

// saveJournalKey persists the attempt journal key to a 0600 file outside
// every worker mount. The daemon removes it after the attempt is terminal.
func saveJournalKey(path string, key []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create attempt-secrets dir: %w", err)
	}
	return os.WriteFile(path, key, 0o600)
}

// loadJournalKey reads the attempt journal key from file.
func loadJournalKey(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// removeJournalKey removes the attempt journal key file.
func removeJournalKey(path string) error {
	return os.RemoveAll(path)
}

// ---------------------------------------------------------------------------
// HMAC verification (used by daemon tailer in T03)
// ---------------------------------------------------------------------------

// verifyJournalRecord verifies the HMAC of a journal record.
// Returns true if the HMAC matches.
func verifyJournalRecord(rec *progressJournalRecord, key []byte) bool {
	expected := rec.HMAC
	rec.HMAC = ""
	canonical, _ := json.Marshal(rec)
	mac := hmac.New(sha256.New, key)
	mac.Write(canonical)
	actual := hex.EncodeToString(mac.Sum(nil))
	rec.HMAC = expected // restore
	return hmac.Equal([]byte(expected), []byte(actual))
}

// ---------------------------------------------------------------------------
// Canonical JSON for checkpoint digest
// ---------------------------------------------------------------------------

// computeCheckpointDigest returns SHA-256 hex of the canonical checkpoint JSON.
func computeCheckpointDigest(rec *progressJournalRecord) string {
	cp := map[string]any{
		"phase":                rec.Phase,
		"completed_work":       rec.CompletedWork,
		"remaining_work":       rec.RemainingWork,
		"last_committed_action": rec.LastCommittedAction,
		"safe_to_resume":       rec.SafeToResume,
		"artifact_references":  rec.ArtifactRefs,
	}
	b, _ := json.Marshal(cp)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
