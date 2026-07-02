package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const validDefaultDenyPolicy = `version: "1"
agent:
  name: ""
  description: ""
egress: []
credentials: []
mcp_servers: []
hooks: []
ingress: []
`

func TestValidateAgentProject_Ready(t *testing.T) {
	projectDir := t.TempDir()
	writeOperatorTestFile(t, projectDir, "agent.yaml", "version: \"1\"\nruntime: python\nname: test\n")
	writeOperatorTestFile(t, projectDir, "policy.yaml", validDefaultDenyPolicy)

	resp, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject() error = %v", err)
	}
	if !resp.GetReady() {
		t.Fatalf("Ready = false, issues = %v", resp.GetIssues())
	}
	if resp.GetRuntime() != "python" {
		t.Fatalf("Runtime = %q, want python", resp.GetRuntime())
	}
	if len(resp.GetIssues()) != 0 {
		t.Fatalf("Issues = %v, want empty", resp.GetIssues())
	}
	if resp.GetSchemaVersion() != operator.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", resp.GetSchemaVersion(), operator.SchemaVersion)
	}
}

func TestValidateAgentProject_MissingAgentYaml(t *testing.T) {
	projectDir := t.TempDir()
	writeOperatorTestFile(t, projectDir, "policy.yaml", validDefaultDenyPolicy)

	resp, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject() error = %v", err)
	}
	assertOperatorIssue(t, resp, string(operator.ErrDependencyConflict))
}

func TestValidateAgentProject_MissingPolicyYaml(t *testing.T) {
	projectDir := t.TempDir()
	writeOperatorTestFile(t, projectDir, "agent.yaml", "version: \"1\"\nruntime: python\nname: test\n")

	resp, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject() error = %v", err)
	}
	assertOperatorIssue(t, resp, string(operator.ErrPolicyValidationFailed))
}

func TestValidateAgentProject_InvalidPolicy(t *testing.T) {
	projectDir := t.TempDir()
	writeOperatorTestFile(t, projectDir, "agent.yaml", "version: \"1\"\nruntime: python\nname: test\n")
	writeOperatorTestFile(t, projectDir, "policy.yaml", "version: \"1\"\negress: \"not-a-list\"\n")

	resp, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject() error = %v", err)
	}
	assertOperatorIssue(t, resp, string(operator.ErrPolicyValidationFailed))
}

func TestValidateAgentProject_EmptyProjectPath(t *testing.T) {
	_, err := (&controlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("status.Code(error) = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func assertOperatorIssue(t *testing.T, resp *controlv1.ValidateAgentProjectResponse, category string) {
	t.Helper()

	if resp.GetReady() {
		t.Fatal("Ready = true, want false")
	}
	if len(resp.GetIssues()) != 1 {
		t.Fatalf("Issues = %v, want one issue", resp.GetIssues())
	}
	if resp.GetIssues()[0].GetCategory() != category {
		t.Fatalf("Category = %q, want %q", resp.GetIssues()[0].GetCategory(), category)
	}
	if resp.GetIssues()[0].GetNextAction() != string(operator.ActionFixCode) {
		t.Fatalf("NextAction = %q, want %q", resp.GetIssues()[0].GetNextAction(), operator.ActionFixCode)
	}
}

func writeOperatorTestFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", name, err)
	}
}
