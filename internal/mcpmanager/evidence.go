// Package mcpmanager — evidence records for MCP calls and service lifecycle.
//
// B33-T07: Persist sanitized call and lifecycle evidence so that after any fault,
// AgentPaaS can state whether an MCP call committed, failed, or is unknown —
// without replaying it or leaking bodies/secrets/capabilities.

package mcpmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// Call status
// ---------------------------------------------------------------------------

// CallStatus categorizes the final state of an MCP call.
type CallStatus string

const (
	CallStatusSucceeded  CallStatus = "SUCCEEDED"
	CallStatusFailed     CallStatus = "FAILED"
	CallStatusCancelled  CallStatus = "CANCELLED"
	CallStatusUnknown    CallStatus = "UNKNOWN"
	CallStatusOverloaded CallStatus = "OVERLOADED"
	CallStatusTimeout    CallStatus = "TIMEOUT"
)

// isTerminalCallStatus returns true if the status is considered terminal
// (the call will never transition to SUCCEEDED). Terminal includes:
// SUCCEEDED, FAILED, CANCELLED, TIMEOUT, OVERLOADED, and restart UNKNOWN
// (where FinishedAt is set by MarkInFlightUnknown).
func isTerminalCallStatus(status CallStatus, finishedAt time.Time) bool {
	switch status {
	case CallStatusSucceeded, CallStatusFailed, CallStatusCancelled,
		CallStatusTimeout, CallStatusOverloaded:
		return true
	case CallStatusUnknown:
		return !finishedAt.IsZero()
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Bounded storage constants
// ---------------------------------------------------------------------------

const (
	// MaxCallRecordsPerWorkflow caps call records per workflow to prevent
	// unbounded memory growth.
	MaxCallRecordsPerWorkflow = 1024
	// MaxLifecycleEventsPerKey caps lifecycle events per (workflow, binding)
	// to prevent unbounded growth.
	MaxLifecycleEventsPerKey = 256
)

// ---------------------------------------------------------------------------
// Key encoding
// ---------------------------------------------------------------------------

// makeCompositeKey builds a collision-safe composite key from two strings
// using a NUL separator. Unlike naive slash joining, this prevents injection
// attacks where a binding ID containing "/" could alias another key.
func makeCompositeKey(a, b string) string {
	// Length-prefix encoding: "<lenA>:<a>\x00<lenB>:<b>"
	return fmt.Sprintf("%d:%s\x00%d:%s", len(a), a, len(b), b)
}

// normalizeIdentityField strips control characters (CR, LF, NUL, and other
// non-printable chars) from identity fields to prevent log injection.
func normalizeIdentityField(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

// validateCorrelationID checks that a correlation ID is non-empty and
// does not contain NUL bytes. Returns an error if invalid.
func validateCorrelationID(id string) error {
	if id == "" {
		return fmt.Errorf("correlation ID must not be empty")
	}
	if strings.Contains(id, "\x00") {
		return fmt.Errorf("correlation ID must not contain NUL bytes")
	}
	return nil
}

// ---------------------------------------------------------------------------
// MCPCallRecord
// ---------------------------------------------------------------------------

// MCPCallRecord is a sanitized, immutable record of a single MCP tool call.
// It NEVER contains raw arguments, raw results, capability tokens, network
// aliases, credentials, or private keys.
type MCPCallRecord struct {
	// CorrelationID uniquely identifies this call across all evidence sources.
	// A single correlation ID is shared between the Router audit and harness
	// audit, mitigating T05 R2 double-audit.
	CorrelationID string `json:"correlation_id"`

	// Caller identity.
	CallerRunID     string `json:"caller_run_id"`
	CallerAttemptID string `json:"caller_attempt_id"`
	CallerAgentID   string `json:"caller_agent_id"`

	// Service identity.
	ServiceRunID     string `json:"service_run_id"`
	ServiceAttemptID string `json:"service_attempt_id"`

	// Lease identity.
	CallerLeaseID  string `json:"caller_lease_id,omitempty"`
	ServiceLeaseID string `json:"service_lease_id,omitempty"`

	// Binding identity.
	WorkflowID string `json:"workflow_id"`
	BindingID  string `json:"binding_id"`
	Tool       string `json:"tool"`

	// Digests only — never raw args/results.
	InputDigest  string `json:"input_digest"`
	OutputDigest string `json:"output_digest,omitempty"`

	// Protocol version (optional, reported by the service).
	ProtocolVersion string `json:"protocol_version,omitempty"`

	// Status and reason.
	Status CallStatus `json:"status"`
	// Reason is a redacted error or status message. Never contains secrets.
	Reason string `json:"reason,omitempty"`

	// Timing.
	TimingMS  int64     `json:"timing_ms"`
	StartedAt time.Time `json:"started_at"`
	// FinishedAt is zero when the call is in-flight (UNKNOWN).
	FinishedAt time.Time `json:"finished_at,omitempty"`

	// EvidenceRefs links to audit sequence numbers or external correlation IDs.
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

// MCPCallRecordJSON returns the JSON representation of the record.
// Used by tests to verify no secret leakage.
func (r *MCPCallRecord) JSON() ([]byte, error) {
	return json.Marshal(r)
}

// ComputeInputDigest returns a SHA-256 hex digest of the input.
func ComputeInputDigest(input any) string {
	return hashRouterJSON(input)
}

// ComputeOutputDigest returns a SHA-256 hex digest of the output.
func ComputeOutputDigest(output any) string {
	return RedactToolOutputHash(output)
}

// NewCorrelationID generates a unique correlation ID for an MCP call.
// Uses a monotonic counter with nanosecond timestamp to avoid collisions
// in tight loops.
var correlationIDCounter int64

func NewCorrelationID() string {
	now := time.Now().UnixNano()
	next := atomic.AddInt64(&correlationIDCounter, 1)
	h := sha256.Sum256([]byte(fmt.Sprintf("mcp-call-%d-%d", now, next)))
	return hex.EncodeToString(h[:])[:16]
}

// ---------------------------------------------------------------------------
// MCPServiceLifecycleEvent
// ---------------------------------------------------------------------------

// MCPServiceLifecycleEvent records a service state transition.
type MCPServiceLifecycleEvent struct {
	// CorrelationID links related events (e.g. Declare→Start→Ready).
	CorrelationID string `json:"correlation_id"`

	WorkflowID       string       `json:"workflow_id"`
	ServiceBindingID string       `json:"service_binding_id"`
	RunID            string       `json:"run_id"`
	AttemptID        string       `json:"attempt_id"`
	LeaseID          string       `json:"lease_id"`
	Generation       int64        `json:"generation"`
	FromState        ServiceState `json:"from_state"`
	ToState          ServiceState `json:"to_state"`
	Reason           string       `json:"reason,omitempty"`
	Timestamp        time.Time    `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// CallEvidenceStore
// ---------------------------------------------------------------------------

// CallEvidenceStore persists MCP call and lifecycle evidence records.
// The interface is designed for:
//   - In-memory impl for tests
//   - Optional file-backed JSONL under run dir for production
type CallEvidenceStore interface {
	// RecordCall persists a sanitized MCP call record.
	RecordCall(record MCPCallRecord) error

	// RecordLifecycleEvent persists a service lifecycle event.
	RecordLifecycleEvent(event MCPServiceLifecycleEvent) error

	// GetCall retrieves a call record by correlation ID.
	GetCall(correlationID string) (MCPCallRecord, bool)

	// GetCallsByWorkflow returns all call records for a workflow.
	GetCallsByWorkflow(workflowID string) []MCPCallRecord

	// GetCallsByBinding returns all call records for a binding.
	GetCallsByBinding(workflowID, bindingID string) []MCPCallRecord

	// GetLifecycleEvents returns lifecycle events for a service binding.
	GetLifecycleEvents(workflowID, bindingID string) []MCPServiceLifecycleEvent

	// MarkInFlightUnknown marks all calls currently in-flight for a workflow
	// as UNKNOWN (used during restart reconciliation).
	MarkInFlightUnknown(workflowID string) int

	// Close releases any resources held by the store.
	Close() error
}

// ---------------------------------------------------------------------------
// InMemoryCallEvidenceStore
// ---------------------------------------------------------------------------

// InMemoryCallEvidenceStore is a thread-safe in-memory implementation of
// CallEvidenceStore for tests.
type InMemoryCallEvidenceStore struct {
	mu        sync.RWMutex
	calls     map[string]MCPCallRecord              // keyed by correlationID
	lifecycle map[string][]MCPServiceLifecycleEvent  // keyed by composite key
	inFlight  map[string]bool                        // correlationIDs that are in-flight
}

// NewInMemoryCallEvidenceStore creates a new in-memory evidence store.
func NewInMemoryCallEvidenceStore() *InMemoryCallEvidenceStore {
	return &InMemoryCallEvidenceStore{
		calls:     make(map[string]MCPCallRecord),
		lifecycle: make(map[string][]MCPServiceLifecycleEvent),
		inFlight:  make(map[string]bool),
	}
}

func (s *InMemoryCallEvidenceStore) RecordCall(record MCPCallRecord) error {
	// Validate correlation ID: reject empty and NUL-containing IDs.
	if err := validateCorrelationID(record.CorrelationID); err != nil {
		return err
	}

	// Normalize identity fields to strip CR/LF/NUL injection.
	record.WorkflowID = normalizeIdentityField(record.WorkflowID)
	record.BindingID = normalizeIdentityField(record.BindingID)
	record.Tool = normalizeIdentityField(record.Tool)
	record.Reason = sanitizeEvidenceReason(record.Reason)
	record.CorrelationID = normalizeIdentityField(record.CorrelationID)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Terminal state machine: prevent fabricated commits and demotion.
	existing, hasExisting := s.calls[record.CorrelationID]
	if hasExisting {
		existingTerminal := isTerminalCallStatus(existing.Status, existing.FinishedAt)

		// Rule 1: Once terminal, never allow SUCCEEDED overwrite
		// (prevents CANCELLED/FAILED/TIMEOUT → SUCCEEDED fabrication).
		if existingTerminal && record.Status == CallStatusSucceeded {
			return nil // silently reject — preserve existing terminal record
		}

		// Rule 2: SUCCEEDED cannot be demoted to UNKNOWN
		// (prevents erasing committed success evidence).
		if existing.Status == CallStatusSucceeded && record.Status == CallStatusUnknown {
			return nil // silently reject demotion
		}

		// Rule 3: Terminal statuses cannot be downgraded to in-flight UNKNOWN.
		if existingTerminal && record.Status == CallStatusUnknown && record.FinishedAt.IsZero() {
			return nil // silently reject
		}
	}

	// Bounded storage: cap per-workflow call records.
	workflowCount := 0
	for _, c := range s.calls {
		if c.WorkflowID == record.WorkflowID && c.CorrelationID != record.CorrelationID {
			workflowCount++
		}
	}
	if workflowCount >= MaxCallRecordsPerWorkflow && !hasExisting {
		return nil // drop oldest would require ordering; reject excess for now
	}

	s.calls[record.CorrelationID] = record

	// Update inFlight map based on terminal status.
	if isTerminalCallStatus(record.Status, record.FinishedAt) {
		delete(s.inFlight, record.CorrelationID)
	} else if record.Status == CallStatusUnknown && record.FinishedAt.IsZero() {
		s.inFlight[record.CorrelationID] = true
	}

	return nil
}

func (s *InMemoryCallEvidenceStore) RecordLifecycleEvent(event MCPServiceLifecycleEvent) error {
	// Normalize identity fields.
	event.WorkflowID = normalizeIdentityField(event.WorkflowID)
	event.ServiceBindingID = normalizeIdentityField(event.ServiceBindingID)
	event.Reason = sanitizeEvidenceReason(event.Reason)
	event.CorrelationID = normalizeIdentityField(event.CorrelationID)
	event.RunID = normalizeIdentityField(event.RunID)
	event.AttemptID = normalizeIdentityField(event.AttemptID)
	event.LeaseID = normalizeIdentityField(event.LeaseID)

	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeCompositeKey(event.WorkflowID, event.ServiceBindingID)
	events := s.lifecycle[key]

	// Bounded storage: cap per-key lifecycle events.
	if len(events) >= MaxLifecycleEventsPerKey {
		// Drop oldest, keep most recent.
		events = events[1:]
	}
	s.lifecycle[key] = append(events, event)
	return nil
}

func (s *InMemoryCallEvidenceStore) GetCall(correlationID string) (MCPCallRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.calls[correlationID]
	return record, ok
}

func (s *InMemoryCallEvidenceStore) GetCallsByWorkflow(workflowID string) []MCPCallRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []MCPCallRecord
	for _, record := range s.calls {
		if record.WorkflowID == workflowID {
			result = append(result, record)
		}
	}
	return result
}

func (s *InMemoryCallEvidenceStore) GetCallsByBinding(workflowID, bindingID string) []MCPCallRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []MCPCallRecord
	for _, record := range s.calls {
		if record.WorkflowID == workflowID && record.BindingID == bindingID {
			result = append(result, record)
		}
	}
	return result
}

func (s *InMemoryCallEvidenceStore) GetLifecycleEvents(workflowID, bindingID string) []MCPServiceLifecycleEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := makeCompositeKey(workflowID, bindingID)
	result := make([]MCPServiceLifecycleEvent, len(s.lifecycle[key]))
	copy(result, s.lifecycle[key])
	return result
}

func (s *InMemoryCallEvidenceStore) MarkInFlightUnknown(workflowID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for correlationID := range s.inFlight {
		if record, ok := s.calls[correlationID]; ok {
			if record.WorkflowID == workflowID {
				// Only mark in-flight calls — never touch already-terminal ones.
				if isTerminalCallStatus(record.Status, record.FinishedAt) {
					continue
				}
				record.Status = CallStatusUnknown
				record.Reason = "daemon restart: call outcome unknown"
				record.FinishedAt = time.Now().UTC()
				record.OutputDigest = "" // clear any forged output digest
				s.calls[correlationID] = record
				delete(s.inFlight, correlationID)
				count++
			}
		}
	}
	return count
}

func (s *InMemoryCallEvidenceStore) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// sanitizeEvidenceReason — central sanitization for evidence reason fields
// ---------------------------------------------------------------------------

// sanitizeEvidenceReason applies the full redaction pipeline to fields stored
// in evidence records (call reason, lifecycle reason, etc.). This ensures
// no secret leakage through any evidence path.
func sanitizeEvidenceReason(s string) string {
	if s == "" {
		return s
	}
	return sanitizeLastError(s)
}

// ---------------------------------------------------------------------------
// InFlightCallTracker
// ---------------------------------------------------------------------------

// InFlightCallTracker tracks MCP calls that are in-flight so they can be
// marked as UNKNOWN/CANCELLED during restart or fence. Never marks them as
// SUCCEEDED.
type InFlightCallTracker struct {
	mu    sync.Mutex
	calls map[string]inFlightCall // keyed by correlationID
}

type inFlightCall struct {
	CorrelationID string
	WorkflowID    string
	BindingID     string
	Tool          string
	StartedAt     time.Time
}

// NewInFlightCallTracker creates a new tracker.
func NewInFlightCallTracker() *InFlightCallTracker {
	return &InFlightCallTracker{
		calls: make(map[string]inFlightCall),
	}
}

// Register records a call as in-flight.
func (t *InFlightCallTracker) Register(correlationID, workflowID, bindingID, tool string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls[correlationID] = inFlightCall{
		CorrelationID: correlationID,
		WorkflowID:    workflowID,
		BindingID:     bindingID,
		Tool:          tool,
		StartedAt:     time.Now().UTC(),
	}
}

// Complete removes a call from in-flight tracking (call succeeded or failed).
func (t *InFlightCallTracker) Complete(correlationID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.calls, correlationID)
}

// Snapshot returns a copy of all in-flight calls so the caller can mark them
// as UNKNOWN/CANCELLED during restart or fence.
func (t *InFlightCallTracker) Snapshot() []inFlightCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]inFlightCall, 0, len(t.calls))
	for _, c := range t.calls {
		result = append(result, c)
	}
	return result
}

// Active returns the count of in-flight calls.
func (t *InFlightCallTracker) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

// Clear removes all tracked calls.
func (t *InFlightCallTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = make(map[string]inFlightCall)
}
