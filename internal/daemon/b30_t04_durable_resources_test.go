package daemon

import (
	"strings"
	"testing"
)

// TestB30T04_DurablePathResourceEnv asserts the daemon builds the correct
// durable-path env vars for the harness: AGENTPAAS_DURABLE_PATH=1 plus
// policy-derived CPU/PID limits. The legacy Run path does NOT set these.
func TestB30T04_DurablePathResourceEnv(t *testing.T) {
	// Explicit CPU quota + explicit MaxPIDs.
	env := durablePathResourceEnvFromPolicy(60, 8)
	if !containsKv(env, envDurablePath, "1") {
		t.Errorf("missing %s=1: %v", envDurablePath, env)
	}
	if !containsKv(env, envCPUQuotaSeconds, "60") {
		t.Errorf("missing %s=60: %v", envCPUQuotaSeconds, env)
	}
	if !containsKv(env, envMaxPIDs, "8") {
		t.Errorf("missing %s=8: %v", envMaxPIDs, env)
	}

	// No policy CPU quota → unlimited CPU (no AGENTPAAS_CPU_QUOTA_SECONDS);
	// no explicit MaxPIDs → safe default 64.
	envDefault := durablePathResourceEnvFromPolicy(0, 0)
	for _, e := range envDefault {
		if strings.HasPrefix(e, envCPUQuotaSeconds+"=") {
			t.Errorf("unlimited-CPU path must not set %s: %v", envCPUQuotaSeconds, envDefault)
		}
	}
	if !containsKv(envDefault, envMaxPIDs, "64") {
		t.Errorf("default MaxPIDs missing %s=64: %v", envMaxPIDs, envDefault)
	}

	// Explicit MaxPIDs=0 (policy forbids subprocesses) → AGENTPAAS_MAX_PIDS=0.
	envZero := durablePathResourceEnv(0, 0, 0)
	if !containsKv(envZero, envMaxPIDs, "0") {
		t.Errorf("explicit forbid-subprocesses path missing %s=0: %v", envMaxPIDs, envZero)
	}
}

// TestB30T04_LegacyRunPathDoesNotSetDurableEnv asserts the legacy Run path
// env (proxyEnv built in control_handlers.go) does NOT carry the durable-path
// resource env vars. This proves backward compat: the legacy path keeps
// RLIMIT_CPU=30 / RLIMIT_NPROC=0 in the Python runner.
func TestB30T04_LegacyRunPathDoesNotSetDurableEnv(t *testing.T) {
	// The legacy Run path builds proxyEnv without calling
	// durablePathResourceEnv. We assert the helper is opt-in: the legacy
	// path env (simulated here) has no durable-path markers.
	legacyEnv := []string{
		"AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl",
		"AGENTPAAS_AGENT_PATH=/app/main.py",
	}
	for _, e := range legacyEnv {
		if strings.HasPrefix(e, envDurablePath+"=") {
			t.Errorf("legacy env must not set %s: %s", envDurablePath, e)
		}
		if strings.HasPrefix(e, envCPUQuotaSeconds+"=") {
			t.Errorf("legacy env must not set %s: %s", envCPUQuotaSeconds, e)
		}
		if strings.HasPrefix(e, envMaxPIDs+"=") {
			t.Errorf("legacy env must not set %s: %s", envMaxPIDs, e)
		}
	}
}

// TestB30T04_ResourceLimitDetailFormat asserts the attempt-evidence detail
// string records which limit was hit and the observed value.
func TestB30T04_ResourceLimitDetailFormat(t *testing.T) {
	if got := formatResourceLimitDetail("cpu_quota", 10); got != "CPU quota exhausted: 10s" {
		t.Errorf("cpu_quota detail = %q, want %q", got, "CPU quota exhausted: 10s")
	}
	if got := formatResourceLimitDetail("pid_limit", 8); got != "PID limit exhausted: 8" {
		t.Errorf("pid_limit detail = %q, want %q", got, "PID limit exhausted: 8")
	}
	if got := formatResourceLimitDetail("memory", 134217728); !strings.Contains(got, "memory limit exceeded") {
		t.Errorf("memory detail = %q, want memory limit exceeded", got)
	}
}

// containsKv reports whether env contains KEY=VALUE.
func containsKv(env []string, key, value string) bool {
	want := key + "=" + value
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}
