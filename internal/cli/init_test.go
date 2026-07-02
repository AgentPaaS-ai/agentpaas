package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCmdBasic(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "agent")
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"init", projectDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, name := range []string{"agent.yaml", "main.py", "requirements.txt", ".agentpaasignore"} {
		if _, err := os.Lstat(filepath.Join(projectDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestInitCmdExplicitRuntime(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"init", projectDir, "--runtime", "langgraph"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "agent.yaml")
	if !strings.Contains(content, "runtime: langgraph") {
		t.Fatalf("agent.yaml = %q, want runtime: langgraph", content)
	}
}

func TestInitCmdExistingAgentYAML(t *testing.T) {
	projectDir := t.TempDir()
	writeCLITestFile(t, projectDir, "agent.yaml", "name: existing\n")
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"init", projectDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestInitCmdJSONOutput(t *testing.T) {
	projectDir := t.TempDir()
	out := new(bytes.Buffer)
	cmd := freshCmd()
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--json", "init", projectDir, "--runtime", "crewai"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result struct {
		ProjectDir string `json:"project_dir"`
		Runtime    string `json:"runtime"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %q", err, out.String())
	}
	if result.ProjectDir != projectDir {
		t.Fatalf("project_dir = %q, want %q", result.ProjectDir, projectDir)
	}
	if result.Runtime != "crewai" {
		t.Fatalf("runtime = %q, want crewai", result.Runtime)
	}
}

func writeCLITestFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", name, err)
	}
}

func readCLITestFile(t *testing.T, dir string, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read test file %s: %v", name, err)
	}

	return string(data)
}
