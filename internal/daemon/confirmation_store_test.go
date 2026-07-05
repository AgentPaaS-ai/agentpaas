package daemon

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/operator"
)

func TestConfirmationStore_CreateAndGet(t *testing.T) {
	store := NewConfirmationStore()
	createdAt := time.Now().UTC().Truncate(time.Second)
	change := PendingConfirmation{
		CreatedAt:     createdAt,
		ExpiresAt:     createdAt.Add(time.Minute),
		ChangeType:    "policy_patch",
		RiskLevel:     "medium",
		Rationale:     "allow required API access",
		AffectedDests: []string{"api.example.com"},
		CredentialIDs: []string{"api-key"},
		EvidenceRefs:  []operator.EvidenceRef{{Type: "audit_seq", Ref: "42"}},
		ProposedPatch: "egress: []",
		Status:        "pending",
	}

	id, err := store.Create(change)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || got.ChangeType != change.ChangeType || got.RiskLevel != change.RiskLevel ||
		got.Rationale != change.Rationale || got.ProposedPatch != change.ProposedPatch ||
		got.Status != "pending" || !got.CreatedAt.Equal(createdAt) || !got.ExpiresAt.Equal(change.ExpiresAt) {
		t.Fatalf("Get() = %#v, want fields from %#v", got, change)
	}
	if len(got.AffectedDests) != 1 || got.AffectedDests[0] != "api.example.com" {
		t.Fatalf("AffectedDests = %#v", got.AffectedDests)
	}
	if len(got.CredentialIDs) != 1 || got.CredentialIDs[0] != "api-key" {
		t.Fatalf("CredentialIDs = %#v", got.CredentialIDs)
	}
	if len(got.EvidenceRefs) != 1 || got.EvidenceRefs[0].Ref != "42" {
		t.Fatalf("EvidenceRefs = %#v", got.EvidenceRefs)
	}
}

func TestConfirmationStore_Approve(t *testing.T) {
	store := NewConfirmationStore()
	id, err := store.Create(PendingConfirmation{ChangeType: "policy_patch"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Approve(id); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("Status = %q, want approved", got.Status)
	}
}

func TestConfirmationStore_Decline(t *testing.T) {
	store := NewConfirmationStore()
	id, err := store.Create(PendingConfirmation{ChangeType: "policy_patch"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Decline(id); err != nil {
		t.Fatalf("Decline: %v", err)
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "declined" {
		t.Fatalf("Status = %q, want declined", got.Status)
	}
}

func TestConfirmationStore_Expire(t *testing.T) {
	store := NewConfirmationStore()
	id, err := store.Create(PendingConfirmation{
		CreatedAt:  time.Now().Add(-time.Minute),
		ExpiresAt:  time.Now().Add(-time.Millisecond),
		ChangeType: "policy_patch",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if count := store.Expire(); count != 1 {
		t.Fatalf("Expire() = %d, want 1", count)
	}
	if _, err := store.Get(id); err == nil {
		t.Fatal("Get after Expire returned nil error")
	}
}

func TestConfirmationStore_GetNotFound(t *testing.T) {
	store := NewConfirmationStore()
	if _, err := store.Get("confirm_unknown"); err == nil {
		t.Fatal("Get returned nil error")
	}
}

func TestConfirmationStore_ConcurrentCreate(t *testing.T) {
	store := NewConfirmationStore()
	const creates = 100
	ids := make(chan string, creates)
	var wg sync.WaitGroup
	for range creates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := store.Create(PendingConfirmation{ChangeType: "policy_patch"})
			if err != nil {
				t.Errorf("Create: %v", err)
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, creates)
	for id := range ids {
		if !strings.HasPrefix(id, "confirm_") {
			t.Errorf("ID = %q, want confirm_ prefix", id)
		}
		if _, exists := seen[id]; exists {
			t.Errorf("duplicate ID %q", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != creates {
		t.Fatalf("created %d unique IDs, want %d", len(seen), creates)
	}
}
