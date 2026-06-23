package daemon

import (
	"context"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"gopkg.in/yaml.v3"
)

func TestRecommendPolicyPatch_EgressDomain(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	if !strings.Contains(resp.GetProposedPatch(), "api.example.com") {
		t.Fatalf("ProposedPatch = %q", resp.GetProposedPatch())
	}
	var patch map[string]interface{}
	if err := yaml.Unmarshal([]byte(resp.GetProposedPatch()), &patch); err != nil {
		t.Fatalf("ProposedPatch is invalid YAML: %v", err)
	}
	if resp.GetRiskLevel() != string(operator.RiskMedium) {
		t.Fatalf("RiskLevel = %q, want medium", resp.GetRiskLevel())
	}
	if len(resp.GetAffectedDestinations()) != 1 || resp.GetAffectedDestinations()[0] != "api.example.com" {
		t.Fatalf("AffectedDestinations = %#v", resp.GetAffectedDestinations())
	}
	if !resp.GetConfirmation().GetRequiresConfirmation() {
		t.Fatal("RequiresConfirmation = false")
	}
}

func TestRecommendPolicyPatch_WellKnownDomain(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to github.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	if resp.GetRiskLevel() != string(operator.RiskLow) {
		t.Fatalf("RiskLevel = %q, want low", resp.GetRiskLevel())
	}
}

func TestRecommendPolicyPatch_WildcardRejected(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to *",
	})
	if err != nil {
		return
	}
	if resp.GetRiskLevel() != string(operator.RiskHigh) {
		t.Fatalf("RiskLevel = %q, want high", resp.GetRiskLevel())
	}
	if !strings.Contains(strings.ToLower(resp.GetRationale()), "wildcard") {
		t.Fatalf("Rationale = %q, want wildcard rejection", resp.GetRationale())
	}
}

func TestRecommendPolicyPatch_CredentialBinding(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "bind credential mykey for openai",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	if len(resp.GetCredentialIds()) != 1 || resp.GetCredentialIds()[0] != "mykey" {
		t.Fatalf("CredentialIDs = %#v", resp.GetCredentialIds())
	}
	if resp.GetRiskLevel() != string(operator.RiskMedium) {
		t.Fatalf("RiskLevel = %q, want medium", resp.GetRiskLevel())
	}
}

func TestRecommendPolicyPatch_ConfirmationIDGenerated(t *testing.T) {
	server := newOperatorTestServer(t)

	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	id := resp.GetConfirmation().GetConfirmationId()
	if id == "" {
		t.Fatal("ConfirmationID is empty")
	}
	stored, err := server.confirmations.Get(id)
	if err != nil {
		t.Fatalf("Get confirmation: %v", err)
	}
	if stored.ID != id || stored.ProposedPatch != resp.GetProposedPatch() {
		t.Fatalf("stored confirmation = %#v", stored)
	}
}

func TestConfirmChange_Approve(t *testing.T) {
	server := newOperatorTestServer(t)
	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	if err := server.ConfirmChange(resp.GetConfirmation().GetConfirmationId(), true); err != nil {
		t.Fatalf("ConfirmChange: %v", err)
	}
}

func TestConfirmChange_Decline(t *testing.T) {
	server := newOperatorTestServer(t)
	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	id := resp.GetConfirmation().GetConfirmationId()
	if err := server.ConfirmChange(id, false); err != nil {
		t.Fatalf("ConfirmChange: %v", err)
	}
	next, err := server.NextAction(context.Background(), &controlv1.NextActionRequest{Context: id})
	if err != nil {
		t.Fatalf("NextAction: %v", err)
	}
	if next.GetNextAction() != string(operator.ActionFixCode) {
		t.Fatalf("NextAction = %q, want fix_code", next.GetNextAction())
	}
}

func TestListPendingConfirmations(t *testing.T) {
	server := newOperatorTestServer(t)
	for _, behavior := range []string{"allow egress to one.example.com", "allow egress to two.example.com"} {
		if _, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
			DesiredBehavior: behavior,
		}); err != nil {
			t.Fatalf("RecommendPolicyPatch: %v", err)
		}
	}
	if got := server.ListPendingConfirmations(); len(got) != 2 {
		t.Fatalf("len(ListPendingConfirmations()) = %d, want 2", len(got))
	}
}

func TestConfirmChange_NotFound(t *testing.T) {
	server := newOperatorTestServer(t)
	if err := server.ConfirmChange("confirm_unknown", true); err == nil {
		t.Fatal("ConfirmChange returned nil error")
	}
}

func TestConfirmChange_AlreadyDecided(t *testing.T) {
	server := newOperatorTestServer(t)
	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to api.example.com",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	id := resp.GetConfirmation().GetConfirmationId()
	if err := server.ConfirmChange(id, true); err != nil {
		t.Fatalf("ConfirmChange first approval: %v", err)
	}
	if err := server.ConfirmChange(id, true); err == nil {
		t.Fatal("ConfirmChange second approval returned nil error")
	}
}

func TestTrustBoundaryChangeTypesRequireConfirmation(t *testing.T) {
	server := newOperatorTestServer(t)
	changeTypes := []string{
		"policy_patch",
		"credential_binding",
		"direct_lease",
		"local_handoff",
		"webhook_destination",
		"exposed_listener",
		"retention_purge",
		"unrelated_run_stop",
		"destructive_op",
	}
	for _, changeType := range changeTypes {
		t.Run(changeType, func(t *testing.T) {
			id, err := server.proposeTrustBoundaryChange(PendingConfirmation{
				ChangeType: changeType,
				RiskLevel:  string(operator.RiskHigh),
				Rationale:  "test proposal",
			})
			if err != nil {
				t.Fatalf("proposeTrustBoundaryChange: %v", err)
			}
			change, err := server.confirmations.Get(id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if change.Status != "pending" {
				t.Fatalf("Status = %q, want pending", change.Status)
			}
		})
	}
}
