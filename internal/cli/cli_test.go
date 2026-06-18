package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// resetAgentCmd sets the package-level cached rootCmd to nil so AgentCmd()
// builds a fresh command tree for each test. This prevents flag state from
// leaking between tests.
func resetAgentCmd() {
	rootCmd = nil
}

// captureStdout executes fn and returns everything written to os.Stdout during
// the call. This is needed because several CLI commands use fmt.Println rather
// than cmd.Println.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = old
	return <-done
}

// executeCmd builds a fresh command, sets args, and runs Execute. It captures
// both the cobra writer output (for cmd.Print/Println calls) AND the os.Stdout
// output (for fmt.Print/Println calls). Returns (cmdOut+stdout, cmdErr+stderr,
// executeError).
func executeCmd(args ...string) (string, string, error) {
	resetAgentCmd()
	cmd := AgentCmd()

	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)

	// Also capture os.Stdout for commands that use fmt.Println.
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	stdoutDone := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rOut)
		stdoutDone <- buf.String()
	}()

	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	stderrDone := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rErr)
		stderrDone <- buf.String()
	}()

	cmd.SetArgs(args)
	err := cmd.Execute()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdoutRaw := <-stdoutDone
	stderrRaw := <-stderrDone

	// Combine cobra-writer output with os.Pipe output.
	outResult := outBuf.String() + stdoutRaw
	errResult := errBuf.String() + stderrRaw

	return outResult, errResult, err
}

// findSubCmd walks the command tree looking for a command with the given use
// prefix. It returns nil if not found.
func findSubCmd(root *cobra.Command, usePrefix string) *cobra.Command {
	for _, sub := range root.Commands() {
		if strings.HasPrefix(sub.Use, usePrefix) {
			return sub
		}
	}
	return nil
}

// freshCmd returns a newly-constructed root command, bypassing the package-level
// cache. Callers MUST use this instead of AgentCmd() when executing commands
// to avoid flag-state leakage.
func freshCmd() *cobra.Command {
	resetAgentCmd()
	return AgentCmd()
}

// ---------------------------------------------------------------------------
// TestAgentVersion_TextOutput
// Tests: agent version text output contains version string, proto version,
// git commit, OS/arch.
//
// Also serves as the golden output test (test case 10).
// ---------------------------------------------------------------------------
func TestAgentVersion_TextOutput(t *testing.T) {
	resetAgentCmd()

	stdout := captureStdout(t, func() {
		cmd := freshCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"version"})
		_ = cmd.Execute()
	})

	// Check required substrings.
	checks := []struct {
		name   string
		substr string
	}{
		{"CLI version", "CLI:"},
		{"proto version", "Proto:"},
		{"git commit", "Commit:"},
		{"OS/Arch", "OS/Arch:"},
		{"version number", "0.1.0-dev"},
		{"proto value", "v1"},
		{"git commit value", "unknown"},
	}
	for _, c := range checks {
		if !strings.Contains(stdout, c.substr) {
			t.Errorf("expected output to contain %q (%s)", c.substr, c.name)
		}
	}

	// Check OS/arch is present.
	expectedArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if !strings.Contains(stdout, expectedArch) {
		t.Errorf("expected output to contain OS/Arch %q; got:\n%s", expectedArch, stdout)
	}

	// Golden output: exact format check.
	expected := fmt.Sprintf(
		"CLI: 0.1.0-dev | Proto: v1 | Commit: unknown | OS/Arch: %s | Docker: unknown | Docker API: unknown",
		expectedArch,
	)
	got := strings.TrimSpace(stdout)
	if got != expected {
		t.Errorf("golden output mismatch\nwant: %q\ngot:  %q", expected, got)
	}
}

// ---------------------------------------------------------------------------
// TestAgentVersion_JSONOutput
// Tests: agent version --json outputs valid JSON with all expected fields.
// ---------------------------------------------------------------------------
func TestAgentVersion_JSONOutput(t *testing.T) {
	resetAgentCmd()

	stdout := captureStdout(t, func() {
		cmd := freshCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"version", "--json"})
		_ = cmd.Execute()
	})

	// Unmarshal and check fields.
	var v VersionOutput
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v\noutput:\n%s", err, stdout)
	}

	if v.CLIVersion != "0.1.0-dev" {
		t.Errorf("cli_version: want %q, got %q", "0.1.0-dev", v.CLIVersion)
	}
	if v.ProtoVersion != "v1" {
		t.Errorf("proto_version: want %q, got %q", "v1", v.ProtoVersion)
	}
	expectedArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if v.OsArch != expectedArch {
		t.Errorf("os_arch: want %q, got %q", expectedArch, v.OsArch)
	}
	if v.GitCommit == "" {
		t.Error("git_commit must not be empty")
	}
	if v.DockerContext == "" {
		t.Error("docker_context must not be empty")
	}
	if v.DockerAPIVersion == "" {
		t.Error("docker_api_version must not be empty")
	}
}

// ---------------------------------------------------------------------------
// TestDaemonStatus_NotRunning
// Tests: agent daemon status when daemon not running returns error containing
// "not running" or "start".
// ---------------------------------------------------------------------------
func TestDaemonStatus_NotRunning(t *testing.T) {
	resetAgentCmd()
	t.Setenv("AGENTPAAS_SOCKET", "/nonexistent/agentpaas-test.sock")

	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"daemon", "status"})

	err := cmd.Execute()

	// Must return an error.
	if err == nil {
		t.Fatal("expected error for 'daemon status' when daemon is not running, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "not running") && !strings.Contains(errMsg, "start") {
		t.Errorf("error message should mention 'not running' or 'start'; got:\n%s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonStatus_NotRunningJSON
// Tests: agent daemon status --json when daemon not running produces valid
// JSON with an error field.
// ---------------------------------------------------------------------------
func TestDaemonStatus_NotRunningJSON(t *testing.T) {
	resetAgentCmd()
	t.Setenv("AGENTPAAS_SOCKET", "/nonexistent/agentpaas-test-json.sock")

	stdout := captureStdout(t, func() {
		cmd := freshCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"daemon", "status", "--json"})
		_ = cmd.Execute()
	})

	var je map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &je); err != nil {
		t.Fatalf("expected valid JSON output, got error: %v\noutput:\n%s", err, stdout)
	}

	if _, ok := je["error"]; !ok {
		t.Errorf("JSON output must contain an 'error' field; got: %+v", je)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonCommands_Exist
// Tests: daemon subcommands start/stop/restart/install/uninstall exist in the
// command tree.
// ---------------------------------------------------------------------------
func TestDaemonCommands_Exist(t *testing.T) {
	resetAgentCmd()
	root := freshCmd()
	daemonCmd := findSubCmd(root, "daemon")
	if daemonCmd == nil {
		t.Fatal("expected 'daemon' subcommand to exist")
	}

	expected := []string{"start", "stop", "restart", "install", "uninstall", "status"}
	for _, name := range expected {
		if sub := findSubCmd(daemonCmd, name); sub == nil {
			t.Errorf("expected 'daemon %s' subcommand to exist", name)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDoctor_StubMessage
// Tests: agent doctor prints a message containing "not yet implemented".
// ---------------------------------------------------------------------------
func TestDoctor_StubMessage(t *testing.T) {
	resetAgentCmd()

	stdout := captureStdout(t, func() {
		cmd := freshCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"doctor"})
		_ = cmd.Execute()
	})

	if !strings.Contains(stdout, "not yet implemented") {
		t.Errorf("expected doctor output to contain 'not yet implemented'; got:\n%s", stdout)
	}
}

// ---------------------------------------------------------------------------
// TestControlCommands_Exist
// Tests: agent pack and other control stubs exist in the command tree.
// ---------------------------------------------------------------------------
func TestControlCommands_Exist(t *testing.T) {
	resetAgentCmd()
	root := freshCmd()

	expected := []struct {
		use  string
		desc string
	}{
		{"pack", "pack"},
		{"run", "run"},
		{"stop", "stop"},
		{"logs", "logs"},
		{"validate", "validate"},
		{"summarize", "summarize"},
		{"explain-failure", "explain-failure"},
		{"explain-denial", "explain-denial"},
		{"recommend-patch", "recommend-patch"},
		{"timeline", "timeline"},
		{"next-action", "next-action"},
		{"policy", "policy"},
		{"secrets", "secrets"},
		{"audit", "audit"},
	}

	for _, e := range expected {
		if sub := findSubCmd(root, e.use); sub == nil {
			t.Errorf("expected 'agent %s' subcommand to exist", e.desc)
		}
	}
}

// ---------------------------------------------------------------------------
// TestGlobalFlags
// Tests: root command has global flags --json, --socket, --home.
// ---------------------------------------------------------------------------
func TestGlobalFlags(t *testing.T) {
	resetAgentCmd()
	root := freshCmd()

	flagTests := []struct {
		name string
		long string
	}{
		{"json", "json"},
		{"socket", "socket"},
		{"home", "home"},
	}

	for _, ft := range flagTests {
		f := root.PersistentFlags().Lookup(ft.long)
		if f == nil {
			t.Errorf("expected persistent flag --%s to be registered on root command", ft.long)
			continue
		}
		if f.Shorthand != "" && f.Shorthand != ft.name[:1] {
			t.Errorf("flag --%s: unexpected shorthand %q", ft.long, f.Shorthand)
		}
	}
}

// ---------------------------------------------------------------------------
// TestHelp_ShowsCommands
// Tests: agent --help shows available commands (daemon, version, doctor, pack,
// etc.).
// ---------------------------------------------------------------------------
func TestHelp_ShowsCommands(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()

	if err != nil {
		t.Fatalf("--help should not return an error: %v", err)
	}

	helpText := outBuf.String()
	if helpText == "" {
		// cobra may write help to stderr in some setups.
		helpText = errBuf.String()
	}

	expectedCommands := []string{
		"daemon",
		"version",
		"doctor",
		"pack",
		"run",
		"stop",
		"logs",
		"policy",
		"secrets",
		"audit",
		"validate",
		"summarize",
		"explain-failure",
		"timeline",
		"next-action",
	}

	for _, name := range expectedCommands {
		if !strings.Contains(helpText, name) {
			t.Errorf("expected --help output to mention %q; got:\n%s", name, helpText)
		}
	}

	// Global flags should also appear in help.
	if !strings.Contains(helpText, "--json") {
		t.Errorf("expected --help output to mention --json flag")
	}
	if !strings.Contains(helpText, "--socket") {
		t.Errorf("expected --help output to mention --socket flag")
	}
	if !strings.Contains(helpText, "--home") {
		t.Errorf("expected --help output to mention --home flag")
	}
}

// ---------------------------------------------------------------------------
// TestVersionCommand_ExecuteDirect ensures the Execute path works end-to-end
// via cobra's Execute() method returning nil error for the version command.
// ---------------------------------------------------------------------------
func TestVersionCommand_ExecuteDirect(t *testing.T) {
	resetAgentCmd()

	stdout, stderr, err := executeCmd("version")
	if err != nil {
		t.Fatalf("'agent version' should not return an error: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "CLI:") {
		t.Errorf("expected stdout to contain 'CLI:'; got:\n%s", stdout)
	}
}