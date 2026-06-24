package cli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
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