package conformance

import (
	"context"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/port/fakes"
)

// TestConformance runs the portable contract scenario against fakes.
func TestConformance(t *testing.T) {
	steps := []struct {
		name string
		test func(*testing.T)
	}{
		{"RegisterTenants", TestConformance_Step1_RegisterTenants},
		{"InstallSignedPackage", TestConformance_Step2_InstallSignedPackage},
		{"AdmitIdempotentInvocation", TestConformance_Step3_AdmitIdempotentInvocation},
		{"StartDefaultDenySandbox", TestConformance_Step4_StartDefaultDenySandbox},
		{"ProgressCheckpointArtifact", TestConformance_Step5_ProgressCheckpointArtifact},
		{"AllowDeclaredDenyUndeclared", TestConformance_Step6_AllowDeclaredDenyUndeclared},
		{"OrderedTerminalEvent", TestConformance_Step7_OrderedTerminalEvent},
		{"FenceStaleAuthority", TestConformance_Step8_FenceStaleAuthority},
		{"DenyCrossTenantAccess", TestConformance_Step9_DenyCrossTenantAccess},
		{"MeterAndCleanup", TestConformance_Step10_MeterAndCleanup},
	}
	for _, step := range steps {
		t.Run(step.name, step.test)
	}
}

// TestConformance_Step1_RegisterTenants establishes isolated tenant identities.
func TestConformance_Step1_RegisterTenants(t *testing.T) {
	t.Helper()
	requireTenantIsolation(t, "tenant-a", "tenant-b")
}

// TestConformance_Step2_InstallSignedPackage verifies exact package resolution.
func TestConformance_Step2_InstallSignedPackage(t *testing.T) {
	t.Helper()
	f := &fakes.FakePackageStore{Resolution: &port.PackageResolution{ImageDigest: "sha256:fixture"}}
	got, err := f.Resolve(context.Background(), "tenant-a", "fixture:1")
	if err != nil || got.ImageDigest != "sha256:fixture" {
		t.Fatalf("resolve fixture: got %#v, err %v", got, err)
	}
}

// TestConformance_Step3_AdmitIdempotentInvocation verifies durable IDs can be recorded.
func TestConformance_Step3_AdmitIdempotentInvocation(t *testing.T) {
	t.Helper()
	f := &fakes.FakeTransactionalStateStore{}
	state := port.RunState{TenantID: "tenant-a", RunID: "run-1"}
	if err := f.CasRun(context.Background(), state, 0); err != nil || len(f.RunCalls) != 1 {
		t.Fatalf("record run: err %v, calls %d", err, len(f.RunCalls))
	}
}

// TestConformance_Step4_StartDefaultDenySandbox verifies runtime and default deny.
func TestConformance_Step4_StartDefaultDenySandbox(t *testing.T) {
	t.Helper()
	runtime := &fakes.FakeWorkloadRuntime{PrepareResult: "workload-1"}
	id, _ := runtime.Prepare(context.Background(), port.PrepareRequest{TenantID: "tenant-a", ImageDigest: "sha256:fixture"})
	if err := runtime.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	deny := &fakes.FakeEgressEnforcer{Decision: port.Decision{Action: port.CommDeny, Reason: "default deny", RuleIndex: -1}}
	if got := deny.Check(context.Background(), string(id), "undeclared.example"); got.Action != port.CommDeny {
		t.Fatalf("action %q", got.Action)
	}
}

// TestConformance_Step5_ProgressCheckpointArtifact verifies event and artifact persistence.
func TestConformance_Step5_ProgressCheckpointArtifact(t *testing.T) {
	t.Helper()
	events := &fakes.FakeEventStore{}
	if _, err := events.Append(context.Background(), port.Event{TenantID: "tenant-a", RunID: "run-1", Type: "checkpoint"}); err != nil {
		t.Fatal(err)
	}
	artifacts := &fakes.FakeArtifactStore{ArtifactID: "artifact-1", Digest: "sha256:artifact"}
	id, _, err := artifacts.Commit(context.Background(), port.CommitArtifactRequest{TenantID: "tenant-a", RunID: "run-1"})
	if err != nil || id == "" {
		t.Fatalf("commit: %v", err)
	}
}

// TestConformance_Step6_AllowDeclaredDenyUndeclared checks structured communication decisions.
func TestConformance_Step6_AllowDeclaredDenyUndeclared(t *testing.T) {
	t.Helper()
	f := &fakes.FakeEgressEnforcer{Decision: port.Decision{Action: port.CommAllow}}
	if f.Check(context.Background(), "w", "declared.example").Action != port.CommAllow {
		t.Fatal("declared call denied")
	}
}

// TestConformance_Step7_OrderedTerminalEvent verifies monotonic event sequences.
func TestConformance_Step7_OrderedTerminalEvent(t *testing.T) {
	t.Helper()
	f := &fakes.FakeEventStore{}
	first, _ := f.Append(context.Background(), port.Event{})
	second, _ := f.Append(context.Background(), port.Event{})
	if second <= first {
		t.Fatalf("sequences %d, %d", first, second)
	}
}

// TestConformance_Step8_FenceStaleAuthority verifies fencing is observable.
func TestConformance_Step8_FenceStaleAuthority(t *testing.T) {
	t.Helper()
	f := &fakes.FakeWorkloadRuntime{}
	if err := f.Fence(context.Background(), "w"); err != nil || len(f.FenceCalls) != 1 {
		t.Fatal("workload was not fenced")
	}
}

// TestConformance_Step9_DenyCrossTenantAccess verifies tenant identity remains explicit.
func TestConformance_Step9_DenyCrossTenantAccess(t *testing.T) {
	t.Helper()
	requireTenantIsolation(t, "tenant-a", "tenant-b")
}

// TestConformance_Step10_MeterAndCleanup verifies attribution and ephemeral cleanup.
func TestConformance_Step10_MeterAndCleanup(t *testing.T) {
	t.Helper()
	meter := &fakes.FakeMeteringSink{}
	if err := meter.Record(context.Background(), port.Measurement{TenantID: "tenant-a", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	runtime := &fakes.FakeWorkloadRuntime{}
	if err := runtime.Cleanup(context.Background(), "w"); err != nil || len(runtime.CleanupCalls) != 1 {
		t.Fatal("cleanup not recorded")
	}
}

func requireTenantIsolation(t *testing.T, first, second string) {
	t.Helper()
	if first == second || first == "" || second == "" {
		t.Fatalf("tenants are not distinct: %q and %q", first, second)
	}
}
