package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func newAdversaryBroker(t *testing.T, store SecretStore, p *policy.Policy, activeRuns []string, ruleMethods map[string][]string) *Broker {
	t.Helper()
	if activeRuns == nil {
		activeRuns = []string{"run-active"}
	}
	if ruleMethods == nil {
		ruleMethods = map[string][]string{"egress[0]": {http.MethodGet, http.MethodPost}}
	}
	b, err := NewBroker(BrokerConfig{
		Store:       store,
		Policy:      p,
		ActiveRuns:  activeRuns,
		RuleMethods: ruleMethods,
		Audit:       &recordingAuditSink{},
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	return b
}

func newBrokeredPolicyWithHeader(domain string, port int, headerName string) *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: domain, Ports: []int{port}, Credential: "api-token"},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: "brokered", Header: headerName, Service: "test-store"},
		},
	}
}

func TestAdversary_B7_T02_SecretValueLeakageInStringAndErrors(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("super-secret-value-123"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	// Check all string representations
	for _, s := range []string{
		fmt.Sprint(inj),
		fmt.Sprintf("%v", inj),
		fmt.Sprintf("%#v", inj),
		inj.String(),
		inj.GoString(),
	} {
		if strings.Contains(s, "super-secret-value-123") {
			t.Fatalf("ADVERSARY BREAK: secret leaked in CredentialInjection string: %q", s)
		}
		if !strings.Contains(s, "[REDACTED]") {
			t.Fatalf("missing redaction in %q", s)
		}
	}

	// Denial error must not leak
	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://evil.com", http.MethodGet)
	if err != nil && strings.Contains(err.Error(), "super-secret-value-123") {
		t.Fatalf("ADVERSARY BREAK: secret leaked in denial error: %v", err)
	}
}

func TestAdversary_B7_T02_WrongDomainBypass_IDN_Homograph_TrailingDot_Case(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	// IDN homograph (cyrillic 'а' looks like latin 'a')
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://аpi.example.com/v1", http.MethodGet)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: IDN homograph bypass succeeded")
	}

	// Trailing dot - normalized so allowed (intended)
	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com./v1", http.MethodGet)
	if err == nil {
		t.Logf("trailing dot normalized (safe)")
	}

	// Case mixing handled
	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://API.Example.COM/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("case mix should succeed: %v", err)
	}

	// Subdomain strict (no wildcard)
	_, err = broker.RequestCredential(ctx, "run-active", "egress[0]", "https://sub.api.example.com/v1", http.MethodGet)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: subdomain bypass without wildcard")
	}
}

func TestAdversary_B7_T02_WrongMethodBypass_Case_Unusual(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, map[string][]string{"egress[0]": {http.MethodGet}})

	// lowercase method - normalized to upper in check (intended safe)
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", "get")
	if err == nil {
		t.Logf("lowercase method normalized (safe)")
	}

	// Unusual methods should be denied
	for _, m := range []string{"CONNECT", "TRACE", "PATCH"} {
		_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", m)
		if err == nil {
			t.Fatalf("ADVERSARY BREAK: unusual method %s allowed", m)
		}
	}
}

func TestAdversary_B7_T02_CredentialedRedirect_Denied(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	// Use dynamic port from server
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.com", http.StatusFound)
	}))
	defer redirector.Close()
	domain, port := mustServerDomainPort(t, redirector.URL)
	p := newBrokeredPolicy(domain, port)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	gw := NewGateway(broker, redirector.Client())
	_, err := gw.Do(ctx, GatewayRequest{
		RunID:        "run-active",
		PolicyRuleID: "egress[0]",
		Method:       http.MethodGet,
		URL:          redirector.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "credentialed redirect") {
		t.Fatalf("ADVERSARY BREAK: credentialed redirect to evil not denied: %v", err)
	}
}

func TestAdversary_B7_T02_PortValidation_Precise(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	// explicit same port should succeed
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com:443/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("explicit 443 should work: %v", err)
	}

	// wrong ports should deny
	for _, d := range []string{
		"https://api.example.com:0/v1",
		"https://api.example.com:65535/v1",
		"https://api.example.com:8080/v1",
	} {
		_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", d, http.MethodGet)
		if err == nil {
			t.Fatalf("ADVERSARY BREAK: port bypass on %s", d)
		}
	}
}

func TestAdversary_B7_T02_PolicyRuleSpoofing(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test"},
		Egress: []policy.EgressRule{
			{Domain: "good.com", Ports: []int{443}, Credential: "api-token"},
			{Domain: "evil.com", Ports: []int{443}, Credential: "other-token"},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: "brokered", Header: "Authorization", Service: "test"},
		},
	}
	broker := newAdversaryBroker(t, store, p, nil, map[string][]string{"egress[0]": {http.MethodGet}, "egress[1]": {http.MethodGet}})

	// rule 0 on evil domain must deny
	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://evil.com", http.MethodGet)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: rule 0 allowed evil domain")
	}
}

func TestAdversary_B7_T02_RunIDValidation(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, []string{"run-active"}, nil)

	cases := []string{"", "run-inactive", "nonexistent"}
	for _, rid := range cases {
		_, err := broker.RequestCredential(ctx, rid, "egress[0]", "https://api.example.com/v1", http.MethodGet)
		if err == nil {
			t.Fatalf("ADVERSARY BREAK: run id %q allowed", rid)
		}
	}
}

func TestAdversary_B7_T02_HeaderInjectionViaPolicy(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	// Malicious header name/value in policy
	p := newBrokeredPolicyWithHeader("api.example.com", 443, "X-Injected\r\nEvil: header")
	broker := newAdversaryBroker(t, store, p, nil, nil)

	inj, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: CRLF header name was not rejected, got injection: %+v", inj)
	}
}

func TestAdversary_B7_T02_ConcurrentRequests_Race(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent error: %v", e)
	}
}

func TestAdversary_B7_T02_MissingSecretError_NoValueLeak(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore() // no secret set
	p := newBrokeredPolicy("api.example.com", 443)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	_, err := broker.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("want ErrSecretNotFound, got %v", err)
	}
	if strings.Contains(err.Error(), testSecretValue) {
		t.Fatalf("ADVERSARY BREAK: missing secret error leaked value")
	}
}

func TestAdversary_B7_T02_AuditEventCompleteness(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	p := newBrokeredPolicy("api.example.com", 443)
	sink := &recordingAuditSink{}
	b, _ := NewBroker(BrokerConfig{
		Store:       store,
		Policy:      p,
		ActiveRuns:  []string{"run-active"},
		RuleMethods: map[string][]string{"egress[0]": {http.MethodGet}},
		Audit:       sink,
		Now:         func() time.Time { return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC) },
	})

	_, err := b.RequestCredential(ctx, "run-active", "egress[0]", "https://api.example.com/v1", http.MethodGet)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	rec := sink.last(t)
	if rec.Payload == nil {
		t.Fatal("no payload")
	}
	if v, ok := rec.Payload["visible_to_agent"]; !ok || v != false {
		t.Fatalf("ADVERSARY BREAK: audit missing visible_to_agent=false or wrong: %#v", rec.Payload)
	}
	if strings.Contains(fmt.Sprint(rec.Payload), "secret") {
		t.Fatalf("ADVERSARY BREAK: audit payload contains secret value")
	}
}

func TestAdversary_B7_T02_GatewayNoFollowCredentialedRedirect(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	_ = store.Set(ctx, "api-token", []byte("secret"))
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.com", http.StatusFound)
	}))
	defer redirector.Close()
	domain, port := mustServerDomainPort(t, redirector.URL)
	p := newBrokeredPolicy(domain, port)
	broker := newAdversaryBroker(t, store, p, nil, nil)

	gw := NewGateway(broker, redirector.Client())
	_, err := gw.Do(ctx, GatewayRequest{RunID: "run-active", PolicyRuleID: "egress[0]", URL: redirector.URL})
	if err == nil || !strings.Contains(err.Error(), "credentialed redirect denied") {
		t.Fatalf("ADVERSARY BREAK: followed credentialed redirect: %v", err)
	}
}
