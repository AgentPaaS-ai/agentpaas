package routedrun

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxControlJournalEventBytes is the per-event payload size cap (spec line
// 284: "reject writes > 64KB per event"). The cap applies to the encoded
// event payload, not the envelope.
const maxControlJournalEventBytes = 64 * 1024

// controlDirName is the subdirectory under <stateRoot>/runs/<runID>/ that
// holds per-attempt control journals. Each attempt gets its own subdir.
const controlDirName = "control"

// controlKeyFileName is the per-attempt HMAC key file, stored OUTSIDE the
// per-attempt control directory at <stateRoot>/runs/<runID>/control-key.
// Mode 0600, owned by the daemon user. The non-root Python worker must not
// be able to read it (spec line 118-119). Full container isolation (worker
// UID != daemon UID) is a T04 concern, but the file mode is enforced here.
const controlKeyFileName = "control-key"

// ControlJournal is the per-attempt append-only control journal for a durable
// invoke job. It records InvokeJobEvent records (accepted, started,
// progress_ref, succeeded, failed, cancelled) with monotonic sequence
// numbers and per-event HMAC authentication.
//
// SECURITY MODEL:
//   - The journal directory is 0700, created under the run state dir.
//   - Every event file is 0600, written atomically (temp + fsync + rename).
//   - Symlink traversal is rejected at write AND read time.
//   - The per-attempt HMAC key is 32 random bytes from crypto/rand, stored
//     OUTSIDE the control directory at <stateRoot>/runs/<runID>/control-key
//     with mode 0600. Python (the non-root worker) must not read it; full
//     UID isolation is T04, but the file mode is enforced here and tested.
//   - Event payloads are bounded to 64KB.
//   - Sequence numbers are monotonic with no gaps.
//
// The journal is safe to read after a daemon restart for reconciliation.
type ControlJournal struct {
	mu sync.Mutex

	stateRoot string
	runID     string
	attemptID string

	dir     string // <stateRoot>/runs/<runID>/control/<attemptID>
	keyPath string // <stateRoot>/runs/<runID>/control-key

	key       []byte
	lastSeq   int64
	createdAt time.Time
	closed    bool
}

// NewControlJournal opens (or creates) a per-attempt control journal rooted
// at <stateRoot>/runs/<runID>/control/<attemptID>. The HMAC key is loaded
// from (or generated into) <stateRoot>/runs/<runID>/control-key (0600).
//
// stateRoot is the daemon state root (e.g. ~/.agentpaas/state). runID and
// attemptID are the per-run / per-attempt identifiers. Both are sanitised
// to single path components via safeID to prevent path traversal.
func NewControlJournal(stateRoot, runID, attemptID string) (*ControlJournal, error) {
	if stateRoot == "" {
		return nil, fmt.Errorf("%w: empty state root", ErrInvalidArgument)
	}
	if runID == "" || attemptID == "" {
		return nil, fmt.Errorf("%w: empty run or attempt id", ErrInvalidArgument)
	}
	runDir := filepath.Join(stateRoot, "runs", safeID(runID))
	controlRoot := filepath.Join(runDir, controlDirName)
	attemptDir := filepath.Join(controlRoot, safeID(attemptID))
	keyPath := filepath.Join(runDir, controlKeyFileName)

	if err := mkdirProtected(attemptDir); err != nil {
		return nil, err
	}
	if err := rejectSymlinkInRoot(runDir, attemptDir); err != nil {
		return nil, err
	}
	if err := rejectSymlinkInRoot(runDir, keyPath); err != nil {
		return nil, err
	}

	key, err := loadOrCreateControlKey(keyPath)
	if err != nil {
		return nil, err
	}

	cj := &ControlJournal{
		stateRoot: stateRoot,
		runID:     runID,
		attemptID: attemptID,
		dir:       attemptDir,
		keyPath:   keyPath,
		key:       key,
		createdAt: time.Now().UTC(),
	}
	if err := cj.loadLastSeq(); err != nil {
		return nil, fmt.Errorf("routedrun: control journal init: %w", err)
	}
	return cj, nil
}

// loadOrCreateControlKey loads the per-attempt HMAC key from keyPath, or
// generates a fresh 32-byte key and persists it atomically at 0600. Returns
// the key.
func loadOrCreateControlKey(keyPath string) ([]byte, error) {
	if err := mkdirProtected(filepath.Dir(keyPath)); err != nil {
		return nil, err
	}
	// If the key already exists, read it strictly (symlink + perm checks).
	if data, err := readFileStrict(keyPath, 64); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("%w: control-key length %d want 32", ErrInvalidArgument, len(data))
		}
		return data, nil
	} else if !errors.Is(err, ErrNotFound) && !os.IsNotExist(err) {
		return nil, err
	}
	// Generate a fresh key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("routedrun: generate control key: %w", err)
	}
	if err := atomicWriteFile(keyPath, key, filePerm); err != nil {
		return nil, err
	}
	return key, nil
}

// loadLastSeq scans existing event files to recover the highest sequence
// number so that append ordering is enforced across daemon restarts.
func (cj *ControlJournal) loadLastSeq() error {
	names, err := listJSONNames(cj.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var maxSeq int64
	for _, name := range names {
		seq, ok := parseEventFileName(name)
		if !ok {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	cj.lastSeq = maxSeq
	return nil
}

// eventFileName returns the file name for a sequence number, zero-padded to
// 10 digits for stable lexical ordering.
func eventFileName(seq int64) string {
	return fmt.Sprintf("event-%010d.json", seq)
}

// parseEventFileName extracts the sequence number from an event file name.
// Accepts the canonical zero-padded 10-digit form only.
func parseEventFileName(name string) (int64, bool) {
	const prefix = "event-"
	const suffix = ".json"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return 0, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if len(body) != 10 {
		return 0, false
	}
	seq, err := strconv.ParseInt(body, 10, 64)
	if err != nil || seq < 1 {
		return 0, false
	}
	// Canonical form check: zero-padded 10 digits.
	if body != fmt.Sprintf("%010d", seq) {
		return 0, false
	}
	return seq, true
}

// Append writes a single event atomically to the journal. The event's
// sequence must be exactly lastSeq+1 (no gaps, no duplicates). The HMAC is
// recomputed and stored alongside the event; on read-back the HMAC is
// verified. Oversized payloads (>64KB) are rejected.
func (cj *ControlJournal) Append(event InvokeJobEvent) error {
	cj.mu.Lock()
	defer cj.mu.Unlock()
	if cj.closed {
		return fmt.Errorf("%w: control journal closed", ErrInvalidArgument)
	}
	if event.SchemaVersion == "" {
		event.SchemaVersion = invokeJobSchemaVersionV1
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	// Bounded payload.
	if len(event.Payload) > maxControlJournalEventBytes {
		return fmt.Errorf("%w: event payload %d bytes > %d", ErrSizeCapExceeded, len(event.Payload), maxControlJournalEventBytes)
	}
	wantSeq := cj.lastSeq + 1
	if event.Sequence == 0 {
		event.Sequence = wantSeq
	}
	if event.Sequence != wantSeq {
		return fmt.Errorf("%w: sequence %d want %d (monotonic, no gaps)", ErrInvalidArgument, event.Sequence, wantSeq)
	}
	// Compute HMAC over (sequence || timestamp || event_kind || payload).
	event.HMAC = cj.computeHMAC(event)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("routedrun: marshal event: %w", err)
	}
	// Envelope-level bound check (payload is the dominant term).
	if len(data) > maxControlJournalEventBytes+4096 {
		return fmt.Errorf("%w: event envelope %d bytes", ErrSizeCapExceeded, len(data))
	}
	path := filepath.Join(cj.dir, eventFileName(event.Sequence))
	// Symlink-safe write: atomicWriteFile refuses symlink paths and checks
	// the parent dir; additionally verify the target is under the run dir.
	if err := rejectSymlinkInRoot(cj.runDir(), path); err != nil {
		return err
	}
	if err := atomicWriteFile(path, data, filePerm); err != nil {
		// atomicWriteFile returns ErrSymlinkRejected for symlink targets.
		return err
	}
	cj.lastSeq = event.Sequence
	return nil
}

// Read returns all events with sequence >= fromSeq, in ascending sequence
// order. Every event's HMAC is verified on read-back; a tampered event
// causes an error and no events are returned.
func (cj *ControlJournal) Read(fromSeq int64) ([]InvokeJobEvent, error) {
	cj.mu.Lock()
	defer cj.mu.Unlock()
	if cj.closed {
		return nil, fmt.Errorf("%w: control journal closed", ErrInvalidArgument)
	}
	if fromSeq < 1 {
		fromSeq = 1
	}
	names, err := listJSONNames(cj.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	type seqName struct {
		seq  int64
		name string
	}
	var entries []seqName
	for _, name := range names {
		seq, ok := parseEventFileName(name)
		if !ok {
			continue
		}
		if seq < fromSeq {
			continue
		}
		entries = append(entries, seqName{seq: seq, name: name})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].seq < entries[j].seq })
	out := make([]InvokeJobEvent, 0, len(entries))
	for _, e := range entries {
		path := filepath.Join(cj.dir, e.name)
		if err := rejectSymlinkInRoot(cj.runDir(), path); err != nil {
			return nil, err
		}
		data, err := readFileStrict(path, maxControlJournalEventBytes+4096)
		if err != nil {
			return nil, err
		}
		var ev InvokeJobEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, fmt.Errorf("routedrun: unmarshal event %s: %w", e.name, err)
		}
		if ev.Sequence != e.seq {
			return nil, fmt.Errorf("%w: event %s sequence mismatch file=%d record=%d", ErrInvalidArgument, e.name, e.seq, ev.Sequence)
		}
		// HMAC verification: recompute and compare.
		want := cj.computeHMAC(ev)
		if !hmac.Equal([]byte(ev.HMAC), []byte(want)) {
			return nil, fmt.Errorf("%w: event %s hmac verification failed (tamper detected)", ErrSymlinkRejected, e.name)
		}
		out = append(out, ev)
	}
	return out, nil
}

// Close releases any held resources. Safe to call multiple times.
func (cj *ControlJournal) Close() error {
	cj.mu.Lock()
	defer cj.mu.Unlock()
	cj.closed = true
	return nil
}

// runDir returns the run directory (parent of the control dir and key).
func (cj *ControlJournal) runDir() string {
	return filepath.Join(cj.stateRoot, "runs", safeID(cj.runID))
}

// computeHMAC returns the hex-encoded HMAC-SHA256 over the canonical event
// content (sequence, timestamp, event kind, payload).
func (cj *ControlJournal) computeHMAC(ev InvokeJobEvent) string {
	mac := hmac.New(sha256.New, cj.key)
	fmt.Fprintf(mac, "%d|%s|%d|", ev.Sequence, ev.Timestamp.UTC().Format(time.RFC3339Nano), int(ev.EventKind))
	mac.Write([]byte(ev.Payload))
	return hex.EncodeToString(mac.Sum(nil))
}
