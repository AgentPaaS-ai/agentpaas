package cli

import (
	"strings"
	"testing"
)

func TestCronAddCmd_RequiresArg(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "add"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when running cron add without arg, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("expected 'accepts 1 arg' error, got: %v", err)
	}
}

func TestCronAddCmd_RequiresExpr(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "add", "test-agent"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when running cron add without --expr, got nil")
	}
	if !strings.Contains(err.Error(), "required flag --expr is missing") {
		t.Errorf("expected '--expr is missing' error, got: %v", err)
	}
}

func TestCronListCmd_NoDaemon(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", "/nonexistent/agentpaas-test.sock")

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "list"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when daemon not running, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") &&
		!strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestCronRemoveCmd_RequiresArg(t *testing.T) {
	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "remove"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when running cron remove without arg, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("expected 'accepts 1 arg' error, got: %v", err)
	}
}

func TestCronAddCmd_NoDaemon(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", "/nonexistent/agentpaas-test.sock")

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "add", "test-agent", "--expr", "*/5 * * * *"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when daemon not running, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") &&
		!strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestCronRemoveCmd_NoDaemon(t *testing.T) {
	t.Setenv("AGENTPAAS_SOCKET", "/nonexistent/agentpaas-test.sock")

	resetAgentCmd()
	cmd := freshCmd()
	cmd.SetArgs([]string{"cron", "remove", "sched-123"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when daemon not running, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") &&
		!strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected connection error, got: %v", err)
	}
}
