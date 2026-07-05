package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyInit_DenyAll(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "deny-all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1"`,
		"agent:",
		`name: ""`,
		"egress: []",
		"credentials: []",
		"mcp_servers: []",
		"hooks: []",
		"ingress: []",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("policy.yaml missing %q; content:\n%s", want, content)
		}
	}
}

func TestPolicyInit_AllowHTTP(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-http"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1"`,
		`domain: "*"`,
		"ports:",
		"443",
		"allow_wildcard: true",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("policy.yaml missing %q; content:\n%s", want, content)
		}
	}
}

func TestPolicyInit_AllowLLM(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-llm"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1"`,
		"openrouter.ai",
		"443",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("policy.yaml missing %q; content:\n%s", want, content)
		}
	}
}

func TestPolicyInit_AllowMCP(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-mcp"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1"`,
		"mcp_servers:",
		"default-mcp",
		"http://localhost:3000",
		"transport: http",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("policy.yaml missing %q; content:\n%s", want, content)
		}
	}
}

func TestPolicyInit_RefusesExisting(t *testing.T) {
	projectDir := t.TempDir()
	const existingPolicy = "version: \"1\"\negress:\n  - domain: example.com\n"
	writeCLITestFile(t, projectDir, "policy.yaml", existingPolicy)

	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "deny-all"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error for existing policy.yaml")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %q, want 'already exists'", err.Error())
	}

	// Verify the file was not overwritten.
	got := readCLITestFile(t, projectDir, "policy.yaml")
	if got != existingPolicy {
		t.Fatalf("policy.yaml changed:\ngot:  %q\nwant: %q", got, existingPolicy)
	}
}

func TestPolicyInit_Force(t *testing.T) {
	projectDir := t.TempDir()
	const existingPolicy = "version: \"1\"\negress:\n  - domain: example.com\n"
	writeCLITestFile(t, projectDir, "policy.yaml", existingPolicy)

	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "deny-all", "--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	if strings.Contains(content, "example.com") {
		t.Fatalf("policy.yaml was not overwritten; content:\n%s", content)
	}
	if !strings.Contains(content, "egress: []") {
		t.Fatalf("policy.yaml does not have deny-all content; content:\n%s", content)
	}
}

func TestPolicyInit_Noninteractive(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--noninteractive"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	if !strings.Contains(content, "egress: []") {
		t.Fatalf("noninteractive should produce deny-all; content:\n%s", content)
	}
	if !strings.Contains(content, `version: "1"`) {
		t.Fatalf("noninteractive should produce version 1; content:\n%s", content)
	}
}

func TestPolicyInit_InvalidTemplate(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown policy template") {
		t.Fatalf("error = %q, want 'unknown policy template'", err.Error())
	}

	// Verify no policy.yaml was created.
	if _, statErr := os.Lstat(filepath.Join(projectDir, "policy.yaml")); statErr == nil {
		t.Fatal("policy.yaml should not have been created for invalid template")
	}
}

func TestPolicyInit_JSON(t *testing.T) {
	projectDir := t.TempDir()

	stdout := captureStdout(t, func() {
		cmd := freshCmd()
		var outBuf, errBuf bytes.Buffer
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetArgs([]string{"--json", "policy", "init", projectDir, "--template", "allow-llm"})
		_ = cmd.Execute()
	})

	var result struct {
		Template string `json:"template"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %q", err, stdout)
	}
	if result.Template != "allow-llm" {
		t.Fatalf("template = %q, want allow-llm", result.Template)
	}
	if result.Path == "" {
		t.Fatal("path must not be empty")
	}
	// Verify the file was actually created.
	content := readCLITestFile(t, projectDir, "policy.yaml")
	if !strings.Contains(content, "openrouter.ai") {
		t.Fatalf("policy.yaml does not contain expected content; content:\n%s", content)
	}
}

func TestPolicyInit_ProjectDirDefault(t *testing.T) {
	// Create an empty dir and run from within it using the default "." path.
	projectDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", "--template", "deny-all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	if !strings.Contains(content, "egress: []") {
		t.Fatalf("default project dir policy.yaml; content:\n%s", content)
	}
}

func TestPolicyInit_AllowLLM_ProviderXAI(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-llm", "--provider", "xai"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	for _, want := range []string{
		`version: "1"`,
		"api.x.ai",
		"443",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("policy.yaml missing %q; content:\n%s", want, content)
		}
	}
	// Verify no other provider domains leaked in.
	if strings.Contains(content, "openrouter.ai") {
		t.Fatalf("policy.yaml should not contain openrouter.ai; content:\n%s", content)
	}
}

func TestPolicyInit_AllowLLM_ProviderOpenAI(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-llm", "--provider", "openai"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	content := readCLITestFile(t, projectDir, "policy.yaml")
	if !strings.Contains(content, "api.openai.com") {
		t.Fatalf("policy.yaml missing api.openai.com; content:\n%s", content)
	}
	if !strings.Contains(content, "443") {
		t.Fatalf("policy.yaml missing 443; content:\n%s", content)
	}
}

func TestPolicyInit_AllowLLM_ProviderInvalid(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "allow-llm", "--provider", "invalid"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error = %q, want 'unknown provider'", err.Error())
	}

	// Verify no policy.yaml was created.
	if _, statErr := os.Lstat(filepath.Join(projectDir, "policy.yaml")); statErr == nil {
		t.Fatal("policy.yaml should not have been created for invalid provider")
	}
}

func TestPolicyInit_ProviderWithNonAllowLLM(t *testing.T) {
	projectDir := t.TempDir()
	cmd := freshCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"policy", "init", projectDir, "--template", "deny-all", "--provider", "openai"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error for --provider with non-allow-llm template")
	}
	if !strings.Contains(err.Error(), "--provider can only be used with --template allow-llm") {
		t.Fatalf("error = %q, want '--provider can only be used with --template allow-llm'", err.Error())
	}
}