package mcpmanager

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type egressRecordingAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (r *egressRecordingAudit) Append(record audit.AuditRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record)
	return nil
}

func (r *egressRecordingAudit) last(t *testing.T) audit.AuditRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.records) == 0 {
		t.Fatal("expected audit record")
	}
	return r.records[len(r.records)-1]
}

func TestEgressPolicyAllowsAllowListedEndpoint(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		MCPServerID: "server-a",
	}}, nil)

	allowed, credentialID, policyRuleID, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
	if err != nil {
		t.Fatalf("CheckEgress returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected egress to be allowed")
	}
	if credentialID != "" {
		t.Fatalf("credentialID = %q, want empty", credentialID)
	}
	if policyRuleID != "egress[0]" {
		t.Fatalf("policyRuleID = %q, want egress[0]", policyRuleID)
	}
}

func TestEgressPolicyDeniesNonAllowListedEndpoint(t *testing.T) {
	appender := &egressRecordingAudit{}
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		MCPServerID: "server-a",
	}}, appender)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "https://other.example.com/v1/tools", "GET")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
	record := appender.last(t)
	if record.EventType != "mcp_egress_decision" {
		t.Fatalf("event type = %q, want mcp_egress_decision", record.EventType)
	}
	if got := record.Payload["decision"]; got != "denied" {
		t.Fatalf("decision = %v, want denied", got)
	}
}

func TestEgressPolicyDefaultDeny(t *testing.T) {
	appender := &egressRecordingAudit{}
	ep := NewEgressPolicy(nil, appender)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
}

func TestEgressPolicyDeniesHostAccess(t *testing.T) {
	for _, destination := range []string{
		"http://LOCALHOST:8080",
		"http://localhost:8080",
		"http://localhost.localdomain:8080",
		"http://localhost4:8080",
		"http://localhost6:8080",
		"http://127.0.0.1:8080",
		"http://127.42.0.1:8080",
		"http://[::1]:8080",
	} {
		t.Run(destination, func(t *testing.T) {
			ep := NewEgressPolicy([]policy.EgressRule{{
				Domain:      "*",
				Methods:     []string{"GET"},
				Ports:       []int{80},
				MCPServerID: "server-a",
			}}, nil)

			allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", destination, "GET")
			if err == nil {
				t.Fatal("expected error")
			}
			if allowed {
				t.Fatal("expected egress to be denied")
			}
		})
	}
}

func TestEgressPolicyDeniesDockerSocket(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "*",
		Methods:     []string{"GET"},
		Ports:       []int{80},
		MCPServerID: "server-a",
	}}, nil)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "unix:///var/run/docker.sock", "GET")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
}

func TestEgressPolicyDeniesLinkLocal(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "*",
		Methods:     []string{"GET"},
		Ports:       []int{80},
		MCPServerID: "server-a",
	}}, nil)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "http://169.254.169.254/latest/meta-data", "GET")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
}

func TestEgressPolicyDeniesMethodMismatch(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		MCPServerID: "server-a",
	}}, nil)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "POST")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
}

func TestEgressPolicyDeniesPortMismatch(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		MCPServerID: "server-a",
	}}, nil)

	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "http://api.example.com:8080/v1/tools", "GET")
	if err == nil {
		t.Fatal("expected error")
	}
	if allowed {
		t.Fatal("expected egress to be denied")
	}
}

func TestEgressPolicyAuditEventIncludesRequiredFields(t *testing.T) {
	appender := &egressRecordingAudit{}
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		Credential:  "cred-a",
		MCPServerID: "server-a",
	}}, appender)

	allowed, credentialID, policyRuleID, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
	if err != nil {
		t.Fatalf("CheckEgress returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected egress to be allowed")
	}
	if credentialID != "cred-a" {
		t.Fatalf("credentialID = %q, want cred-a", credentialID)
	}
	if policyRuleID != "egress[0]" {
		t.Fatalf("policyRuleID = %q, want egress[0]", policyRuleID)
	}

	record := appender.last(t)
	for _, key := range []string{"server_id", "destination", "method", "credential_id", "policy_rule_id", "decision"} {
		if _, ok := record.Payload[key]; !ok {
			t.Fatalf("missing audit payload key %q", key)
		}
	}
	if got := record.Payload["server_id"]; got != "server-a" {
		t.Fatalf("server_id = %v, want server-a", got)
	}
	if got := record.Payload["destination"]; got != "https://api.example.com/v1/tools" {
		t.Fatalf("destination = %v, want destination URL", got)
	}
	if got := record.Payload["method"]; got != "GET" {
		t.Fatalf("method = %v, want GET", got)
	}
	if got := record.Payload["credential_id"]; got != "cred-a" {
		t.Fatalf("credential_id = %v, want cred-a", got)
	}
	if got := record.Payload["policy_rule_id"]; got != "egress[0]" {
		t.Fatalf("policy_rule_id = %v, want egress[0]", got)
	}
	if got := record.Payload["decision"]; got != "allowed" {
		t.Fatalf("decision = %v, want allowed", got)
	}
}

func TestEgressPolicyReturnsCredentialIDForBrokeredEgress(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		Credential:  "brokered-token",
		MCPServerID: "server-a",
	}}, nil)

	allowed, credentialID, _, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
	if err != nil {
		t.Fatalf("CheckEgress returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected egress to be allowed")
	}
	if credentialID != "brokered-token" {
		t.Fatalf("credentialID = %q, want brokered-token", credentialID)
	}
}

func TestEgressPolicyMultipleRulesFirstMatchWins(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{
		{
			Domain:      "api.example.com",
			Methods:     []string{"GET"},
			Ports:       []int{443},
			Credential:  "cred-a",
			MCPServerID: "server-a",
		},
		{
			Domain:      "api.example.com",
			Methods:     []string{"GET"},
			Ports:       []int{443},
			Credential:  "cred-b",
			MCPServerID: "server-a",
		},
	}, nil)

	allowed, credentialID, policyRuleID, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
	if err != nil {
		t.Fatalf("CheckEgress returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected egress to be allowed")
	}
	if credentialID != "cred-a" {
		t.Fatalf("credentialID = %q, want cred-a", credentialID)
	}
	if policyRuleID != "egress[0]" {
		t.Fatalf("policyRuleID = %q, want egress[0]", policyRuleID)
	}
}

func TestEgressPolicyConcurrentCheckEgress(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{"GET"},
		Ports:       []int{443},
		MCPServerID: "server-a",
	}}, nil)

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed, _, _, err := ep.CheckEgress(context.Background(), "server-a", "https://api.example.com/v1/tools", "GET")
			if err != nil {
				errs <- err
				return
			}
			if !allowed {
				errs <- errors.New("expected egress to be allowed")
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
