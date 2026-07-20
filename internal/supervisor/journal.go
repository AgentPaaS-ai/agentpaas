package supervisor

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// appendJournalEvent appends a control-journal event for the attempt. It opens
// the journal via the factory, appends, and closes. The journal's own HMAC is
// computed internally; callers do not supply an HMAC here.
func (s *Supervisor) appendJournalEvent(trk *attemptTracker, kind routedrun.InvokeJobEventKind, payload string) error {
	trk.mu.Lock()
	defer trk.mu.Unlock()
	return s.appendJournalEventLocked(trk, kind, payload)
}

// appendJournalEventLocked is the same as appendJournalEvent but assumes the
// caller already holds trk.mu.
func (s *Supervisor) appendJournalEventLocked(trk *attemptTracker, kind routedrun.InvokeJobEventKind, payload string) error {
	journal, err := s.journals.OpenControlJournal(trk.runID, trk.attemptID)
	if err != nil {
		return fmt.Errorf("open control journal: %w", err)
	}
	defer func() { _ = journal.Close() }()

	// Build the event. Retry on sequence-conflict errors (up to 3 times)
	// to handle read-modify-write races between supervisor instances (F17).
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		next := int64(1)
		if events, rerr := journal.Read(1); rerr == nil && len(events) > 0 {
			next = events[len(events)-1].Sequence + 1
		}
		event := routedrun.InvokeJobEvent{
			SchemaVersion: "1.0",
			Sequence:      next,
			Timestamp:     s.nowWall(),
			EventKind:     kind,
			Payload:       payload,
		}
		lastErr = journal.Append(event)
		if lastErr == nil {
			return nil
		}
		if !errors.Is(lastErr, routedrun.ErrJournalSequenceConflict) {
			return lastErr
		}
	}
	return fmt.Errorf("journal append after retries: %w", lastErr)
}

// loadOrCreateControlKey loads the per-attempt HMAC key from the control-key
// file at <stateRoot>/runs/<runID>/control-key, or generates a fresh 32-byte
// key and persists it at 0600. The key is shared across the run's attempts
// (the routedrun ControlJournal uses the same path).
//
// For the in-memory test journals, the factory owns the key and this method
// defers to the factory by reading the key it provisioned. For the real
// routedrun.ControlJournal, the key file is the source of truth.
func (s *Supervisor) loadOrCreateControlKey(runID routedrun.RunID, attemptID routedrun.AttemptID) ([]byte, error) {
	// If the factory is the fake (tests), it owns the key and OpenControlJournal
	// has provisioned it. Ask the factory for a journal and read its key. We
	// cannot read the key directly from the interface, so we use a type
	// assertion to a key-exposing interface when available.
	if k, ok := s.journals.(interface{ KeyFor(runID routedrun.RunID, attemptID routedrun.AttemptID) ([]byte, error) }); ok {
		return k.KeyFor(runID, attemptID)
	}
	// Real path: read or create the control-key file under the state root.
	if s.stateRoot == "" {
		// No state root: generate an ephemeral key (tests without a file
		// journal). This key is lost on restart, which is acceptable for the
		// in-memory test journals that do not persist.
		return ephemeralKey()
	}
	runDir := filepath.Join(s.stateRoot, "runs", string(runID))
	keyPath := filepath.Join(runDir, "control-key")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir run dir: %w", err)
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("%w: control-key length %d want 32", ErrInvalidArgument, len(data))
		}
		return data, nil
	} else if !os.IsNotExist(err) && !errors.Is(err, routedrun.ErrNotFound) {
		return nil, err
	}
	key, err := ephemeralKey()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("write control key: %w", err)
	}
	return key, nil
}

// ephemeralKey generates a 32-byte random key.
func ephemeralKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate control key: %w", err)
	}
	return key, nil
}
