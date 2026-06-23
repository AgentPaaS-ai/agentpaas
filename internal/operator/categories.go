package operator

// SchemaVersion is the versioned operator contract schema. All operator
// method responses include this version in their JSON output. Bumping the
// major version indicates a breaking change; minor bumps add
// backward-compatible fields.
const SchemaVersion = "1.0.0"

// ErrorCategory is a stable, versioned enum identifying the category of an
// operator diagnosis. Categories are part of the public contract: adding a
// new category is backward-compatible, but renaming or removing one requires
// a major schema version bump.
type ErrorCategory string

const (
	// ErrDependencyConflict indicates a conflicting or missing dependency
	// declaration in the agent project.
	ErrDependencyConflict ErrorCategory = "dependency_conflict"

	// ErrDockerUnavailable indicates the Docker daemon is not running or
	// unreachable.
	ErrDockerUnavailable ErrorCategory = "docker_unavailable"

	// ErrPolicyDenied indicates a policy rule blocked an agent action.
	ErrPolicyDenied ErrorCategory = "policy_denied"

	// ErrMissingSecretBinding indicates a required credential/secret is not
	// bound to the agent project.
	ErrMissingSecretBinding ErrorCategory = "missing_secret_binding"

	// ErrBudgetExceeded indicates the run exceeded its configured budget.
	ErrBudgetExceeded ErrorCategory = "budget_exceeded"

	// ErrTriggerAuthFailed indicates a trigger (webhook/cron) failed
	// authentication.
	ErrTriggerAuthFailed ErrorCategory = "trigger_auth_failed"

	// ErrHarnessHealthFailed indicates the agent harness reported a health
	// check failure.
	ErrHarnessHealthFailed ErrorCategory = "harness_health_failed"

	// ErrAgentRuntimeException indicates the agent process crashed or
	// returned an unhandled exception.
	ErrAgentRuntimeException ErrorCategory = "agent_runtime_exception"

	// ErrPolicyValidationFailed indicates the policy compiler rejected a
	// policy.yaml change.
	ErrPolicyValidationFailed ErrorCategory = "policy_validation_failed"

	// ErrNetworkSandboxFailed indicates the network sandbox blocked or
	// failed an egress attempt.
	ErrNetworkSandboxFailed ErrorCategory = "network_sandbox_failed"

	// ErrSecretScanFailed indicates the packaging secret scanner found
	// sensitive material in the build context.
	ErrSecretScanFailed ErrorCategory = "secret_scan_failed"

	// ErrPackageVerificationFailed indicates package signature or SBOM
	// verification failed.
	ErrPackageVerificationFailed ErrorCategory = "package_verification_failed"

	// ErrDashboardUnavailable indicates the dashboard/OTel endpoint is not
	// reachable.
	ErrDashboardUnavailable ErrorCategory = "dashboard_unavailable"
)

// AllErrorCategories returns the complete set of defined error categories.
// Used by schema golden tests to verify every category has a fixture and by
// the CLI to validate --json output against the enum.
func AllErrorCategories() []ErrorCategory {
	return []ErrorCategory{
		ErrDependencyConflict,
		ErrDockerUnavailable,
		ErrPolicyDenied,
		ErrMissingSecretBinding,
		ErrBudgetExceeded,
		ErrTriggerAuthFailed,
		ErrHarnessHealthFailed,
		ErrAgentRuntimeException,
		ErrPolicyValidationFailed,
		ErrNetworkSandboxFailed,
		ErrSecretScanFailed,
		ErrPackageVerificationFailed,
		ErrDashboardUnavailable,
	}
}

// IsValidErrorCategory returns true if cat is a defined ErrorCategory.
func IsValidErrorCategory(cat ErrorCategory) bool {
	for _, c := range AllErrorCategories() {
		if c == cat {
			return true
		}
	}
	return false
}

// NextAction is the fixed enum of operator next-action recommendations. An
// agentic tool returns exactly one NextAction per diagnosis. The values are
// part of the versioned contract.
type NextAction string

const (
	// ActionFixCode: the agent source code has a bug; fix the code and rerun.
	ActionFixCode NextAction = "fix_code"

	// ActionInstallDependency: a dependency is missing; install it and rerun.
	ActionInstallDependency NextAction = "install_dependency"

	// ActionStartDocker: Docker is not running; start it and rerun.
	ActionStartDocker NextAction = "start_docker"

	// ActionSetSecret: a required secret is not bound; bind it and rerun.
	ActionSetSecret NextAction = "set_secret"

	// ActionReviewPolicyPatch: a policy rule blocked the run; review the
	// proposed policy patch and confirm or decline.
	ActionReviewPolicyPatch NextAction = "review_policy_patch"

	// ActionReviewHandoff: a local handoff trigger requires review.
	ActionReviewHandoff NextAction = "review_handoff"

	// ActionIncreaseBudget: the run exceeded its budget; increase the budget
	// and rerun.
	ActionIncreaseBudget NextAction = "increase_budget"

	// ActionRerun: the issue is resolved; rerun the agent.
	ActionRerun NextAction = "rerun"

	// ActionExportAudit: export the audit bundle for external review.
	ActionExportAudit NextAction = "export_audit"

	// ActionAskUser: the operator cannot determine a next action; ask the
	// human user.
	ActionAskUser NextAction = "ask_user"
)

// AllNextActions returns the complete set of defined next-action values.
func AllNextActions() []NextAction {
	return []NextAction{
		ActionFixCode,
		ActionInstallDependency,
		ActionStartDocker,
		ActionSetSecret,
		ActionReviewPolicyPatch,
		ActionReviewHandoff,
		ActionIncreaseBudget,
		ActionRerun,
		ActionExportAudit,
		ActionAskUser,
	}
}

// IsValidNextAction returns true if a is a defined NextAction.
func IsValidNextAction(a NextAction) bool {
	for _, v := range AllNextActions() {
		if v == a {
			return true
		}
	}
	return false
}

// RiskLevel classifies the potential impact of a trust-boundary change.
type RiskLevel string

const (
	// RiskLow: the change has minimal security impact (e.g. adding an egress
	// rule for a well-known API with no credentials).
	RiskLow RiskLevel = "low"

	// RiskMedium: the change affects egress scope or credential usage but
	// does not expose secrets or broaden policy silently.
	RiskMedium RiskLevel = "medium"

	// RiskHigh: the change touches trust boundaries — direct leases, exposed
	// listeners, retention purges, or destructive operations.
	RiskHigh RiskLevel = "high"
)
