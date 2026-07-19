// Package runtime implements activation policy validation, zero-authority
// idle-state enforcement, and the interactive inbox / suspend-wake protocol
// (B29-T05).
//
// This file implements the durable inbox store: approved senders can append
// input/approval messages to a task's inbox. The inbox is durable (survives
// process restart) and tenant-scoped. Inbox content is untrusted data — it is
// stored verbatim but never expands worker authority. Approval resolutions are
// validated against the original allowed options before they take effect.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// Sentinel errors for the inbox store.
var (
	// ErrInboxClosed is returned after the inbox store is closed.
	ErrInboxClosed = errors.New("runtime: inbox store closed")
	// ErrInboxInvalidPath is returned for empty/unsafe state paths.
	ErrInboxInvalidPath = errors.New("runtime: invalid inbox store path component")
	// ErrInboxSymlink is returned when a path component is a symlink.
	ErrInboxSymlink = errors.New("runtime: symlink rejected in inbox path")
	// ErrInboxUnsafePerm is returned when a path component is world-readable.
	ErrInboxUnsafePerm = errors.New("runtime: unsafe permissions in inbox path")
	// ErrInboxEmptyArg is returned when tenant_id/run_id/task_id is empty.
	ErrInboxEmptyArg = errors.New("runtime: tenant_id, run_id, and task_id must not be empty")
	// ErrInboxTooLarge is returned when a message exceeds the size cap.
	ErrInboxTooLarge = errors.New("runtime: inbox payload exceeds size cap")
	// ErrInboxUnknownMessage is returned by MarkDelivered for an unknown id.
	ErrInboxUnknownMessage = errors.New("runtime: unknown inbox message")
)

// Inbox message types. The Type field discriminates user input from
// approvals. Both are stored as opaque Content; only approval resolutions are
// validated against an allowed option set (see approval.go).
type InboxMessageType string

const (
	// InboxTypeInput is a free-form input message from an approved sender.
	InboxTypeInput InboxMessageType = "input"
	// InboxTypeApproval is an approval resolution attached to a task inbox.
	InboxTypeApproval InboxMessageType = "approval"
)

// MessageID uniquely identifies an inbox message within a tenant. It is
// assigned by Append and is stable across restarts.
type MessageID string

// InboxMessage is a single durable inbox entry. Content is untrusted data:
// it is stored and delivered verbatim and must never be interpreted as
// authority (e.g. it cannot grant new credentials or expand scope).
type InboxMessage struct {
	MessageID  string            `json:"message_id"`
	TenantID   string            `json:"tenant_id"`
	RunID      string            `json:"run_id"`
	TaskID     string            `json:"task_id"`
	SenderID   string            `json:"sender_id"`
	Type       InboxMessageType  `json:"type"`
	Content    []byte            `json:"content"`
	CreatedAt  time.Time         `json:"created_at"`
	Delivered  bool              `json:"delivered"`
}

// InboxStore is the durable inbox interface. Implementations must be
// tenant-scoped: cross-tenant List/Append returns empty/error.
type InboxStore interface {
	// Append adds a message to a task's inbox. Returns the assigned
	// MessageID. The append is durable (WAL + fsync) and emits a wake event
	// to the event store so a waiting worker is notified without polling.
	Append(ctx context.Context, msg InboxMessage) (MessageID, error)
	// List returns the undelivered messages for a task in append order.
	List(ctx context.Context, tenantID, runID, taskID string) ([]InboxMessage, error)
	// MarkDelivered marks the given messages as consumed by the worker.
	MarkDelivered(ctx context.Context, messageIDs []MessageID) error
	// Purge removes all messages for a run (cleanup after terminal state).
	Purge(ctx context.Context, tenantID, runID string) error
}

const (
	// inboxDirPerm matches the routedrun/home convention of 0700.
	inboxDirPerm = os.FileMode(0o700)
	// inboxFilePerm matches the routedrun WAL convention of 0600.
	inboxFilePerm = os.FileMode(0o600)
	// maxInboxPayloadBytes caps a single message content (matches the event
	// store's 1 MiB cap so an inbox message can always be mirrored as an
	// event).
	maxInboxPayloadBytes = 1 << 20
	// maxInboxWALFileBytes caps a single inbox WAL file so recovery cannot OOM.
	maxInboxWALFileBytes int64 = 64 << 20
	// inboxWALSuffix is the on-disk write-ahead log suffix per run.
	inboxWALSuffix = ".wal"
)

// inboxWALRecord is one JSON-encoded line in the per-run inbox WAL.
type inboxWALRecord struct {
	SchemaVersion string           `json:"schema_version"`
	MessageID    string           `json:"message_id"`
	TenantID     string           `json:"tenant_id"`
	RunID        string           `json:"run_id"`
	TaskID       string           `json:"task_id"`
	SenderID     string           `json:"sender_id"`
	Type         InboxMessageType `json:"type"`
	Content      []byte           `json:"content"`
	CreatedAt    time.Time         `json:"created_at"`
	Delivered    bool             `json:"delivered"`
}

// inboxRunState holds the per-run in-memory index reconstructed from the WAL.
type inboxRunState struct {
	mu       sync.Mutex
	messages []InboxMessage
	closed   bool
}

// DurableInboxStore implements InboxStore with an append-only WAL per
// tenant/run. Messages survive process restart. Each Append also appends a
// wake event to the paired event store so a waiting worker is notified.
type DurableInboxStore struct {
	stateDir string
	events   port.EventStore

	mu   sync.Mutex
	runs map[string]*inboxRunState

	// randID generates a new MessageID. Overridable for tests.
	randID func() (string, error)
}

// NewDurableInboxStore opens (or creates) a durable inbox store at stateDir,
// paired with an event store used to emit wake events on Append. Existing WAL
// files are replayed on open.
func NewDurableInboxStore(stateDir string, events port.EventStore) (*DurableInboxStore, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("%w: state dir is empty", ErrInboxInvalidPath)
	}
	if events == nil {
		return nil, fmt.Errorf("%w: event store is nil", ErrInboxInvalidPath)
	}
	cleaned := filepath.Clean(stateDir)
	if err := inboxRejectSymlinkPath(cleaned); err != nil {
		return nil, err
	}
	if err := inboxMkdirProtected(cleaned); err != nil {
		return nil, fmt.Errorf("runtime: create inbox state dir %s: %w", cleaned, err)
	}
	s := &DurableInboxStore{
		stateDir: cleaned,
		events:   events,
		runs:     make(map[string]*inboxRunState),
		randID:   defaultInboxRandID,
	}
	if err := s.recover(); err != nil {
		return nil, fmt.Errorf("runtime: recover inbox store: %w", err)
	}
	return s, nil
}

// defaultInboxRandID generates a 16-byte random hex ID.
func defaultInboxRandID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "msg-" + hex.EncodeToString(b), nil
}

// Close releases resources. After Close, Append/List/MarkDelivered/Purge
// return ErrInboxClosed. It is idempotent.
func (s *DurableInboxStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
	}
	s.runs = nil
	return nil
}

// Append adds a message to the task's inbox and emits a wake event.
func (s *DurableInboxStore) Append(_ context.Context, msg InboxMessage) (MessageID, error) {
	if msg.TenantID == "" || msg.RunID == "" || msg.TaskID == "" {
		return "", fmt.Errorf("%w: tenant=%q run=%q task=%q", ErrInboxEmptyArg, msg.TenantID, msg.RunID, msg.TaskID)
	}
	if int64(len(msg.Content)) > maxInboxPayloadBytes {
		return "", fmt.Errorf("%w: %d bytes", ErrInboxTooLarge, len(msg.Content))
	}
	if msg.Type == "" {
		msg.Type = InboxTypeInput
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	id, err := s.randID()
	if err != nil {
		return "", fmt.Errorf("runtime: inbox generate id: %w", err)
	}
	msg.MessageID = id
	msg.Delivered = false

	r, err := s.runState(msg.TenantID, msg.RunID, true)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", ErrInboxClosed
	}
	rec := inboxWALRecord{
		SchemaVersion: "1.0",
		MessageID:    msg.MessageID,
		TenantID:     msg.TenantID,
		RunID:        msg.RunID,
		TaskID:       msg.TaskID,
		SenderID:     msg.SenderID,
		Type:         msg.Type,
		Content:      msg.Content,
		CreatedAt:    msg.CreatedAt,
		Delivered:    false,
	}
	if err := s.appendWAL(msg.TenantID, msg.RunID, rec); err != nil {
		return "", fmt.Errorf("runtime: inbox append wal: %w", err)
	}
	r.messages = append(r.messages, msg)

	// Emit a wake event so a waiting worker is notified without polling.
	// The event payload carries the message id and task id; content is NOT
	// placed in the event (it is untrusted and the worker reads it via List).
	//
	// A durable message existing without a corresponding wake event is the
	// worst failure mode of the inbox protocol: a waiting worker would
	// never be resumed. Surface the Append error to the caller so the
	// failure is observable and the caller can retry or reconcile.
	wakePayload, _ := json.Marshal(inboxWakePayload{
		MessageID: msg.MessageID,
		TaskID:    msg.TaskID,
		Type:      string(msg.Type),
	})
	if _, err := s.events.Append(context.Background(), port.Event{
		TenantID:  msg.TenantID,
		RunID:     msg.RunID,
		Type:      wakeEventTypeInbox,
		Payload:   wakePayload,
		Timestamp: msg.CreatedAt,
	}); err != nil {
		return MessageID(msg.MessageID), fmt.Errorf("runtime: inbox append wake event: %w", err)
	}
	return MessageID(msg.MessageID), nil
}

// inboxWakePayload is the event payload emitted on inbox Append.
type inboxWakePayload struct {
	MessageID string `json:"message_id"`
	TaskID    string `json:"task_id"`
	Type      string `json:"type"`
}

// List returns the undelivered messages for a task in append order.
// Cross-tenant List returns empty (never an error).
func (s *DurableInboxStore) List(_ context.Context, tenantID, runID, taskID string) ([]InboxMessage, error) {
	if tenantID == "" || runID == "" || taskID == "" {
		return nil, nil
	}
	r, ok := s.lookupRun(tenantID, runID)
	if !ok {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]InboxMessage, 0)
	for _, m := range r.messages {
		if m.Delivered {
			continue
		}
		if m.TaskID != taskID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// MarkDelivered marks the given messages as consumed.
func (s *DurableInboxStore) MarkDelivered(_ context.Context, ids []MessageID) error {
	if len(ids) == 0 {
		return nil
	}
	// Index ids for quick lookup.
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[string(id)] = true
	}
	// Rewrite every affected run's WAL. We scan all runs; only the runs that
	// hold any of the requested ids are rewritten.
	s.mu.Lock()
	runs := make([]*inboxRunState, 0, len(s.runs))
	for _, r := range s.runs {
		runs = append(runs, r)
	}
	s.mu.Unlock()

	for _, r := range runs {
		r.mu.Lock()
		changed := false
		for i := range r.messages {
			if want[r.messages[i].MessageID] {
				if !r.messages[i].Delivered {
					r.messages[i].Delivered = true
					changed = true
				}
			}
		}
		if changed {
			if err := s.rewriteWALLocked(r); err != nil {
				r.mu.Unlock()
				return fmt.Errorf("runtime: inbox rewrite wal: %w", err)
			}
		}
		r.mu.Unlock()
	}
	return nil
}

// Purge removes all messages for a run (cleanup after terminal state).
func (s *DurableInboxStore) Purge(_ context.Context, tenantID, runID string) error {
	if tenantID == "" || runID == "" {
		return nil
	}
	key := inboxRunKey(tenantID, runID)
	s.mu.Lock()
	r, ok := s.runs[key]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	delete(s.runs, key)
	s.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	path := s.walPath(tenantID, runID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("runtime: purge inbox %s: %w", path, err)
	}
	return nil
}

// runState returns the in-memory state for (tenantID, runID), creating it if
// create is true. The caller is responsible for locking the returned state.
func (s *DurableInboxStore) runState(tenantID, runID string, create bool) (*inboxRunState, error) {
	key := inboxRunKey(tenantID, runID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[key]; ok {
		return r, nil
	}
	if !create {
		return nil, nil
	}
	r := &inboxRunState{}
	s.runs[key] = r
	return r, nil
}

// lookupRun is the non-creating variant of runState.
func (s *DurableInboxStore) lookupRun(tenantID, runID string) (*inboxRunState, bool) {
	r, err := s.runState(tenantID, runID, false)
	if err != nil || r == nil {
		return nil, false
	}
	return r, true
}

// inboxRunKey produces a stable map key for a tenant/run pair.
func inboxRunKey(tenantID, runID string) string {
	return tenantID + "\x00" + runID
}

// walPath returns the on-disk WAL path for a tenant/run pair.
func (s *DurableInboxStore) walPath(tenantID, runID string) string {
	return filepath.Join(s.stateDir, inboxSafeID(tenantID), inboxSafeID(runID)+inboxWALSuffix)
}

// appendWAL writes one JSON line + newline to the per-run WAL and fsyncs. The
// caller holds the runState mutex.
func (s *DurableInboxStore) appendWAL(tenantID, runID string, rec inboxWALRecord) error {
	path := s.walPath(tenantID, runID)
	dir := filepath.Dir(path)
	if err := inboxMkdirProtected(dir); err != nil {
		return err
	}
	if err := inboxRejectSymlinkPath(path); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if int64(len(line)) > maxInboxPayloadBytes+1024 {
		return fmt.Errorf("%w: encoded record %d bytes", ErrInboxTooLarge, len(line))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, inboxFilePerm)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s mode %#o", ErrInboxUnsafePerm, path, fi.Mode().Perm())
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// rewriteWALLocked rewrites the entire WAL for the run from the in-memory
// index. Called after MarkDelivered mutates the in-memory state. The caller
// holds r.mu.
func (s *DurableInboxStore) rewriteWALLocked(r *inboxRunState) error {
	// We need the tenant/run for this run state; scan messages for the first
	// non-empty pair (all messages in a run share tenant/run).
	if len(r.messages) == 0 {
		return nil
	}
	tenantID := r.messages[0].TenantID
	runID := r.messages[0].RunID
	path := s.walPath(tenantID, runID)
	dir := filepath.Dir(path)
	if err := inboxMkdirProtected(dir); err != nil {
		return err
	}
	if err := inboxRejectSymlinkPath(path); err != nil {
		return err
	}
	// Write to a temp file then atomically rename.
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, inboxFilePerm)
	if err != nil {
		return err
	}
	w := newBufioWriter(f)
	for _, m := range r.messages {
		rec := inboxWALRecord{
			SchemaVersion: "1.0",
			MessageID:    m.MessageID,
			TenantID:     m.TenantID,
			RunID:        m.RunID,
			TaskID:       m.TaskID,
			SenderID:     m.SenderID,
			Type:         m.Type,
			Content:      m.Content,
			CreatedAt:    m.CreatedAt,
			Delivered:    m.Delivered,
		}
		line, err := json.Marshal(rec)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// recover replays every WAL file under stateDir, reconstructing the
// in-memory index. Corrupt lines are rejected (fail closed).
func (s *DurableInboxStore) recover() error {
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, tenantEntry := range entries {
		if !tenantEntry.IsDir() {
			continue
		}
		if err := inboxRejectSymlinkLeaf(filepath.Join(s.stateDir, tenantEntry.Name())); err != nil {
			return err
		}
		tenantPath := filepath.Join(s.stateDir, tenantEntry.Name())
		runEntries, err := os.ReadDir(tenantPath)
		if err != nil {
			return err
		}
		for _, runEntry := range runEntries {
			if runEntry.IsDir() || !strings.HasSuffix(runEntry.Name(), inboxWALSuffix) {
				continue
			}
			path := filepath.Join(tenantPath, runEntry.Name())
			if err := s.recoverWALFile(path); err != nil {
				return err
			}
		}
	}
	return nil
}

// recoverWALFile replays a single WAL file into the in-memory index.
func (s *DurableInboxStore) recoverWALFile(path string) error {
	data, err := inboxReadFileStrict(path, maxInboxWALFileBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range splitInboxWALLines(data) {
		if len(line) == 0 {
			continue
		}
		var rec inboxWALRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("runtime: corrupt inbox wal %s: %w", path, err)
		}
		r, err := s.runState(rec.TenantID, rec.RunID, true)
		if err != nil {
			return err
		}
		r.mu.Lock()
		r.messages = append(r.messages, InboxMessage{
			MessageID: rec.MessageID,
			TenantID:  rec.TenantID,
			RunID:     rec.RunID,
			TaskID:    rec.TaskID,
			SenderID:  rec.SenderID,
			Type:      rec.Type,
			Content:   rec.Content,
			CreatedAt: rec.CreatedAt,
			Delivered: rec.Delivered,
		})
		r.mu.Unlock()
	}
	return nil
}

// splitInboxWALLines splits a WAL file's bytes into per-record line slices.
func splitInboxWALLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// Compile-time assertion: DurableInboxStore implements InboxStore.
var _ InboxStore = (*DurableInboxStore)(nil)
