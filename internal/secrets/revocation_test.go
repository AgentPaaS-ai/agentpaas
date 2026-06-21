package secrets

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

type countingStore struct {
	SecretStore

	mu       sync.Mutex
	getCalls int
}

func (s *countingStore) Get(ctx context.Context, name string) ([]byte, error) {
	s.mu.Lock()
	s.getCalls++
	s.mu.Unlock()

	return s.SecretStore.Get(ctx, name)
}

func (s *countingStore) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCalls
}

func TestBrokerRequestCredentialDeniedWhenRevoked(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("RequestCredential error = %v, want revoked denial", err)
	}

	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", "api.example.com:443", http.MethodGet)
}

func TestBrokerIsRevokedReflectsRevoke(t *testing.T) {
	ctx := context.Background()
	broker := newTestBroker(t, newSecretStore(t), &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	if broker.IsRevoked("api-token") {
		t.Fatal("IsRevoked before Revoke = true, want false")
	}
	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !broker.IsRevoked("api-token") {
		t.Fatal("IsRevoked after Revoke = false, want true")
	}
}

func TestBrokerRevokedCredentialDoesNotReadStore(t *testing.T) {
	ctx := context.Background()
	store := &countingStore{SecretStore: newSecretStore(t)}
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, store, auditSink, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("RequestCredential error = %v, want revoked denial", err)
	}
	if got := store.calls(); got != 0 {
		t.Fatalf("store.Get calls = %d, want 0", got)
	}
}

func TestBrokerRestartAffectedAgentsReturnsActiveDirectLeaseRuns(t *testing.T) {
	ctx := context.Background()
	broker := newTestBroker(t, newSecretStore(t), &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	broker.mu.Lock()
	broker.activeRuns["run-other"] = struct{}{}
	broker.activeDirectLeases["api-token"] = map[string]struct{}{
		"run-active": {},
		"run-other":  {},
		"run-stale":  {},
	}
	broker.activeDirectLeases["other-token"] = map[string]struct{}{
		"run-active": {},
	}
	broker.mu.Unlock()

	got, err := broker.RestartAffectedAgents(ctx, "api-token")
	if err != nil {
		t.Fatalf("RestartAffectedAgents: %v", err)
	}
	sort.Strings(got)
	want := []string{"run-active", "run-other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RestartAffectedAgents = %#v, want %#v", got, want)
	}
}

func TestBrokerRevokeNonExistentCredentialIsNoop(t *testing.T) {
	ctx := context.Background()
	broker := newTestBroker(t, newSecretStore(t), &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "does-not-exist"); err != nil {
		t.Fatalf("Revoke non-existent credential: %v", err)
	}
	if !broker.IsRevoked("does-not-exist") {
		t.Fatal("IsRevoked for non-existent revoked credential = false, want true")
	}
}

func TestBrokerConcurrentRevokeAndRequestCredentialNoRace(t *testing.T) {
	ctx := context.Background()
	broker := newTestBroker(t, newSecretStore(t), &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := broker.Revoke(ctx, "api-token"); err != nil {
				t.Errorf("Revoke: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
			if err != nil && !strings.Contains(err.Error(), "revoked") && !errors.Is(err, ErrSecretNotFound) {
				t.Errorf("RequestCredential error = %v, want nil or revoked denial", err)
			}
		}()
	}
	wg.Wait()
}

func TestBrokerRevocationAuditIncludesReason(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("RequestCredential error = %v, want revoked denial", err)
	}

	rec := auditSink.last(t)
	if rec.Payload["reason"] != "revoked" {
		t.Fatalf("Payload[reason] = %#v, want revoked; payload=%#v", rec.Payload["reason"], rec.Payload)
	}
}
