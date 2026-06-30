package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/home"
	"github.com/parvezsyed/agentpaas/internal/secrets"
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

func executeSecretCmd(t *testing.T, store secrets.SecretStore, input string, args ...string) (string, string, error) {
	t.Helper()
	resetAgentCmd()

	oldFactory := secretStoreFactory
	secretStoreFactory = func(cmd *cobra.Command) (secrets.SecretStore, error) {
		return store, nil
	}
	t.Cleanup(func() {
		secretStoreFactory = oldFactory
		resetAgentCmd()
	})

	cmd := AgentCmd()
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)

	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), err
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
// TestDaemonStart_ExitImmediate_NoPanic
// Tests: runDaemonStart returns an error (not a panic) when the daemon process
// exits immediately after Start().
// ---------------------------------------------------------------------------
func TestDaemonStart_ExitImmediate_NoPanic(t *testing.T) {
	tmpHome := t.TempDir()

	// Use a compiled binary (/usr/bin/false exits with code 1) instead of a
	// shell script. Shell scripts incur shell startup overhead which under
	// heavy parallel test contention (20+ test binaries) can exceed the grace
	// period, causing a false "success" and a flaky test. A compiled binary
	// exits within microseconds regardless of system load.
	fakeDaemon := "/usr/bin/false"
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("fake daemon binary only available on unix")
	}

	oldResolver := daemonBinaryResolver
	daemonBinaryResolver = func() (string, error) { return fakeDaemon, nil }
	t.Cleanup(func() {
		daemonBinaryResolver = oldResolver
		resetAgentCmd()
	})

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"daemon", "start", "--home", tmpHome})

	var execErr error
	_ = captureStdout(t, func() {
		execErr = cmd.Execute()
	})

	if execErr == nil {
		t.Fatal("expected error when daemon exits immediately, got nil")
	}
	errMsg := execErr.Error()
	if !strings.Contains(errMsg, "daemon exited immediately") {
		t.Fatalf("error should mention immediate exit; got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "exit code 1") {
		t.Fatalf("error should include exit code 1; got: %s", errMsg)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonStart_StaysAlive_Success
// Tests: runDaemonStart returns success when the daemon stays alive past the
// 500ms grace period.
// ---------------------------------------------------------------------------
func TestDaemonStart_StaysAlive_Success(t *testing.T) {
	tmpHome := t.TempDir()

	fakeDaemon := filepath.Join(t.TempDir(), "agentpaasd")
	if err := os.WriteFile(fakeDaemon, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}

	oldResolver := daemonBinaryResolver
	daemonBinaryResolver = func() (string, error) { return fakeDaemon, nil }
	t.Cleanup(func() {
		daemonBinaryResolver = oldResolver
		resetAgentCmd()
	})

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"daemon", "start", "--home", tmpHome})

	var execErr error
	stdout := captureStdout(t, func() {
		execErr = cmd.Execute()
	})

	if execErr != nil {
		t.Fatalf("expected success when daemon stays alive, got: %v", execErr)
	}
	if !strings.Contains(stdout, "Daemon is running") {
		t.Fatalf("output should mention daemon is running; got:\n%s", stdout)
	}

	var pid int
	if _, err := fmt.Sscanf(stdout, "Daemon started (PID %d)", &pid); err != nil {
		t.Fatalf("parse PID from output: %v; output:\n%s", err, stdout)
	}
	t.Cleanup(func() {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	})
}

// ---------------------------------------------------------------------------
// TestBuildDaemonStartCommand_RedirectsStdioToLogFile
// Tests: daemon subprocess stdout/stderr are redirected to daemon.log, not the
// terminal.
// ---------------------------------------------------------------------------
func TestBuildDaemonStartCommand_RedirectsStdioToLogFile(t *testing.T) {
	tmpHome := t.TempDir()
	paths := home.NewHomePaths(tmpHome)

	resetAgentCmd()
	root := freshCmd()
	if err := root.PersistentFlags().Set("home", tmpHome); err != nil {
		t.Fatalf("set home flag: %v", err)
	}
	root.PersistentFlags().Lookup("home").Changed = true

	daemonCmd := findSubCmd(root, "daemon")
	if daemonCmd == nil {
		t.Fatal("expected 'daemon' subcommand")
	}
	startCmd := findSubCmd(daemonCmd, "start")
	if startCmd == nil {
		t.Fatal("expected 'daemon start' subcommand")
	}

	cmdDaemon, logFile, err := buildDaemonStartCommand(startCmd, "/fake/agentpaasd", paths)
	if err != nil {
		t.Fatalf("buildDaemonStartCommand: %v", err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	if cmdDaemon.Stdout == os.Stdout {
		t.Error("daemon stdout must not be os.Stdout")
	}
	if cmdDaemon.Stderr == os.Stderr {
		t.Error("daemon stderr must not be os.Stderr")
	}
	if cmdDaemon.Stdin != nil {
		t.Error("daemon stdin should be nil")
	}

	logPath := filepath.Join(paths.Logs, "daemon.log")
	stdoutFile, ok := cmdDaemon.Stdout.(*os.File)
	if !ok {
		t.Fatalf("stdout should be *os.File, got %T", cmdDaemon.Stdout)
	}
	if stdoutFile.Name() != logPath {
		t.Errorf("stdout file = %q, want %q", stdoutFile.Name(), logPath)
	}
	if stderrFile, ok := cmdDaemon.Stderr.(*os.File); !ok || stderrFile.Name() != logPath {
		t.Errorf("stderr should point to %s", logPath)
	}
}

// ---------------------------------------------------------------------------
// TestBuildDaemonStartCommand_DedupesEnvVars
// Tests: when AGENTPAAS_HOME is already in the environment and --home is passed,
// the daemon subprocess env has exactly one AGENTPAAS_HOME with the flag value.
// ---------------------------------------------------------------------------
func TestBuildDaemonStartCommand_DedupesEnvVars(t *testing.T) {
	tmpHome := t.TempDir()
	paths := home.NewHomePaths(tmpHome)

	t.Setenv("AGENTPAAS_HOME", "/old/home")
	t.Setenv("AGENTPAAS_SOCKET", "/old/socket.sock")

	resetAgentCmd()
	root := freshCmd()
	if err := root.PersistentFlags().Set("home", tmpHome); err != nil {
		t.Fatalf("set home flag: %v", err)
	}
	root.PersistentFlags().Lookup("home").Changed = true

	daemonCmd := findSubCmd(root, "daemon")
	startCmd := findSubCmd(daemonCmd, "start")

	cmdDaemon, logFile, err := buildDaemonStartCommand(startCmd, "/fake/agentpaasd", paths)
	if err != nil {
		t.Fatalf("buildDaemonStartCommand: %v", err)
	}
	_ = logFile.Close()

	var homeCount int
	var homeValue string
	var socketCount int
	for _, e := range cmdDaemon.Env {
		if strings.HasPrefix(e, "AGENTPAAS_HOME=") {
			homeCount++
			homeValue = strings.TrimPrefix(e, "AGENTPAAS_HOME=")
		}
		if strings.HasPrefix(e, "AGENTPAAS_SOCKET=") {
			socketCount++
		}
	}

	if homeCount != 1 {
		t.Fatalf("expected exactly one AGENTPAAS_HOME, got %d", homeCount)
	}
	if homeValue != tmpHome {
		t.Errorf("AGENTPAAS_HOME = %q, want %q", homeValue, tmpHome)
	}
	if socketCount != 1 {
		t.Fatalf("expected exactly one AGENTPAAS_SOCKET, got %d", socketCount)
	}
}

// ---------------------------------------------------------------------------
// TestDaemonStart_ReturnsPromptly
// Tests: runDaemonStart returns within a few seconds when the daemon stays
// alive (does not hang waiting on inherited terminal FDs).
// ---------------------------------------------------------------------------
func TestDaemonStart_ReturnsPromptly(t *testing.T) {
	tmpHome := t.TempDir()

	fakeDaemon := filepath.Join(t.TempDir(), "agentpaasd")
	if err := os.WriteFile(fakeDaemon, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatalf("write fake daemon: %v", err)
	}

	oldResolver := daemonBinaryResolver
	daemonBinaryResolver = func() (string, error) { return fakeDaemon, nil }
	t.Cleanup(func() {
		daemonBinaryResolver = oldResolver
		resetAgentCmd()
	})

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"daemon", "start", "--home", tmpHome})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Execute()
	}()

	select {
	case execErr := <-done:
		if execErr != nil {
			t.Fatalf("expected success, got: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon start hung — did not return within 5 seconds")
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
		{"secret", "secret"},
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
		"secret",
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

func TestSecretSetReadsFromStdinNeverArgv(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	secretValue := "sensitive-value-from-stdin"

	stdout, stderr, err := executeSecretCmd(t, store, secretValue, "secret", "set", "mykey")
	if err != nil {
		t.Fatalf("secret set returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got, err := store.Get(context.Background(), "mykey")
	if err != nil {
		t.Fatalf("Get mykey: %v", err)
	}
	if string(got) != secretValue {
		t.Fatalf("stored value = %q, want %q", got, secretValue)
	}

	processListFixture := "agent secret set mykey"
	if strings.Contains(processListFixture, secretValue) {
		t.Fatalf("process list fixture leaked secret value: %s", processListFixture)
	}
	if strings.Contains(strings.Join(os.Args, " "), secretValue) {
		t.Fatalf("os.Args leaked secret value: %v", os.Args)
	}

	_, _, err = executeSecretCmd(t, secrets.NewFakeKeyStore(), "", "secret", "set", "mykey", secretValue)
	if err == nil {
		t.Fatal("secret set accepted a value through argv; want error")
	}
}

func TestSecretSetRejectsOversizeBeforeStorage(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	oversize := strings.Repeat("x", secrets.MaxSecretValueSize+1)

	_, _, err := executeSecretCmd(t, store, oversize, "secret", "set", "too_large")
	if err == nil {
		t.Fatal("secret set oversize value: want error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds 65536 byte limit") {
		t.Fatalf("oversize error = %v, want clear size limit", err)
	}

	_, err = store.Get(context.Background(), "too_large")
	if !errorsIsSecretNotFound(err) {
		t.Fatalf("Get after oversize set error = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretNameValidationBeforeStoreInteraction(t *testing.T) {
	resetAgentCmd()
	calls := 0
	oldFactory := secretStoreFactory
	secretStoreFactory = func(cmd *cobra.Command) (secrets.SecretStore, error) {
		calls++
		return secrets.NewFakeKeyStore(), nil
	}
	t.Cleanup(func() {
		secretStoreFactory = oldFactory
		resetAgentCmd()
	})

	cmd := AgentCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetIn(strings.NewReader("value"))
	cmd.SetArgs([]string{"secret", "set", "bad name"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("secret set with invalid name: want error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid secret name") {
		t.Fatalf("invalid-name error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("secret store factory called %d times before name validation, want 0", calls)
	}
}

func TestSecretListOutputsMetadataOnly(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	secretValue := "never-render-this-secret"
	if err := store.Set(context.Background(), "display_only", []byte(secretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.TouchLastUsed(context.Background(), "display_only"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "list")
	if err != nil {
		t.Fatalf("secret list returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	for _, want := range []string{"NAME", "CREATED_AT", "UPDATED_AT", "LAST_USED_AT", "REFERENCED_BY", "display_only"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("secret list output missing %q:\n%s", want, stdout)
		}
	}
	for _, forbidden := range []string{secretValue, "never-render", "secret"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("secret list output leaked forbidden value %q:\n%s", forbidden, stdout)
		}
	}
}

func TestSecretRmDeletesSecret(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	if err := store.Set(context.Background(), "remove_me", []byte("delete-value")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "rm", "remove_me")
	if err != nil {
		t.Fatalf("secret rm returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	_, err = store.Get(context.Background(), "remove_me")
	if !errorsIsSecretNotFound(err) {
		t.Fatalf("Get after rm error = %v, want ErrSecretNotFound", err)
	}
}

func TestSecretRotateReplacesValue(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	oldValue := "old-secret-value"
	if err := store.Set(context.Background(), "rotate_key", []byte(oldValue)); err != nil {
		t.Fatalf("Set old value: %v", err)
	}

	newValue := "new-secret-value"
	stdout, stderr, err := executeSecretCmd(t, store, newValue, "secret", "rotate", "rotate_key")
	if err != nil {
		t.Fatalf("secret rotate returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	got, err := store.Get(context.Background(), "rotate_key")
	if err != nil {
		t.Fatalf("Get rotate_key: %v", err)
	}
	if string(got) != newValue {
		t.Fatalf("stored value = %q, want %q", got, newValue)
	}

	if !strings.Contains(stdout, "rotated") {
		t.Fatalf("output does not contain 'rotated':\n%s", stdout)
	}
	if strings.Contains(stdout, oldValue) {
		t.Fatalf("output leaked old value %q:\n%s", oldValue, stdout)
	}
	if strings.Contains(stdout, newValue) {
		t.Fatalf("output leaked new value %q:\n%s", newValue, stdout)
	}
}

func TestSecretRotateRejectsOversizePreservesOld(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	oldValue := "preserved-old-value"
	if err := store.Set(context.Background(), "rotate_oversize", []byte(oldValue)); err != nil {
		t.Fatalf("Set old value: %v", err)
	}

	oversize := strings.Repeat("x", secrets.MaxSecretValueSize+1)
	_, _, err := executeSecretCmd(t, store, oversize, "secret", "rotate", "rotate_oversize")
	if err == nil {
		t.Fatal("secret rotate oversize value: want error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds 65536 byte limit") {
		t.Fatalf("oversize error = %v, want clear size limit", err)
	}

	got, err := store.Get(context.Background(), "rotate_oversize")
	if err != nil {
		t.Fatalf("Get rotate_oversize after oversize rotate: %v", err)
	}
	if string(got) != oldValue {
		t.Fatalf("old value = %q, want preserved %q", got, oldValue)
	}
}

func errorsIsSecretNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), secrets.ErrSecretNotFound.Error())
}
func TestSecretTest_CommandExists(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	testCmd, _, err := cmd.Find([]string{"secret", "test"})
	if err != nil {
		t.Fatalf("Find secret test: %v", err)
	}
	if testCmd == nil {
		t.Fatal("secret test command not found")
	}

	// Verify it is indeed the test subcommand by checking the name.
	if testCmd.Name() != "test" {
		t.Fatalf("unexpected command name: %s", testCmd.Name())
	}
}

func TestSecretTest_NeverPrintsValue(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	secretValue := "sk-this-must-never-leak-abc123"
	if err := store.Set(context.Background(), "openai-key", []byte(secretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Set up a mock server that returns 401 (invalid key).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	restore := secrets.SetTestEndpoints(srv.URL, srv.URL, srv.URL)
	defer restore()

	stdout, stderr, _ := executeSecretCmd(t, store, "", "secret", "test", "openai-key")

	// Neither stdout nor stderr should contain the secret value.
	if strings.Contains(stdout, secretValue) {
		t.Fatalf("stdout leaked secret value:\n%s", stdout)
	}
	if strings.Contains(stderr, secretValue) {
		t.Fatalf("stderr leaked secret value:\n%s", stderr)
	}
}

