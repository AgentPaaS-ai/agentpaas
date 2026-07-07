package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

const adversaryB23T03Sentinel = "ADVERSARY-B23T03-SECRET-MUST-NOT-LEAK-999"

func TestAdversary_B23T03_ScopeWideningMappedCredCannotCrossEgressRule(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "shared-local", []byte(adversaryB23T03Sentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := dualEgressBrokeredPolicy()
	broker := newMappedBroker(t, store, pol, map[string]string{
		"key-a": "shared-local",
	}, "route-agent@00000000")

	// Rule A credential mapped; attempt rule B destination.
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api-b.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("want domain denial for cross-rule scope widening, got %v", err)
	}
}

func TestAdversary_B23T03_UnmappedFailClosedNoInjectionAuditDenied(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "only-local", []byte(adversaryB23T03Sentinel))
	auditSink := &recordingAuditSink{}
	pol := newBrokeredPolicy("api.example.com", 443)
	broker, err := NewBroker(BrokerConfig{
		Store:              store,
		Policy:             pol,
		ActiveRuns:         []string{"run-active"},
		RuleMethods:        map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:              auditSink,
		CredentialResolver: MapCredentialResolver{Map: map[string]string{}},
		Now:                func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrCredentialUnmapped) {
		t.Fatalf("want ErrCredentialUnmapped, got %v", err)
	}
	if strings.Contains(err.Error(), adversaryB23T03Sentinel) {
		t.Fatal("unmapped denial leaked secret value")
	}
	rec := auditSink.last(t)
	if rec.Payload["status"] != "denied" {
		t.Fatalf("audit status = %v", rec.Payload["status"])
	}
	if rec.Payload["reason"] != "unmapped" {
		t.Fatalf("audit reason = %v, want unmapped", rec.Payload["reason"])
	}
}

func TestAdversary_B23T03_SentinelAbsentFromBrokerErrorsAndAudit(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "mapped-local", []byte(adversaryB23T03Sentinel))
	auditSink := &recordingAuditSink{}
	pol := newBrokeredPolicy("api.example.com", 443)
	broker, err := NewBroker(BrokerConfig{
		Store:              store,
		Policy:             pol,
		ActiveRuns:         []string{"run-active"},
		RuleMethods:        map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:              auditSink,
		CredentialResolver: MapCredentialResolver{Map: map[string]string{}},
		InstallRef:         "agent@deadbeef",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("expected unmapped denial")
	}
	blobs := []string{err.Error(), fmt.Sprint(auditSink.last(t).Payload)}
	for _, blob := range blobs {
		if strings.Contains(blob, adversaryB23T03Sentinel) {
			t.Fatalf("sentinel leaked in broker output: %q", blob)
		}
	}
}

func TestAdversary_B23T03_DeferredNeverFallsBackToDeclaredStoreKey(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	// Attacker pre-seeds store under declared policy ID; mapping is deferred.
	if err := store.Set(ctx, "api-token", []byte(adversaryB23T03Sentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := newBrokeredPolicy("api.example.com", 443)
	broker := newMappedBroker(t, store, pol, map[string]string{}, "agent@00000000")

	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrCredentialUnmapped) {
		t.Fatalf("want ErrCredentialUnmapped (no direct lookup), got inj=%+v err=%v", inj, err)
	}
	if inj.HeaderValue != "" {
		t.Fatalf("must not inject value on deferred credential, got %q", inj.HeaderValue)
	}
}

func TestAdversary_B23T03_NilResolverBackwardCompatDeclaredKey(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	const legacy = "legacy-direct-key-value"
	if err := store.Set(ctx, "api-token", []byte(legacy)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	broker, err := NewBroker(BrokerConfig{
		Store:       store,
		Policy:      newBrokeredPolicy("api.example.com", 443),
		ActiveRuns:  []string{"run-active"},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if inj.HeaderValue != legacy {
		t.Fatalf("nil resolver should use declared id as store key, got %q", inj.HeaderValue)
	}
}

func TestAdversary_B23T03_TamperedMapEntryPolicyRuleStillGatesDestination(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "wide-local", []byte(adversaryB23T03Sentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := dualEgressBrokeredPolicy()
	// Manifest attacker maps key-b to a local that exists; rule for egress[1] still requires api-b host.
	broker := newMappedBroker(t, store, pol, map[string]string{
		"key-b": "wide-local",
	}, "route-agent@00000000")

	_, err := broker.RequestCredential(ctx, "run-active", "egress[1]", "https://api-a.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("signed egress rule must gate destination despite map entry, got %v", err)
	}
}

func TestAdversary_B23T03_CredentialInjectionStringNeverContainsValue(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "local", []byte(adversaryB23T03Sentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	broker := newMappedBroker(t, store, newBrokeredPolicy("api.example.com", 443),
		map[string]string{"api-token": "local"}, "ref@00000000")
	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	rendered := fmt.Sprintf("%s %#v", inj.String(), inj)
	if strings.Contains(rendered, adversaryB23T03Sentinel) {
		t.Fatalf("CredentialInjection formatting leaked value: %s", rendered)
	}
}

func TestAdversary_B23T03_AuditPayloadJSONNoSentinel(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "x", []byte(adversaryB23T03Sentinel))
	auditSink := &recordingAuditSink{}
	broker, err := NewBroker(BrokerConfig{
		Store:              store,
		Policy:             newBrokeredPolicy("api.example.com", 443),
		ActiveRuns:         []string{"run-active"},
		RuleMethods:        map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:              auditSink,
		CredentialResolver: MapCredentialResolver{Map: map[string]string{}},
		InstallRef:         "ref@00000000",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	_, _ = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	rec := auditSink.last(t)
	raw, err := json.Marshal(rec.Payload)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	if strings.Contains(string(raw), adversaryB23T03Sentinel) {
		t.Fatalf("audit JSON leaked sentinel: %s", string(raw))
	}
	if rec.EventType != audit.EventTypeSecretInjected {
		t.Fatalf("event type = %s", rec.EventType)
	}
}