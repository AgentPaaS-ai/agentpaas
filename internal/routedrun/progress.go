package routedrun

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

// ---------------------------------------------------------------------------
// Checkpoint persistence
// ---------------------------------------------------------------------------

// SemanticCheckpoint is a durable safe-resume point.
// It is stored atomically and never mutated after creation.
type SemanticCheckpoint struct {
	SchemaVersion string `json:"schema_version"`

	CheckpointID    CheckpointID `json:"checkpoint_id"`
	AttemptID       AttemptID    `json:"attempt_id"`
	RunID           RunID        `json:"run_id"`
	WorkflowID      WorkflowID   `json:"workflow_id"`
	NodeID          NodeID       `json:"node_id,omitempty"`
	LeaseID         LeaseID      `json:"lease_id"`

	// Semantic fields from the worker.
	Phase               string   `json:"phase"`
	CompletedWork       []string `json:"completed_work"`
	RemainingWork      []string `json:"remaining_work"`
	ArtifactRefs       []string `json:"artifact_references"`
	LastCommittedAction string   `json:"last_committed_action"`
	SafeToResume       bool     `json:"safe_to_resume"`

	// Integrity digests.
	CheckpointDigest   string `json:"checkpoint_digest"`
	ArtifactMetaDigest string `json:"artifact_meta_digest"`

	// Sequence from the journal.
	Sequence int64 `json:"sequence"`

	// Timestamps.
	CreatedAt time.Time `json:"created_at"`
}

// ComputeDigest returns SHA-256 hex of the canonical checkpoint content
// (Phase, CompletedWork, RemainingWork, LastCommittedAction, SafeToResume,
// ArtifactRefs). This is used to verify checkpoint integrity on read-back.
// An empty digest for a safe-to-resume checkpoint is invalid per pitfall #149.
func (cp *SemanticCheckpoint) ComputeDigest() string {
	canonical := map[string]any{
		"phase":                 cp.Phase,
		"completed_work":        cp.CompletedWork,
		"remaining_work":        cp.RemainingWork,
		"last_committed_action": cp.LastCommittedAction,
		"safe_to_resume":        cp.SafeToResume,
		"artifact_references":   cp.ArtifactRefs,
	}
	b, _ := json.Marshal(canonical)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// VerifyDigest checks that the checkpoint's digest is non-empty and matches
// the SHA-256 of the canonical checkpoint content. Returns nil if valid, or
// an error describing the mismatch.
func (cp *SemanticCheckpoint) VerifyDigest() error {
	if cp.CheckpointDigest == "" {
		return fmt.Errorf("checkpoint digest is empty (pitfall #149)")
	}
	expected := cp.ComputeDigest()
	if cp.CheckpointDigest != expected {
		return fmt.Errorf("checkpoint digest mismatch: stored=%s computed=%s", cp.CheckpointDigest, expected)
	}
	return nil
}

// CheckpointStore defines persistence operations for semantic checkpoints.
type CheckpointStore interface {
	// SaveCheckpoint atomically persists a semantic checkpoint.
	// It must never mutate an existing checkpoint.
	SaveCheckpoint(ctx context.Context, cp *SemanticCheckpoint) error

	// GetCheckpoint retrieves a checkpoint by ID.
	GetCheckpoint(ctx context.Context, checkpointID CheckpointID) (*SemanticCheckpoint, error)

	// GetLatestCheckpoint returns the latest safe checkpoint for a given attempt.
	GetLatestCheckpoint(ctx context.Context, attemptID AttemptID) (*SemanticCheckpoint, error)
}

// ---------------------------------------------------------------------------
// LocalStore checkpoint implementation
// ---------------------------------------------------------------------------

const checkpointDir = "checkpoints"

// SaveCheckpoint atomically persists a semantic checkpoint.
// If a checkpoint with the same ID already exists, it returns ErrAlreadyExists
// (idempotent: never mutates).
func (s *LocalStore) SaveCheckpoint(ctx context.Context, cp *SemanticCheckpoint) error {
	if cp == nil {
		return fmt.Errorf("%w: nil checkpoint", ErrInvalidArgument)
	}
	if cp.CheckpointID == "" {
		return fmt.Errorf("%w: empty checkpoint ID", ErrInvalidArgument)
	}
	if cp.AttemptID == "" {
		return fmt.Errorf("%w: empty attempt ID", ErrInvalidArgument)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cpPath := s.checkpointPath(cp.CheckpointID)

	// Idempotent: if file exists, return already-exists.
	if _, err := os.Stat(cpPath); err == nil {
		return fmt.Errorf("%w: checkpoint %s", ErrAlreadyExists, cp.CheckpointID)
	}

	cp.SchemaVersion = CurrentSchemaVersion
	// B30-2 (F8): verify caller-supplied digest matches ComputeDigest().
	// If the caller provides a digest that doesn't match, fail closed.
	// If the caller doesn't supply a digest, compute one.
	if cp.CheckpointDigest != "" {
		expected := cp.ComputeDigest()
		if cp.CheckpointDigest != expected {
			return fmt.Errorf("%w: caller-supplied checkpoint digest does not match computed digest", ErrInvalidArgument)
		}
	} else {
		cp.CheckpointDigest = cp.ComputeDigest()
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if int64(len(data)) > maxCheckpointBytes {
		return fmt.Errorf("%w: checkpoint %d bytes", ErrSizeCapExceeded, len(data))
	}

	return atomicWriteFile(cpPath, data, filePerm)
}

// GetCheckpoint retrieves a checkpoint by ID.
func (s *LocalStore) GetCheckpoint(ctx context.Context, checkpointID CheckpointID) (*SemanticCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cpPath := s.checkpointPath(checkpointID)
	data, err := os.ReadFile(cpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: checkpoint %s", ErrNotFound, checkpointID)
		}
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var cp SemanticCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	// Verify integrity digest.
	if err := cp.VerifyDigest(); err != nil {
		return nil, fmt.Errorf("checkpoint digest verification failed: %w", err)
	}
	return &cp, nil
}

// GetLatestCheckpoint returns the latest safe checkpoint for a given attempt.
// It scans the checkpoints directory for checkpoints with matching attempt_id
// and returns the one with the highest sequence number.
func (s *LocalStore) GetLatestCheckpoint(ctx context.Context, attemptID AttemptID) (*SemanticCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cpDir := filepath.Join(s.root, checkpointDir)
	entries, err := os.ReadDir(cpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: no checkpoints", ErrNotFound)
		}
		return nil, fmt.Errorf("read checkpoints dir: %w", err)
	}

	var latest *SemanticCheckpoint
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cpDir, entry.Name()))
		if err != nil {
			continue
		}
		var cp SemanticCheckpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			continue
		}
		if cp.AttemptID != attemptID {
			continue
		}
		if !cp.SafeToResume {
			continue
		}
		// Verify integrity digest; skip checkpoints that fail verification.
		if err := cp.VerifyDigest(); err != nil {
			continue
		}
		if latest == nil || cp.Sequence > latest.Sequence {
			latest = &cp
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("%w: no safe checkpoint for attempt %s", ErrNotFound, attemptID)
	}
	return latest, nil
}

// SaveAttemptProgress updates the attempt record with progress metadata.
// This is called by the progress tailer after ingesting a heartbeat.
func (s *LocalStore) SaveAttemptProgress(ctx context.Context, attemptID AttemptID, progress *AttemptProgress) error {
	if progress == nil {
		return fmt.Errorf("%w: nil progress", ErrInvalidArgument)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ppPath := s.progressPath(attemptID)
	data, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("marshal progress: %w", err)
	}
	return atomicWriteFile(ppPath, data, filePerm)
}

// GetAttemptProgress retrieves the latest progress metadata for an attempt.
func (s *LocalStore) GetAttemptProgress(ctx context.Context, attemptID AttemptID) (*AttemptProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ppPath := s.progressPath(attemptID)
	data, err := os.ReadFile(ppPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: progress for attempt %s", ErrNotFound, attemptID)
		}
		return nil, fmt.Errorf("read progress: %w", err)
	}

	var p AttemptProgress
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshal progress: %w", err)
	}
	return &p, nil
}

// AttemptProgress is the live progress metadata for an attempt.
type AttemptProgress struct {
	SchemaVersion string    `json:"schema_version"`
	AttemptID     AttemptID `json:"attempt_id"`
	RunID         RunID     `json:"run_id"`

	// Latest heartbeat.
	LastPhase      string    `json:"last_phase"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	LastSequence   int64     `json:"last_sequence"`

	// Latest checkpoint reference.
	LatestCheckpointID CheckpointID `json:"latest_checkpoint_id,omitempty"`
	ResumeCapability  *ResumeCapability `json:"resume_capability,omitempty"`
}

// path helpers

func (s *LocalStore) checkpointPath(cpID CheckpointID) string {
	return filepath.Join(s.root, checkpointDir, string(cpID)+".json")
}

func (s *LocalStore) progressPath(attemptID AttemptID) string {
	return filepath.Join(s.root, "progress", string(attemptID)+".json")
}

// maxCheckpointBytes is the 64 KiB limit from the B27 spec.
const maxCheckpointBytes = 64 * 1024

// ---------------------------------------------------------------------------
// Progress journal tailer
// ---------------------------------------------------------------------------

// ProgressTailer reads authenticated journal records and persists checkpoints.
// It verifies HMAC and sequence before updating any state.
type ProgressTailer struct {
	journalPath   string
	key           []byte
	store         CheckpointStore
	attemptID     AttemptID
	runID         RunID
	auditAppender audit.AuditAppender

	mu          sync.Mutex
	lastSeq     int64
	stopCh      chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
}

// NewProgressTailer creates a tailer for the given journal.
func NewProgressTailer(
	journalPath string,
	key []byte,
	store CheckpointStore,
	attemptID AttemptID,
	runID RunID,
) *ProgressTailer {
	return &ProgressTailer{
		journalPath: journalPath,
		key:         key,
		store:       store,
		attemptID:   attemptID,
		runID:       runID,
		stopCh:      make(chan struct{}),
		done:        make(chan struct{}),
	}
}

// SetAuditAppender attaches an audit appender for recording journal
// validation failures (e.g. tampered/malformed journal records).
// When set, the tailer emits a progress_journal_invalid audit event and
// marks ResumeCapability as ResumeCapNone on the first journal error.
func (t *ProgressTailer) SetAuditAppender(appender audit.AuditAppender) {
	t.auditAppender = appender
}

// journalLine is a single line from the progress journal.
type journalLine struct {
	SchemaVersion       string   `json:"schema_version"`
	WorkflowID          string   `json:"workflow_id"`
	NodeID              string   `json:"node_id"`
	RunID               string   `json:"run_id"`
	AttemptID           string   `json:"attempt_id"`
	LeaseID             string   `json:"lease_id"`
	Sequence            int64    `json:"sequence"`
	Timestamp           string   `json:"timestamp"`
	EventID             string   `json:"event_id"`
	Phase               string   `json:"phase"`
	CompletedWork       []string `json:"completed_work"`
	RemainingWork       []string `json:"remaining_work"`
	ArtifactRefs        []string `json:"artifact_references"`
	LastCommittedAction string   `json:"last_committed_action"`
	SafeToResume        bool     `json:"safe_to_resume"`
	CheckpointDigest    string   `json:"checkpoint_digest"`
	ArtifactMetaDigest  string   `json:"artifact_meta_digest"`
	HMAC                string   `json:"hmac"`
}

// IngestRecord verifies and ingests a single journal record.
// Returns the checkpoint ID if a checkpoint was created, or "" for a heartbeat.
// Returns an error if the record fails verification.
func (t *ProgressTailer) IngestRecord(ctx context.Context, line []byte) (string, error) {
	var rec journalLine
	if err := json.Unmarshal(line, &rec); err != nil {
		return "", fmt.Errorf("unmarshal journal line: %w", err)
	}

	// Verify HMAC.
	if !t.verifyHMAC(&rec) {
		return "", fmt.Errorf("HMAC verification failed for sequence %d", rec.Sequence)
	}

	// Verify run/attempt identity.
	if rec.RunID != string(t.runID) {
		return "", fmt.Errorf("journal run_id mismatch: expected %s got %s", t.runID, rec.RunID)
	}
	if rec.AttemptID != string(t.attemptID) {
		return "", fmt.Errorf("journal attempt_id mismatch: expected %s got %s", t.attemptID, rec.AttemptID)
	}

	// Verify monotonic sequence.
	t.mu.Lock()
	defer t.mu.Unlock()
	if rec.Sequence <= t.lastSeq {
		return "", fmt.Errorf("reordered or replayed record: seq %d <= last %d", rec.Sequence, t.lastSeq)
	}

	// Update heartbeat metadata.
	progress := &AttemptProgress{
		SchemaVersion: CurrentSchemaVersion,
		AttemptID:     t.attemptID,
		RunID:         t.runID,
		LastPhase:     rec.Phase,
		LastHeartbeat: time.Now().UTC(),
		LastSequence:  rec.Sequence,
	}

	// Persist checkpoint if safe_to_resume.
	var checkpointID string
	if rec.SafeToResume {
		cp := &SemanticCheckpoint{
			CheckpointID:       CheckpointID(fmt.Sprintf("cp-%s-%d", rec.AttemptID, rec.Sequence)),
			AttemptID:          t.attemptID,
			RunID:              t.runID,
			WorkflowID:         WorkflowID(rec.WorkflowID),
			NodeID:             NodeID(rec.NodeID),
			LeaseID:            LeaseID(rec.LeaseID),
			Phase:              rec.Phase,
			CompletedWork:      rec.CompletedWork,
			RemainingWork:      rec.RemainingWork,
			ArtifactRefs:       rec.ArtifactRefs,
			LastCommittedAction: rec.LastCommittedAction,
			SafeToResume:       true,
			CheckpointDigest:   rec.CheckpointDigest,
			ArtifactMetaDigest: rec.ArtifactMetaDigest,
			Sequence:           rec.Sequence,
			CreatedAt:          time.Now().UTC(),
		}
		if err := t.store.SaveCheckpoint(ctx, cp); err != nil {
			// Idempotent: if checkpoint already exists, that's fine.
			if !isAlreadyExists(err) {
				return "", fmt.Errorf("save checkpoint: %w", err)
			}
		}
		checkpointID = string(cp.CheckpointID)
		progress.LatestCheckpointID = cp.CheckpointID
		rc := ResumeCapSafeCheckpoint
		progress.ResumeCapability = &rc
	}

	// Persist progress metadata.
	if err := t.store.(*LocalStore).SaveAttemptProgress(ctx, t.attemptID, progress); err != nil {
		return checkpointID, fmt.Errorf("save progress: %w", err)
	}

	t.lastSeq = rec.Sequence
	return checkpointID, nil
}

// verifyHMAC computes and verifies the HMAC of a journal record.
func (t *ProgressTailer) verifyHMAC(rec *journalLine) bool {
	expected := rec.HMAC
	rec.HMAC = ""
	canonical, _ := json.Marshal(rec)
	mac := hmac.New(sha256.New, t.key)
	mac.Write(canonical)
	actual := hex.EncodeToString(mac.Sum(nil))
	rec.HMAC = expected
	return hmac.Equal([]byte(expected), []byte(actual))
}

// start begins tailing the journal file in a goroutine.
func (t *ProgressTailer) Start(ctx context.Context) {
	go t.run(ctx)
}

func (t *ProgressTailer) run(ctx context.Context) {
	defer close(t.done)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var offset int64
	for {
		select {
		case <-t.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := os.ReadFile(t.journalPath)
			if err != nil {
				continue
			}
			if int64(len(data)) <= offset {
				continue
			}
			newData := data[offset:]

			// Only advance past complete lines (terminated by \n).
			// Trailing fragments are held back for the next poll.
			lastNL := bytes.LastIndex(newData, []byte("\n"))
			if lastNL < 0 {
				// No complete line yet; wait for more data.
				continue
			}
			completeData := newData[:lastNL+1]
			offset += int64(lastNL + 1)

			lines := splitLines(completeData)
			for _, line := range lines {
				if len(line) == 0 {
					continue
				}
				if _, err := t.IngestRecord(ctx, line); err != nil {
					// On tampered/malformed journal, stop accepting progress.
					// Emit audit event if an appender is configured.
					if t.auditAppender != nil {
						_ = t.auditAppender.Append(audit.AuditRecord{
							Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
							EventType: "progress_journal_invalid",
							Actor:     "daemon",
							Payload: map[string]interface{}{
								"run_id":     string(t.runID),
								"attempt_id": string(t.attemptID),
								"error":      err.Error(),
							},
						})
					}
					// Mark resume capability as none.
					rc := ResumeCapNone
					progress := &AttemptProgress{
						SchemaVersion:    CurrentSchemaVersion,
						AttemptID:        t.attemptID,
						RunID:            t.runID,
						LastPhase:        "journal_invalid",
						LastHeartbeat:    time.Now().UTC(),
						LastSequence:     t.lastSeq,
						ResumeCapability: &rc,
					}
					if t.store != nil {
						if ls, ok := t.store.(*LocalStore); ok {
							_ = ls.SaveAttemptProgress(ctx, t.attemptID, progress)
						}
					}
					return
				}
			}
		}
	}
}

// Stop signals the tailer to stop and waits.
func (t *ProgressTailer) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
	<-t.done
}

// splitLines splits newline-delimited JSON.
// Only complete lines (terminated by \n) are returned. Trailing fragments
// without a newline are held back — the caller must not advance past them
// so the next poll picks them up after the writer flushes the complete line.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	// Do NOT include trailing fragment without a newline.
	return lines
}

// isAlreadyExists checks if an error is ErrAlreadyExists.
func isAlreadyExists(err error) bool {
	return err != nil && err.Error() != "" && containsStr(err.Error(), "already exists")
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
