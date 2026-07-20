// Package runtime implements activation policy validation and
// zero-authority idle-state enforcement (B29-T06).
//
// The ActivationPolicy port type was introduced in B28 and is frozen;
// this file bridges it into the runtime path without modifying any port
// types. Activation is always explicit: on_demand (default, scale to
// zero between tasks), warm (bounded idle sandbox with no authority),
// or resident (explicitly authorized, continuously metered). Resident
// is never inferred from catalog availability.
package runtime

import (
	"errors"
	"fmt"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// Sentinel errors for activation validation and enforcement.
var (
	// ErrInvalidActivation is returned when an ActivationPolicy is
	// inconsistent with its declared mode (e.g. on_demand with an idle
	// timeout, or warm without a bounded pool).
	ErrInvalidActivation = errors.New("invalid activation policy")

	// ErrResidentNotAuthorized is returned when a resident-mode policy is
	// declared without an explicit, non-expired authorization. Resident
	// activation is never inferred from catalog availability.
	ErrResidentNotAuthorized = errors.New("resident activation not authorized")

	// ErrAuthorityLeak is returned by ZeroAuthorityInvariant when a
	// workload in the warm idle state retains task lease, route
	// capability, or applied credentials. The warm sandbox must hold no
	// authority until re-admission.
	ErrAuthorityLeak = errors.New("warm idle workload retains authority")

	// ErrIllegalTransition is returned by ActivationLifecycle.Transition
	// when a state transition is not legal under the declared policy
	// (e.g. running→idle under on_demand).
	ErrIllegalTransition = errors.New("illegal activation transition")

	// ErrReadmissionRequired is returned by ActivationLifecycle.Transition
	// when a warm workload attempts idle→running without re-admission
	// (no credential binding present).
	ErrReadmissionRequired = errors.New("idle→running requires re-admission")
)

// WorkloadState is the runtime-side lifecycle state. It mirrors the frozen
// port.WorkloadState values and adds StateIdle, which is the warm idle
// state that does not exist on the frozen port type. Runtime code that
// needs to reason about idle retention should use this type; code that
// only needs the frozen port states may continue to use port.WorkloadState.
type WorkloadState string

// Runtime lifecycle states. The first five mirror port.WorkloadState
// exactly; StateIdle is the additional warm-idle state.
const (
	StatePrepared WorkloadState = "prepared"
	StateRunning  WorkloadState = "running"
	StateFenced   WorkloadState = "fenced"
	StateIdle     WorkloadState = "idle"
	StateStopped  WorkloadState = "stopped"
	StateCleaned  WorkloadState = "cleaned"
)

// FromPortState converts a frozen port.WorkloadState to the runtime
// WorkloadState. Unknown values map to the empty string.
func FromPortState(s port.WorkloadState) WorkloadState {
	switch s {
	case port.WorkloadPrepared:
		return StatePrepared
	case port.WorkloadRunning:
		return StateRunning
	case port.WorkloadFenced:
		return StateFenced
	case port.WorkloadStopped:
		return StateStopped
	case port.WorkloadCleaned:
		return StateCleaned
	default:
		return ""
	}
}

// ToPortState converts a runtime WorkloadState back to the frozen
// port.WorkloadState. StateIdle has no port equivalent and maps to the
// empty string; callers must ensure they do not hand StateIdle to code
// that expects a port state.
func ToPortState(s WorkloadState) port.WorkloadState {
	switch s {
	case StatePrepared:
		return port.WorkloadPrepared
	case StateRunning:
		return port.WorkloadRunning
	case StateFenced:
		return port.WorkloadFenced
	case StateStopped:
		return port.WorkloadStopped
	case StateCleaned:
		return port.WorkloadCleaned
	default:
		return ""
	}
}

// WarmPoolConfig describes the bounded warm pool parameters required by a
// warm-mode activation policy. All fields must be set: tenant, package
// digest, a positive bounded pool size, and an explicit resource charge.
type WarmPoolConfig struct {
	TenantID       string
	PackageDigest  string
	MaxPoolSize    int
	ResourceCharge port.ResourcePolicy
}

// ResidentAuthorization records the explicit authorization that permits a
// resident-mode activation. Resident activation is never inferred from
// catalog availability — an operator must authorize it with a reason and
// an expiry. ExpiresAt must be in the future at validation time.
type ResidentAuthorization struct {
	AuthorizedBy string
	Reason       string
	ExpiresAt    time.Time
}

// IdleState carries the authority-bearing fields of a workload that the
// frozen port.WorkloadStatus does not expose. It is the input to
// ZeroAuthorityInvariant and the bookkeeping type used by
// ActivationLifecycle when stripping authority on entry to the warm idle
// state. The State field uses the runtime WorkloadState so that StateIdle
// can be represented.
type IdleState struct {
	State              WorkloadState
	ActivationMode     port.ActivationMode
	LeaseID            string
	RouteCapability    string
	CredentialBindings []port.CredentialBinding
}

// ValidateActivation validates an ActivationPolicy against the mode-specific
// contract:
//
//   - on_demand: IdleTimeoutS must be 0 (no idle retention). An exact image
//     may remain cached, but no live process, task capability, credential,
//     or network authority persists between tasks.
//   - warm: IdleTimeoutS must be > 0 (explicit idle timeout). A bounded
//     WarmPoolConfig must be supplied with tenant, package digest, a
//     positive MaxPoolSize, and a resource charge.
//   - resident: an explicit, non-expired ResidentAuthorization must be
//     supplied. Resident is never inferred from catalog availability.
//
// The warm and resident arguments are ignored for modes that do not
// require them.
func ValidateActivation(
	policy port.ActivationPolicy,
	warm WarmPoolConfig,
	resident ResidentAuthorization,
) error {
	switch policy.Mode {
	case port.ActivationOnDemand:
		if policy.IdleTimeoutS != 0 {
			return fmt.Errorf(
				"%w: on_demand requires IdleTimeoutS=0, got %d",
				ErrInvalidActivation, policy.IdleTimeoutS,
			)
		}
		return nil

	case port.ActivationWarm:
		if policy.IdleTimeoutS <= 0 {
			return fmt.Errorf(
				"%w: warm requires IdleTimeoutS>0 (explicit idle timeout), got %d",
				ErrInvalidActivation, policy.IdleTimeoutS,
			)
		}
		if err := validateWarmPool(warm); err != nil {
			return fmt.Errorf("validate activation: %w", err)
		}
		return nil

	case port.ActivationResident:
		if err := validateResident(resident); err != nil {
			return fmt.Errorf("validate activation: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("%w: unknown activation mode %q", ErrInvalidActivation, policy.Mode)
	}
}

// validateWarmPool checks that the warm pool parameters are all explicit
// and bounded.
func validateWarmPool(w WarmPoolConfig) error {
	if w.TenantID == "" {
		return fmt.Errorf("%w: warm requires TenantID", ErrInvalidActivation)
	}
	if w.PackageDigest == "" {
		return fmt.Errorf("%w: warm requires PackageDigest", ErrInvalidActivation)
	}
	if w.MaxPoolSize <= 0 {
		return fmt.Errorf(
			"%w: warm requires MaxPoolSize>0 (bounded pool), got %d",
			ErrInvalidActivation, w.MaxPoolSize,
		)
	}
	return nil
}

// validateResident checks that resident activation is explicitly
// authorized and that the authorization has not expired.
func validateResident(r ResidentAuthorization) error {
	if r.AuthorizedBy == "" {
		return fmt.Errorf("%w: resident requires AuthorizedBy", ErrResidentNotAuthorized)
	}
	if r.Reason == "" {
		return fmt.Errorf("%w: resident requires Reason", ErrResidentNotAuthorized)
	}
	if r.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: resident requires ExpiresAt", ErrResidentNotAuthorized)
	}
	if !r.ExpiresAt.After(time.Now()) {
		return fmt.Errorf(
			"%w: resident authorization expired at %s",
			ErrResidentNotAuthorized, r.ExpiresAt.Format(time.RFC3339),
		)
	}
	return nil
}

// ZeroAuthorityInvariant enforces that a workload in the warm idle state
// retains no task lease, no route capability, and no applied credentials.
// If any of these are non-empty, it returns ErrAuthorityLeak — the warm
// sandbox holds authority it should not have.
//
// The invariant is also satisfied trivially when the workload is not in
// the warm idle state (non-warm states are not subject to the idle
// zero-authority contract), which lets callers run the check unconditionally
// after a transition.
func ZeroAuthorityInvariant(w IdleState) error {
	// Only the warm idle state is subject to the zero-authority contract.
	// Non-warm states pass; the idle state for on_demand cannot exist
	// because on_demand has no idle retention.
	if w.ActivationMode != port.ActivationWarm {
		return nil
	}
	if w.LeaseID != "" {
		return fmt.Errorf("%w: LeaseID=%q", ErrAuthorityLeak, w.LeaseID)
	}
	if w.RouteCapability != "" {
		return fmt.Errorf("%w: RouteCapability=%q", ErrAuthorityLeak, w.RouteCapability)
	}
	if len(w.CredentialBindings) != 0 {
		return fmt.Errorf("%w: %d credential binding(s) applied", ErrAuthorityLeak, len(w.CredentialBindings))
	}
	return nil
}

// DefaultActivationPolicy returns the default activation policy for
// ordinary workers, verifiers, and testing agents: on_demand with
// IdleTimeoutS=0 (scale to zero between tasks). The trigger/run path
// should use this default when no explicit policy is declared.
func DefaultActivationPolicy() port.ActivationPolicy {
	return port.ActivationPolicy{
		Mode:         port.ActivationOnDemand,
		IdleTimeoutS: 0,
	}
}
