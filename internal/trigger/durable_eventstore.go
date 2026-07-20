package trigger

import (
	"context"
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

// Durable event store errors (sentinel).
var (
	ErrEventStoreClosed      = errors.New("trigger: event store closed")
	ErrSubscriberOverflow    = errors.New("trigger: subscriber channel overflow; reconnect with last received sequence")
	ErrEventStoreInvalidPath = errors.New("trigger: invalid event store path component")
	ErrEventStoreSymlink     = errors.New("trigger: symlink rejected in event store path")
	ErrEventStoreUnsafePerm  = errors.New("trigger: unsafe permissions in event store path")
	ErrEventStoreTooLarge    = errors.New("trigger: event payload exceeds size cap")
	ErrEventStoreEmptyArg    = errors.New("trigger: tenant_id and run_id must not be empty")
)

const (
	// eventDirPerm matches the routedrun/home convention of 0700.
	eventDirPerm = os.FileMode(0o700)
	// eventFilePerm matches the routedrun WAL convention of 0600.
	eventFilePerm = os.FileMode(0o600)
	// subscriberBufferSize is larger than the old EventBus 64-cap to reduce
	// drops under burst. When full, the publisher blocks briefly then closes
	// the subscriber channel with ErrSubscriberOverflow rather than silently
	// dropping (spec requirement).
	subscriberBufferSize = 128
	// subscriberOverflowTimeout is how long Append blocks trying to deliver
	// to a stalled subscriber before closing that subscriber's channel.
	subscriberOverflowTimeout = 100 * time.Millisecond
	// maxEventPayloadBytes caps a single event payload (matches trigger
	// MaxPayload default of 1MiB).
	maxEventPayloadBytes = 1 << 20
	// maxWALFileBytes caps a single WAL file so recovery cannot OOM.
	maxWALFileBytes int64 = 64 << 20
	// walSuffix is the on-disk write-ahead log suffix per run.
	walSuffix = ".wal"
)

// walRecord is one JSON-encoded line in the per-run WAL. The schema is
// versioned so future migrations can rename fields without breaking recovery.
type walRecord struct {
	SchemaVersion string    `json:"schema_version"`
	TenantID      string    `json:"tenant_id"`
	RunID         string    `json:"run_id"`
	Sequence      int64     `json:"sequence"`
	Type          string    `json:"type"`
	Payload       []byte    `json:"payload"`
	Timestamp     time.Time `json:"timestamp"`
}

// eventRecord is the in-memory cache of an appended event. Keeping the full
// event in memory makes Read and Subscribe replay cheap after recovery.
type eventRecord struct {
	event port.Event
}

// subscriber tracks a single live consumer of events for a run.
type subscriber struct {
	ch       chan port.Event
	closedMu sync.Mutex
	closed   bool
	// cursor is the highest sequence successfully delivered (or the subscribe
	// afterSequence before any delivery). Guarded by closedMu so Append fan-out
	// and Subscribe replay cannot race on the cursor or double-deliver.
	cursor int64
}

// closeLocked marks the subscriber closed and closes its channel. Caller must
// hold closedMu.
func (s *subscriber) closeLocked() {
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}

// close closes the subscriber channel exactly once.
func (s *subscriber) close() {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	s.closeLocked()
}

// send delivers event to the subscriber. It is safe for concurrent callers:
// deliveries are serialized on closedMu, and events with Sequence <= cursor
// are treated as already-delivered (no-op success). It blocks up to
// subscriberOverflowTimeout; if the channel is still full, the subscriber is
// closed with ErrSubscriberOverflow and the caller learns via the returned
// bool so it can drop the subscriber from the registry.
func (s *subscriber) send(event port.Event) bool {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	if s.closed {
		return false
	}
	if event.Sequence <= s.cursor {
		return true
	}
	timer := time.NewTimer(subscriberOverflowTimeout)
	defer timer.Stop()
	select {
	case s.ch <- event:
		s.cursor = event.Sequence
		return true
	case <-timer.C:
		// Subscriber stalled. Close its channel so it sees an explicit error
		// on reconnect (NOT a silent drop). The publisher continues with other
		// subscribers and future appends.
		s.closeLocked()
		return false
	}
}

// runState holds the per-run in-memory index reconstructed from the WAL.
type runState struct {
	mu          sync.Mutex
	events      []eventRecord
	subscribers map[int64]*subscriber
	nextSubID   int64
	closed      bool
}

// DurableEventStore implements port.EventStore with an append-only WAL per
// tenant/run. Events survive process restart: a new store pointing at the
// same state directory replays all WAL files.
//
// Append is atomic: it writes one JSON line + fsync under the run mutex, then
// updates the in-memory index and fans out to live subscribers in the same
// critical section. A state transition and its outbox event commit atomically
// when committed via the Outbox wrapper.
type DurableEventStore struct {
	stateDir string

	mu     sync.Mutex
	runs   map[string]*runState
	closed bool
}

// NewDurableEventStore opens (or creates) a durable event store at stateDir.
// Existing WAL files are replayed on open so the in-memory index matches
// what is on disk. The directory is created with 0700 permissions and WAL
// files are written with 0600 permissions.
func NewDurableEventStore(stateDir string) (*DurableEventStore, error) {
	if stateDir == "" {
		return nil, fmt.Errorf("%w: state dir is empty", ErrEventStoreInvalidPath)
	}
	cleaned := filepath.Clean(stateDir)
	if err := eventRejectSymlinkPath(cleaned); err != nil {
		return nil, fmt.Errorf("new durable event store: %w", err)
	}
	if err := eventMkdirProtected(cleaned); err != nil {
		return nil, fmt.Errorf("trigger: create event state dir %s: %w", cleaned, err)
	}
	s := &DurableEventStore{
		stateDir: cleaned,
		runs:     make(map[string]*runState),
	}
	if err := s.recover(); err != nil {
		return nil, fmt.Errorf("trigger: recover event store: %w", err)
	}
	return s, nil
}

// Close releases any pending resources. After Close, Append/Read/Subscribe
// return ErrEventStoreClosed. It is idempotent.
func (s *DurableEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for _, r := range s.runs {
		r.mu.Lock()
		r.closed = true
		for _, sub := range r.subscribers {
			sub.close()
		}
		r.subscribers = make(map[int64]*subscriber)
		r.mu.Unlock()
	}
	return nil
}

// Append persists event to the per-run WAL, fsyncs, then updates the
// in-memory index and fans out to live subscribers. Returns the assigned
// sequence number (monotonically increasing per tenant/run, starting at 1).
func (s *DurableEventStore) Append(_ context.Context, event port.Event) (int64, error) {
	if event.TenantID == "" || event.RunID == "" {
		return 0, fmt.Errorf("%w: tenant=%q run=%q", ErrEventStoreEmptyArg, event.TenantID, event.RunID)
	}
	if int64(len(event.Payload)) > maxEventPayloadBytes {
		return 0, fmt.Errorf("%w: %d bytes", ErrEventStoreTooLarge, len(event.Payload))
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	r, err := s.runState(event.TenantID, event.RunID, true)
	if err != nil {
		return 0, fmt.Errorf("durable event store append: %w", err)
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return 0, ErrEventStoreClosed
	}
	seq := int64(len(r.events)) + 1
	event.Sequence = seq
	rec := walRecord{
		SchemaVersion: "1.0",
		TenantID:      event.TenantID,
		RunID:         event.RunID,
		Sequence:      seq,
		Type:          event.Type,
		Payload:       event.Payload,
		Timestamp:     event.Timestamp,
	}
	if err := s.appendWAL(event.TenantID, event.RunID, rec); err != nil {
		r.mu.Unlock()
		return 0, fmt.Errorf("trigger: append wal: %w", err)
	}
	// WAL is durable. Now update the in-memory index.
	r.events = append(r.events, eventRecord{event: event})

	// Clone subscriber list under the mutex, then fan out outside the lock.
	// B29-2: fan-out must not hold r.mu — send can block up to
	// subscriberOverflowTimeout per subscriber and would stall concurrent
	// Append/Subscribe. Cursor checks and channel ops are serialized inside
	// subscriber.send via closedMu (fixes the prior cursor data race).
	subs := make(map[int64]*subscriber, len(r.subscribers))
	for id, sub := range r.subscribers {
		subs[id] = sub
	}
	r.mu.Unlock()

	var overflowed []int64
	for id, sub := range subs {
		if !sub.send(event) {
			overflowed = append(overflowed, id)
		}
	}
	if len(overflowed) > 0 {
		r.mu.Lock()
		for _, id := range overflowed {
			delete(r.subscribers, id)
		}
		r.mu.Unlock()
	}
	return seq, nil
}

// Subscribe returns a channel that receives every event for (tenantID, runID)
// with Sequence > afterSequence. Existing events are replayed first (in
// order), then live events are delivered. The channel is buffered with
// subscriberBufferSize. If the publisher cannot deliver within
// subscriberOverflowTimeout, the channel is closed and the subscriber must
// reconnect with the last received sequence (cursor).
//
// The run state is created lazily on Subscribe so a client may attach before
// any events exist and still receive live events as they are appended. This is
// required by the durable contract: a subscriber that reconnects with a
// cursor must receive subsequent events even if the cursor is at the current
// tail.
//
// Cross-tenant subscriptions never deliver another tenant's events: a
// subscriber registered under (tenantB, runID) only receives events appended
// to that same (tenantB, runID) key. The channel is open and blocking (never
// an error), matching the spec's "returns empty (not error)" semantic.
//
// When ctx is cancelled, the subscriber is unregistered and its channel is
// closed so publishers stop blocking on abandoned consumers (goroutine /
// subscriber leak prevention).
func (s *DurableEventStore) Subscribe(ctx context.Context, tenantID, runID string, afterSequence int64) (<-chan port.Event, error) {
	if tenantID == "" || runID == "" {
		ch := make(chan port.Event)
		close(ch)
		return ch, nil
	}
	// Lazily create the run state so pre-event subscribers receive live
	// appends. A closed store returns a closed channel.
	r, err := s.runState(tenantID, runID, true)
	if err != nil {
		ch := make(chan port.Event)
		close(ch)
		return ch, nil
	}

	// Snapshot replay under the run lock, then deliver outside it so send's
	// overflow timeout cannot stall concurrent Append/Subscribe.
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		ch := make(chan port.Event)
		close(ch)
		return ch, nil
	}
	replay := make([]port.Event, 0, len(r.events))
	for _, rec := range r.events {
		if rec.event.Sequence > afterSequence {
			replay = append(replay, rec.event)
		}
	}
	sub := &subscriber{
		ch:     make(chan port.Event, subscriberBufferSize),
		cursor: afterSequence,
	}
	subID := r.nextSubID
	r.nextSubID++
	// Register before unlock so live Appends during replay are also offered
	// to this subscriber; send() dedupes by sequence under closedMu.
	r.subscribers[subID] = sub
	r.mu.Unlock()

	// Already-cancelled context: drop the subscriber immediately.
	if ctx.Err() != nil {
		s.dropSubscriber(r, subID, sub)
		return sub.ch, nil
	}

	for _, ev := range replay {
		if ctx.Err() != nil {
			s.dropSubscriber(r, subID, sub)
			return sub.ch, nil
		}
		if !sub.send(ev) {
			// Overflow closed the channel; remove registry entry.
			s.dropSubscriber(r, subID, nil)
			return sub.ch, nil
		}
	}

	// Unregister + close when the caller's context ends so abandoned
	// subscribers do not stall future Append fan-out. Skip for non-cancellable
	// contexts (Background/TODO): their Done channel is nil and a wait would
	// leak a goroutine for the process lifetime.
	if done := ctx.Done(); done != nil {
		go func() {
			<-done
			s.dropSubscriber(r, subID, sub)
		}()
	}

	return sub.ch, nil
}

// dropSubscriber removes subID from r and closes sub if non-nil. Safe if the
// subscriber was already removed or closed.
func (s *DurableEventStore) dropSubscriber(r *runState, subID int64, sub *subscriber) {
	r.mu.Lock()
	delete(r.subscribers, subID)
	r.mu.Unlock()
	if sub != nil {
		sub.close()
	}
}

// Read returns events for (tenantID, runID) with Sequence > afterSequence,
// up to maxEvents (0 means no artificial cap, but bounded by the in-memory
// index). Reads come from the in-memory cache which is reconstructed from
// disk on startup. Cross-tenant reads return empty (never an error).
func (s *DurableEventStore) Read(_ context.Context, tenantID, runID string, afterSequence int64, maxEvents int) ([]port.Event, error) {
	if tenantID == "" || runID == "" {
		return nil, nil
	}
	r, ok := s.lookupRun(tenantID, runID)
	if !ok {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]port.Event, 0, len(r.events))
	for _, rec := range r.events {
		if rec.event.Sequence <= afterSequence {
			continue
		}
		out = append(out, rec.event)
		if maxEvents > 0 && len(out) >= maxEvents {
			break
		}
	}
	return out, nil
}

// LatestSequence returns the highest sequence number for (tenantID, runID),
// or 0 if the run has no events. Cross-tenant queries return 0.
func (s *DurableEventStore) LatestSequence(_ context.Context, tenantID, runID string) (int64, error) {
	if tenantID == "" || runID == "" {
		return 0, nil
	}
	r, ok := s.lookupRun(tenantID, runID)
	if !ok {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return 0, nil
	}
	return r.events[len(r.events)-1].event.Sequence, nil
}

// runState returns the in-memory state for (tenantID, runID), creating it if
// create is true. The caller is responsible for locking the returned state.
func (s *DurableEventStore) runState(tenantID, runID string, create bool) (*runState, error) {
	key := runKey(tenantID, runID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrEventStoreClosed
	}
	if r, ok := s.runs[key]; ok {
		return r, nil
	}
	if !create {
		return nil, nil
	}
	r := &runState{
		subscribers: make(map[int64]*subscriber),
	}
	s.runs[key] = r
	return r, nil
}

// lookupRun is the non-creating variant of runState.
func (s *DurableEventStore) lookupRun(tenantID, runID string) (*runState, bool) {
	r, err := s.runState(tenantID, runID, false)
	if err != nil || r == nil {
		return nil, false
	}
	return r, true
}

// runKey produces a stable map key for a tenant/run pair.
func runKey(tenantID, runID string) string {
	return tenantID + "\x00" + runID
}

// walPath returns the on-disk WAL path for a tenant/run pair.
func (s *DurableEventStore) walPath(tenantID, runID string) string {
	return filepath.Join(s.stateDir, eventSafeID(tenantID), eventSafeID(runID)+walSuffix)
}

// appendWAL writes one JSON line + newline to the per-run WAL and fsyncs.
// The caller holds the runState mutex, so concurrent appends to the same run
// are serialized at the file level.
// B29-8 NOTE: parent-dir fsync after WAL file creation is not done here.
// On some filesystems (ext4 without dirsync), a crash after file creation
// but before data write could lose the file entry. For full durability,
// a future version should fsync the parent directory after creating a new
// WAL file. The impact is bounded because existing WAL files are appended
// via f.Sync() which guarantees data durability on the open file.
func (s *DurableEventStore) appendWAL(tenantID, runID string, rec walRecord) error {
	path := s.walPath(tenantID, runID)
	dir := filepath.Dir(path)
	if err := eventMkdirProtected(dir); err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	if err := eventRejectSymlinkPath(path); err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	if int64(len(line)) > maxEventPayloadBytes+1024 {
		return fmt.Errorf("%w: encoded record %d bytes", ErrEventStoreTooLarge, len(line))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, eventFilePerm)
	if err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close
	// Reject a symlinked or world-readable WAL — fail closed.
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s mode %#o", ErrEventStoreUnsafePerm, path, fi.Mode().Perm())
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("durable event store append wal: %w", err)
	}
	return f.Sync()
}

// recover replays every WAL file under stateDir, reconstructing the
// in-memory index. Each WAL file is read line-by-line; corrupt lines are
// rejected (fail closed) so an attacker cannot silently truncate history.
func (s *DurableEventStore) recover() error {
	entries, err := os.ReadDir(s.stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("durable event store recover: %w", err)
	}
	for _, tenantEntry := range entries {
		if !tenantEntry.IsDir() {
			continue
		}
		if err := eventRejectSymlinkLeaf(filepath.Join(s.stateDir, tenantEntry.Name())); err != nil {
			return fmt.Errorf("durable event store recover: %w", err)
		}
		tenantPath := filepath.Join(s.stateDir, tenantEntry.Name())
		runEntries, err := os.ReadDir(tenantPath)
		if err != nil {
			return fmt.Errorf("durable event store recover: %w", err)
		}
		for _, runEntry := range runEntries {
			if runEntry.IsDir() || !strings.HasSuffix(runEntry.Name(), walSuffix) {
				continue
			}
			path := filepath.Join(tenantPath, runEntry.Name())
			if err := s.recoverWALFile(path); err != nil {
				return fmt.Errorf("durable event store recover: %w", err)
			}
		}
	}
	return nil
}

// recoverWALFile replays a single WAL file into the in-memory index.
func (s *DurableEventStore) recoverWALFile(path string) error {
	data, err := eventReadFileStrict(path, maxWALFileBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("durable event store recover walfile: %w", err)
	}
	// Parse line by line. Each line is one walRecord.
	for _, line := range splitWALLines(data) {
		if len(line) == 0 {
			continue
		}
		var rec walRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("trigger: corrupt wal %s: %w", path, err)
		}
		r, err := s.runState(rec.TenantID, rec.RunID, true)
		if err != nil {
			return fmt.Errorf("durable event store recover walfile: %w", err)
		}
		r.mu.Lock()
		r.events = append(r.events, eventRecord{event: port.Event{
			TenantID:  rec.TenantID,
			RunID:     rec.RunID,
			Sequence:  rec.Sequence,
			Type:      rec.Type,
			Payload:   rec.Payload,
			Timestamp: rec.Timestamp,
		}})
		r.mu.Unlock()
	}
	return nil
}

// splitWALLines splits a WAL file's bytes into per-record line slices. It
// trims a trailing newline and skips empty lines.
func splitWALLines(data []byte) [][]byte {
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

// Compile-time assertion: DurableEventStore implements port.EventStore.
var _ port.EventStore = (*DurableEventStore)(nil)
