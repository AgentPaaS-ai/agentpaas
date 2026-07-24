package mcpmanager

import (
	"context"
	"errors"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// testBindingWithDigest creates a ServiceBinding with an explicit digest.
func testBindingWithDigest(serviceID, packageName, packageVersion, digest string) pack.ServiceBinding {
	return pack.ServiceBinding{
		ServiceID:      serviceID,
		PackageName:    packageName,
		PackageVersion: packageVersion,
		BundleDigest:   digest,
		AllowedTools:   []string{"tool_a", "tool_b"},
	}
}

// ---------------------------------------------------------------------------
// Tests: EnsureServices
// ---------------------------------------------------------------------------

func TestEnsureServices_Empty(t *testing.T) {
	reg, _ := newFakeRegistry()
	err := reg.EnsureServices(context.Background(), "wf-1", nil)
	if err != nil {
		t.Fatalf("EnsureServices() with nil services should return nil, got %v", err)
	}
	err = reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{})
	if err != nil {
		t.Fatalf("EnsureServices() with empty services should return nil, got %v", err)
	}
}

func TestEnsureServices_DeclareAndStartOne(t *testing.T) {
	reg, driver := newFakeRegistry()

	binding := testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123")
	err := reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{binding})
	if err != nil {
		t.Fatalf("EnsureServices() error = %v", err)
	}

	// Verify instance is READY.
	inst, getErr := reg.Get("wf-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if inst.State != StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
	if inst.Capability == "" {
		t.Fatal("Capability should not be empty")
	}
	if inst.NetworkAlias == "" {
		t.Fatal("NetworkAlias should not be empty")
	}

	// Verify a container was created.
	if driver.createdCount() == 0 {
		t.Fatal("expected at least one container to be created")
	}
}

func TestEnsureServices_IdempotentSecondCall(t *testing.T) {
	reg, driver := newFakeRegistry()

	binding := testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123")
	services := []pack.ServiceBinding{binding}

	// First call.
	err := reg.EnsureServices(context.Background(), "wf-1", services)
	if err != nil {
		t.Fatalf("first EnsureServices() error = %v", err)
	}

	countAfterFirst := driver.createdCount()

	// Second call — should be idempotent.
	err = reg.EnsureServices(context.Background(), "wf-1", services)
	if err != nil {
		t.Fatalf("second EnsureServices() error = %v", err)
	}

	// Should not create a second container.
	if driver.createdCount() != countAfterFirst {
		t.Fatalf("idempotent call created extra containers: %d -> %d", countAfterFirst, driver.createdCount())
	}

	// Instance should still be READY.
	inst, getErr := reg.Get("wf-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if inst.State != StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
}

func TestEnsureServices_StartFailureCleansPrior(t *testing.T) {
	reg, _ := newFakeRegistry()

	binding1 := testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123")
	binding2 := testBindingWithDigest("accounts", "accounts-tools", "1.0.0", "sha256:def456")

	// Pre-declare both so EnsureServices doesn't need to declare them.
	_, err := reg.Declare("wf-1", binding1, "sha256:abc123", binding1.AllowedTools)
	if err != nil {
		t.Fatalf("Declare binding1: %v", err)
	}
	_, err = reg.Declare("wf-1", binding2, "sha256:def456", binding2.AllowedTools)
	if err != nil {
		t.Fatalf("Declare binding2: %v", err)
	}

	// Use a readiness probe that fails for the second service (accounts)
	// but not the first (feedback). This way binding1 starts successfully,
	// binding2 fails, and the rollback cleans up binding1.
	reg.mu.Lock()
	reg.readinessProbe = &selectiveReadinessProbe{
		failFor: map[string]bool{"accounts": true},
	}
	reg.mu.Unlock()

	err = reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{binding1, binding2})
	if err == nil {
		t.Fatal("expected EnsureServices to fail on second binding")
	}

	// binding1 should have been rolled back (stopped) because binding2 failed.
	inst1, getErr := reg.Get("wf-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get() binding1 error = %v", getErr)
	}
	if inst1.State != StateStopped && inst1.State != StateFailed {
		t.Fatalf("binding1 should be stopped or failed after rollback, got %s", inst1.State)
	}

	// binding2 should be in FAILED state (readiness failed).
	inst2, getErr := reg.Get("wf-1", "accounts")
	if getErr != nil {
		t.Fatalf("Get() binding2 error = %v", getErr)
	}
	if inst2.State != StateFailed {
		t.Fatalf("binding2 should be FAILED, got %s", inst2.State)
	}
}

// selectiveReadinessProbe returns ready=true unless the service ID is in the
// failFor set.
type selectiveReadinessProbe struct {
	failFor map[string]bool
}

func (p *selectiveReadinessProbe) Check(_ context.Context, inst *ServiceInstance) (bool, error) {
	if p.failFor[inst.ServiceBindingID] {
		return false, nil
	}
	return true, nil
}

func TestEnsureServices_Unpromoted(t *testing.T) {
	// Use a promotion checker that returns false.
	driver := newFakeRuntimeDriver()
	checker := &fakePromotionChecker{promoted: false}
	probe := &fakeReadinessProbe{ready: true}
	reg := NewServiceRegistry(driver, checker, probe)

	binding := testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123")
	err := reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{binding})
	if err == nil {
		t.Fatal("expected promotion check failure")
	}
	if !errors.Is(err, ErrServiceNotPromoted) {
		t.Fatalf("expected ErrServiceNotPromoted, got %v", err)
	}

	// No containers should be created.
	if driver.createdCount() != 0 {
		t.Fatalf("expected 0 containers created, got %d", driver.createdCount())
	}
}

func TestEnsureServices_AlreadyDeclaredStarts(t *testing.T) {
	reg, _ := newFakeRegistry()

	binding := testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123")
	// Pre-declare the service.
	_, err := reg.Declare("wf-1", binding, "sha256:abc123", binding.AllowedTools)
	if err != nil {
		t.Fatalf("Declare() error = %v", err)
	}

	// EnsureServices should Start the already-declared service.
	err = reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{binding})
	if err != nil {
		t.Fatalf("EnsureServices() error = %v", err)
	}

	inst, getErr := reg.Get("wf-1", "feedback")
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if inst.State != StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
}

func TestEnsureServices_MultipleServices(t *testing.T) {
	reg, driver := newFakeRegistry()

	bindings := []pack.ServiceBinding{
		testBindingWithDigest("feedback", "feedback-tools", "1.0.0", "sha256:abc123"),
		testBindingWithDigest("accounts", "accounts-tools", "1.0.0", "sha256:def456"),
		testBindingWithDigest("inventory", "inventory-tools", "2.0.0", "sha256:ghi789"),
	}

	err := reg.EnsureServices(context.Background(), "wf-1", bindings)
	if err != nil {
		t.Fatalf("EnsureServices() error = %v", err)
	}

	// All three should be READY.
	for _, binding := range bindings {
		inst, getErr := reg.Get("wf-1", binding.ServiceID)
		if getErr != nil {
			t.Fatalf("Get(%q) error = %v", binding.ServiceID, getErr)
		}
		if inst.State != StateReady {
			t.Fatalf("%s: expected READY, got %s", binding.ServiceID, inst.State)
		}
	}

	if driver.createdCount() != 3 {
		t.Fatalf("expected 3 containers, got %d", driver.createdCount())
	}
}

func TestEnsureServices_ServiceWithEmptyAllowedTools(t *testing.T) {
	reg, _ := newFakeRegistry()

	binding := pack.ServiceBinding{
		ServiceID:      "noop",
		PackageName:    "noop-tools",
		PackageVersion: "1.0.0",
		BundleDigest:   "sha256:abc123",
		AllowedTools:   nil, // empty tools is allowed
	}

	err := reg.EnsureServices(context.Background(), "wf-1", []pack.ServiceBinding{binding})
	if err != nil {
		t.Fatalf("EnsureServices() with nil AllowedTools error = %v", err)
	}

	inst, getErr := reg.Get("wf-1", "noop")
	if getErr != nil {
		t.Fatalf("Get() error = %v", getErr)
	}
	if inst.State != StateReady {
		t.Fatalf("expected READY, got %s", inst.State)
	}
}
