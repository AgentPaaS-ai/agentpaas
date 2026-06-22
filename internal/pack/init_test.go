package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitScaffoldNewDir(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "new-agent")

	if err := InitScaffold(projectDir, RuntimePython); err != nil {
		t.Fatalf("InitScaffold() error = %v", err)
	}

	for _, name := range []string{"agent.yaml", "main.py", "requirements.txt", ".agentpaasignore"} {
		if _, err := os.Lstat(filepath.Join(projectDir, name)); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
	}
}

func TestInitScaffoldExistingDir(t *testing.T) {
	projectDir := t.TempDir()

	if err := InitScaffold(projectDir, RuntimePython); err != nil {
		t.Fatalf("InitScaffold() error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(projectDir, "agent.yaml")); err != nil {
		t.Fatalf("expected agent.yaml to exist: %v", err)
	}
}

func TestInitScaffoldAgentYAMLExists(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", "name: existing\n")

	err := InitScaffold(projectDir, RuntimePython)
	if err == nil {
		t.Fatal("InitScaffold() error = nil, want error")
	}
}

func TestInitScaffoldPython(t *testing.T) {
	projectDir := t.TempDir()

	if err := InitScaffold(projectDir, RuntimePython); err != nil {
		t.Fatalf("InitScaffold() error = %v", err)
	}

	content := readTestFile(t, projectDir, "agent.yaml")
	if !strings.Contains(content, "runtime: python3.12") {
		t.Fatalf("agent.yaml = %q, want runtime: python3.12", content)
	}
}

func TestInitScaffoldLangGraph(t *testing.T) {
	projectDir := t.TempDir()

	if err := InitScaffold(projectDir, RuntimeLangGraph); err != nil {
		t.Fatalf("InitScaffold() error = %v", err)
	}

	content := readTestFile(t, projectDir, "agent.yaml")
	if !strings.Contains(content, "runtime: langgraph") {
		t.Fatalf("agent.yaml = %q, want runtime: langgraph", content)
	}
}

func TestDefaultAgentYAML(t *testing.T) {
	content := DefaultAgentYAML(RuntimeCrewAI)

	for _, want := range []string{"name:", "version:", "runtime: crewai", "entry:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("DefaultAgentYAML() = %q, want %q", content, want)
		}
	}
}

func TestDefaultAgentPaasIgnore(t *testing.T) {
	content := DefaultAgentPaasIgnore()

	for _, want := range []string{".git", "__pycache__", "*.pyc", ".venv", "node_modules", ".pytest_cache", ".mypy_cache", ".ruff_cache", "dist", "build", "*.egg-info", ".env", ".DS_Store"} {
		if !strings.Contains(content, want) {
			t.Fatalf("DefaultAgentPaasIgnore() missing %q in %q", want, content)
		}
	}
}

func TestDefaultMainPy(t *testing.T) {
	content := DefaultMainPy()
	if !strings.Contains(content, "def app") {
		t.Fatalf("DefaultMainPy() = %q, want def app", content)
	}
}

func readTestFile(t *testing.T, dir string, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read test file %s: %v", name, err)
	}

	return string(data)
}
