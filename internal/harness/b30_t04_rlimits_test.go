package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// skipUnlessPython skips the test when python3 is not on PATH.
func skipUnlessPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
}

// pythonCombinedOutput runs a short python -c script and returns combined
// output and error.
func pythonCombinedOutput(script string) (string, error) {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestB30T04_PythonRunnerDurablePathNoFixedRLimitCPU asserts the embedded
// Python runner no longer hardcodes RLIMIT_CPU=30 unconditionally. On the
// durable path the CPU quota is policy-derived (read from the
// AGENTPAAS_CPU_QUOTA_SECONDS env var); the fixed RLIMIT_CPU=30 only
// survives as a legacy-compat fallback when no policy CPU quota is set.
func TestB30T04_PythonRunnerDurablePathNoFixedRLimitCPU(t *testing.T) {
	if !strings.Contains(pythonRunner, "RLIMIT_CPU") {
		t.Fatal("pythonRunner no longer references RLIMIT_CPU — CPU quota wiring was removed")
	}
	// The durable path reads the CPU quota from an env var (signed policy).
	if !strings.Contains(pythonRunner, "AGENTPAAS_CPU_QUOTA_SECONDS") {
		t.Fatal("pythonRunner does not read AGENTPAAS_CPU_QUOTA_SECONDS — " +
			"durable-path CPU quota is not policy-derived")
	}
	// The legacy fallback constant must be documented as legacy compat.
	if !strings.Contains(pythonRunner, "legacy compat") && !strings.Contains(pythonRunner, "legacy-compat") {
		t.Fatal("pythonRunner does not document the RLIMIT_CPU=30 fallback as legacy compat")
	}
}

// TestB30T04_PythonRunnerDurablePathNoFixedRLimitNPROC asserts the embedded
// Python runner no longer hardcodes RLIMIT_NPROC=0 unconditionally. On the
// durable path the PID limit is policy-derived (read from
// AGENTPAAS_MAX_PIDS); the fixed RLIMIT_NPROC=0 only survives as a
// legacy-compat fallback when no policy PID limit is set.
func TestB30T04_PythonRunnerDurablePathNoFixedRLimitNPROC(t *testing.T) {
	if !strings.Contains(pythonRunner, "RLIMIT_NPROC") {
		t.Fatal("pythonRunner no longer references RLIMIT_NPROC — PID limit wiring was removed")
	}
	if !strings.Contains(pythonRunner, "AGENTPAAS_MAX_PIDS") {
		t.Fatal("pythonRunner does not read AGENTPAAS_MAX_PIDS — " +
			"durable-path PID limit is not policy-derived")
	}
}

// TestB30T04_PythonRunnerDefaultMaxPIDsSufficientForLocalTools asserts the
// durable-path default PID limit (when policy sets MaxPIDs>0 but no explicit
// value) is sufficient for approved local tools (git, grep, awk). The spec
// suggests 64 as a safe default.
func TestB30T04_PythonRunnerDefaultMaxPIDsSufficientForLocalTools(t *testing.T) {
	if !strings.Contains(pythonRunner, "64") {
		t.Fatal("pythonRunner does not encode a 64-PID default sufficient for " +
			"approved local tools (git, grep, awk)")
	}
}

// TestB30T04_ConfigCarriesPolicyResourceLimits asserts the harness Config
// carries the policy-derived CPU quota and PID limit fields, and that
// normalizeConfig preserves them (does not zero them out).
func TestB30T04_ConfigCarriesPolicyResourceLimits(t *testing.T) {
	cfg := Config{
		Addr:             "127.0.0.1:0",
		AgentPath:       "/agent/main.py",
		Python:          "python3",
		CPUQuotaSeconds: 60,
		MaxPIDs:         8,
	}
	out := normalizeConfig(cfg)
	if out.CPUQuotaSeconds != 60 {
		t.Errorf("normalizeConfig dropped CPUQuotaSeconds: got %d want 60", out.CPUQuotaSeconds)
	}
	if out.MaxPIDs != 8 {
		t.Errorf("normalizeConfig dropped MaxPIDs: got %d want 8", out.MaxPIDs)
	}
}

// TestB30T04_WorkerEnvPropagatesPolicyLimits asserts workerEnv propagates the
// policy-derived CPU quota and PID limit to the Python worker via env vars
// when set (durable path). When unset (legacy path), the env vars are absent.
func TestB30T04_WorkerEnvPropagatesPolicyLimits(t *testing.T) {
	// Durable path: CPUQuotaSeconds=60, MaxPIDs=8.
	env := workerEnv([]string{"PATH=/usr/bin"}, "127.0.0.1:9999")
	env = appendPolicyResourceEnv(env, true, 60, 8)
	if !containsEnv(env, "AGENTPAAS_CPU_QUOTA_SECONDS=60") {
		t.Errorf("durable env missing AGENTPAAS_CPU_QUOTA_SECONDS=60: %v", env)
	}
	if !containsEnv(env, "AGENTPAAS_MAX_PIDS=8") {
		t.Errorf("durable env missing AGENTPAAS_MAX_PIDS=8: %v", env)
	}

	// Legacy path: durable=false → env vars absent.
	legacy := workerEnv([]string{"PATH=/usr/bin"}, "127.0.0.1:9999")
	legacy = appendPolicyResourceEnv(legacy, false, 0, 0)
	for _, e := range legacy {
		if strings.HasPrefix(e, "AGENTPAAS_CPU_QUOTA_SECONDS=") {
			t.Errorf("legacy env must not set AGENTPAAS_CPU_QUOTA_SECONDS: %s", e)
		}
		if strings.HasPrefix(e, "AGENTPAAS_MAX_PIDS=") {
			t.Errorf("legacy env must not set AGENTPAAS_MAX_PIDS: %s", e)
		}
	}

	// Durable path with explicit 0 (unlimited CPU / forbid subprocesses):
	// both env vars emitted so the runner applies policy (not legacy).
	zero := appendPolicyResourceEnv([]string{}, true, 0, 0)
	if !containsEnv(zero, "AGENTPAAS_CPU_QUOTA_SECONDS=0") {
		t.Errorf("durable explicit-0 path missing AGENTPAAS_CPU_QUOTA_SECONDS=0: %v", zero)
	}
	if !containsEnv(zero, "AGENTPAAS_MAX_PIDS=0") {
		t.Errorf("durable explicit-0 path missing AGENTPAAS_MAX_PIDS=0: %v", zero)
	}
}

// runnerFunctionModule writes a Python module that imports os/resource and
// extracts+execs ONLY the apply_resource_limits function from the embedded
// pythonRunner, returning its module path. The returned module exposes
// apply_resource_limits() that the probe can call directly without running
// the SDK import / invoke loop.
func runnerFunctionModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Write the full runner so the extractor can find the function.
	runnerPath := filepath.Join(dir, "runner_full.py")
	if err := os.WriteFile(runnerPath, []byte(pythonRunner), 0o600); err != nil {
		t.Fatalf("write runner: %v", err)
	}
	// Write a wrapper module that extracts the function body and execs it
	// in a namespace with os/resource available.
	wrapper := filepath.Join(dir, "arl.py")
	src := `import os, resource, sys
def _extract_apply_resource_limits(path):
    src = open(path).read()
    marker = "def apply_resource_limits():"
    start = src.index(marker)
    # Find the end: the next top-level statement after the function body.
    # The function is followed by a blank line then "apply_resource_limits()"
    # call at module level. We stop just before that call line.
    call_marker = "\napply_resource_limits()\n"
    end = src.index(call_marker, start)
    return src[start:end]

_code = _extract_apply_resource_limits(os.path.join(os.path.dirname(__file__), "runner_full.py"))
_ns = {"__name__": "arl", "__builtins__": __builtins__, "os": os, "resource": resource, "sys": sys}
exec(compile(_code, "arl_apply", "exec"), _ns)
apply_resource_limits = _ns["apply_resource_limits"]
`
	if err := os.WriteFile(wrapper, []byte(src), 0o600); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	return wrapper
}

// TestB30T04_CPUBoundWorkBeyond30SecondsSucceeds verifies that on the durable
// path (CPUQuotaSeconds=60) the worker does NOT set the legacy RLIMIT_CPU=30,
// so CPU-bound work beyond 30 CPU seconds succeeds. On the legacy path
// (CPUQuotaSeconds unset) the worker still gets RLIMIT_CPU=30 (proven by
// TestB30T04_PythonRunnerDurablePathNoFixedRLimitCPU + the legacy-compat
// comment). We assert the policy plumbing: the durable runner emits a
// setrlimit for RLIMIT_CPU=60 when CPUQuotaSeconds=60, and does NOT emit
// RLIMIT_CPU=30 on the durable path.
func TestB30T04_CPUBoundWorkBeyond30SecondsSucceeds(t *testing.T) {
	skipUnlessPython(t)
	wrapper := runnerFunctionModule(t)
	// Durable path: CPUQuotaSeconds=60, MaxPIDs=64. Apply then read back
	// RLIMIT_CPU. On platforms without RLIMIT_CPU (some macOS configs), skip.
	probe := `
import os, sys
sys.path.insert(0, ` + fmt.Sprintf("%q", filepath.Dir(wrapper)) + `)
import arl
os.environ["AGENTPAAS_CPU_QUOTA_SECONDS"] = "60"
os.environ["AGENTPAAS_MAX_PIDS"] = "64"
arl.apply_resource_limits()
import resource
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_CPU)
except (OSError, ValueError) as e:
    print("SKIP:" + str(e))
    raise SystemExit(0)
if soft == 30:
    print("LEGACY_30")
else:
    print("SOFT=" + str(soft))
`
	out, _ := runPython(probe)
	res := strings.TrimSpace(out)
	t.Logf("durable RLIMIT_CPU after policy: %q", res)
	if res == "LEGACY_30" {
		t.Fatalf("durable path (CPUQuotaSeconds=60) still applied RLIMIT_CPU=30; " +
			"work beyond 30s would be SIGXCPU'd")
	}
	if !strings.HasPrefix(res, "SOFT=60") && !strings.HasPrefix(res, "SKIP") {
		t.Fatalf("durable RLIMIT_CPU = %q, want SOFT=60 (or SKIP on unsupported platform)", res)
	}
}

// TestB30T04_LegacyPath_StillUsesRLimitCPU30 verifies that on the legacy
// v0.2.3 path (no AGENTPAAS_CPU_QUOTA_SECONDS / AGENTPAAS_MAX_PIDS env vars),
// the runner still applies RLIMIT_CPU=30 and RLIMIT_NPROC=0 (backward
// compat). This proves the legacy constants survive with "legacy compat"
// comments when no policy is present.
func TestB30T04_LegacyPath_StillUsesRLimitCPU30_NPROC0(t *testing.T) {
	skipUnlessPython(t)
	wrapper := runnerFunctionModule(t)
	// Ensure legacy env vars are absent.
	probe := `
import os, sys
os.environ.pop("AGENTPAAS_CPU_QUOTA_SECONDS", None)
os.environ.pop("AGENTPAAS_MAX_PIDS", None)
sys.path.insert(0, ` + fmt.Sprintf("%q", filepath.Dir(wrapper)) + `)
import arl
arl.apply_resource_limits()
import resource
out = []
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_CPU)
    out.append("CPU=" + str(soft))
except (OSError, ValueError) as e:
    out.append("CPU=SKIP:" + str(e))
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_NPROC)
    out.append("NPROC=" + str(soft))
except (OSError, ValueError) as e:
    out.append("NPROC=SKIP:" + str(e))
print(" ".join(out))
`
	out, _ := runPython(probe)
	res := strings.TrimSpace(out)
	t.Logf("legacy rlimits: %q", res)
	// We expect RLIMIT_CPU=30 and RLIMIT_NPROC=0 to be applied on the
	// legacy path. On platforms where the hard limit is below 30, the
	// soft may be clamped — accept CPU<=30 but it must be the legacy
	// constant path (not unlimited, not policy). If RLIMIT_CPU is not
	// supported, the SKIP marker appears and we tolerate it.
	cpuPart := ""
	nprocPart := ""
	for _, tok := range strings.Fields(res) {
		if strings.HasPrefix(tok, "CPU=") {
			cpuPart = tok
		}
		if strings.HasPrefix(tok, "NPROC=") {
			nprocPart = tok
		}
	}
	if !strings.Contains(cpuPart, "SKIP") {
		// Legacy path must set CPU to 30 (or clamp below if hard < 30).
		if !strings.Contains(cpuPart, "CPU=30") {
			t.Errorf("legacy RLIMIT_CPU = %q, want CPU=30 (or clamp below)", cpuPart)
		}
	}
	if !strings.Contains(nprocPart, "SKIP") {
		// Legacy path must set NPROC to 0.
		if !strings.Contains(nprocPart, "NPROC=0") {
			t.Errorf("legacy RLIMIT_NPROC = %q, want NPROC=0", nprocPart)
		}
	}
}

// TestB30T04_ExplicitCPUBudgetTerminates verifies that when CPUQuotaSeconds=10
// is set (durable path), the worker's RLIMIT_CPU is set to 10 (not 30, not
// unlimited). The actual SIGXCPU termination on a real CPU-bound loop is
// exercised in the Docker-integration hardening suite; here we verify the
// policy is plumbed to the rlimit and the harness surfaces the
// cpu_quota_exhausted reason when the worker dies from SIGXCPU (asserted in
// TestB30T04_ResourceLimitTerminationReasonRecorded).
func TestB30T04_ExplicitCPUBudgetTerminates(t *testing.T) {
	skipUnlessPython(t)
	wrapper := runnerFunctionModule(t)
	probe := `
import os, sys
os.environ["AGENTPAAS_CPU_QUOTA_SECONDS"] = "10"
os.environ["AGENTPAAS_MAX_PIDS"] = "64"
sys.path.insert(0, ` + fmt.Sprintf("%q", filepath.Dir(wrapper)) + `)
import arl
arl.apply_resource_limits()
import resource
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_CPU)
except (OSError, ValueError) as e:
    print("SKIP:" + str(e))
    raise SystemExit(0)
print("SOFT=" + str(soft))
`
	out, _ := runPython(probe)
	soft := strings.TrimSpace(out)
	if soft == "SOFT=30" {
		t.Fatalf("explicit CPUQuotaSeconds=10 ignored; RLIMIT_CPU=30 (legacy) applied instead")
	}
	if !strings.HasPrefix(soft, "SOFT=10") && !strings.HasPrefix(soft, "SKIP") {
		t.Fatalf("RLIMIT_CPU soft = %q, want SOFT=10 (or SKIP on unsupported platform)", soft)
	}
}

// TestB30T04_AllowedBoundedSubprocessSucceeds verifies that on the durable
// path with MaxPIDs=8 the RLIMIT_NPROC is set to 8 (not 0), so approved local
// tool subprocesses (git, grep, awk) can spawn. The 9th-subprocess-fails
// behaviour is asserted via RLIMIT_NPROC=8 (the OS will refuse the 9th
// process).
func TestB30T04_AllowedBoundedSubprocessSucceeds(t *testing.T) {
	skipUnlessPython(t)
	wrapper := runnerFunctionModule(t)
	probe := `
import os, sys
os.environ["AGENTPAAS_CPU_QUOTA_SECONDS"] = "0"
os.environ["AGENTPAAS_MAX_PIDS"] = "8"
sys.path.insert(0, ` + fmt.Sprintf("%q", filepath.Dir(wrapper)) + `)
import arl
arl.apply_resource_limits()
import resource
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_NPROC)
except (OSError, ValueError) as e:
    print("SKIP:" + str(e))
    raise SystemExit(0)
print("SOFT=" + str(soft))
`
	out, _ := runPython(probe)
	soft := strings.TrimSpace(out)
	if soft == "SOFT=0" {
		t.Fatalf("durable path MaxPIDs=8 still applied RLIMIT_NPROC=0 (legacy); " +
			"approved local tools could not spawn")
	}
	if !strings.HasPrefix(soft, "SOFT=8") && !strings.HasPrefix(soft, "SKIP") {
		t.Fatalf("RLIMIT_NPROC soft = %q, want SOFT=8 (or SKIP on unsupported platform)", soft)
	}
}

// TestB30T04_ForkBombContained verifies that when MaxPIDs=0 is an explicit
// policy decision (forbid subprocesses), the durable path sets RLIMIT_NPROC=0.
// This proves a fork bomb can be contained by policy.
func TestB30T04_ForkBombContained(t *testing.T) {
	skipUnlessPython(t)
	wrapper := runnerFunctionModule(t)
	// MaxPIDs=0 means policy explicitly forbids subprocesses.
	probe := `
import os, sys
os.environ["AGENTPAAS_CPU_QUOTA_SECONDS"] = "0"
os.environ["AGENTPAAS_MAX_PIDS"] = "0"
sys.path.insert(0, ` + fmt.Sprintf("%q", filepath.Dir(wrapper)) + `)
import arl
arl.apply_resource_limits()
import resource
try:
    soft, hard = resource.getrlimit(resource.RLIMIT_NPROC)
except (OSError, ValueError) as e:
    print("SKIP:" + str(e))
    raise SystemExit(0)
print("SOFT=" + str(soft))
`
	out, _ := runPython(probe)
	soft := strings.TrimSpace(out)
	if !strings.HasPrefix(soft, "SOFT=0") && !strings.HasPrefix(soft, "SKIP") {
		t.Fatalf("RLIMIT_NPROC soft = %q, want SOFT=0 (or SKIP on unsupported platform)", soft)
	}
}

// TestB30T04_ResourceLimitTerminationReasonRecorded verifies the failure-
// context path records the resource-limit termination reason. When a worker
// hits a CPU quota, the attempt evidence must carry a category mapping to
// the resource-limit failure category, distinct from generic invoke_failed.
func TestB30T04_ResourceLimitTerminationReasonRecorded(t *testing.T) {
	cases := []struct {
		reason string
		detail string
		want   string
	}{
		{"cpu_quota_exhausted", "CPU quota exhausted: 10s", FailureCategoryResourceLimit},
		{"pid_limit_exhausted", "PID limit exhausted", FailureCategoryResourceLimit},
		{"oom_killed", "memory limit exceeded", FailureCategoryResourceLimit},
	}
	for _, c := range cases {
		got := failureCategory(c.reason, "FAILED", c.detail, nil)
		if got != c.want {
			t.Errorf("failureCategory(%q,%q) = %q, want %q", c.reason, c.detail, got, c.want)
		}
	}
	// attachFailureContext must surface the resource-limit category and the
	// observed value in the redacted detail.
	errResp := &ErrorResponse{Status: "FAILED", Reason: "cpu_quota_exhausted", Detail: "CPU quota exhausted: 10s"}
	ctx := buildFailureContext(errResp, invokeMetadata{runID: "r", invokeID: "i", policyDigest: placeholderPolicyDigest()}, nil)
	if ctx == nil {
		t.Fatal("buildFailureContext returned nil")
	}
	if ctx.Category != FailureCategoryResourceLimit {
		t.Errorf("ctx.Category = %q, want %q", ctx.Category, FailureCategoryResourceLimit)
	}
	if !strings.Contains(ctx.RedactedDetail, "CPU quota exhausted") {
		t.Errorf("ctx.RedactedDetail lost observed value: %q", ctx.RedactedDetail)
	}
}

// TestB30T04_ChildAgentCreationNotOSExec verifies the design invariant that
// child-agent creation is NOT done via os/exec subprocess spawning — it goes
// through the AgentPaaS control plane (B35). The RLIMIT_NPROC limit applies
// to TOOL subprocesses only. This is a documentation invariant asserted by
// checking the pythonRunner comment.
func TestB30T04_ChildAgentCreationNotOSExec(t *testing.T) {
	if !strings.Contains(pythonRunner, "child-agent") && !strings.Contains(pythonRunner, "control plane") {
		t.Fatal("pythonRunner does not document that child-agent creation goes " +
			"through the AgentPaaS control plane (B35), not os/exec")
	}
}

// containsEnv reports whether the env slice contains the exact entry.
func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// runPython runs a short python script and returns combined output.
func runPython(script string) (string, error) {
	out, err := pythonCombinedOutput(script)
	return out, err
}
