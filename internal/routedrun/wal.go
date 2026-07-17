package routedrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WALOp is a single materialization operation inside a workflow transition.
type WALOp struct {
	// Kind is one of: workflow, node, run, handoff, child_batch, child_result, service, desired_state, amendment, control.
	Kind string `json:"kind"`
	// ID is the resource identity (node_id, run_id, etc.).
	ID string `json:"id,omitempty"`
	// Payload is the full JSON body to materialize (optional for delete).
	Payload json.RawMessage `json:"payload,omitempty"`
	// Action is put (default) or delete.
	Action string `json:"action,omitempty"`
}

// WALEntry is one durable journal entry for ApplyTransition.
type WALEntry struct {
	SchemaVersion string `json:"schema_version"`

	EntryID      string    `json:"entry_id"`
	WorkflowID   string    `json:"workflow_id"`
	Generation   int64     `json:"generation"` // expected generation before apply
	NewGeneration int64    `json:"new_generation"`
	Command      string    `json:"command"`
	Operations   []WALOp   `json:"operations,omitempty"`
	Committed    bool      `json:"committed"`
	CreatedAt    time.Time `json:"created_at"`
	CommittedAt  *time.Time `json:"committed_at,omitempty"`
}

// walDir returns the transactions directory for a workflow.
func (s *LocalStore) walDir(workflowID WorkflowID) string {
	return filepath.Join(s.workflowsDir(), safeID(string(workflowID)), "transactions")
}

// writeWALEntry writes an uncommitted WAL entry atomically.
func (s *LocalStore) writeWALEntry(wfID WorkflowID, entry *WALEntry) (string, error) {
	if entry.SchemaVersion == "" {
		entry.SchemaVersion = CurrentSchemaVersion
	}
	if entry.EntryID == "" {
		id, err := generateID("wal-")
		if err != nil {
			return "", err
		}
		entry.EntryID = id
	}
	dir := s.walDir(wfID)
	if err := mkdirProtected(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, entry.EntryID+".json")
	data, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(path, data, filePerm); err != nil {
		return "", err
	}
	return path, nil
}

// commitWALEntry marks the entry committed and fsyncs.
func (s *LocalStore) commitWALEntry(path string, entry *WALEntry) error {
	now := s.now()
	entry.Committed = true
	entry.CommittedAt = &now
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, filePerm)
}

// RecoverWAL replays committed entries and discards uncommitted ones for a workflow.
// Safe to call on store open or after a crash.
func (s *LocalStore) RecoverWAL(wfID WorkflowID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoverWAL(wfID)
}

// recoverWAL is the unlocked implementation of RecoverWAL.
func (s *LocalStore) recoverWAL(wfID WorkflowID) error {
	dir := s.walDir(wfID)
	if err := rejectSymlinkPath(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// dir may not exist
		if _, statErr := os.Lstat(dir); os.IsNotExist(statErr) {
			return nil
		}
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".tmp-") {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := readFileStrict(path, maxStateFileBytes)
		if err != nil {
			return err
		}
		var entry WALEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return fmt.Errorf("routedrun: corrupt wal %s: %w", path, err)
		}
		if !entry.Committed {
			// Uncommitted: discard (pre-transition state is authoritative).
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		// Committed: ensure materialization is present (idempotent re-apply).
		if err := s.materializeWALOps(WorkflowID(entry.WorkflowID), entry.Operations); err != nil {
			return err
		}
	}
	return nil
}

// materializeWALOps applies each operation to store files. Idempotent.
func (s *LocalStore) materializeWALOps(wfID WorkflowID, ops []WALOp) error {
	for _, op := range ops {
		action := op.Action
		if action == "" {
			action = "put"
		}
		path, err := s.pathForWALOp(wfID, op)
		if err != nil {
			return err
		}
		switch action {
		case "delete":
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		case "put":
			if len(op.Payload) == 0 {
				return fmt.Errorf("routedrun: wal put missing payload for %s/%s", op.Kind, op.ID)
			}
			if err := mkdirProtected(filepath.Dir(path)); err != nil {
				return err
			}
			if err := atomicWriteFile(path, op.Payload, filePerm); err != nil {
				return err
			}
		default:
			return fmt.Errorf("routedrun: unknown wal action %q", action)
		}
	}
	return nil
}

func (s *LocalStore) pathForWALOp(wfID WorkflowID, op WALOp) (string, error) {
	id := safeID(op.ID)
	base := filepath.Join(s.workflowsDir(), safeID(string(wfID)))
	switch op.Kind {
	case "workflow":
		return filepath.Join(base, "workflow.json"), nil
	case "node":
		return filepath.Join(base, "nodes", id+".json"), nil
	case "handoff":
		return filepath.Join(base, "handoffs", id+".json"), nil
	case "service":
		return filepath.Join(base, "services", id+".json"), nil
	case "child_batch":
		return filepath.Join(base, "child-batches", id+".json"), nil
	case "child_result":
		return filepath.Join(base, "child-results", id+".json"), nil
	case "desired_state":
		return filepath.Join(base, "desired_state.json"), nil
	case "amendment":
		return filepath.Join(base, "amendments", id+".json"), nil
	case "run":
		return filepath.Join(s.runsDir(), id, "run.json"), nil
	case "control":
		// controls append-only; path is not used for put of full file
		return filepath.Join(base, "controls", id+".json"), nil
	default:
		return "", fmt.Errorf("routedrun: unknown wal kind %q", op.Kind)
	}
}

// applyTransitionLocked implements ApplyTransition under s.mu.
// Sequence: write uncommitted WAL → fsync → materialize → commit WAL → fsync.
// Crash before commit leaves pre-transition state; after commit, recovery
// re-materializes.
func (s *LocalStore) applyTransitionLocked(wfID WorkflowID, expectedGeneration int64, command string, ops []WALOp) error {
	wf, gen, err := s.loadWorkflowCAS(wfID)
	if err != nil {
		return err
	}
	if gen != expectedGeneration {
		return fmt.Errorf("%w: workflow %s expected generation %d got %d", ErrCASConflict, wfID, expectedGeneration, gen)
	}

	newGen := gen + 1
	wf.Generation = newGen
	wf.UpdatedAt = s.now()
	if wf.SchemaVersion == "" {
		wf.SchemaVersion = CurrentSchemaVersion
	}

	// Always include updated workflow as first op (unless already present).
	wfPayload, err := marshalPersisted(newGen, wf)
	if err != nil {
		return err
	}
	fullOps := make([]WALOp, 0, len(ops)+1)
	hasWorkflow := false
	for _, op := range ops {
		if op.Kind == "workflow" {
			hasWorkflow = true
		}
		fullOps = append(fullOps, op)
	}
	if !hasWorkflow {
		fullOps = append([]WALOp{{
			Kind:    "workflow",
			ID:      string(wfID),
			Action:  "put",
			Payload: wfPayload,
		}}, fullOps...)
	}

	entry := &WALEntry{
		SchemaVersion: CurrentSchemaVersion,
		WorkflowID:    string(wfID),
		Generation:    expectedGeneration,
		NewGeneration: newGen,
		Command:       command,
		Operations:    fullOps,
		Committed:     false,
		CreatedAt:     s.now(),
	}
	path, err := s.writeWALEntry(wfID, entry)
	if err != nil {
		return err
	}
	// Materialize.
	if err := s.materializeWALOps(wfID, fullOps); err != nil {
		// Leave uncommitted entry for recovery to discard.
		return err
	}
	if err := s.commitWALEntry(path, entry); err != nil {
		return err
	}
	// Append to events.jsonl for observability.
	_ = s.appendWorkflowEvent(wfID, entry)
	return nil
}

func (s *LocalStore) appendWorkflowEvent(wfID WorkflowID, entry *WALEntry) error {
	dir := filepath.Join(s.workflowsDir(), safeID(string(wfID)))
	if err := mkdirProtected(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "events.jsonl")
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(line) > maxLedgerLineBytes {
		return fmt.Errorf("%w: event line", ErrSizeCapExceeded)
	}
	return appendJSONL(path, line)
}
