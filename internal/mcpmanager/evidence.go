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
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Call status
// ---------------------------------------------------------------------------

// CallStatus categorizes the final state of an MCP call.
type CallStatus string

const (
	CallStatusSucceeded CallStatus = "SUCCEEDED"
	CallStatusFailed    CallStatus = "FAILED"
	CallStatusCancelled CallStatus = "CANCELLED"
	CallStatusUnknown   CallStatus = "UNKNOWN"
	CallStatusOverloaded CallStatus = "OVERLOADED"
	CallStatusTimeout   CallStatus = "TIMEOUT"
)

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
	WorkflowID  string `json:"workflow_id"`
	BindingID   string `json:"binding_id"`
	Tool        string `json:"tool"`

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
func NewCorrelationID() string {
	now := time.Now().UnixNano()
	h := sha256.Sum256([]byte(fmt.Sprintf("mcp-call-%d-%d", now, now)))
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
	mu       sync.RWMutex
	calls       map[string]MCPCallRecord           // keyed by correlationID
	lifecycle   map[string][]MCPServiceLifecycleEvent // keyed by workflowID/bindingID
	inFlight    map[string]bool                     // correlationIDs that are in-flight
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[record.CorrelationID] = record
	if record.Status == CallStatusUnknown {
		s.inFlight[record.CorrelationID] = true
	} else {
		delete(s.inFlight, record.CorrelationID)
	}
	return nil
}

func (s *InMemoryCallEvidenceStore) RecordLifecycleEvent(event MCPServiceLifecycleEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := event.WorkflowID + "/" + event.ServiceBindingID
	s.lifecycle[key] = append(s.lifecycle[key], event)
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
	key := workflowID + "/" + bindingID
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
				record.Status = CallStatusUnknown
				record.Reason = "daemon restart: call outcome unknown"
				record.FinishedAt = time.Now().UTC()
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
// InFlightCallTracker
// ---------------------------------------------------------------------------

// InFlightCallTracker tracks MCP calls that are in-flight so they can be
// marked as UNKNOWN/CANCELLED during restart or fence. Never marks them as
// SUCCEEDED.
type InFlightCallTracker struct {
	mu     sync.Mutex
	calls  map[string]inFlightCall // keyed by correlationID
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