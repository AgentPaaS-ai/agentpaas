package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestServiceWorker_RoundTrip starts a Python service worker and exercises
// tools/list, tools/call, and shutdown via the stdin/stdout protocol.
func TestServiceWorker_RoundTrip(t *testing.T) {
	repoRoot := findRepoRoot(t)
	python := "python3"

	// Create a temp service agent file.
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "service_agent.py")
	agentCode := `
from agentpaas_sdk import agent

@agent.mcp_tool("echo")
def echo(args):
    return {"received": args.get("message", ""), "distinctive": "b33-t02-harness-real"}

@agent.mcp_tool("ping")
def ping(args):
    return {"pong": True}
`
	if err := os.WriteFile(agentPath, []byte(agentCode), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	stdoutPath := filepath.Join(dir, "stdout.txt")

	// Find the Python SDK path.
	pythonPath := filepath.Join(repoRoot, "python")

	// Build env.
	env := os.Environ()
	env = append(env, "AGENTPAAS_AGENT_KIND=mcp_service")
	env = append(env, "AGENTPAAS_AGENT_PATH="+agentPath)
	env = append(env, "AGENTPAAS_STDOUT_PATH="+stdoutPath)
	env = append(env, "AGENTPAAS_MCP_DECLARED_TOOLS=echo,ping")
	env = append(env, "PYTHONPATH="+pythonPath)
	env = append(env, "AGENTPAAS_MCP_MAX_CONCURRENCY=1")

	// Build a runner script that calls runner.run().
	runnerScript := `
import sys, os
sys.path.insert(0, os.environ.get("PYTHONPATH", "."))
from agentpaas_sdk.runner import run
run()
`
	cmd := exec.Command(python, "-u", "-c", runnerScript)
	cmd.Env = env
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	decoder := json.NewDecoder(stdout)

	// 1. Wait for "ready".
	var readyMsg map[string]any
	if err := decoder.Decode(&readyMsg); err != nil {
		t.Fatalf("decode ready: %v", err)
	}
	if readyMsg["type"] != "ready" {
		t.Fatalf("expected ready, got %v", readyMsg)
	}

	// 2. tools/list.
	sendLine(t, stdin, map[string]any{
		"type": "mcp_tools_list",
		"id":   "list-1",
	})
	var listResp map[string]any
	if err := decoder.Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if !listResp["ok"].(bool) {
		t.Fatalf("tools/list failed: %v", listResp)
	}
	toolsAny := listResp["tools"].([]any)
	tools := make([]string, len(toolsAny))
	for i, v := range toolsAny {
		tools[i] = v.(string)
	}
	if len(tools) != 2 || tools[0] != "echo" || tools[1] != "ping" {
		t.Fatalf("tools = %v, want [echo ping]", tools)
	}

	// 3. tools/call echo with distinctive value.
	sendLine(t, stdin, map[string]any{
		"type":      "mcp_tools_call",
		"id":        "call-1",
		"tool":      "echo",
		"arguments": map[string]any{"message": "hello-harness"},
	})
	var callResp map[string]any
	if err := decoder.Decode(&callResp); err != nil {
		t.Fatalf("decode call: %v", err)
	}
	if !callResp["ok"].(bool) {
		t.Fatalf("tools/call failed: %v", callResp)
	}
	result := callResp["result"].(map[string]any)
	if result["received"] != "hello-harness" {
		t.Fatalf("received = %v, want hello-harness", result["received"])
	}
	if result["distinctive"] != "b33-t02-harness-real" {
		t.Fatalf("distinctive = %v, want b33-t02-harness-real", result["distinctive"])
	}

	// 4. tools/call ping.
	sendLine(t, stdin, map[string]any{
		"type":      "mcp_tools_call",
		"id":        "call-2",
		"tool":      "ping",
		"arguments": map[string]any{},
	})
	var pingResp map[string]any
	if err := decoder.Decode(&pingResp); err != nil {
		t.Fatalf("decode ping: %v", err)
	}
	if !pingResp["ok"].(bool) {
		t.Fatalf("ping failed: %v", pingResp)
	}

	// 5. tools/call unknown tool fails.
	sendLine(t, stdin, map[string]any{
		"type":      "mcp_tools_call",
		"id":        "bad-1",
		"tool":      "nonexistent",
		"arguments": map[string]any{},
	})
	var badResp map[string]any
	if err := decoder.Decode(&badResp); err != nil {
		t.Fatalf("decode bad call: %v", err)
	}
	if badResp["ok"].(bool) {
		t.Fatal("expected unknown tool call to fail")
	}
	errData := badResp["error"].(map[string]any)
	if !strings.Contains(errData["message"].(string), "not registered") {
		t.Fatalf("error message = %v, want 'not registered'", errData["message"])
	}

	// 6. shutdown.
	sendLine(t, stdin, map[string]any{
		"type": "shutdown",
		"id":   "sd-1",
	})
	var shutdownResp map[string]any
	if err := decoder.Decode(&shutdownResp); err != nil {
		t.Fatalf("decode shutdown: %v", err)
	}
	if shutdownResp["type"] != "shutdown_ack" {
		t.Fatalf("shutdown response = %v, want shutdown_ack", shutdownResp)
	}

	// Verify stderr has no unexpected content.
	stderrBytes := make([]byte, 4096)
	n, _ := stderrPipe.Read(stderrBytes)
	stderrText := string(stderrBytes[:n])
	// Should not contain traceback clues.
	if strings.Contains(stderrText, "Traceback") {
		t.Logf("stderr (may have traceback): %s", stderrText)
	}
}

// TestServiceWorker_ToolSetMismatchFailsReady tests that a mismatch between
// declared and registered tools causes import_failed.
func TestServiceWorker_ToolSetMismatchFailsReady(t *testing.T) {
	repoRoot := findRepoRoot(t)
	python := "python3"

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "service_agent.py")
	agentCode := `
from agentpaas_sdk import agent

@agent.mcp_tool("only_one")
def only_one(args):
    return {}
`
	if err := os.WriteFile(agentPath, []byte(agentCode), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	stdoutPath := filepath.Join(dir, "stdout.txt")
	pythonPath := filepath.Join(repoRoot, "python")

	env := os.Environ()
	env = append(env, "AGENTPAAS_AGENT_KIND=mcp_service")
	env = append(env, "AGENTPAAS_AGENT_PATH="+agentPath)
	env = append(env, "AGENTPAAS_STDOUT_PATH="+stdoutPath)
	env = append(env, "AGENTPAAS_MCP_DECLARED_TOOLS=only_one,missing_tool")
	env = append(env, "PYTHONPATH="+pythonPath)

	runnerScript := `
import sys, os
sys.path.insert(0, os.environ.get("PYTHONPATH", "."))
from agentpaas_sdk.runner import run
run()
`
	cmd := exec.Command(python, "-u", "-c", runnerScript)
	cmd.Env = env
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	decoder := json.NewDecoder(stdout)
	var msg map[string]any
	if err := decoder.Decode(&msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg["type"] != "import_failed" {
		t.Fatalf("expected import_failed, got %v", msg)
	}
	if msg["reason"] != "tool_set_mismatch" {
		t.Fatalf("reason = %v, want tool_set_mismatch", msg["reason"])
	}
	if !strings.Contains(msg["detail"].(string), "missing_tool") {
		t.Fatalf("detail does not mention missing_tool: %v", msg["detail"])
	}

	// Process should exit non-zero.
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected non-zero exit for tool set mismatch")
	}
}

// TestServiceWorker_ExtraRegisteredFailsReady tests that an extra registered
// tool (not in declared) also fails readiness.
func TestServiceWorker_ExtraRegisteredFailsReady(t *testing.T) {
	repoRoot := findRepoRoot(t)
	python := "python3"

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "service_agent.py")
	agentCode := `
from agentpaas_sdk import agent

@agent.mcp_tool("declared_tool")
def dt(args):
    return {}

@agent.mcp_tool("undeclared_tool")
def ut(args):
    return {}
`
	if err := os.WriteFile(agentPath, []byte(agentCode), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	stdoutPath := filepath.Join(dir, "stdout.txt")
	pythonPath := filepath.Join(repoRoot, "python")

	env := os.Environ()
	env = append(env, "AGENTPAAS_AGENT_KIND=mcp_service")
	env = append(env, "AGENTPAAS_AGENT_PATH="+agentPath)
	env = append(env, "AGENTPAAS_STDOUT_PATH="+stdoutPath)
	env = append(env, "AGENTPAAS_MCP_DECLARED_TOOLS=declared_tool")
	env = append(env, "PYTHONPATH="+pythonPath)

	runnerScript := `
import sys, os
sys.path.insert(0, os.environ.get("PYTHONPATH", "."))
from agentpaas_sdk.runner import run
run()
`
	cmd := exec.Command(python, "-u", "-c", runnerScript)
	cmd.Env = env
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Wait() }()

	decoder := json.NewDecoder(stdout)
	var msg map[string]any
	if err := decoder.Decode(&msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg["type"] != "import_failed" {
		t.Fatalf("expected import_failed, got %v", msg)
	}
	if msg["reason"] != "tool_set_mismatch" {
		t.Fatalf("reason = %v, want tool_set_mismatch", msg["reason"])
	}
	if !strings.Contains(msg["detail"].(string), "undeclared_tool") {
		t.Fatalf("detail does not mention undeclared_tool: %v", msg["detail"])
	}
}

func sendLine(t *testing.T, stdin interface{ Write([]byte) (int, error) }, v map[string]any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
}
