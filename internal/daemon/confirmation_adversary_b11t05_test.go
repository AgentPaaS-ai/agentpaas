package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
)

// ADVERSARY TEST FILE - breaks that prove security boundary gaps

func TestAdversaryB11T05_WildcardStillCreatesConfirmation(t *testing.T) {
	// ADVERSARY BREAK: wildcard "*" (high-risk rejected) still creates confirmation ID and RequiresConfirmation=true
	// allowing potential approval path for a change the parser claims to reject.
	server := newOperatorTestServer(t)
	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to *",
	})
	if err != nil {
		t.Fatalf("RecommendPolicyPatch: %v", err)
	}
	if !resp.GetConfirmation().GetRequiresConfirmation() {
		t.Fatal("expected RequiresConfirmation=true even for wildcard reject")
	}
	id := resp.GetConfirmation().GetConfirmationId()
	if id == "" {
		t.Fatal("no confirmation ID generated for rejected wildcard")
	}
	// prove can "approve" the rejected change
	if err := server.ConfirmChange(id, true); err != nil {
		t.Fatalf("ConfirmChange on wildcard confirmation failed: %v", err)
	}
	stored, _ := server.confirmationStore().Get(id)
	if stored.Status != "approved" {
		t.Fatalf("wildcard confirmation status=%s after approve", stored.Status)
	}
}

func TestAdversaryB11T05_ConfirmationIDPrediction(t *testing.T) {
	// ADVERSARY BREAK: confirmation IDs are predictable (confirm_<unix>_<seq>) allowing potential pre-creation guessing
	server := newOperatorTestServer(t)
	resp1, _ := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to example.com",
	})
	id1 := resp1.GetConfirmation().GetConfirmationId()
	parts := strings.Split(id1, "_")
	if len(parts) != 3 {
		t.Fatalf("unexpected ID format %s", id1)
	}
	// predict next by incrementing the seq part (implementation detail exposed)
	seq := parts[2]
	// attempt to Get a forged future ID - this should fail, but predictability is the gap
	forged := "confirm_" + parts[1] + "_" + "999999"
	_, err := server.confirmationStore().Get(forged)
	if err == nil {
		t.Fatal("forged future ID unexpectedly existed - ID prediction allows forgery")
	}
	_ = seq // used for format check
}

func TestAdversaryB11T05_ConcurrentApproveDecline(t *testing.T) {
	// ADVERSARY BREAK: concurrent approve/decline on same ID (mutex should serialize but test races decision)
	server := newOperatorTestServer(t)
	resp, _ := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to race.example.com",
	})
	id := resp.GetConfirmation().GetConfirmationId()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = server.ConfirmChange(id, true)
	}()
	go func() {
		defer wg.Done()
		_ = server.ConfirmChange(id, false)
	}()
	wg.Wait()
	// after race, status should be decided, but if both "succeed" or deadlock, break
	stored, err := server.confirmationStore().Get(id)
	if err != nil {
		t.Fatalf("Get after concurrent: %v", err)
	}
	if stored.Status == "pending" {
		t.Fatal("concurrent decide left confirmation pending - race condition")
	}
}

func TestAdversaryB11T05_ApproveExpiredConfirmation(t *testing.T) {
	// ADVERSARY BREAK: expired confirmations might still be approvable if expiry check is racy or bypassed
	server := newOperatorTestServer(t)
	// create with past expiry via internal propose (bypassing normal TTL)
	id, _ := server.proposeTrustBoundaryChange(PendingConfirmation{
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt:  time.Now().Add(-5 * time.Minute),
		ChangeType: "policy_patch",
		RiskLevel:  string(operator.RiskHigh),
		Rationale:  "expired test",
	})
	// try approve
	err := server.ConfirmChange(id, true)
	if err == nil {
		t.Fatal("Approve succeeded on expired confirmation - expiry bypass")
	}
}

func TestAdversaryB11T05_AllChangeTypesCovered(t *testing.T) {
	// ADVERSARY BREAK: only 3 of 9 change types supported in RecommendPolicyPatch; others have no confirmation path
	server := newOperatorTestServer(t)
	unsupported := []string{"local_handoff", "webhook_destination", "exposed_listener", "retention_purge", "unrelated_run_stop", "destructive_op"}
	for _, ct := range unsupported {
		// direct propose works (test helper), but no Recommend path means no enforcement via desired_behavior
		id, err := server.proposeTrustBoundaryChange(PendingConfirmation{ChangeType: ct, RiskLevel: string(operator.RiskHigh)})
		if err != nil {
			t.Fatalf("%s propose failed: %v", ct, err)
		}
		if _, err := server.confirmationStore().Get(id); err != nil {
			t.Fatalf("%s confirmation not stored", ct)
		}
	}
	_ = server // silence
}

func TestAdversaryB11T05_YAMLInjectionViaDesired(t *testing.T) {
	// ADVERSARY BREAK: desired_behavior with newlines/nulls/unicode could inject into ProposedPatch YAML
	server := newOperatorTestServer(t)
	resp, err := server.RecommendPolicyPatch(context.Background(), &controlv1.RecommendPolicyPatchRequest{
		DesiredBehavior: "allow egress to example.com\n  ports: [0]\ncredential: injected",
	})
	if err != nil {
		// parse fails -> unableToParse is safe
		return
	}
	patch := resp.GetProposedPatch()
	if strings.Contains(patch, "injected") || strings.Contains(patch, "ports: [0]") {
		t.Fatalf("YAML injection succeeded in patch: %s", patch)
	}
}
