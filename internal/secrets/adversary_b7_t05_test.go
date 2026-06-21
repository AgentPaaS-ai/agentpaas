package secrets

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// ADVERSARY TEST FILE - permanent regression/break tests for B7-T05 revocation

func TestAdversary_B7T05_RevocationDenialAuditMissingAgentID(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte("secret")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	broker := newTestBroker(t, store, auditSink, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked denial, got %v", err)
	}

	rec := auditSink.last(t)
	// Check for agent_id or agent in payload - required per enterprise doc and attack surface
	if _, ok := rec.Payload["agent_id"]; ok {
		t.Log("agent_id present")
	} else if _, ok := rec.Payload["agent"]; ok {
		t.Log("agent present")
	} else {
		// ADVERSARY BREAK: revocation denial audit payload missing agent ID (policy has Agent.Name but not emitted in auditSecret)
		t.Fatalf("ADVERSARY BREAK: audit payload missing agent_id/agent; payload=%#v", rec.Payload)
	}
}

func TestAdversary_B7T05_DirectStoreGetBypassesRevocation(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte("secret")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	broker := newTestBroker(t, store, &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	if err := broker.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// Direct store access bypasses broker revocation check entirely
	val, err := store.Get(ctx, "api-token")
	if err != nil {
		t.Fatalf("direct Get after revoke: %v", err)
	}
	if string(val) != "secret" {
		t.Fatalf("direct Get value mismatch")
	}
	// ADVERSARY BREAK: revocation cannot prevent direct SecretStore.Get calls (broker is not the enforced only path)
	t.Log("ADVERSARY BREAK: direct store.Get succeeds for revoked credential (bypass possible if store reference held)")
}

func TestAdversary_B7T05_RevokeNonExistentNoPanic(t *testing.T) {
	ctx := context.Background()
	broker := newTestBroker(t, NewFakeKeyStore(), &recordingAuditSink{}, newBrokeredPolicy("api.example.com", 443))

	// Should be no-op, no panic
	if err := broker.Revoke(ctx, "nonexistent"); err != nil {
		t.Fatalf("Revoke nonexistent: %v", err)
	}
	if !broker.IsRevoked("nonexistent") {
		t.Fatal("expected IsRevoked true after revoke nonexistent")
	}
}

func TestAdversary_B7T05_RestartAffectedOnlyActiveLeased(t *testing.T) {
	ctx := context.Background()
	broker, err := NewBroker(BrokerConfig{
		Store:      NewFakeKeyStore(),
		Policy:     newBrokeredPolicy("api.example.com", 443),
		ActiveRuns: []string{"run-active"},
		ActiveDirectLeases: map[string][]string{
			"api-token": {"run-active", "run-completed", "run-no-lease"},
		},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:       &recordingAuditSink{},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	got, err := broker.RestartAffectedAgents(ctx, "api-token")
	if err != nil {
		t.Fatalf("RestartAffectedAgents: %v", err)
	}
	if len(got) != 1 || got[0] != "run-active" {
		t.Fatalf("RestartAffectedAgents returned unexpected: %v (should only active leased runs)", got)
	}
}

func TestAdversary_B7T05_RevocationPerBrokerInstance(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte("secret")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	p := newBrokeredPolicy("api.example.com", 443)
	b1 := newTestBroker(t, store, &recordingAuditSink{}, p)
	b2 := newTestBroker(t, store, &recordingAuditSink{}, p)

	if err := b1.Revoke(ctx, "api-token"); err != nil {
		t.Fatalf("Revoke b1: %v", err)
	}
	if !b1.IsRevoked("api-token") {
		t.Fatal("b1 should see revoked")
	}
	if b2.IsRevoked("api-token") {
		t.Fatal("ADVERSARY BREAK: revocation leaked across broker instances")
	}
}