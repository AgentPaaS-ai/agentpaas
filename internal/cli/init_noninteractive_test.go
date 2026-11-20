package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestInitFromCodeNoninteractive_CreatesAgentYamlAndPolicy(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "My Agent")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatalf("os.Mkdir() error = %v", err)
	}
	writeCLITestFile(t, projectDir, "main.py", "def app(input):\n    return input\n")

	if err := pack.InitFromCode(projectDir, pack.RuntimePython); err != nil {
		t.Fatalf("InitFromCode() error = %v", err)
	}
	if err := pack.InitPolicy(projectDir); err != nil {
		t.Fatalf("InitPolicy() error = %v", err)
	}

	agentYAML := readCLITestFile(t, projectDir, "agent.yaml")
	for _, want := range []string{
		`version: "1.0"`,
		"runtime: python",
		"name: my-agent",
		`description: ""`,
	} {
		if !strings.Contains(agentYAML, want) {
			t.Fatalf("agent.yaml = %q, want %q", agentYAML, want)
		}
	}

	policyYAML := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1.0"`,
		"agent:",
		`name: ""`,
		"egress: []",
		"credentials: []",
		"mcp_servers: []",
		"hooks: []",
		"ingress: []",
	} {
		if !strings.Contains(policyYAML, want) {
			t.Fatalf("policy.yaml = %q, want %q", policyYAML, want)
		}
	}
}

func TestInitFromCodeNoninteractive_ExistingPolicyUntouched(t *testing.T) {
	projectDir := t.TempDir()
	const existingPolicy = "version: \"1\"\negress:\n  - domain: example.com\n"
	writeCLITestFile(t, projectDir, "policy.yaml", existingPolicy)

	if err := pack.InitPolicy(projectDir); err != nil {
		t.Fatalf("InitPolicy() error = %v", err)
	}

	if got := readCLITestFile(t, projectDir, "policy.yaml"); got != existingPolicy {
		t.Fatalf("policy.yaml changed:\ngot:  %q\nwant: %q", got, existingPolicy)
	}
}

func TestValidateInitProjectPath_AbsolutePath(t *testing.T) {
	// Absolute paths should be accepted (resolved via filepath.Abs)
	abs, err := validateInitProjectPath("/tmp/test-project")
	if err != nil {
		t.Fatalf("validateInitProjectPath() error = %v", err)
	}
	if abs != "/tmp/test-project" {
		t.Fatalf("validateInitProjectPath() = %q, want /tmp/test-project", abs)
	}
}

func TestValidateInitProjectPath_NullByte(t *testing.T) {
	_, err := validateInitProjectPath("test\x00path")
	if err == nil {
		t.Fatal("validateInitProjectPath() error = nil for null byte")
	}
}
