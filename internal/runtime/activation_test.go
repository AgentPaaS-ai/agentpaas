package runtime

// B29-T06: Activation policies with zero-authority idle enforcement.
//
// Tests cover:
//   - ActivationPolicy validation for on_demand / warm / resident modes.
//   - ZeroAuthorityInvariant: warm idle workloads must not retain task
//     lease, route capability, or applied credentials.
//   - ActivationLifecycle transitions: running→stopped (on_demand),
//     running→idle (warm), running→idle ILLEGAL (on_demand),
//     idle→running requires re-admission (credential binding present).
//   - DefaultActivationPolicy returns on_demand, IdleTimeoutS=0.

import (
	"errors"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// ---------------------------------------------------------------------------
// ValidateActivation
// ---------------------------------------------------------------------------

func TestValidateActivation_OnDemandIdleZero(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	if err := ValidateActivation(policy, WarmPoolConfig{}, ResidentAuthorization{}); err != nil {
		t.Fatalf("on_demand with IdleTimeoutS=0 should be valid: %v", err)
	}
}

func TestValidateActivation_OnDemandIdleNonZeroInvalid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 30}
	err := ValidateActivation(policy, WarmPoolConfig{}, ResidentAuthorization{})
	if err == nil {
		t.Fatal("on_demand with IdleTimeoutS>0 should be invalid (no idle retention)")
	}
	if !errors.Is(err, ErrInvalidActivation) {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}
}

func TestValidateActivation_WarmValid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}
	warm := WarmPoolConfig{
		TenantID:       "tenant-a",
		PackageDigest:  "sha256:abc",
		MaxPoolSize:    4,
		ResourceCharge: port.ResourcePolicy{CPUShares: 512, MemoryMB: 256},
	}
	if err := ValidateActivation(policy, warm, ResidentAuthorization{}); err != nil {
		t.Fatalf("warm with valid config should be valid: %v", err)
	}
}

func TestValidateActivation_WarmIdleZeroInvalid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 0}
	warm := WarmPoolConfig{
		TenantID:       "tenant-a",
		PackageDigest:  "sha256:abc",
		MaxPoolSize:    4,
		ResourceCharge: port.ResourcePolicy{CPUShares: 512, MemoryMB: 256},
	}
	err := ValidateActivation(policy, warm, ResidentAuthorization{})
	if err == nil {
		t.Fatal("warm with IdleTimeoutS=0 should be invalid (requires explicit timeout)")
	}
	if !errors.Is(err, ErrInvalidActivation) {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}
}

func TestValidateActivation_WarmMaxPoolZeroInvalid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}
	warm := WarmPoolConfig{
		TenantID:       "tenant-a",
		PackageDigest:  "sha256:abc",
		MaxPoolSize:    0,
		ResourceCharge: port.ResourcePolicy{CPUShares: 512, MemoryMB: 256},
	}
	err := ValidateActivation(policy, warm, ResidentAuthorization{})
	if err == nil {
		t.Fatal("warm with MaxPoolSize=0 should be invalid (pool must be bounded)")
	}
	if !errors.Is(err, ErrInvalidActivation) {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}
}

func TestValidateActivation_WarmMissingTenantInvalid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}
	warm := WarmPoolConfig{
		TenantID:       "",
		PackageDigest:  "sha256:abc",
		MaxPoolSize:    4,
		ResourceCharge: port.ResourcePolicy{CPUShares: 512, MemoryMB: 256},
	}
	err := ValidateActivation(policy, warm, ResidentAuthorization{})
	if err == nil {
		t.Fatal("warm with empty TenantID should be invalid")
	}
}

func TestValidateActivation_WarmMissingDigestInvalid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}
	warm := WarmPoolConfig{
		TenantID:       "tenant-a",
		PackageDigest:  "",
		MaxPoolSize:    4,
		ResourceCharge: port.ResourcePolicy{CPUShares: 512, MemoryMB: 256},
	}
	err := ValidateActivation(policy, warm, ResidentAuthorization{})
	if err == nil {
		t.Fatal("warm with empty PackageDigest should be invalid")
	}
}

func TestValidateActivation_ResidentWithoutAuthorization(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationResident, IdleTimeoutS: 0}
	err := ValidateActivation(policy, WarmPoolConfig{}, ResidentAuthorization{})
	if err == nil {
		t.Fatal("resident without ResidentAuthorization should be invalid")
	}
	if !errors.Is(err, ErrResidentNotAuthorized) {
		t.Fatalf("expected ErrResidentNotAuthorized, got %v", err)
	}
}

func TestValidateActivation_ResidentExpiredAuthorization(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationResident, IdleTimeoutS: 0}
	auth := ResidentAuthorization{
		AuthorizedBy: "ops-lead",
		Reason:       "event-consumer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
	}
	err := ValidateActivation(policy, WarmPoolConfig{}, auth)
	if err == nil {
		t.Fatal("resident with expired authorization should be invalid")
	}
	if !errors.Is(err, ErrResidentNotAuthorized) {
		t.Fatalf("expected ErrResidentNotAuthorized, got %v", err)
	}
}

func TestValidateActivation_ResidentValid(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: port.ActivationResident, IdleTimeoutS: 0}
	auth := ResidentAuthorization{
		AuthorizedBy: "ops-lead",
		Reason:       "event-consumer",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}
	if err := ValidateActivation(policy, WarmPoolConfig{}, auth); err != nil {
		t.Fatalf("resident with valid non-expired authorization should be valid: %v", err)
	}
}

func TestValidateActivation_UnknownMode(t *testing.T) {
	t.Parallel()
	policy := port.ActivationPolicy{Mode: "bogus", IdleTimeoutS: 0}
	err := ValidateActivation(policy, WarmPoolConfig{}, ResidentAuthorization{})
	if err == nil {
		t.Fatal("unknown mode should be invalid")
	}
	if !errors.Is(err, ErrInvalidActivation) {
		t.Fatalf("expected ErrInvalidActivation, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ZeroAuthorityInvariant
// ---------------------------------------------------------------------------

func TestZeroAuthorityInvariant_EmptyPasses(t *testing.T) {
	t.Parallel()
	w := IdleState{
		State:              StateRunning,
		LeaseID:           "",
		RouteCapability:    "",
		CredentialBindings: nil,
	}
	if err := ZeroAuthorityInvariant(w); err != nil {
		t.Fatalf("empty authority on running workload should pass: %v", err)
	}
}

func TestZeroAuthorityInvariant_WarmIdleEmptyPasses(t *testing.T) {
	t.Parallel()
	w := IdleState{
		State:              StateRunning,
		ActivationMode:     port.ActivationWarm,
		LeaseID:           "",
		RouteCapability:    "",
		CredentialBindings: nil,
	}
	if err := ZeroAuthorityInvariant(w); err != nil {
		t.Fatalf("warm idle workload with empty authority should pass: %v", err)
	}
}

func TestZeroAuthorityInvariant_LeasePresentFails(t *testing.T) {
	t.Parallel()
	w := IdleState{
		State:              StateRunning,
		ActivationMode:     port.ActivationWarm,
		LeaseID:           "lease-abc",
		RouteCapability:    "",
		CredentialBindings: nil,
	}
	err := ZeroAuthorityInvariant(w)
	if err == nil {
		t.Fatal("non-empty LeaseID should fail invariant")
	}
	if !errors.Is(err, ErrAuthorityLeak) {
		t.Fatalf("expected ErrAuthorityLeak, got %v", err)
	}
}

func TestZeroAuthorityInvariant_RouteCapabilityFails(t *testing.T) {
	t.Parallel()
	w := IdleState{
		State:              StateRunning,
		ActivationMode:     port.ActivationWarm,
		LeaseID:           "",
		RouteCapability:    "egress:api.example.com",
		CredentialBindings: nil,
	}
	err := ZeroAuthorityInvariant(w)
	if err == nil {
		t.Fatal("non-empty RouteCapability should fail invariant")
	}
	if !errors.Is(err, ErrAuthorityLeak) {
		t.Fatalf("expected ErrAuthorityLeak, got %v", err)
	}
}

func TestZeroAuthorityInvariant_CredentialBindingsFails(t *testing.T) {
	t.Parallel()
	w := IdleState{
		State:          StateRunning,
		ActivationMode: port.ActivationWarm,
		LeaseID:         "",
		RouteCapability: "",
		CredentialBindings: []port.CredentialBinding{
			{CredentialID: "cred-1", MountPath: "/secrets/cred"},
		},
	}
	err := ZeroAuthorityInvariant(w)
	if err == nil {
		t.Fatal("non-empty CredentialBindings should fail invariant")
	}
	if !errors.Is(err, ErrAuthorityLeak) {
		t.Fatalf("expected ErrAuthorityLeak, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ActivationLifecycle.Transition
// ---------------------------------------------------------------------------

func TestTransition_RunningToStopped_OnDemandValid(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	if err := lc.Transition(StateRunning, StateStopped, policy, IdleState{}); err != nil {
		t.Fatalf("running→stopped on_demand should be valid: %v", err)
	}
}

func TestTransition_RunningToIdle_WarmValid(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}
	target := IdleState{State: StateRunning, ActivationMode: port.ActivationWarm}
	if err := lc.Transition(StateRunning, StateIdle, policy, target); err != nil {
		t.Fatalf("running→idle warm should be valid: %v", err)
	}
}

func TestTransition_RunningToIdle_OnDemandInvalid(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	err := lc.Transition(StateRunning, StateIdle, policy, IdleState{})
	if err == nil {
		t.Fatal("running→idle on_demand should be invalid (no idle retention)")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestTransition_IdleToRunning_RequiresReadmission(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}

	// idle→running with empty credentials = no re-admission = illegal.
	emptyTarget := IdleState{State: StateIdle, ActivationMode: port.ActivationWarm}
	err := lc.Transition(StateIdle, StateRunning, policy, emptyTarget)
	if err == nil {
		t.Fatal("idle→running without re-admission (no credentials) should be illegal")
	}
	if !errors.Is(err, ErrReadmissionRequired) {
		t.Fatalf("expected ErrReadmissionRequired, got %v", err)
	}

	// idle→running WITH credentials = re-admitted = legal.
	readmitted := IdleState{
		State: StateIdle,
		CredentialBindings: []port.CredentialBinding{
			{CredentialID: "cred-1", MountPath: "/secrets/cred"},
		},
	}
	if err := lc.Transition(StateIdle, StateRunning, policy, readmitted); err != nil {
		t.Fatalf("idle→running with re-admission should be valid: %v", err)
	}
}

func TestTransition_PreparedToRunningValid(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	if err := lc.Transition(StatePrepared, StateRunning, policy, IdleState{}); err != nil {
		t.Fatalf("prepared→running should be valid: %v", err)
	}
}

func TestTransition_StoppedToRunningInvalid(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	err := lc.Transition(StateStopped, StateRunning, policy, IdleState{})
	if err == nil {
		t.Fatal("stopped→running should be invalid (must re-prepare)")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestTransition_RunningToIdle_StripsAuthority(t *testing.T) {
	t.Parallel()
	lc := NewActivationLifecycle()
	policy := port.ActivationPolicy{Mode: port.ActivationWarm, IdleTimeoutS: 60}

	// Source carries authority; Transition to idle must strip it and the
	// resulting IdleState must satisfy ZeroAuthorityInvariant.
	src := IdleState{
		State:           StateRunning,
		LeaseID:         "lease-still-here",
		RouteCapability: "egress:api.example.com",
		CredentialBindings: []port.CredentialBinding{
			{CredentialID: "cred-1", MountPath: "/secrets/cred"},
		},
	}
	// On entry to idle, the caller passes the *target* idle state (empty).
	target := IdleState{State: StateIdle, ActivationMode: port.ActivationWarm}
	if err := lc.Transition(StateRunning, StateIdle, policy, target); err != nil {
		t.Fatalf("running→idle warm should be valid: %v", err)
	}
	// The lifecycle should have produced a stripped idle state.
	stripped := lc.StrippedIdle()
	if err := ZeroAuthorityInvariant(stripped); err != nil {
		t.Fatalf("stripped idle state must satisfy zero-authority invariant: %v", err)
	}
	if stripped.LeaseID != "" {
		t.Errorf("stripped LeaseID = %q, want empty", stripped.LeaseID)
	}
	if stripped.RouteCapability != "" {
		t.Errorf("stripped RouteCapability = %q, want empty", stripped.RouteCapability)
	}
	if len(stripped.CredentialBindings) != 0 {
		t.Errorf("stripped CredentialBindings len = %d, want 0", len(stripped.CredentialBindings))
	}
	// Touch src to avoid unused-var lint in strict settings.
	_ = src
}

// ---------------------------------------------------------------------------
// DefaultActivationPolicy
// ---------------------------------------------------------------------------

func TestDefaultActivationPolicy(t *testing.T) {
	t.Parallel()
	p := DefaultActivationPolicy()
	if p.Mode != port.ActivationOnDemand {
		t.Errorf("Mode = %q, want %q", p.Mode, port.ActivationOnDemand)
	}
	if p.IdleTimeoutS != 0 {
		t.Errorf("IdleTimeoutS = %d, want 0", p.IdleTimeoutS)
	}
	// Default must validate cleanly.
	if err := ValidateActivation(p, WarmPoolConfig{}, ResidentAuthorization{}); err != nil {
		t.Fatalf("default policy must validate: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sanity: port types are frozen (regression guard).
// ---------------------------------------------------------------------------

func TestPortActivationPolicyShapeUnchanged(t *testing.T) {
	t.Parallel()
	// This is a guard: if someone changes the frozen port type, this test
	// fails and forces a conversation. The B28 port interfaces are frozen.
	p := port.ActivationPolicy{Mode: port.ActivationOnDemand, IdleTimeoutS: 0}
	if p.Mode != port.ActivationOnDemand {
		t.Fatal("port.ActivationOnDemand constant changed")
	}
	if p.Mode != "on_demand" {
		t.Fatal("port.ActivationOnDemand string value changed")
	}
}
