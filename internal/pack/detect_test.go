package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPlainPython(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "main.py", "def app(input):\n    return input\n")
	writeTestFile(t, projectDir, "requirements.txt", "requests\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimePython {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimePython)
	}
}

func TestDetectLangGraphViaRequirements(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "requirements.txt", "langgraph==0.2.1\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeLangGraph {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeLangGraph)
	}
}

func TestDetectLangGraphViaPyproject(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "pyproject.toml", "[project]\ndependencies = [\"langgraph\"]\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeLangGraph {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeLangGraph)
	}
}

func TestDetectLangGraphViaSource(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.py", "from langgraph import graph\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeLangGraph {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeLangGraph)
	}
}

func TestDetectCrewAIViaRequirements(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "requirements.txt", "crewai>=0.86\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeCrewAI {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeCrewAI)
	}
}

func TestDetectCrewAIViaSource(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "crew.py", "import crewai\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeCrewAI {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeCrewAI)
	}
}

func TestDetectExplicitRuntimeOverrides(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", "name: test\nruntime: python3.12\n")
	writeTestFile(t, projectDir, "requirements.txt", "langgraph==0.2.1\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimePython {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimePython)
	}
	if !result.ExplicitRuntime {
		t.Fatal("ExplicitRuntime = false, want true")
	}
}

func TestDetectNoAgentYAML(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "requirements.txt", "langgraph\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.HasAgentYAML {
		t.Fatal("HasAgentYAML = true, want false")
	}
	if result.Runtime != RuntimeLangGraph {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeLangGraph)
	}
}

func TestDetectExplicitRuntimeLangGraph(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", "name: test\nruntime: langgraph\n")

	result, err := DetectProject(projectDir)
	if err != nil {
		t.Fatalf("DetectProject() error = %v", err)
	}
	if result.Runtime != RuntimeLangGraph {
		t.Fatalf("Runtime = %q, want %q", result.Runtime, RuntimeLangGraph)
	}
	if !result.ExplicitRuntime {
		t.Fatal("ExplicitRuntime = false, want true")
	}
}

func TestLoadAgentYAMLNotExist(t *testing.T) {
	projectDir := t.TempDir()

	agentYAML, err := LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML() error = %v", err)
	}
	if agentYAML != nil {
		t.Fatalf("LoadAgentYAML() = %#v, want nil", agentYAML)
	}
}

func TestLoadAgentYAMLValid(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", "name: test-agent\nversion: 1.2.3\nruntime: crewai\nentry: main:app\ndescription: test\n")

	agentYAML, err := LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML() error = %v", err)
	}
	if agentYAML == nil {
		t.Fatal("LoadAgentYAML() = nil, want value")
	}
	if agentYAML.Name != "test-agent" || agentYAML.Version != "1.2.3" || agentYAML.Runtime != "crewai" || agentYAML.Entry != "main:app" || agentYAML.Description != "test" {
		t.Fatalf("LoadAgentYAML() = %#v, want parsed fields", agentYAML)
	}
}

func TestLoadAgentYAMLV1Schema(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", `apiVersion: v1
kind: Agent
metadata:
  name: weather-agent
  description: "Simple weather lookup agent"
spec:
  entrypoint: main.py
  runtime: python
`)

	agentYAML, err := LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML() error = %v", err)
	}
	if agentYAML == nil {
		t.Fatal("LoadAgentYAML() = nil, want value")
	}
	if agentYAML.Name != "weather-agent" || agentYAML.Runtime != "python" || agentYAML.Entry != "main.py" {
		t.Fatalf("LoadAgentYAML() = %#v, want v1 schema fields", agentYAML)
	}
}

func TestLoadAgentYAMLInvalid(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", "name: [\n")

	agentYAML, err := LoadAgentYAML(projectDir)
	if err == nil {
		t.Fatalf("LoadAgentYAML() error = nil, value = %#v", agentYAML)
	}
}

func writeTestFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", name, err)
	}
}
