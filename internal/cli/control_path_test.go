package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

type capturePackServer struct {
	controlv1.UnimplementedControlServiceServer
	captured *controlv1.PackRequest
}

func (s *capturePackServer) Pack(_ context.Context, req *controlv1.PackRequest) (*controlv1.PackResponse, error) {
	s.captured = req
	return &controlv1.PackResponse{ImageDigest: "sha256:test"}, nil
}

func startCapturePackDaemon(t *testing.T) (string, *capturePackServer) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ap-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("Listen(unix): %v", err)
	}
	server := &capturePackServer{}
	grpcServer := grpc.NewServer()
	controlv1.RegisterControlServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = lis.Close()
	})
	return sock, server
}

func TestResolveRunTarget_BareDirectoryName(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	base := t.TempDir()
	projectDir := filepath.Join(base, "weather-agent")
	if err := pack.InitScaffold(projectDir, pack.RuntimePython); err != nil {
		t.Fatalf("InitScaffold: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	got, err := resolveRunTarget(cmd, nil, "weather-agent")
	if err != nil {
		t.Fatalf("resolveRunTarget() error = %v", err)
	}
	if got != "weather-agent" {
		t.Fatalf("resolveRunTarget() = %q, want %q", got, "weather-agent")
	}
}

func TestResolveRunTarget_AgentNameWhenNotDirectory(t *testing.T) {
	cmd := &cobra.Command{}
	got, err := resolveRunTarget(cmd, nil, "my-deployed-agent")
	if err != nil {
		t.Fatalf("resolveRunTarget() error = %v", err)
	}
	if got != "my-deployed-agent" {
		t.Fatalf("resolveRunTarget() = %q, want %q", got, "my-deployed-agent")
	}
}

func TestResolveCLIProjectPath_AbsoluteUnchanged(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "myagent")
	if err := os.Mkdir(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveCLIProjectPath(abs)
	if err != nil {
		t.Fatalf("resolveCLIProjectPath() error = %v", err)
	}
	if got != abs {
		t.Fatalf("resolveCLIProjectPath() = %q, want %q", got, abs)
	}
}

func TestPackCmd_SendsAbsoluteProjectPath(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	base := t.TempDir()
	projectDir := filepath.Join(base, "myagent")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(base, "work")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	sock, capture := startCapturePackDaemon(t)
	t.Setenv("AGENTPAAS_SOCKET", sock)

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"pack", "../myagent"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pack execute: %v", err)
	}
	if capture.captured == nil {
		t.Fatal("Pack RPC was not called")
	}
	want := filepath.Join(base, "myagent")
	if !pathsEqual(capture.captured.GetAgentProjectPath(), want) {
		t.Fatalf("AgentProjectPath = %q, want absolute %q", capture.captured.GetAgentProjectPath(), want)
	}
	if !filepath.IsAbs(capture.captured.GetAgentProjectPath()) {
		t.Fatalf("AgentProjectPath %q is not absolute", capture.captured.GetAgentProjectPath())
	}
}

// agentYAMLCustomPII is the user-observable agent.yaml that must not pack
// when policy.yaml is missing or has no mapping-form guardrails.pii.
const agentYAMLCustomPII = `name: weather-agent
version: "1.0"
runtime: python
entry: agent.py
guardrails:
  pii:
    action: reject
    builtins: [Ssn]
    patterns: ["\\d{3}-\\d{3}-\\d{4}"]
`

const policyYAMLNoMappingPII = `version: "1.0"
agent:
  name: weather-agent
egress:
  - domain: "api.weather.gov"
    ports: [443]
`

func assertCustomPIIPatternRefusal(t *testing.T, msg string) {
	t.Helper()
	for _, want := range []string{
		"guardrails.pii.patterns",
		"custom PII patterns are not supported",
		"CreditCard",
		"Ssn",
		"Email",
		"DriversLicense",
		"Key",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal %q missing %q", msg, want)
		}
	}
}

const policyYAMLSequenceGuardrails = `version: "1.0"
agent:
  name: weather-agent
egress:
  - domain: "api.weather.gov"
    ports: [443]
guardrails:
  - type: regex
    pattern: "(?i)password"
    action: block
`

func TestPackCmd_RefusesAgentYAMLCustomPIIPatternsBeforeImageBuild(t *testing.T) {
	cases := []struct {
		name   string
		policy string
	}{
		{name: "missing policy"},
		{name: "policy without mapping-form pii", policy: policyYAMLNoMappingPII},
		{name: "sequence-form guardrails", policy: policyYAMLSequenceGuardrails},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(agentYAMLCustomPII), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.policy != "" {
				if err := os.WriteFile(filepath.Join(dir, "policy.yaml"), []byte(tc.policy), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			sock, capture := startCapturePackDaemon(t)
			t.Setenv("AGENTPAAS_SOCKET", sock)
			resetAgentCmd()
			cmd := AgentCmd()
			cmd.SetArgs([]string{"pack", dir})
			err := cmd.Execute()
			if err == nil {
				t.Fatal("pack accepted custom agent.yaml PII patterns")
			}
			assertCustomPIIPatternRefusal(t, err.Error())
			if capture.captured != nil {
				t.Fatal("pack reached the daemon before refusing custom patterns; image build must not start")
			}
		})
	}
}

func TestPackCmd_AbsolutePathPassedThrough(t *testing.T) {
	base := t.TempDir()
	projectDir := filepath.Join(base, "myagent")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sock, capture := startCapturePackDaemon(t)
	t.Setenv("AGENTPAAS_SOCKET", sock)

	resetAgentCmd()
	cmd := AgentCmd()
	cmd.SetArgs([]string{"pack", projectDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pack execute: %v", err)
	}
	if capture.captured == nil {
		t.Fatal("Pack RPC was not called")
	}
	if capture.captured.GetAgentProjectPath() != projectDir {
		t.Fatalf("AgentProjectPath = %q, want %q", capture.captured.GetAgentProjectPath(), projectDir)
	}
}

func TestValidateCmd_ResolvesRelativePath(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	base := t.TempDir()
	projectDir := filepath.Join(base, "agent")
	if err := os.Mkdir(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(base, "nested")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	rel := "../agent"
	got, err := resolveCLIProjectPath(rel)
	if err != nil {
		t.Fatalf("resolveCLIProjectPath() error = %v", err)
	}
	want := filepath.Join(base, "agent")
	if !pathsEqual(got, want) {
		t.Fatalf("resolveCLIProjectPath(%q) = %q, want %q", rel, got, want)
	}
}

func pathsEqual(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	aReal, errA := filepath.EvalSymlinks(a)
	bReal, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return aReal == bReal
	}
	return false
}
