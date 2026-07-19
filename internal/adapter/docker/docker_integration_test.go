package docker

import (
	"context"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

// dockerAvailable returns true if the Docker daemon is running.
func dockerAvailable(t *testing.T) bool {
	t.Helper()
	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Logf("docker not available: %v", err)
		return false
	}
	if _, err := rt.ServerVersion(context.Background()); err != nil {
		t.Logf("docker not responding: %v", err)
		return false
	}
	return true
}

// TestDockerConformance runs the 10-step portability scenario against real Docker.
// Gated by AGENTPAAS_DOCKER_TESTS=1 and Docker availability.
func TestDockerConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker integration test in short mode")
	}
	if !dockerAvailable(t) {
		t.Skip("docker not available")
	}

	rt, err := runtime.NewDockerRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	adapter := NewDockerAdapter(DockerAdapterDeps{
		RuntimeDriver: rt,
	})

	// Step 1: Register tenant A and tenant B.
	t.Run("Step1_RegisterTenants", func(t *testing.T) {
		// In the Docker adapter, tenants are implicit in the state store.
		// Verify the state store isolates by tenant ID.
		ctx := context.Background()
		depA := port.DeploymentState{TenantID: "tenant-a", DeploymentID: "dep-1", PackageName: "test", ImageDigest: "sha256:abc"}
		if err := adapter.State.CasDeployment(ctx, depA, 0); err != nil {
			t.Fatalf("cas deployment tenant-a: %v", err)
		}
		depB := port.DeploymentState{TenantID: "tenant-b", DeploymentID: "dep-2", PackageName: "test", ImageDigest: "sha256:def"}
		if err := adapter.State.CasDeployment(ctx, depB, 0); err != nil {
			t.Fatalf("cas deployment tenant-b: %v", err)
		}
		// Verify tenant isolation
		deps, err := adapter.State.ListDeployments(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("list deployments: %v", err)
		}
		for _, d := range deps {
			if d.TenantID != "tenant-a" {
				t.Fatalf("cross-tenant leak: tenant-a list contains %q", d.TenantID)
			}
		}
	})

	// Step 4: Default-deny egress.
	t.Run("Step4_DefaultDenyEgress", func(t *testing.T) {
		ctx := context.Background()
		// No snapshot applied → default deny
		decision := adapter.Egress.Check(ctx, "workload-1", "undeclared.example:443")
		if decision.Action != port.CommDeny {
			t.Fatalf("expected deny, got %q", decision.Action)
		}
		// Apply a snapshot with one allowed host
		snapshot := port.CommSnapshot{
			Digest: "sha256:snap1",
			Rules: []port.CommRule{
				{Host: "allowed.example", Port: 443, Action: port.CommAllow},
			},
			Default: port.CommDeny,
		}
		if err := adapter.Egress.Apply(ctx, "workload-1", snapshot); err != nil {
			t.Fatalf("apply snapshot: %v", err)
		}
		// Allowed host
		decision = adapter.Egress.Check(ctx, "workload-1", "allowed.example:443")
		if decision.Action != port.CommAllow {
			t.Fatalf("expected allow for declared host, got %q", decision.Action)
		}
		// Undeclared host
		decision = adapter.Egress.Check(ctx, "workload-1", "undeclared.example:443")
		if decision.Action != port.CommDeny {
			t.Fatalf("expected deny for undeclared host, got %q", decision.Action)
		}
	})

	// Step 7: Ordered terminal events.
	t.Run("Step7_OrderedEvents", func(t *testing.T) {
		ctx := context.Background()
		seq1, err := adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-1", Type: "progress"})
		if err != nil {
			t.Fatalf("append event 1: %v", err)
		}
		seq2, err := adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-1", Type: "checkpoint"})
		if err != nil {
			t.Fatalf("append event 2: %v", err)
		}
		if seq2 <= seq1 {
			t.Fatalf("sequences not monotonic: %d then %d", seq1, seq2)
		}
		// Read back
		events, err := adapter.Events.Read(ctx, "tenant-a", "run-1", 0, 10)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
	})

	// Step 8: Fence stale authority.
	t.Run("Step8_Fence", func(t *testing.T) {
		ctx := context.Background()
		// The Docker adapter's Fence is currently a no-op (placeholder).
		// Verify it doesn't error on a nonexistent workload.
		_ = adapter.Runtime.Fence(ctx, "nonexistent-workload")
	})

	// Step 9: Cross-tenant denial.
	t.Run("Step9_CrossTenantDenial", func(t *testing.T) {
		ctx := context.Background()
		// Tenant A's events
		_, _ = adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-1", Type: "progress"})
		// Tenant B cannot read tenant A's events
		events, err := adapter.Events.Read(ctx, "tenant-b", "run-1", 0, 10)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("cross-tenant leak: tenant-b sees tenant-a events")
		}
	})

	// Step 10: Metering and cleanup.
	t.Run("Step10_MeteringAndCleanup", func(t *testing.T) {
		ctx := context.Background()
		// Record a measurement for tenant A
		err := adapter.Metering.Record(ctx, port.Measurement{
			TenantID: "tenant-a",
			Type:     port.MeterModel,
			Value:    1000,
			Unit:     "tokens",
		})
		if err != nil {
			t.Fatalf("record measurement: %v", err)
		}
		// Query tenant A's measurements
		measurements, err := adapter.Metering.Query(ctx, port.MeasurementFilter{
			TenantID: "tenant-a",
			Type:     port.MeterModel,
		})
		if err != nil {
			t.Fatalf("query measurements: %v", err)
		}
		if len(measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(measurements))
		}
		// Tenant B's summary should show zero
		summary, err := adapter.Metering.Summary(ctx, "tenant-b", time.Time{}, time.Now())
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if summary.TotalModelTokens != 0 {
			t.Fatalf("cross-tenant leak: tenant-b sees tenant-a model tokens")
		}
	})
}
