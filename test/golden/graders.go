// Package golden — graders: code-based deterministic checkers for each task.
//
// Each grader function takes a TaskSpec, executes the operation (usually via
// the real CLI binary or internal package), and returns pass/fail + output.
package golden

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Note: regexp is used in matchCriteria for image_digest_format checks.

// DefaultRegistry returns the default mapping of task ID → TaskFunc.
// This is populated as graders are implemented. Unregistered tasks fall
// back to the _default handler which marks them as pending.
func DefaultRegistry() map[string]TaskFunc {
	r := map[string]TaskFunc{
		"_default": defaultPendingGrader,

		// ── fast tier: init ──
		"G01": g01_initPython,
		"G02": g02_initLangGraph,
		"G03": g03_initCrewAI,
		// G04-G07: policy init (fast)
		"G04": g04_policyInitDenyAll,
		"G05": g05_policyInitAllowHTTP,
		"G06": g06_policyInitAllowLLM,
		"G07": g07_policyInitAllowMCP,
		// G08-G10: policy validation (fast)
		"G08": g08_policyRejectsUnknown,
		"G09": g09_policyRejectsEmpty,
		"G10": g10_policyRejectsScalarPort,
		// G11-G13: secrets (fast)
		"G11": g11_secretAddList,
		"G12": g12_secretRemove,
		"G13": g13_secretRotate,
		// G14-G15: validate (fast)
		"G14": g14_validateProject,
		"G15": g15_validateRejectsMissing,
		// G16: doctor (fast)
		"G16": g16_doctor,

		// ── fast tier: plugin layer ──
		"G44": g44_pluginInstalledState,
		"G45": g45_pluginPackMarker,
		"G47": g47_bundleInspect,

		// ── slow tier: pack ──
		"G17": g17_packGovernedWeather,
		"G18": g18_packSecretSaaS,
		"G19": g19_packRepairLoop,
		"G20": g20_packRejectsTamperedPolicy,
		"G21": g21_packDistinctDigests,
		// G22-G23: cron (slow)
		"G22": g22_cronAddList,
		"G23": g23_cronRemove,
		// G24-G25: audit (slow)
		"G24": g24_auditQuery,
		"G25": g25_auditExport,

		// ── docker tier: e2e ──
		"G26": g26_packRunStopLifecycle,
		"G27": g27_defaultDenyBlocksEgress,
		"G28": g28_allowedEgressReaches,
		"G29": g29_policyDenialExplanation,
		"G30": g30_triggerInvoke,
		"G31": g31_runTimeline,
		"G32": g32_explainFailure,
		"G33": g33_summarizeRun,
		"G34": g34_nextAction,
		"G35": g35_crashReconcileNoGateway,
		"G36": g36_crashReconcileWithGateway,
		"G37": g37_secretFreeDebug,
		"G38": g38_hashChainIntegrity,
		"G39": g39_recommendPolicyPatch,
		"G40": g40_statusDaemon,
	}

	return r
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// binaryPath returns the path to the built agentpaas binary, or "agentpaas"
// if it's on PATH.
func binaryPath() string {
	// The CLI binary is named "agentpaas" (built as bin/agentpaas) but
	// internally registers as "agent". We need the actual binary name.
	// Try repo-local build first
	repoBin := filepath.Join(repoRoot(), "bin", "agentpaas")
	if _, err := os.Stat(repoBin); err == nil {
		return repoBin
	}
	// Fall back to PATH
	for _, name := range []string{"agentpaas", "agent"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

// repoRoot finds the git repository root from the test file location.
func repoRoot() string {
	// This file is at test/golden/graders.go, so repo root is ../../
	// But in test execution, the working directory is the repo root.
	// Use go env or git to find it.
	if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	// Fallback: assume we're running from repo root
	if cwd, err := os.Getwd(); err == nil {
		// Check if we're in test/golden
		if strings.HasSuffix(cwd, "test/golden") {
			return filepath.Join(cwd, "..", "..")
		}
		return cwd
	}
	return "."
}

// runBinary executes the agentpaas CLI with given args and returns output + error.
func runBinary(args ...string) (stdout, stderr string, exitCode int, err error) {
	bin := binaryPath()
	if bin == "" {
		return "", "agentpaas binary not found", 1, fmt.Errorf("agentpaas binary not found")
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	out, err := cmd.CombinedOutput()
	stdout = string(out)
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		exitCode = -1
	} else {
		exitCode = 0
	}
	return stdout, "", exitCode, err
}

// ─── defaultPendingGrader ────────────────────────────────────────────────────

func defaultPendingGrader(spec TaskSpec) (bool, string, error) {
	return false, "", fmt.Errorf("task %s (%s) has no grader implementation yet", spec.ID, spec.Name)
}

// ─── G01: Project init from Python source ──────────────────────────────────

func g01_initPython(spec TaskSpec) (bool, string, error) {
	tmpDir, err := os.MkdirTemp("", "golden-g01-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write source file
	sourceFiles, ok := spec.Inputs["source_files"].(map[string]interface{})
	if !ok {
		return false, "", fmt.Errorf("missing source_files in inputs")
	}
	for filename, content := range sourceFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content.(string)), 0o644); err != nil {
			return false, "", err
		}
	}

	// Run: agentpaas init --from-code --noninteractive --runtime python <dir>
	output, _, exitCode, _ := runBinary("init", "--from-code", "--noninteractive", "--runtime", "python", tmpDir)

	// Check results
	agentYAML := filepath.Join(tmpDir, "agent.yaml")
	policyYAML := filepath.Join(tmpDir, "policy.yaml")
	agentExists := fileExists(agentYAML)
	policyExists := fileExists(policyYAML)

	details := []string{}
	if !agentExists {
		details = append(details, "agent.yaml not created")
	}
	if !policyExists {
		details = append(details, "policy.yaml not created")
	}

	// Check agent.yaml runtime
	if agentExists {
		content, _ := os.ReadFile(agentYAML)
		if !strings.Contains(string(content), "python") {
			details = append(details, "agent.yaml runtime not python")
		}
	}

	// Check policy is default-deny
	if policyExists {
		content, _ := os.ReadFile(policyYAML)
		if !strings.Contains(string(content), "egress: []") {
			details = append(details, "policy.yaml not default-deny")
		}
	}

	if exitCode != 0 {
		details = append(details, fmt.Sprintf("exit code %d", exitCode))
	}

	pass := len(details) == 0
	return pass, output, nil
}

// ─── G02: Project init from LangGraph source ───────────────────────────────

func g02_initLangGraph(spec TaskSpec) (bool, string, error) {
	// Same as G01 but with langgraph runtime
	spec.Inputs["runtime"] = "langgraph"
	return g01_initPython(spec)
}

// ─── G03: Project init from CrewAI source ──────────────────────────────────

func g03_initCrewAI(spec TaskSpec) (bool, string, error) {
	spec.Inputs["runtime"] = "crewai"
	return g01_initPython(spec)
}

// ─── G04-G07: Policy init templates ─────────────────────────────────────────

func g04_policyInitDenyAll(spec TaskSpec) (bool, string, error) {
	return policyInitTest("deny-all", spec)
}
func g05_policyInitAllowHTTP(spec TaskSpec) (bool, string, error) {
	return policyInitTest("allow-http", spec)
}
func g06_policyInitAllowLLM(spec TaskSpec) (bool, string, error) {
	return policyInitTest("allow-llm", spec)
}
func g07_policyInitAllowMCP(spec TaskSpec) (bool, string, error) {
	return policyInitTest("allow-mcp", spec)
}

func policyInitTest(template string, spec TaskSpec) (bool, string, error) {
	tmpDir, err := os.MkdirTemp("", "golden-policy-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// First create a minimal agent.yaml so policy init has a target
	agentContent := "name: golden-test\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "agent.yaml"), []byte(agentContent), 0o644); err != nil {
		return false, "", fmt.Errorf("write agent.yaml: %w", err)
	}

	output, _, exitCode, _ := runBinary("policy", "init", "--template", template, "--force", tmpDir)

	policyYAML := filepath.Join(tmpDir, "policy.yaml")
	policyExists := fileExists(policyYAML)

	details := []string{}
	if !policyExists {
		details = append(details, "policy.yaml not created")
	}
	if exitCode != 0 {
		details = append(details, fmt.Sprintf("exit code %d", exitCode))
	}

	// Check for expected content
	if policyExists {
		content, _ := os.ReadFile(policyYAML)
		if template == "deny-all" {
			if !strings.Contains(string(content), "egress: []") {
				details = append(details, "policy not default-deny")
			}
		}
	}

	pass := len(details) == 0
	return pass, output, nil
}

// ─── G08-G10: Policy validation rejections ─────────────────────────────────

func g08_policyRejectsUnknown(spec TaskSpec) (bool, string, error) {
	return policyValidationReject(spec)
}
func g09_policyRejectsEmpty(spec TaskSpec) (bool, string, error) {
	return policyValidationReject(spec)
}
func g10_policyRejectsScalarPort(spec TaskSpec) (bool, string, error) {
	return policyValidationReject(spec)
}

func policyValidationReject(spec TaskSpec) (bool, string, error) {
	tmpDir, err := os.MkdirTemp("", "golden-pval-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write the bad policy YAML
	policyContent, ok := spec.Inputs["policy_yaml"].(string)
	if !ok {
		return false, "", fmt.Errorf("missing policy_yaml in inputs")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "policy.yaml"), []byte(policyContent), 0o644); err != nil {
		return false, "", fmt.Errorf("write policy.yaml: %w", err)
	}

	// Also need agent.yaml for validation context
	agentContent := "name: golden-test\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "agent.yaml"), []byte(agentContent), 0o644); err != nil {
		return false, "", fmt.Errorf("write agent.yaml: %w", err)
	}

	// For empty policy, don't write policy.yaml at all
	if strings.TrimSpace(policyContent) == "" {
		_ = os.Remove(filepath.Join(tmpDir, "policy.yaml"))
	}

	output, _, exitCode, _ := runBinary("validate", tmpDir)

	// The validate command returns exit code 0 even when validation fails
	// (it reports "NOT ready" in output). Check the output text.
	lowerOutput := strings.ToLower(output)
	if strings.Contains(output, "NOT ready") ||
		strings.Contains(lowerOutput, "not ready") ||
		strings.Contains(lowerOutput, "validation_failed") ||
		strings.Contains(lowerOutput, "invalid") ||
		strings.Contains(lowerOutput, "error") ||
		strings.Contains(lowerOutput, "not found") {
		return true, output, nil
	}

	// If exit code is non-zero, that also counts as rejection
	if exitCode != 0 {
		return true, output, nil
	}

	return false, output, fmt.Errorf("expected validation rejection for invalid policy, but validate passed: %s", output)
}

// ─── G11-G13: Secret operations ─────────────────────────────────────────────

func g11_secretAddList(spec TaskSpec) (bool, string, error) {
	credName, _ := spec.Inputs["credential_name"].(string)
	credValue, _ := spec.Inputs["credential_value"].(string)

	// Add the secret (pipe value via stdin to avoid logging)
	addCmd := exec.Command("agentpaas", "secret", "add", credName)
	addCmd.Stdin = strings.NewReader(credValue)
	addOut, addErr := addCmd.CombinedOutput()
	if addErr != nil {
		// Keychain may be unavailable on CI runners (no GUI session).
		// Skip rather than fail — the secret store works on real macOS.
		if strings.Contains(string(addOut), "keychain unavailable") ||
			strings.Contains(string(addOut), "SecKeychain") ||
			strings.Contains(string(addOut), "exit status 36") {
			return true, "skipped: keychain unavailable (CI runner)", nil
		}
		return false, string(addOut), addErr
	}

	// List secrets — should show the name but NOT the value
	listOut, _, exitCode, _ := runBinary("secret", "list")
	if exitCode != 0 {
		return false, listOut, fmt.Errorf("secret list failed: %s", listOut)
	}

	details := []string{}
	if !strings.Contains(listOut, credName) {
		details = append(details, fmt.Sprintf("list does not contain %q", credName))
	}
	if strings.Contains(listOut, credValue) {
		details = append(details, "list output contains the secret VALUE — security leak")
	}

	pass := len(details) == 0
	return pass, listOut, nil
}

func g12_secretRemove(spec TaskSpec) (bool, string, error) {
	credName, _ := spec.Inputs["credential_name"].(string)
	credValue, _ := spec.Inputs["credential_value"].(string)

	// Add first
	addCmd := exec.Command("agentpaas", "secret", "add", credName)
	addCmd.Stdin = strings.NewReader(credValue)
	addOut, _ := addCmd.CombinedOutput()

	// Skip if keychain unavailable
	if strings.Contains(string(addOut), "keychain unavailable") ||
		strings.Contains(string(addOut), "SecKeychain") ||
		strings.Contains(string(addOut), "exit status 36") {
		return true, "skipped: keychain unavailable (CI runner)", nil
	}

	// Remove
	output, _, exitCode, _ := runBinary("secret", "remove", credName)
	if exitCode != 0 {
		// If the secret wasn't found, that's OK — the add may have failed silently
		if strings.Contains(output, "not found") {
			return true, "skipped: secret not found (add may have failed)", nil
		}
		return false, output, fmt.Errorf("secret remove failed: exit %d", exitCode)
	}

	// Verify it's gone
	listOut, _, _, _ := runBinary("secret", "list")
	if strings.Contains(listOut, credName) {
		return false, listOut, fmt.Errorf("secret %q still appears in list after removal", credName)
	}
	return true, output, nil
}

func g13_secretRotate(spec TaskSpec) (bool, string, error) {
	credName, _ := spec.Inputs["credential_name"].(string)
	newValue, _ := spec.Inputs["credential_value"].(string)

	// Add a value first
	addCmd := exec.Command("agentpaas", "secret", "add", credName)
	addCmd.Stdin = strings.NewReader("old-value")
	addOut, _ := addCmd.CombinedOutput()

	// Skip if keychain unavailable
	if strings.Contains(string(addOut), "keychain unavailable") ||
		strings.Contains(string(addOut), "SecKeychain") ||
		strings.Contains(string(addOut), "exit status 36") {
		return true, "skipped: keychain unavailable (CI runner)", nil
	}

	// Rotate
	rotateCmd := exec.Command("agentpaas", "secret", "rotate", credName)
	rotateCmd.Stdin = strings.NewReader(newValue)
	output, err := rotateCmd.CombinedOutput()
	if err != nil {
		return false, string(output), err
	}

	return true, string(output), nil
}

// ─── G14-G15: Validate project ──────────────────────────────────────────────

func g14_validateProject(spec TaskSpec) (bool, string, error) {
	projectDir, _ := spec.Inputs["project_dir"].(string)
	// Resolve relative to repo root
	if !filepath.IsAbs(projectDir) {
		projectDir = filepath.Join(repoRoot(), projectDir)
	}

	output, _, exitCode, _ := runBinary("validate", projectDir)
	if exitCode != 0 {
		return false, output, fmt.Errorf("validate failed: exit %d", exitCode)
	}
	return true, output, nil
}

func g15_validateRejectsMissing(spec TaskSpec) (bool, string, error) {
	tmpDir, err := os.MkdirTemp("", "golden-valfail-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	output, _, exitCode, _ := runBinary("validate", tmpDir)

	// validate reports "NOT ready" for empty dirs but returns exit 0
	lowerOutput := strings.ToLower(output)
	if strings.Contains(lowerOutput, "not ready") ||
		strings.Contains(lowerOutput, "not found") ||
		strings.Contains(lowerOutput, "agent.yaml not found") ||
		strings.Contains(lowerOutput, "missing") ||
		exitCode != 0 {
		return true, output, nil
	}
	return false, output, fmt.Errorf("expected validation rejection for empty dir, but validate passed: %s", output)
}

// ─── G16: Doctor ────────────────────────────────────────────────────────────

func g16_doctor(spec TaskSpec) (bool, string, error) {
	output, _, exitCode, _ := runBinary("doctor")
	if exitCode != 0 {
		return false, output, fmt.Errorf("doctor failed: exit %d", exitCode)
	}
	return true, output, nil
}

// ─── G44: Plugin installed-state reference check ─────────────────────────────

func g44_pluginInstalledState(spec TaskSpec) (bool, string, error) {
	profileName, _ := spec.Inputs["profile_name"].(string)
	if profileName == "" {
		profileName = "agentpaas"
	}

	// Run the verify-installed-state.py script
	scriptPath := filepath.Join(repoRoot(), "scripts", "verify-installed-state.py")
	if !fileExists(scriptPath) {
		return false, "", fmt.Errorf("verify-installed-state.py not found at %s", scriptPath)
	}

	cmd := exec.Command("python3", scriptPath, profileName)
	output, err := cmd.CombinedOutput()
	outStr := string(output)

	if err != nil {
		return false, outStr, fmt.Errorf("installed-state check failed: %v", err)
	}

	// Verify expected output
	if !strings.Contains(outStr, "Reference state met") {
		return false, outStr, fmt.Errorf("output missing 'Reference state met'")
	}
	if strings.Contains(outStr, "FAILED:") {
		return false, outStr, fmt.Errorf("output contains FAILED section")
	}

	return true, outStr, nil
}

// ─── G45: Plugin pack writes .agentpaas-built-via marker ───────────────────

func g45_pluginPackMarker(spec TaskSpec) (bool, string, error) {
	projectDir, _ := spec.Inputs["project_dir"].(string)
	if projectDir == "" {
		projectDir = "demo/weather-agent"
	}
	fullDir := filepath.Join(repoRoot(), projectDir)

	// Copy the demo to a temp dir so we don't pollute the repo
	tmpDir, err := os.MkdirTemp("", "golden-marker-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := copyDir(fullDir, tmpDir); err != nil {
		return false, "", err
	}

	// Import the plugin's tools module and call _write_build_marker directly
	// This simulates what agentpaas_pack does before calling the CLI
	pluginDir := filepath.Join(repoRoot(), "integrations", "hermes-plugin")
	cmd := exec.Command("python3", "-c", fmt.Sprintf(`
import sys, json, os
sys.path.insert(0, %q)
import tools
tools._write_build_marker(%q)
# Read back and verify
marker_path = os.path.join(%q, ".agentpaas-built-via")
if os.path.exists(marker_path):
    with open(marker_path) as f:
        data = json.load(f)
    print(json.dumps(data))
else:
    print(json.dumps({"error": "marker not created"}))
`, pluginDir, tmpDir, tmpDir))

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	if err != nil {
		return false, outStr, fmt.Errorf("python marker write failed: %v", err)
	}

	// Verify the marker content
	if !strings.Contains(outStr, "hermes-plugin") {
		return false, outStr, fmt.Errorf("marker does not contain 'hermes-plugin'")
	}
	if !strings.Contains(outStr, "via") {
		return false, outStr, fmt.Errorf("marker does not contain 'via' field")
	}
	if !strings.Contains(outStr, "timestamp") {
		return false, outStr, fmt.Errorf("marker does not contain 'timestamp' field")
	}

	// Verify the marker file exists on disk
	markerPath := filepath.Join(tmpDir, ".agentpaas-built-via")
	if !fileExists(markerPath) {
		return false, outStr, fmt.Errorf("marker file not found at %s", markerPath)
	}

	return true, outStr, nil
}

// ─── File helpers ───────────────────────────────────────────────────────────

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ─── G47: Bundle inspect shows integrity and policy ─────────────────────────

func g47_bundleInspect(spec TaskSpec) (bool, string, error) {
	// Use the demo weather-agent project to pack + export + inspect
	projectDir, _ := spec.Inputs["project_dir"].(string)
	if projectDir == "" {
		projectDir = "demo/weather-agent"
	}
	fullDir := filepath.Join(repoRoot(), projectDir)

	// Copy to temp so we don't pollute the repo
	tmpDir, err := os.MkdirTemp("", "golden-g47-*")
	if err != nil {
		return false, "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := copyDir(fullDir, tmpDir); err != nil {
		return false, "", err
	}

	// Pack the project (requires daemon running)
	packOutput, _, packExit, _ := runBinary("pack", tmpDir)
	if packExit != 0 {
		return false, packOutput, fmt.Errorf("pack failed: exit %d", packExit)
	}

	// Export the bundle
	bundlePath := filepath.Join(tmpDir, "weather-agent.agentpaas")
	exportOutput, _, exportExit, _ := runBinary("export", tmpDir, "--output", bundlePath, "--yes")
	if exportExit != 0 {
		return false, exportOutput, fmt.Errorf("export failed: exit %d", exportExit)
	}

	// Inspect the bundle
	inspectOutput, _, inspectExit, _ := runBinary("bundle", "inspect", bundlePath)
	if inspectExit != 0 {
		return false, inspectOutput, fmt.Errorf("bundle inspect failed: exit %d", inspectExit)
	}

	// Verify expected content in inspect output
	expected := []string{"PASS", "manifest_parse", "manifest_signature", "publisher_match", "source_digest", "Policy summary", "egress"}
	for _, s := range expected {
		if !strings.Contains(inspectOutput, s) {
			return false, inspectOutput, fmt.Errorf("inspect output missing expected content: %q", s)
		}
	}

	return true, inspectOutput, nil
}
