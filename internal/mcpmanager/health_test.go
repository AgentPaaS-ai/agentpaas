package mcpmanager

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// HealthSummary tests
// ---------------------------------------------------------------------------

func TestHealthSummary_ReadyService(t *testing.T) {
	reg := newTestRegistry()

	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", "cap-abc", []string{"tool_a"})
	inst.RunID = "svc-run-1"
	inst.AttemptID = "svc-att-1"
	inst.LeaseID = "svc-lease-1"
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)
	inst.Generation = 3
	reg.instances["wf-1/svc-1"] = inst

	summary := reg.HealthSummary("wf-1", "svc-1")
	if summary == nil {
		t.Fatal("HealthSummary returned nil")
	}
	if len(summary.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(summary.Services))
	}

	s := summary.Services[0]
	if s.State != StateReady {
		t.Errorf("State = %q, want READY", s.State)
	}
	if s.Readiness != ReadinessReady {
		t.Errorf("Readiness = %q, want ready", s.Readiness)
	}
	if s.Generation != 3 {
		t.Errorf("Generation = %d, want 3", s.Generation)
	}
	if s.LeaseDeadline.IsZero() {
		t.Error("LeaseDeadline should not be zero")
	}
	if s.ActiveCalls != 0 {
		t.Errorf("ActiveCalls = %d, want 0", s.ActiveCalls)
	}
}

func TestHealthSummary_MultipleServices(t *testing.T) {
	reg := newTestRegistry()

	inst1 := TestServiceInstance("wf-1", "svc-a", StateReady, "http://a:8080", "cap-a", []string{"t1"})
	inst1.Generation = 1
	reg.instances["wf-1/svc-a"] = inst1

	inst2 := TestServiceInstance("wf-1", "svc-b", StateFailed, "http://b:8080", "cap-b", []string{"t2"})
	inst2.Generation = 2
	inst2.LastError = "something went wrong"
	reg.instances["wf-1/svc-b"] = inst2

	summary := reg.HealthSummary("wf-1", "")
	if len(summary.Services) != 2 {
		t.Fatalf("got %d services, want 2", len(summary.Services))
	}

	// Should be sorted by ServiceBindingID.
	if summary.Services[0].ServiceBindingID != "svc-a" {
		t.Errorf("services[0].ServiceBindingID = %q, want svc-a", summary.Services[0].ServiceBindingID)
	}
	if summary.Services[1].ServiceBindingID != "svc-b" {
		t.Errorf("services[1].ServiceBindingID = %q, want svc-b", summary.Services[1].ServiceBindingID)
	}
	if summary.Services[0].Readiness != ReadinessReady {
		t.Errorf("svc-a readiness = %q, want ready", summary.Services[0].Readiness)
	}
	if summary.Services[1].Readiness != ReadinessUnhealthy {
		t.Errorf("svc-b readiness = %q, want unhealthy", summary.Services[1].Readiness)
	}
}

func TestHealthSummary_FilterByService(t *testing.T) {
	reg := newTestRegistry()

	inst1 := TestServiceInstance("wf-1", "svc-a", StateReady, "http://a:8080", "cap-a", []string{"t1"})
	inst2 := TestServiceInstance("wf-1", "svc-b", StateReady, "http://b:8080", "cap-b", []string{"t2"})
	reg.instances["wf-1/svc-a"] = inst1
	reg.instances["wf-1/svc-b"] = inst2

	summary := reg.HealthSummary("wf-1", "svc-a")
	if len(summary.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(summary.Services))
	}
	if summary.Services[0].ServiceBindingID != "svc-a" {
		t.Errorf("ServiceBindingID = %q, want svc-a", summary.Services[0].ServiceBindingID)
	}
}

func TestHealthSummary_EmptyWorkflow(t *testing.T) {
	reg := newTestRegistry()
	summary := reg.HealthSummary("nonexistent", "")
	if summary == nil {
		t.Fatal("HealthSummary returned nil")
	}
	if len(summary.Services) != 0 {
		t.Errorf("got %d services, want 0", len(summary.Services))
	}
}

func TestHealthSummary_RecentFailures(t *testing.T) {
	reg := newTestRegistry()

	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", "cap-abc", []string{"tool_a"})
	reg.instances["wf-1/svc-1"] = inst

	// Record some failures.
	reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeTimeout, "call timed out", "tool_a")
	reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeOverloaded, "too many requests", "tool_a")
	reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeLeaseExpired, "lease expired", "tool_b")

	summary := reg.HealthSummary("wf-1", "svc-1")
	if len(summary.Services) != 1 {
		t.Fatalf("got %d services, want 1", len(summary.Services))
	}

	failures := summary.Services[0].RecentFailures
	if len(failures) != 3 {
		t.Fatalf("got %d failures, want 3", len(failures))
	}
	if failures[0].StatusCode != ErrCodeTimeout {
		t.Errorf("failures[0].StatusCode = %q, want %s", failures[0].StatusCode, ErrCodeTimeout)
	}
	if failures[1].StatusCode != ErrCodeOverloaded {
		t.Errorf("failures[1].StatusCode = %q, want %s", failures[1].StatusCode, ErrCodeOverloaded)
	}
	if failures[2].Tool != "tool_b" {
		t.Errorf("failures[2].Tool = %q, want tool_b", failures[2].Tool)
	}
}

func TestHealthSummary_RecentFailuresBoundedCapped(t *testing.T) {
	reg := newTestRegistry()

	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", "cap-abc", []string{"tool_a"})
	reg.instances["wf-1/svc-1"] = inst

	// Record more than MaxRecentFailures (16) failures.
	const total = MaxRecentFailures + 10
	for i := 0; i < total; i++ {
		reg.RecordHealthFailure("wf-1", "svc-1", ErrCodeTimeout, "failure", "tool_a")
	}

	summary := reg.HealthSummary("wf-1", "svc-1")
	failures := summary.Services[0].RecentFailures
	if len(failures) > MaxRecentFailures {
		t.Errorf("got %d failures, want at most %d", len(failures), MaxRecentFailures)
	}
}

// ---------------------------------------------------------------------------
// CleanupServiceResources tests
// ---------------------------------------------------------------------------

func TestCleanupServiceResources_Idempotent(t *testing.T) {
	// Test with nil driver (no real containers).
	reg := NewServiceRegistry(nil, nil, nil)

	inst := TestServiceInstance("wf-1", "svc-1", StateReady, "http://svc:8080", "cap-abc", []string{"tool_a"})
	inst.ContainerID = "container-123"
	inst.Generation = 1
	reg.instances["wf-1/svc-1"] = inst

	// First cleanup.
	cleaned, err := reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	if err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	// With nil driver, no resources were cleaned but state transitions to STOPPED.
	_ = cleaned // may be false with nil driver

	// Verify state is STOPPED.
	got, err := reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get after cleanup: %v", err)
	}
	if got.State != StateStopped {
		t.Errorf("State = %q, want STOPPED", got.State)
	}
	if got.ContainerID != "" {
		t.Errorf("ContainerID = %q, want empty", got.ContainerID)
	}
	if got.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty", got.Endpoint)
	}
	if got.Capability != "" {
		t.Errorf("Capability = %q, want empty", got.Capability)
	}

	// Second cleanup — idempotent.
	cleaned, err = reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	if err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if cleaned {
		t.Error("second cleanup should be idempotent (no-op)")
	}

	// State should still be STOPPED.
	got, err = reg.Get("wf-1", "svc-1")
	if err != nil {
		t.Fatalf("Get after second cleanup: %v", err)
	}
	if got.State != StateStopped {
		t.Errorf("State = %q, want STOPPED", got.State)
	}
}

func TestCleanupServiceResources_NotFound(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)

	cleaned, err := reg.CleanupServiceResources(context.Background(), "wf-1", "nonexistent")
	if err != nil {
		t.Fatalf("cleanup nonexistent: %v", err)
	}
	if cleaned {
		t.Error("cleanup of nonexistent service should not clean anything")
	}
}

func TestCleanupServiceResources_AlreadyStopped(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)

	inst := TestServiceInstance("wf-1", "svc-1", StateStopped, "", "", []string{"tool_a"})
	inst.ContainerID = "" // already cleared
	reg.instances["wf-1/svc-1"] = inst

	cleaned, err := reg.CleanupServiceResources(context.Background(), "wf-1", "svc-1")
	if err != nil {
		t.Fatalf("cleanup stopped: %v", err)
	}
	if cleaned {
		t.Error("cleanup of already-stopped service should be no-op")
	}
}

// ---------------------------------------------------------------------------
// DiscoverOrphans tests
// ---------------------------------------------------------------------------

func TestDiscoverOrphans_NoDriver(t *testing.T) {
	reg := NewServiceRegistry(nil, nil, nil)
	orphans, err := reg.DiscoverOrphans(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("DiscoverOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("got %d orphans with nil driver, want 0", len(orphans))
	}
}

// ---------------------------------------------------------------------------
// healthState ring buffer tests
// ---------------------------------------------------------------------------

func TestHealthState_RingBuffer(t *testing.T) {
	hs := newHealthState()

	// Fill the ring buffer exactly.
	for i := 0; i < MaxRecentFailures; i++ {
		hs.recordFailure(HealthFailureItem{
			StatusCode: "code",
			Reason:     "reason",
			Timestamp:  time.Now().UTC(),
		})
	}
	failures := hs.getFailures()
	if len(failures) != MaxRecentFailures {
		t.Fatalf("got %d failures, want %d", len(failures), MaxRecentFailures)
	}

	// Add one more — should overwrite oldest.
	hs.recordFailure(HealthFailureItem{
		StatusCode: "newest",
		Reason:     "newest",
		Timestamp:  time.Now().UTC(),
	})

	failures = hs.getFailures()
	if len(failures) != MaxRecentFailures {
		t.Fatalf("got %d failures after overflow, want %d", len(failures), MaxRecentFailures)
	}

	// The last entry should be "newest".
	last := failures[len(failures)-1]
	if last.StatusCode != "newest" {
		t.Errorf("last.StatusCode = %q, want newest", last.StatusCode)
	}
}

// newTestRegistry creates a ServiceRegistry with empty instances and healthStates.
func newTestRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		instances:    make(map[string]*ServiceInstance),
		healthStates: make(map[string]*healthState),
	}
}