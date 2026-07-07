package secrets

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const brokerMapSentinel = "SENTINEL-B23-BROKER-MAP-xyzzy"
const receiverSentinel = "RECEIVER-B23-T03-DISTINCT-VALUE"

func dualEgressBrokeredPolicy() *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "route-agent"},
		Egress: []policy.EgressRule{
			{Domain: "api-a.example.com", Ports: []int{443}, Credential: "key-a"},
			{Domain: "api-b.example.com", Ports: []int{443}, Credential: "key-b"},
		},
		Credentials: []policy.Credential{
			{ID: "key-a", Type: "brokered", Header: "Authorization"},
			{ID: "key-b", Type: "brokered", Header: "Authorization"},
		},
	}
}

func newMappedBroker(t *testing.T, store SecretStore, pol *policy.Policy, credMap map[string]string, installRef string) *Broker {
	t.Helper()
	b, err := NewBroker(BrokerConfig{
		Store:              store,
		Policy:             pol,
		ActiveRuns:         []string{"run-active"},
		RuleMethods:        map[string][]string{"egress[0]": {http.MethodGet}, "egress[1]": {http.MethodGet}},
		CredentialResolver: MapCredentialResolver{Map: credMap},
		InstallRef:         installRef,
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	return b
}

func TestBrokerMappedCredential_InjectsReceiverValue(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "receiver-secret", []byte(receiverSentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := newBrokeredPolicy("api.example.com", 443)
	broker := newMappedBroker(t, store, pol, map[string]string{"api-token": "receiver-secret"}, "test-agent@00000000")

	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if inj.HeaderValue != receiverSentinel {
		t.Fatalf("HeaderValue = %q, want receiver sentinel", inj.HeaderValue)
	}
}

func TestBrokerDeferredCredential_ActionableDenial(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	pol := newBrokeredPolicy("api.example.com", 443)
	broker := newMappedBroker(t, store, pol, map[string]string{}, "weather@a1b2c3d4")

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrCredentialUnmapped) {
		t.Fatalf("want ErrCredentialUnmapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "map credential api-token") {
		t.Fatalf("error = %q", err.Error())
	}
	if !strings.Contains(err.Error(), "agentpaas installed map-credential weather@a1b2c3d4 api-token=<local>") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBrokerMappedCredential_RouteScopeRuleACannotHitRuleB(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "shared-local", []byte(receiverSentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := dualEgressBrokeredPolicy()
	broker := newMappedBroker(t, store, pol, map[string]string{
		"key-a": "shared-local",
		"key-b": "shared-local",
	}, "route-agent@00000000")

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api-b.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("want domain denial, got %v", err)
	}
}

func TestBrokerNilResolver_BackwardsCompat(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte(testSecretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	broker, err := NewBroker(BrokerConfig{
		Store: store, Policy: newBrokeredPolicy("api.example.com", 443),
		ActiveRuns: []string{"run-active"},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
		Audit: auditSink,
		Now: func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if inj.HeaderValue != testSecretValue {
		t.Fatalf("HeaderValue = %q", inj.HeaderValue)
	}
}

func TestAdversary_BrokerMapScopeWideningDenied(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "evil-local", []byte(brokerMapSentinel)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	pol := dualEgressBrokeredPolicy()
	broker := newMappedBroker(t, store, pol, map[string]string{"key-b": "evil-local"}, "")

	_, err := broker.RequestCredential(ctx, "run-active", "egress[1]", "https://api-a.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("want domain denial, got %v", err)
	}
}

func TestAdversary_UnmappedDenialNoValueLeak(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "local", []byte(brokerMapSentinel))
	auditSink := &recordingAuditSink{}
	pol := newBrokeredPolicy("api.example.com", 443)
	broker, err := NewBroker(BrokerConfig{
		Store: store, Policy: pol, ActiveRuns: []string{"run-active"},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
		Audit: auditSink, CredentialResolver: MapCredentialResolver{Map: map[string]string{}},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrCredentialUnmapped) {
		t.Fatalf("want ErrCredentialUnmapped, got %v", err)
	}
	if strings.Contains(err.Error(), brokerMapSentinel) {
		t.Fatalf("denial leaked sentinel")
	}
	rec := auditSink.last(t)
	if rec.Payload["status"] != "denied" {
		t.Fatalf("audit = %+v", rec.Payload)
	}
}

func TestAdversary_BrokerDenialAndAuditNoSentinel(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "mapped", []byte(brokerMapSentinel))
	pol := newBrokeredPolicy("api.example.com", 443)
	broker := newMappedBroker(t, store, pol, map[string]string{}, "ref@00000000")
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, s := range []string{err.Error()} {
		if strings.Contains(s, brokerMapSentinel) {
			t.Fatalf("leaked sentinel in %q", s)
		}
	}
}