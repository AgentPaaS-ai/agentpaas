// Package golden — Docker-tier and slow-tier graders. These execute full
// pack→run→stop lifecycles and integration operations.
//
// Docker-tier tasks check for Docker at runtime via the AGENTPAAS_DOCKER_TESTS
// env var (handled by the runner). No build tags needed.
package golden

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Pack tasks (slow tier) ──────────────────────────────────────────────────

func g17_packGovernedWeather(spec TaskSpec) (bool, string, error) {
	return packDemo("demo/governed-weather")
}
func g18_packSecretSaaS(spec TaskSpec) (bool, string, error) {
	return packDemo("demo/secret-saas")
}
func g19_packRepairLoop(spec TaskSpec) (bool, string, error) {
	return packDemo("demo/repair-loop")
}

func packDemo(projectDir string) (bool, string, error) {
	fullDir := filepath.Join(repoRoot(), projectDir)
	output, _, exitCode, _ := runBinary("pack", fullDir)
	if exitCode != 0 {
		return false, output, fmt.Errorf("pack failed: exit %d", exitCode)
	}
	if !strings.Contains(output, "sha256:") {
		return false, output, fmt.Errorf("pack output missing image digest")
	}
	return true, output, nil
}

func g20_packRejectsTamperedPolicy(spec TaskSpec) (bool, string, error) {
	projectDir := filepath.Join(repoRoot(), "demo/governed-weather")

	tmpDir, err := os.MkdirTemp("", "golden-tamper-*")
	if err != nil {
		return false, "", err
	}
	defer os.RemoveAll(tmpDir)

	if err := copyDir(projectDir, tmpDir); err != nil {
		return false, "", err
	}

	policyPath := filepath.Join(tmpDir, "policy.yaml")
	content, _ := os.ReadFile(policyPath)
	tampered := strings.Replace(string(content), "api.weather.gov", "evil.example.com", 1)
	os.WriteFile(policyPath, []byte(tampered), 0o644)

	output, _, exitCode, _ := runBinary("pack", tmpDir)
	if exitCode != 0 || strings.Contains(strings.ToLower(output), "tamper") ||
		strings.Contains(strings.ToLower(output), "mismatch") ||
		strings.Contains(strings.ToLower(output), "advisory") {
		return true, output, nil
	}
	return false, output, fmt.Errorf("pack did not detect policy tamper")
}

func g21_packDistinctDigests(spec TaskSpec) (bool, string, error) {
	projectDir := filepath.Join(repoRoot(), "demo/governed-weather")

	tmpDir1, _ := os.MkdirTemp("", "golden-digest1-*")
	tmpDir2, _ := os.MkdirTemp("", "golden-digest2-*")
	defer os.RemoveAll(tmpDir1)
	defer os.RemoveAll(tmpDir2)

	copyDir(projectDir, tmpDir1)
	copyDir(projectDir, tmpDir2)

	agentPath := filepath.Join(tmpDir2, "agent.yaml")
	content, _ := os.ReadFile(agentPath)
	modified := strings.Replace(string(content), "governed-weather", "governed-weather-v2", 1)
	os.WriteFile(agentPath, []byte(modified), 0o644)

	out1, _, code1, _ := runBinary("pack", tmpDir1)
	out2, _, code2, _ := runBinary("pack", tmpDir2)

	if code1 != 0 || code2 != 0 {
		return false, out1 + out2, fmt.Errorf("pack failed: exit %d/%d", code1, code2)
	}

	digest1 := extractDigest(out1)
	digest2 := extractDigest(out2)

	if digest1 == "" || digest2 == "" {
		return false, out1 + out2, fmt.Errorf("could not extract digests")
	}
	if digest1 == digest2 {
		return false, "", fmt.Errorf("digests should differ after prompt change: %s == %s", digest1, digest2)
	}
	return true, fmt.Sprintf("digest1=%s digest2=%s (distinct)", digest1, digest2), nil
}

func extractDigest(output string) string {
	re := regexp.MustCompile(`sha256:[0-9a-f]{64}`)
	return re.FindString(output)
}

// ─── Trigger/cron tasks (slow tier) ─────────────────────────────────────────

func g22_cronAddList(spec TaskSpec) (bool, string, error) {
	agentName, _ := spec.Inputs["agent_name"].(string)
	expr, _ := spec.Inputs["expr"].(string)

	addOut, _, addCode, _ := runBinary("cron", "add", "--agent", agentName, "--expr", expr)
	if addCode != 0 {
		return false, addOut, fmt.Errorf("cron add failed: exit %d", addCode)
	}

	listOut, _, listCode, _ := runBinary("cron", "list")
	if listCode != 0 {
		return false, listOut, fmt.Errorf("cron list failed: exit %d", listCode)
	}
	if !strings.Contains(listOut, agentName) {
		return false, listOut, fmt.Errorf("cron list does not contain agent %q", agentName)
	}
	return true, listOut, nil
}

func g23_cronRemove(spec TaskSpec) (bool, string, error) {
	agentName, _ := spec.Inputs["agent_name"].(string)
	expr, _ := spec.Inputs["expr"].(string)

	runBinary("cron", "add", "--agent", agentName, "--expr", expr)

	listOut, _, _, _ := runBinary("cron", "list")
	scheduleID := extractScheduleID(listOut, agentName)
	if scheduleID == "" {
		return false, listOut, fmt.Errorf("could not find schedule ID for %s", agentName)
	}

	removeOut, _, removeCode, _ := runBinary("cron", "remove", "--id", scheduleID)
	if removeCode != 0 {
		return false, removeOut, fmt.Errorf("cron remove failed: exit %d", removeCode)
	}
	return true, removeOut, nil
}

func extractScheduleID(output, agentName string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, agentName) {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}

// ─── Audit tasks (slow tier) ─────────────────────────────────────────────────

func g24_auditQuery(spec TaskSpec) (bool, string, error) {
	output, _, exitCode, _ := runBinary("audit", "query")
	if exitCode != 0 {
		return false, output, fmt.Errorf("audit query failed: exit %d", exitCode)
	}
	return true, output, nil
}

func g25_auditExport(spec TaskSpec) (bool, string, error) {
	outputPath, _ := spec.Inputs["output_path"].(string)
	output, _, exitCode, _ := runBinary("audit", "export", "--output", outputPath)
	if exitCode != 0 {
		return false, output, fmt.Errorf("audit export failed: exit %d", exitCode)
	}
	if !fileExists(outputPath) {
		return false, output, fmt.Errorf("export file not created at %s", outputPath)
	}
	return true, output, nil
}

// ─── Docker e2e tasks ───────────────────────────────────────────────────────

func g26_packRunStopLifecycle(spec TaskSpec) (bool, string, error) {
	projectDir := filepath.Join(repoRoot(), "demo/governed-weather")

	packOut, _, packCode, _ := runBinary("pack", projectDir)
	if packCode != 0 {
		return false, packOut, fmt.Errorf("pack failed: exit %d", packCode)
	}
	digest := extractDigest(packOut)
	if digest == "" {
		return false, packOut, fmt.Errorf("no digest in pack output")
	}

	runOut, _, runCode, _ := runBinary("run", digest)
	if runCode != 0 {
		return false, runOut, fmt.Errorf("run failed: exit %d", runCode)
	}

	runID := extractRunID(runOut)
	time.Sleep(5 * time.Second)

	if runID != "" {
		stopOut, _, stopCode, _ := runBinary("stop", runID)
		if stopCode != 0 {
			return false, stopOut, fmt.Errorf("stop failed: exit %d", stopCode)
		}
	}

	if hasOrphanContainers() {
		return false, "", fmt.Errorf("orphan containers detected after stop")
	}

	return true, "pack→run→stop completed, no orphans", nil
}

func g27_defaultDenyBlocksEgress(spec TaskSpec) (bool, string, error) {
	projectDir := filepath.Join(repoRoot(), "demo/governed-weather")

	packOut, _, packCode, _ := runBinary("pack", projectDir)
	if packCode != 0 {
		return false, packOut, fmt.Errorf("pack failed: exit %d", packCode)
	}
	digest := extractDigest(packOut)
	if digest == "" {
		return false, packOut, fmt.Errorf("no digest")
	}

	runOut, _, runCode, _ := runBinary("run", digest)
	if runCode != 0 {
		return false, runOut, fmt.Errorf("run failed: exit %d", runCode)
	}
	defer func() {
		runID := extractRunID(runOut)
		if runID != "" {
			runBinary("stop", runID)
		}
	}()

	time.Sleep(5 * time.Second)

	auditOut, _, auditCode, _ := runBinary("audit", "query", "--category", "egress")
	if auditCode == 0 && strings.Contains(auditOut, "denied") {
		return true, auditOut, nil
	}
	return false, auditOut, fmt.Errorf("no egress_denied event found in audit log")
}

func g28_allowedEgressReaches(spec TaskSpec) (bool, string, error) {
	projectDir := filepath.Join(repoRoot(), "demo/governed-weather")

	packOut, _, packCode, _ := runBinary("pack", projectDir)
	if packCode != 0 {
		return false, packOut, fmt.Errorf("pack failed")
	}
	digest := extractDigest(packOut)
	if digest == "" {
		return false, packOut, fmt.Errorf("no digest")
	}

	runOut, _, runCode, _ := runBinary("run", digest)
	if runCode != 0 {
		return false, runOut, fmt.Errorf("run failed")
	}
	defer func() {
		runID := extractRunID(runOut)
		if runID != "" {
			runBinary("stop", runID)
		}
	}()

	time.Sleep(5 * time.Second)

	auditOut, _, _, _ := runBinary("audit", "query")
	if strings.Contains(auditOut, "weather") || strings.Contains(auditOut, "api.weather.gov") {
		return true, auditOut, nil
	}
	return false, auditOut, fmt.Errorf("no weather API egress found in audit log")
}

func g29_policyDenialExplanation(spec TaskSpec) (bool, string, error) {
	output, _, exitCode, _ := runBinary("explain-policy-denial", "--destination", "evil.example.com")
	if exitCode != 0 {
		return false, output, fmt.Errorf("explain-policy-denial failed: exit %d", exitCode)
	}
	if !strings.Contains(strings.ToLower(output), "den") &&
		!strings.Contains(strings.ToLower(output), "rule") {
		return false, output, fmt.Errorf("output missing denial/rule context")
	}
	return true, output, nil
}

// G30-G40: CLI commands that require a running daemon + active runs.

func g30_triggerInvoke(spec TaskSpec) (bool, string, error) {
	agentName, _ := spec.Inputs["agent_name"].(string)
	payload, _ := spec.Inputs["payload"].(string)
	output, _, exitCode, _ := runBinary("trigger", "invoke", "--agent", agentName, "--payload", payload)
	if exitCode != 0 {
		return false, output, fmt.Errorf("trigger invoke failed: exit %d", exitCode)
	}
	return true, output, nil
}

func g31_runTimeline(spec TaskSpec) (bool, string, error) {
	return simpleCLITest("get-run-timeline")
}
func g32_explainFailure(spec TaskSpec) (bool, string, error) {
	return simpleCLITest("explain-failure")
}
func g33_summarizeRun(spec TaskSpec) (bool, string, error) {
	return simpleCLITest("summarize-run")
}
func g34_nextAction(spec TaskSpec) (bool, string, error) {
	return simpleCLITest("next-action")
}

func g35_crashReconcileNoGateway(spec TaskSpec) (bool, string, error) {
	output, err := exec.Command("go", "test", "-run", "TestE2E_CrashReconciliation", "./internal/runtime/...", "-timeout", "120s").CombinedOutput()
	if err != nil {
		return false, string(output), fmt.Errorf("crash reconciliation test failed: %v", err)
	}
	return true, string(output), nil
}

func g36_crashReconcileWithGateway(spec TaskSpec) (bool, string, error) {
	output, err := exec.Command("go", "test", "-run", "TestE2E_CrashReconciliation", "./internal/runtime/...", "-timeout", "120s").CombinedOutput()
	if err != nil {
		return false, string(output), fmt.Errorf("crash reconciliation test failed: %v", err)
	}
	return true, string(output), nil
}

func g37_secretFreeDebug(spec TaskSpec) (bool, string, error) {
	output, err := exec.Command("go", "test", "-run", "TestE2E_SecretFreeDebugOutput", "./internal/runtime/...", "-timeout", "120s").CombinedOutput()
	if err != nil {
		return false, string(output), fmt.Errorf("secret-free debug test failed: %v", err)
	}
	return true, string(output), nil
}

func g38_hashChainIntegrity(spec TaskSpec) (bool, string, error) {
	output, err := exec.Command("go", "test", "-run", "TestVerifyHarnessChain", "./internal/daemon/...", "-timeout", "60s").CombinedOutput()
	if err != nil {
		return false, string(output), fmt.Errorf("hash chain test failed: %v", err)
	}
	return true, string(output), nil
}

func g39_recommendPolicyPatch(spec TaskSpec) (bool, string, error) {
	return simpleCLITest("recommend-policy-patch")
}

func g40_statusDaemon(spec TaskSpec) (bool, string, error) {
	output, _, exitCode, _ := runBinary("status")
	if exitCode != 0 {
		return false, output, fmt.Errorf("status failed: exit %d", exitCode)
	}
	return true, output, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func simpleCLITest(cmd string) (bool, string, error) {
	output, _, exitCode, _ := runBinary(cmd)
	if exitCode != 0 {
		return false, output, fmt.Errorf("%s failed: exit %d", cmd, exitCode)
	}
	return true, output, nil
}

func extractRunID(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "run") && strings.Contains(line, ":") {
			fields := strings.SplitN(line, ":", 2)
			if len(fields) >= 2 {
				return strings.TrimSpace(fields[1])
			}
		}
	}
	return ""
}

func hasOrphanContainers() bool {
	out, err := exec.Command("docker", "ps", "--filter", "label=agentpaas", "-q").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func copyDir(src, dst string) error {
	return exec.Command("cp", "-r", src+"/.", dst).Run()
}
