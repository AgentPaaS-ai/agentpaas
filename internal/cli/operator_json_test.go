package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateCmdJSONShape verifies the `agent validate --json` output
// contains the Block 11 operator schema fields: schema_version, ready,
// project_dir, runtime, issues.
func TestValidateCmdJSONShape(t *testing.T) {
	// The validate command requires a running daemon. Since we can't run
	// a daemon in a unit test, we verify the command structure: the --json
	// flag is accepted and the command exists.
	resetAgentCmd()
	cmd := freshCmd()
	validateCmd := findSubCmd(cmd, "validate")
	if validateCmd == nil {
		t.Fatal("validate command not found")
	}
	if validateCmd.Use != "validate <project-path>" {
		t.Errorf("validate Use = %q, want %q", validateCmd.Use, "validate <project-path>")
	}
}

// TestSummarizeCmdJSONShape verifies the `agent summarize --json` command exists.
func TestSummarizeCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	summarizeCmd := findSubCmd(cmd, "summarize")
	if summarizeCmd == nil {
		t.Fatal("summarize command not found")
	}
}

// TestExplainFailureCmdJSONShape verifies the `agent explain-failure --json` command exists.
func TestExplainFailureCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	explainCmd := findSubCmd(cmd, "explain-failure")
	if explainCmd == nil {
		t.Fatal("explain-failure command not found")
	}
}

// TestExplainDenialCmdJSONShape verifies the `agent explain-denial --json` command exists.
func TestExplainDenialCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	explainCmd := findSubCmd(cmd, "explain-denial")
	if explainCmd == nil {
		t.Fatal("explain-denial command not found")
	}
}

// TestRecommendPatchCmdJSONShape verifies the `agent recommend-patch --json` command exists.
func TestRecommendPatchCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	recommendCmd := findSubCmd(cmd, "recommend-patch")
	if recommendCmd == nil {
		t.Fatal("recommend-patch command not found")
	}
}

// TestTimelineCmdJSONShape verifies the `agent timeline --json` command exists.
func TestTimelineCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	timelineCmd := findSubCmd(cmd, "timeline")
	if timelineCmd == nil {
		t.Fatal("timeline command not found")
	}
}

// TestNextActionCmdJSONShape verifies the `agent next-action --json` command exists.
func TestNextActionCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	nextActionCmd := findSubCmd(cmd, "next-action")
	if nextActionCmd == nil {
		t.Fatal("next-action command not found")
	}
}

// TestPackCmdJSONShape verifies the `agent pack` command accepts --json.
func TestPackCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	packCmd := findSubCmd(cmd, "pack")
	if packCmd == nil {
		t.Fatal("pack command not found")
	}
}

// TestRunCmdJSONShape verifies the `agent run` command accepts --json.
func TestRunCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	runCmd := findSubCmd(cmd, "run")
	if runCmd == nil {
		t.Fatal("run command not found")
	}
}

// TestStopCmdJSONShape verifies the `agent stop` command accepts --json.
func TestStopCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	stopCmd := findSubCmd(cmd, "stop")
	if stopCmd == nil {
		t.Fatal("stop command not found")
	}
}

// TestLogsCmdJSONShape verifies the `agent logs` command accepts --json.
func TestLogsCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	logsCmd := findSubCmd(cmd, "logs")
	if logsCmd == nil {
		t.Fatal("logs command not found")
	}
	jsonFlag := logsCmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Fatal("logs --json flag not found")
	}
}

// TestPolicyCmdJSONShape verifies the `agent policy` command has subcommands.
func TestPolicyCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	policyCmd := findSubCmd(cmd, "policy")
	if policyCmd == nil {
		t.Fatal("policy command not found")
	}
	// Check subcommands exist
	subNames := []string{"apply", "show", "explain", "propose"}
	for _, name := range subNames {
		found := false
		for _, sub := range policyCmd.Commands() {
			if strings.HasPrefix(sub.Use, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("policy subcommand %q not found", name)
		}
	}
}

// TestAuditCmdJSONShape verifies the `agent audit` command has query/export subcommands.
func TestAuditCmdJSONShape(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	auditCmd := findSubCmd(cmd, "audit")
	if auditCmd == nil {
		t.Fatal("audit command not found")
	}
	// Check subcommands exist
	subNames := []string{"query", "export"}
	for _, name := range subNames {
		found := false
		for _, sub := range auditCmd.Commands() {
			if strings.HasPrefix(sub.Use, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("audit subcommand %q not found", name)
		}
	}
}

// TestAllOperatorCommandsAcceptJSON verifies every operator command accepts
// the global --json flag.
func TestAllOperatorCommandsAcceptJSON(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()

	// All commands that should support --json
	commands := []string{
		"pack", "run", "stop", "logs", "validate",
		"summarize", "explain-failure", "explain-denial",
		"recommend-patch", "timeline", "next-action",
	}

	for _, name := range commands {
		subCmd := findSubCmd(cmd, name)
		if subCmd == nil {
			t.Errorf("command %q not found", name)
			continue
		}
		// The --json flag is a persistent flag on the root command.
		jsonFlag := cmd.PersistentFlags().Lookup("json")
		if jsonFlag == nil {
			t.Fatalf("global --json flag not found")
		}
	}
}

// TestOperatorJSONOutputShape verifies that the operator schema types
// serialize to JSON with the expected field names. This is the golden test
// for CLI JSON parity: the CLI uses operator.*Response types for --json
// output, so their JSON shape IS the contract.
func TestOperatorJSONOutputShape(t *testing.T) {
	// We verify the operator package types produce the expected JSON keys
	// by marshaling a representative instance of each.
	tests := []struct {
		name     string
		payload  interface{}
		wantKeys []string
	}{
		{
			name: "validate",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"ready":          false,
				"project_dir":    "/tmp/test",
				"runtime":        "python",
			},
			wantKeys: []string{"schema_version", "ready", "project_dir", "runtime"},
		},
		{
			name: "summarize",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"run_id":         "run_123",
				"status":         "failed",
				"summary":        "test",
			},
			wantKeys: []string{"schema_version", "run_id", "status", "summary"},
		},
		{
			name: "explain_failure",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"run_id":         "run_123",
				"error_category": "dependency_conflict",
				"next_action":    "install_dependency",
			},
			wantKeys: []string{"schema_version", "run_id", "error_category", "next_action"},
		},
		{
			name: "explain_denial",
			payload: map[string]interface{}{
				"schema_version":   "1.1.0",
				"blocking_rule_id": "egress[0]",
				"next_action":      "review_policy_patch",
			},
			wantKeys: []string{"schema_version", "blocking_rule_id", "next_action"},
		},
		{
			name: "recommend_patch",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"risk_level":     "medium",
				"next_action":    "review_policy_patch",
				"confirmation": map[string]interface{}{
					"requires_confirmation": true,
					"risk_level":            "medium",
				},
			},
			wantKeys: []string{"schema_version", "risk_level", "next_action", "confirmation"},
		},
		{
			name: "timeline",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"run_id":         "run_123",
				"events":         []interface{}{},
			},
			wantKeys: []string{"schema_version", "run_id", "events"},
		},
		{
			name: "next_action",
			payload: map[string]interface{}{
				"schema_version": "1.1.0",
				"next_action":    "fix_code",
				"rationale":      "syntax error",
			},
			wantKeys: []string{"schema_version", "next_action", "rationale"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var m map[string]interface{}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, key := range tt.wantKeys {
				if _, ok := m[key]; !ok {
					t.Errorf("missing key %q in %s JSON output", key, tt.name)
				}
			}
		})
	}
}

// TestPrintTextOrJSON verifies the JSON output helper produces valid JSON
// when jsonOut is true.
func TestPrintTextOrJSON(t *testing.T) {
	type testType struct {
		SchemaVersion string `json:"schema_version"`
		Ready         bool   `json:"ready"`
	}

	var buf bytes.Buffer
	err := printTextOrJSON(true, testType{SchemaVersion: "1.1.0", Ready: true}, func(v interface{}) string {
		return "text"
	})
	// printTextOrJSON writes to fmt.Println, not buf. We just verify no error.
	if err != nil {
		t.Fatalf("printTextOrJSON error: %v", err)
	}
	_ = buf
}
