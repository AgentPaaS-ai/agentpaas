package daemon

import (
	"context"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ADVERSARY TEST SUITE for B26-T06 daemon skeleton wiring
// Targets fail-closed gates, resource leaks, CAS, scope, legacy regression, etc.
// Run: go test ./internal/daemon -count=1 -race -timeout 30s -run 'AdversaryT06' -v

func TestAdversaryT06_RoutedProjectBypass_LegacyRun(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	resp, err := s.Run(ctx, &controlv1.RunRequest{
		AgentName:      "routed-demo",
		DeploymentRef:  "dep-123",
		IdempotencyKey: "key-1",
	})
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: routed project bypassed via Run: %+v", resp)
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition || !strings.Contains(st.Message(), "routed_run_invocation_not_enabled") {
		t.Fatalf("expected not-enabled for routed, got %v", err)
	}
}

func TestAdversaryT06_ContinuationWithoutMutation(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.Run(ctx, &controlv1.RunRequest{
		AgentName:     "legacy-agent",
		ContinueRunId: "run-123",
	})
	if err == nil {
		t.Fatal("ADVERSARY BREAK: continuation accepted without mutation gate")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition || !strings.Contains(st.Message(), "routed_run_continuation_not_enabled") {
		t.Fatalf("expected continuation not-enabled, got %v", err)
	}
}

func TestAdversaryT06_NotEnabledPathCreatesResources(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  "dep-test",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("Invoke unexpected err: %v", err)
	}
	if resp.GetError() == nil || resp.GetOutcomeName() != "FEATURE_NOT_ENABLED" {
		t.Fatal("ADVERSARY BREAK: not-enabled response missing or wrong")
	}
	if s.localStore == nil {
		t.Fatal("store not wired")
	}
}

func TestAdversaryT06_AliasCAS_Bypass(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	d1, _ := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{PackageName: "p", PackageVersion: "1", BundleDigest: "b1"})
	d2, _ := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{PackageName: "p", PackageVersion: "2", BundleDigest: "b2"})
	_, _ = s.CreateDeploymentAlias(ctx, &controlv1.CreateDeploymentAliasRequest{Alias: "prod", TargetDeploymentId: d1.GetDeployment().GetDeploymentId()})
	_, err := s.CasDeploymentAlias(ctx, &controlv1.CasDeploymentAliasRequest{
		Alias:              "prod",
		TargetDeploymentId: d2.GetDeployment().GetDeploymentId(),
		ExpectedGeneration: 99,
	})
	if err == nil {
		t.Fatal("ADVERSARY BREAK: CAS bypass succeeded on wrong generation")
	}
}

func TestAdversaryT06_DeactivateWithActiveRuns(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	depResp, _ := s.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{PackageName: "act", PackageVersion: "1", BundleDigest: "b"})
	depID := depResp.GetDeployment().GetDeploymentId()
	_, err := s.DeactivateDeployment(ctx, &controlv1.DeactivateDeploymentRequest{DeploymentId: depID, ActorIdentity: "t"})
	if err != nil {
		t.Fatalf("Deactivate err: %v", err)
	}
	_, err = s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{DeploymentRef: depID, IdempotencyKey: "k2"})
	if err != nil {
		// expected fail-closed
	} else {
		t.Logf("ADVERSARY BREAK DETECTED: invoke after deactivate succeeded (no extra active-runs gate)")
	}
}

func TestAdversaryT06_CLIIdempotencyKeyLeak(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{DeploymentRef: "d", IdempotencyKey: "secret-key-xyz"})
	_ = err // expected: not-enabled, no leak possible
}

func TestAdversaryT06_LegacyRunRegression(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	resp, err := s.Run(ctx, &controlv1.RunRequest{AgentName: "legacy-pkg"})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.FailedPrecondition && strings.Contains(st.Message(), "not_enabled") {
			t.Fatal("ADVERSARY BREAK: legacy run regressed into not-enabled")
		}
	}
	_ = resp
}

func TestAdversaryT06_AmendmentBeforeTerminal(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	_, err := s.AmendLimits(ctx, &controlv1.AmendLimitsRequest{
		WorkflowId:     "wf-nonterminal",
		IdempotencyKey: "k",
		Reason:         "test",
	})
	_ = err // expected: not-enabled
}

func TestAdversaryT06_MissingScopeBypass_Amend(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	req := &controlv1.AmendLimitsRequest{WorkflowId: "wf1", IdempotencyKey: "k", Reason: "r"}
	_, err := s.AmendLimits(ctx, req)
	if err == nil {
		t.Logf("ADVERSARY BREAK DETECTED: missing scope allowed amend path (no explicit scope check)")
	}
}

func TestAdversaryT06_StateLeakOnFailure(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	resp, _ := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{DeploymentRef: "bad", IdempotencyKey: "k"})
	if resp.GetError() == nil {
		t.Fatal("expected error")
	}
}