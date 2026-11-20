package daemon

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/operator"
)

const confirmationTTL = 5 * time.Minute

var errConfirmationNotFound = errors.New("confirmation not found")

// ConfirmationStore tracks trust-boundary changes awaiting a human decision.
type ConfirmationStore struct {
	mu      sync.Mutex
	pending map[string]*PendingConfirmation
	nextID  int64
}

// PendingConfirmation describes one proposed trust-boundary change.
type PendingConfirmation struct {
	ID            string                 `json:"id"`
	CreatedAt     time.Time              `json:"created_at"`
	ExpiresAt     time.Time              `json:"expires_at"`
	ChangeType    string                 `json:"change_type"`
	RiskLevel     string                 `json:"risk_level"`
	Rationale     string                 `json:"rationale"`
	AffectedDests []string               `json:"affected_destinations,omitempty"`
	CredentialIDs []string               `json:"credential_ids,omitempty"`
	EvidenceRefs  []operator.EvidenceRef `json:"evidence_refs,omitempty"`
	ProposedPatch string                 `json:"proposed_patch,omitempty"`
	Status        string                 `json:"status"`
}

// NewConfirmationStore creates an empty confirmation store.
func NewConfirmationStore() *ConfirmationStore {
	return &ConfirmationStore{pending: make(map[string]*PendingConfirmation)}
}

// Create stores a proposed change in the pending state.
func (s *ConfirmationStore) Create(change PendingConfirmation) (string, error) {
	if s == nil {
		return "", errors.New("confirmation store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if change.CreatedAt.IsZero() {
		change.CreatedAt = now
	}
	if change.ExpiresAt.IsZero() {
		change.ExpiresAt = change.CreatedAt.Add(confirmationTTL)
	}
	s.nextID++
	change.ID = fmt.Sprintf("confirm_%d_%d", change.CreatedAt.Unix(), s.nextID)
	change.Status = "pending"
	s.pending[change.ID] = clonePendingConfirmation(&change)
	return change.ID, nil
}

// Get retrieves a confirmation by id.
func (s *ConfirmationStore) Get(id string) (*PendingConfirmation, error) {
	if s == nil {
		return nil, errConfirmationNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errConfirmationNotFound, id)
	}
	return clonePendingConfirmation(change), nil
}

// Approve records explicit human approval without applying the change.
func (s *ConfirmationStore) Approve(id string) error {
	return s.decide(id, "approved")
}

// Decline records explicit human rejection of the change.
func (s *ConfirmationStore) Decline(id string) error {
	return s.decide(id, "declined")
}

func (s *ConfirmationStore) decide(id, decision string) error {
	if s == nil {
		return errConfirmationNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	change, ok := s.pending[id]
	if !ok {
		return fmt.Errorf("%w: %s", errConfirmationNotFound, id)
	}
	if change.Status != "pending" {
		return fmt.Errorf("confirmation %s already %s", id, change.Status)
	}
	if !change.ExpiresAt.After(time.Now()) {
		change.Status = "expired"
		return fmt.Errorf("confirmation %s expired", id)
	}
	change.Status = decision
	return nil
}

// Expire removes confirmations whose expiry time has passed.
func (s *ConfirmationStore) Expire() int {
	if s == nil {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	count := 0
	for id, change := range s.pending {
		if !change.ExpiresAt.After(now) {
			delete(s.pending, id)
			count++
		}
	}
	return count
}

// ListPending returns snapshots of all confirmations still awaiting a decision.
func (s *ConfirmationStore) ListPending() []PendingConfirmation {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changes := make([]PendingConfirmation, 0, len(s.pending))
	for _, change := range s.pending {
		if change.Status == "pending" && change.ExpiresAt.After(time.Now()) {
			changes = append(changes, *clonePendingConfirmation(change))
		}
	}
	return changes
}

func clonePendingConfirmation(change *PendingConfirmation) *PendingConfirmation {
	clone := *change
	clone.AffectedDests = append([]string(nil), change.AffectedDests...)
	clone.CredentialIDs = append([]string(nil), change.CredentialIDs...)
	clone.EvidenceRefs = append([]operator.EvidenceRef(nil), change.EvidenceRefs...)
	return &clone
}
