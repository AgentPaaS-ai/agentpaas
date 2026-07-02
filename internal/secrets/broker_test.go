package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
)

const testSecretValue = "Bearer brokered-secret-value"

type recordingAuditSink struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (r *recordingAuditSink) Append(record audit.AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func (r *recordingAuditSink) last(t *testing.T) audit.AuditRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) == 0 {
		t.Fatal("expected at least one audit record")
	}
	return r.records[len(r.records)-1]
}

func newTestBroker(t *testing.T, store SecretStore, auditSink *recordingAuditSink, p *policy.Policy) *Broker {
	t.Helper()
	b, err := NewBroker(BrokerConfig{
		Store:       store,
		Policy:      p,
		ActiveRuns:  []string{"run-active"},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:       auditSink,
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	return b
}

func newBrokeredPolicy(domain string, port int) *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: domain, Ports: []int{port}, Credential: "api-token"},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: "brokered", Header: "Authorization", Service: "test-store"},
		},
	}
}

func newSecretStore(t *testing.T) SecretStore {
	t.Helper()
	store := NewFakeKeyStore()
	if err := store.Set(context.Background(), "api-token", []byte(testSecretValue)); err != nil {
		t.Fatalf("Set secret: %v", err)
	}
	return store
}

func TestBrokerRequestCredentialValidatesAndAuditsInjection(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	injection, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	if injection.HeaderName != "Authorization" {
		t.Fatalf("HeaderName = %q, want Authorization", injection.HeaderName)
	}
	if injection.HeaderValue != testSecretValue {
		t.Fatalf("HeaderValue = %q, want secret value", injection.HeaderValue)
	}

	rec := auditSink.last(t)
	assertSecretAudit(t, rec, "injected", "run-active", "egress[0]", "api-token", "api.example.com:443", http.MethodGet)
}

func TestBrokerDeniesInactiveRun(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	_, err := broker.RequestCredential(ctx, "run-stale", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "active run") {
		t.Fatalf("RequestCredential error = %v, want active run denial", err)
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-stale", "egress[0]", "api-token", "api.example.com:443", http.MethodGet)
}

func TestBrokerDeniesWrongDomainBeforeInjection(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://wrong.com/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination domain") {
		t.Fatalf("RequestCredential error = %v, want domain denial", err)
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", "wrong.com:443", http.MethodGet)
}

func TestBrokerDeniesWrongMethodBeforeInjection(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodPost)
	if err == nil || !strings.Contains(err.Error(), "method") {
		t.Fatalf("RequestCredential error = %v, want method denial", err)
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", "api.example.com:443", http.MethodPost)
}

func TestBrokerDeniesWrongPortBeforeInjection(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com:8080/v1", http.MethodGet)
	if err == nil || !strings.Contains(err.Error(), "destination port") {
		t.Fatalf("RequestCredential error = %v, want port denial", err)
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", "api.example.com:8080", http.MethodGet)
}

func TestBrokerMissingSecretReturnsActionableError(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, NewFakeKeyStore(), auditSink, newBrokeredPolicy("api.example.com", 443))

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("RequestCredential error = %v, want ErrSecretNotFound", err)
	}
	if !strings.Contains(err.Error(), "api-token") {
		t.Fatalf("missing secret error = %q, want credential id", err.Error())
	}
	if strings.Contains(err.Error(), testSecretValue) {
		t.Fatalf("missing secret error leaked value: %v", err)
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", "api.example.com:443", http.MethodGet)
}

func TestCredentialInjectionRedactsStringOutputAndErrors(t *testing.T) {
	ctx := context.Background()
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy("api.example.com", 443))

	injection, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("RequestCredential: %v", err)
	}
	for _, rendered := range []string{
		fmt.Sprint(injection),
		fmt.Sprintf("%v", injection),
		fmt.Sprintf("%+v", injection),
		injection.String(),
	} {
		if strings.Contains(rendered, testSecretValue) {
			t.Fatalf("CredentialInjection string leaked secret: %q", rendered)
		}
		if !strings.Contains(rendered, "[REDACTED]") {
			t.Fatalf("CredentialInjection string = %q, want redaction marker", rendered)
		}
	}

	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://wrong.com/v1", http.MethodGet)
	if err == nil {
		t.Fatal("RequestCredential wrong domain returned nil error")
	}
	if strings.Contains(err.Error(), testSecretValue) {
		t.Fatalf("denial error leaked secret: %v", err)
	}
}

func TestGatewayInjectsAuthorizationHeaderForValidRequest(t *testing.T) {
	ctx := context.Background()
	received := make(chan string, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer func() { upstream.Close() }()

	domain, port := mustServerDomainPort(t, upstream.URL)
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy(domain, port))
	gateway := NewGateway(broker, upstream.Client())

	resp, err := gateway.Do(ctx, GatewayRequest{
		RunID:        "run-active",
		PolicyRuleID: "egress[0]",
		Method:       http.MethodGet,
		URL:          upstream.URL,
	})
	if err != nil {
		t.Fatalf("Gateway Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll response: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("response body = %q, want ok", body)
	}

	select {
	case got := <-received:
		if got != testSecretValue {
			t.Fatalf("Authorization header = %q, want secret value", got)
		}
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive request")
	}
}

func TestGatewayDeniesCredentialedRedirectBeforeInjection(t *testing.T) {
	ctx := context.Background()
	otherHit := make(chan struct{}, 1)
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherHit <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() { other.Close() }()
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer func() { redirector.Close() }()

	domain, port := mustServerDomainPort(t, redirector.URL)
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, newBrokeredPolicy(domain, port))
	gateway := NewGateway(broker, redirector.Client())

	resp, err := gateway.Do(ctx, GatewayRequest{
		RunID:        "run-active",
		PolicyRuleID: "egress[0]",
		Method:       http.MethodGet,
		URL:          redirector.URL,
	})
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err == nil || !strings.Contains(err.Error(), "credentialed redirect") {
		t.Fatalf("Gateway Do error = %v, want credentialed redirect denial", err)
	}
	if strings.Contains(err.Error(), testSecretValue) {
		t.Fatalf("redirect denial leaked secret: %v", err)
	}
	select {
	case <-otherHit:
		t.Fatal("gateway followed credentialed redirect to different destination")
	default:
	}
	assertSecretAudit(t, auditSink.last(t), "denied", "run-active", "egress[0]", "api-token", mustDestination(t, other.URL), http.MethodGet)
}

func TestGatewayRechecksNonCredentialedRedirectsPerHop(t *testing.T) {
	ctx := context.Background()
	finalHit := make(chan struct{}, 1)
	final := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finalHit <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer func() { final.Close() }()
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer func() { redirector.Close() }()

	redirectDomain, redirectPort := mustServerDomainPort(t, redirector.URL)
	finalDomain, finalPort := mustServerDomainPort(t, final.URL)
	auditSink := &recordingAuditSink{}
	broker := newTestBroker(t, newSecretStore(t), auditSink, &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: redirectDomain, Ports: []int{redirectPort}},
			{Domain: finalDomain, Ports: []int{finalPort}},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: "brokered", Header: "Authorization", Service: "test-store"},
		},
	})
	gateway := NewGateway(broker, redirector.Client())

	resp, err := gateway.Do(ctx, GatewayRequest{
		RunID:  "run-active",
		Method: http.MethodGet,
		URL:    redirector.URL,
	})
	if err != nil {
		t.Fatalf("Gateway Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	select {
	case <-finalHit:
	case <-time.After(time.Second):
		t.Fatal("final redirect target was not reached")
	}
}

func assertSecretAudit(t *testing.T, rec audit.AuditRecord, status, runID, policyRuleID, credentialID, destination, method string) {
	t.Helper()
	if rec.EventType != audit.EventTypeSecretInjected {
		t.Fatalf("EventType = %q, want %q", rec.EventType, audit.EventTypeSecretInjected)
	}
	if rec.Timestamp != "2026-06-21T12:00:00Z" {
		t.Fatalf("Timestamp = %q, want fixed time", rec.Timestamp)
	}
	want := map[string]interface{}{
		"status":           status,
		"run_id":           runID,
		"policy_rule_id":   policyRuleID,
		"credential_id":    credentialID,
		"destination":      destination,
		"method":           method,
		"visible_to_agent": false,
	}
	for key, value := range want {
		if rec.Payload[key] != value {
			t.Fatalf("Payload[%q] = %#v, want %#v; payload=%#v", key, rec.Payload[key], value, rec.Payload)
		}
	}
	if fmt.Sprint(rec.Payload) == testSecretValue || strings.Contains(fmt.Sprint(rec.Payload), testSecretValue) {
		t.Fatalf("audit payload leaked secret value: %#v", rec.Payload)
	}
}

func mustServerDomainPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	dest, err := parseDestination(rawURL)
	if err != nil {
		t.Fatalf("parseDestination(%q): %v", rawURL, err)
	}
	return dest.domain, dest.port
}

func mustDestination(t *testing.T, rawURL string) string {
	t.Helper()
	dest, err := parseDestination(rawURL)
	if err != nil {
		t.Fatalf("parseDestination(%q): %v", rawURL, err)
	}
	return dest.String()
}
