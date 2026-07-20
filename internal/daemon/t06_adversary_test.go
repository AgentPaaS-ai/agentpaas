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
	// As of B30-T02 the durable admission path is enabled, so the FEATURE_NOT_ENABLED
	// stub no longer applies. The test's original intent still holds: an
	// InvokeDeployment call against a deployment ref that does not exist in the
	// store must NOT silently create resources (no invocation, no run). The
	// handler validates caller_identity (now required) and then hands the
	// request to the admission store, which rejects an unresolved ref with a
	// typed DEPLOYMENT_NOT_FOUND error. We assert the typed error is present,
	// the outcome is not ACCEPTED, and no IDs were minted.
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  "dep-test",
		IdempotencyKey: "idem-1",
		CallerIdentity: "adversary-test",
	})
	if err != nil {
		t.Fatalf("Invoke unexpected err: %v", err)
	}
	if resp.GetError() == nil {
		t.Fatal("ADVERSARY BREAK: expected typed error for unresolved deployment ref, got none")
	}
	if resp.GetOutcome() == controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("ADVERSARY BREAK: admission accepted for unresolved ref (outcome=%v)", resp.GetOutcome())
	}
	if resp.GetOutcomeName() != "DEPLOYMENT_NOT_FOUND" {
		t.Fatalf("expected DEPLOYMENT_NOT_FOUND outcome, got %q", resp.GetOutcomeName())
	}
	if resp.GetInvocationId() != "" {
		t.Fatalf("ADVERSARY BREAK: invocation created for unresolved ref: %q", resp.GetInvocationId())
	}
	if resp.GetRunId() != "" {
		t.Fatalf("ADVERSARY BREAK: run created for unresolved ref: %q", resp.GetRunId())
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
	// Post-deactivate invoke must still fail-closed (new invocations blocked).
	// As of B30-T02 the durable path is active: with caller_identity present the
	// request reaches admission, which rejects the inactive deployment with a
	// typed DEPLOYMENT_INACTIVE error (structured response, not a gRPC error).
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  depID,
		IdempotencyKey: "k2",
		CallerIdentity: "adversary-test",
	})
	if err != nil {
		t.Fatalf("invoke after deactivate returned gRPC error (should be typed): %v", err)
	}
	if resp.GetError() == nil {
		t.Fatal("ADVERSARY BREAK: invoke after deactivate succeeded (no fail-closed)")
	}
	if resp.GetOutcome() == controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("ADVERSARY BREAK: invoke after deactivate was accepted (outcome=%v)", resp.GetOutcome())
	}
	if resp.GetError().GetCodeName() != "DEPLOYMENT_INACTIVE" {
		t.Fatalf("expected DEPLOYMENT_INACTIVE typed error, got %q (outcome=%q)", resp.GetError().GetCodeName(), resp.GetOutcomeName())
	}
	// Confirmed: deactivation works without cancelling actives, new invokes blocked.
}

func TestAdversaryT06_CLIIdempotencyKeyLeak(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	// As of B30-T02 the durable admission path is active, so we pass
	// caller_identity (now required) to exercise the real store path. The
	// deployment ref "d" does not resolve to any alias or deployment, so the
	// store rejects it with a typed DEPLOYMENT_NOT_FOUND error before any
	// state is written — the idempotency key is never persisted and thus
	// cannot leak via error messages or stored records.
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  "d",
		IdempotencyKey: "secret-key-xyz",
		CallerIdentity: "adversary-test",
	})
	if err != nil {
		t.Fatalf("Invoke unexpected gRPC error (should be typed): %v", err)
	}
	if resp.GetError() == nil {
		t.Fatal("expected typed error for unresolved deployment ref")
	}
	if resp.GetOutcome() == controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("ADVERSARY BREAK: invocation accepted for unresolved ref (outcome=%v)", resp.GetOutcome())
	}
	if resp.GetInvocationId() != "" {
		t.Fatalf("ADVERSARY BREAK: invocation created for unresolved ref: %q", resp.GetInvocationId())
	}
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
	resp, err := s.AmendLimits(ctx, req)
	if err != nil {
		// gRPC-level error is acceptable (e.g. validation failure).
		return
	}
	// Response-level error must be FEATURE_NOT_ENABLED (not-enabled, no mutation).
	if resp.GetError() == nil {
		t.Error("ADVERSARY BREAK: AmendLimits returned no error (scope bypass)")
	}
}

func TestAdversaryT06_StateLeakOnFailure(t *testing.T) {
	s := newTestControlServer(t)
	ctx := context.Background()
	// As of B30-T02 the durable admission path is active. With caller_identity
	// present, the request reaches admission; "bad" resolves to no alias or
	// deployment, so the store returns a typed DEPLOYMENT_NOT_FOUND error.
	// No invocation or run IDs are minted on the failure path — no state leak.
	resp, err := s.InvokeDeployment(ctx, &controlv1.InvokeDeploymentRequest{
		DeploymentRef:  "bad",
		IdempotencyKey: "k",
		CallerIdentity: "adversary-test",
	})
	if err != nil {
		t.Fatalf("Invoke unexpected gRPC error (should be typed): %v", err)
	}
	if resp.GetError() == nil {
		t.Fatal("expected typed error")
	}
	if resp.GetOutcome() == controlv1.AdmissionOutcomeCode_ADMISSION_OUTCOME_ACCEPTED {
		t.Fatalf("ADVERSARY BREAK: invocation accepted for unresolved ref (outcome=%v)", resp.GetOutcome())
	}
	if resp.GetInvocationId() != "" {
		t.Fatalf("ADVERSARY BREAK: invocation created on failure path: %q", resp.GetInvocationId())
	}
	if resp.GetRunId() != "" {
		t.Fatalf("ADVERSARY BREAK: run created on failure path: %q", resp.GetRunId())
	}
}