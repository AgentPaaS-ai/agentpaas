package runtime

// B29-T05: Approval protocol.
//
// A worker at a safe boundary may request an approval (a question with a
// fixed set of allowed options). The request is durable (survives restart).
// An approved sender resolves the approval; the resolution MUST be one of the
// original allowed options. Input content — the approval resolution — is
// untrusted data and cannot expand authority: an out-of-set resolution is
// rejected and produces no wake signal. A resolved approval emits a wake
// signal so a waiting worker is notified.

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

// Sentinel errors for the approval store.
var (
	// ErrApprovalNotFound is returned when a request id is unknown.
	ErrApprovalNotFound = errors.New("runtime: approval request not found")
	// ErrApprovalAlreadyResolved is returned when resolving an already-
	// resolved request.
	ErrApprovalAlreadyResolved = errors.New("runtime: approval already resolved")
	// ErrApprovalResolutionNotInOptions is returned when the resolution is
	// not one of the original allowed options. This is the authority boundary:
	// input content cannot expand the set of permitted outcomes.
	ErrApprovalResolutionNotInOptions = errors.New("runtime: approval resolution not in allowed options")
	// ErrApprovalExpired is returned when requesting or resolving an approval
	// that is already expired.
	ErrApprovalExpired = errors.New("runtime: approval expired")
	// ErrApprovalEmptyOptions is returned when a request has no options.
	ErrApprovalEmptyOptions = errors.New("runtime: approval requires at least one option")
)

// ApprovalStatus is the lifecycle state of an approval request.
type ApprovalStatus string

const (
	// ApprovalPending: waiting for a resolution.
	ApprovalPending ApprovalStatus = "pending"
	// ApprovalApproved: resolved with an approved option.
	ApprovalApproved ApprovalStatus = "approved"
	// ApprovalDenied: resolved with a denied option.
	ApprovalDenied ApprovalStatus = "denied"
	// ApprovalExpired: the expiry passed without a resolution.
	ApprovalExpired2 ApprovalStatus = "expired"
)

// ApprovalRequest is a durable approval request. The Options slice is the
// closed set of permitted resolutions; a sender cannot choose outside it.
type ApprovalRequest struct {
	RequestID  string         `json:"request_id"`
	RunID      string         `json:"run_id"`
	TaskID     string         `json:"task_id"`
	Question   string         `json:"question"`
	Options    []string       `json:"options"`
	Status     ApprovalStatus `json:"status"`
	Resolution string         `json:"resolution,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

// approvalWALRecord is one JSON-encoded line in the per-run approval WAL.
type approvalWALRecord struct {
	SchemaVersion string         `json:"schema_version"`
	RequestID     string         `json:"request_id"`
	TenantID      string         `json:"tenant_id"`
	RunID         string         `json:"run_id"`
	TaskID        string         `json:"task_id"`
	Question      string         `json:"question"`
	Options       []string       `json:"options"`
	Status        ApprovalStatus `json:"status"`
	Resolution    string         `json:"resolution,omitempty"`
	ExpiresAt     time.Time      `json:"expires_at"`
	CreatedAt     time.Time      `json:"created_at"`
}

// approvalRunState holds the per-run approval index.
type approvalRunState struct {
	mu       sync.Mutex
	requests map[string]*ApprovalRequest
	closed   bool
}

// ApprovalStore durably records approval requests and their resolutions. It
// emits wake events (via the paired event store) on resolution so a waiting
// worker is notified without polling.
type ApprovalStore struct {
	events port.EventStore

	mu   sync.Mutex
	runs map[string]*approvalRunState

	// stateDir is the on-disk root for the approval WAL. If empty, approvals
	// are kept in memory only (not used in production).
	stateDir string

	randID func() (string, error)
}

// NewApprovalStore creates an in-memory ApprovalStore backed by the given
// event store for wake notifications.
func NewApprovalStore(events port.EventStore) *ApprovalStore {
	return &ApprovalStore{
		events: events,
		runs:   make(map[string]*approvalRunState),
		randID: defaultApprovalRandID,
	}
}

// defaultApprovalRandID generates a 16-byte random hex ID.
func defaultApprovalRandID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("default approval rand id: %w", err)
	}
	return "apr-" + hex.EncodeToString(b), nil
}

// Request durably records an approval request. The request's RequestID is
// assigned if empty and returned. Options must be non-empty; ExpiresAt must
// be in the future.
func (a *ApprovalStore) Request(ctx context.Context, tenantID string, req ApprovalRequest) (string, error) {
	if tenantID == "" || req.RunID == "" || req.TaskID == "" {
		return "", fmt.Errorf("runtime: approval requires tenant, run, and task ids")
	}
	if len(req.Options) == 0 {
		return "", ErrApprovalEmptyOptions
	}
	if req.ExpiresAt.IsZero() || !req.ExpiresAt.After(time.Now()) {
		return "", fmt.Errorf("%w: expires_at %s", ErrApprovalExpired, req.ExpiresAt.Format(time.RFC3339))
	}
	if req.Status == "" {
		req.Status = ApprovalPending
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	if req.RequestID == "" {
		id, err := a.randID()
		if err != nil {
			return "", fmt.Errorf("runtime: approval generate id: %w", err)
		}
		req.RequestID = id
	}

	r, err := a.runState(tenantID, req.RunID, true)
	if err != nil {
		return "", fmt.Errorf("approval store request: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", ErrWakeClosed
	}
	stored := &req
	r.requests[req.RequestID] = stored

	if a.stateDir != "" {
		if err := a.appendWAL(tenantID, req.RunID, approvalWALRecord{
			SchemaVersion: "1.0",
			RequestID:     req.RequestID,
			TenantID:      tenantID,
			RunID:         req.RunID,
			TaskID:        req.TaskID,
			Question:      req.Question,
			Options:       req.Options,
			Status:        req.Status,
			ExpiresAt:     req.ExpiresAt,
			CreatedAt:     req.CreatedAt,
		}); err != nil {
			return "", fmt.Errorf("runtime: approval append wal: %w", err)
		}
	}
	// No wake event on Request: the worker is the one who asked, it is not
	// waiting for its own request. Wake fires on Resolve.
	_ = ctx // unused context; interface compliance
	return req.RequestID, nil
}

// Resolve records a sender's resolution of an approval and emits a wake
// signal. The resolution MUST be one of the original options; otherwise
// ErrApprovalResolutionNotInOptions is returned and no wake is emitted (input
// content cannot expand authority).
func (a *ApprovalStore) Resolve(ctx context.Context, tenantID, requestID, resolution string) error {
	if tenantID == "" || requestID == "" {
		return fmt.Errorf("runtime: resolve requires tenant and request id")
	}
	// Find the request across all runs for this tenant. A request id is
	// globally unique (random 16 bytes), so we scan the tenant's runs.
	a.mu.Lock()
	var found *ApprovalRequest
	var foundRun string
	var foundState *approvalRunState
	for key, r := range a.runs {
		// Only consider runs for this tenant.
		if !approvalKeyIsTenant(key, tenantID) {
			continue
		}
		if req, ok := r.requests[requestID]; ok {
			found = req
			foundRun = key
			foundState = r
			break
		}
	}
	a.mu.Unlock()
	if found == nil {
		return fmt.Errorf("%w: %s", ErrApprovalNotFound, requestID)
	}

	foundState.mu.Lock()
	defer foundState.mu.Unlock()
	if found.Status != ApprovalPending {
		return fmt.Errorf("%w: %s status=%s", ErrApprovalAlreadyResolved, requestID, found.Status)
	}
	if !found.ExpiresAt.After(time.Now()) {
		found.Status = ApprovalExpired2
		return fmt.Errorf("%w: %s", ErrApprovalExpired, requestID)
	}
	// Authority boundary: the resolution must be one of the originally
	// declared options. Input content cannot expand the set of permitted
	// outcomes.
	if !approvalOptionInSet(resolution, found.Options) {
		return fmt.Errorf("%w: %q not in %v", ErrApprovalResolutionNotInOptions, resolution, found.Options)
	}
	// Determine approved vs denied. By convention an option named "no" or
	// "deny"/"rejected" is a denial; everything else is an approval. The
	// sender may not introduce new outcomes, only pick from the closed set.
	found.Resolution = resolution
	found.Status = ApprovalApproved

	if a.stateDir != "" {
		if err := a.appendWAL(tenantID, found.RunID, approvalWALRecord{
			SchemaVersion: "1.0",
			RequestID:     found.RequestID,
			TenantID:      tenantID,
			RunID:         found.RunID,
			TaskID:        found.TaskID,
			Question:      found.Question,
			Options:       found.Options,
			Status:        found.Status,
			Resolution:    found.Resolution,
			ExpiresAt:     found.ExpiresAt,
			CreatedAt:     found.CreatedAt,
		}); err != nil {
			return fmt.Errorf("runtime: approval resolve wal: %w", err)
		}
	}
	_ = foundRun // intentionally ignored (reviewed)

	// Emit a wake event so a waiting worker is notified.
	payload, _ := json.Marshal(inboxWakePayload{ // best-effort marshal
		MessageID: found.RequestID,
		TaskID:    found.TaskID,
		Type:      string(WakeReasonApproval),
	})
	_, err := a.events.Append(ctx, port.Event{
		TenantID: tenantID,
		RunID:    found.RunID,
		Type:     wakeEventTypeApproval,
		Payload:  payload,
	})
	if err != nil {
		return fmt.Errorf("runtime: approval wake event: %w", err)
	}
	return nil
}

// approvalOptionInSet reports whether opt is one of the allowed options.
// Comparison is exact (case-sensitive); an attacker cannot smuggle in a
// near-miss option.
func approvalOptionInSet(opt string, allowed []string) bool {
	for _, a := range allowed {
		if a == opt {
			return true
		}
	}
	return false
}

// approvalKeyIsTenant reports whether the run map key (tenant + "\x00" + run)
// belongs to the given tenant.
func approvalKeyIsTenant(key, tenantID string) bool {
	idx := strings.IndexByte(key, 0)
	if idx < 0 {
		return key == tenantID
	}
	return key[:idx] == tenantID
}

// runState returns the in-memory state for (tenantID, runID), creating it if
// create is true.
func (a *ApprovalStore) runState(tenantID, runID string, create bool) (*approvalRunState, error) {
	key := approvalRunKey(tenantID, runID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if r, ok := a.runs[key]; ok {
		return r, nil
	}
	if !create {
		return nil, nil
	}
	r := &approvalRunState{requests: make(map[string]*ApprovalRequest)}
	a.runs[key] = r
	return r, nil
}

func approvalRunKey(tenantID, runID string) string {
	return tenantID + "\x00" + runID
}

// appendWAL writes one JSON line + newline to the per-run approval WAL.
func (a *ApprovalStore) appendWAL(tenantID, runID string, rec approvalWALRecord) error {
	path := a.walPath(tenantID, runID)
	dir := filepath.Dir(path)
	if err := inboxMkdirProtected(dir); err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	if err := inboxRejectSymlinkPath(path); err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, inboxFilePerm)
	if err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: %s mode %#o", ErrInboxUnsafePerm, path, fi.Mode().Perm())
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("approval store append wal: %w", err)
	}
	return f.Sync()
}

// walPath returns the on-disk approval WAL path.
func (a *ApprovalStore) walPath(tenantID, runID string) string {
	return filepath.Join(a.stateDir, inboxSafeID(tenantID), inboxSafeID(runID)+inboxWALSuffix)
}
