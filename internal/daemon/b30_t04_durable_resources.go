package daemon

import (
	"fmt"
	"strconv"
)

// B30-T04 durable-path resource env vars. When the daemon launches a
// container for a durable InvokeDeployment run (T05 supervisor claim), it
// sets AGENTPAAS_DURABLE_PATH=1 plus the policy-derived CPU/PID limits so
// the harness Python runner applies policy-derived rlimits instead of the
// legacy fixed constants.
//
// On the legacy v0.2.3 trigger path (controlServer.Run), these env vars are
// NOT set — the runner falls back to RLIMIT_CPU=30 / RLIMIT_NPROC=0 with
// "legacy compat" comments (see internal/harness/python_worker.go).
const (
	envDurablePath       = "AGENTPAAS_DURABLE_PATH"
	envCPUQuotaSeconds   = "AGENTPAAS_CPU_QUOTA_SECONDS"
	envMaxPIDs           = "AGENTPAAS_MAX_PIDS"
	defaultDurableMaxPIDs = 64
)

// durablePathResourceEnv returns the env-var additions the daemon must set on
// a durable-path container launch so the harness applies policy-derived
// resource limits. T05 (supervisor claim / container launch for
// InvokeDeployment) calls this when building the agent container env.
//
// cpuQuotaSeconds == 0 means unlimited CPU (bounded by the container CFS
// quota the runtime driver applies); the runner then does NOT set RLIMIT_CPU.
// maxPIDs == 0 means an explicit policy decision to forbid ALL subprocesses;
// maxPIDs > 0 sets RLIMIT_NPROC to that many processes.
//
// When cpuQuotaSeconds <= 0 and maxPIDs <= 0 (no policy), defaultMaxPIDs is
// applied so the durable path has a safe PID ceiling sufficient for approved
// local tools (git, grep, awk) but not a fork bomb. Callers may pass
// defaultMaxPIDs=0 to override with an explicit forbid-subprocesses policy.
func durablePathResourceEnv(cpuQuotaSeconds int64, maxPIDs, defaultMaxPIDs int) []string {
	env := []string{envDurablePath + "=1"}
	if cpuQuotaSeconds > 0 {
		env = append(env, envCPUQuotaSeconds+"="+strconv.FormatInt(cpuQuotaSeconds, 10))
	}
	if maxPIDs > 0 {
		env = append(env, envMaxPIDs+"="+strconv.Itoa(maxPIDs))
	} else if defaultMaxPIDs > 0 {
		env = append(env, envMaxPIDs+"="+strconv.Itoa(defaultMaxPIDs))
	} else {
		// Explicit policy decision to forbid subprocesses (MaxPIDs=0).
		env = append(env, envMaxPIDs+"=0")
	}
	return env
}

// durablePathResourceEnvFromPolicy is a thin wrapper that materialises the
// durable-path env from a routedrun policy snapshot. cpuQuotaSeconds and
// maxPIDs come from the InvokeJob policy fields (B30-T04).
func durablePathResourceEnvFromPolicy(cpuQuotaSeconds int64, maxPIDs int) []string {
	return durablePathResourceEnv(cpuQuotaSeconds, maxPIDs, defaultDurableMaxPIDs)
}

// formatResourceLimitDetail formats a resource-limit termination detail for
// the attempt evidence (b30-summary.md:414). e.g. "CPU quota exhausted: 10s".
func formatResourceLimitDetail(limit string, observed int64) string {
	switch limit {
	case "cpu_quota":
		return fmt.Sprintf("CPU quota exhausted: %ds", observed)
	case "pid_limit":
		return fmt.Sprintf("PID limit exhausted: %d", observed)
	case "memory":
		return fmt.Sprintf("memory limit exceeded: %d bytes", observed)
	default:
		return fmt.Sprintf("%s limit exhausted", limit)
	}
}
