package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func TestE2E_CapNetAdminDropped_AgentCannotFlushIPTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker integration test in -short mode")
	}
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	ensureHarnessLinuxARM64(t)
	repoRoot := findRepoRoot(t)

	imageTag := fmt.Sprintf("agentpaas-capset-verify:%d", time.Now().UnixNano())
	buildCapsetVerifyImage(t, repoRoot, imageTag)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return
		}
		defer func() { _ = cli.Close() }()
		_, _ = cli.ImageRemove(cleanupCtx, imageTag, image.RemoveOptions{Force: true})
	})

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("NewClientWithOpts: %v", err)
	}
	defer func() { _ = cli.Close() }()

	containerName := fmt.Sprintf("capset-verify-%d", time.Now().UnixNano())
	createResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageTag,
		Env: []string{
			"AGENTPAAS_EGRESS_FIREWALL=1",
			"AGENTPAAS_GATEWAY_IP=172.18.0.2",
			"AGENTPAAS_GATEWAY_SUBNET=172.18.0.0/16",
			"AGENTPAAS_AGENT_PATH=/dev/null",
		},
		User: "root",
	}, &container.HostConfig{
		CapAdd: []string{"NET_ADMIN"},
	}, nil, nil, containerName)
	if err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}
	containerID := createResp.ID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = cli.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{Force: true})
	})

	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}

	time.Sleep(3 * time.Second)

	stdout, stderr, exitCode := dockerExecAsUser(t, cli, ctx, containerID, "64000", []string{"iptables", "-F"})
	if exitCode == 0 {
		t.Fatalf("iptables -F as UID 64000 succeeded (exit 0); want permission denied\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(strings.ToLower(combined), "permission denied") &&
		!strings.Contains(strings.ToLower(combined), "operation not permitted") {
		t.Logf("iptables -F stderr/stdout (non-zero exit %d): %s", exitCode, combined)
	}

	stdout, _, exitCode = dockerExecAsUser(t, cli, ctx, containerID, "0", []string{"iptables", "-L", "OUTPUT"})
	if exitCode != 0 {
		t.Fatalf("iptables -L OUTPUT as root failed (exit %d): %s", exitCode, stdout)
	}
	if !strings.Contains(stdout, "DROP") {
		t.Fatalf("OUTPUT chain policy not DROP as root (firewall init did not run):\n%s", stdout)
	}
}

func buildCapsetVerifyImage(t *testing.T, repoRoot, imageTag string) {
	t.Helper()
	dockerfilePath := filepath.Join(repoRoot, fmt.Sprintf(".capset-verify-Dockerfile-%d", time.Now().UnixNano()))
	dockerfile := `FROM alpine:latest
COPY bin/agentpaas-harness-linux /agentpaas-harness
RUN apk add --no-cache iptables
RUN chmod +x /agentpaas-harness
ENTRYPOINT ["/agentpaas-harness"]
`
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("WriteFile Dockerfile: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(dockerfilePath) })

	cmd := exec.CommandContext(context.Background(), "docker", "build", "-t", imageTag, "-f", dockerfilePath, repoRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}
}

func dockerExecAsUser(t *testing.T, cli *client.Client, ctx context.Context, containerID, user string, cmd []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	execCreate, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		User:         user,
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		t.Fatalf("ContainerExecCreate(%v as %s): %v", cmd, user, err)
	}

	hijacked, err := cli.ContainerExecAttach(ctx, execCreate.ID, container.ExecAttachOptions{})
	if err != nil {
		t.Fatalf("ContainerExecAttach: %v", err)
	}
	defer hijacked.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, hijacked.Reader); err != nil {
		t.Fatalf("stdcopy: %v", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execCreate.ID)
	if err != nil {
		t.Fatalf("ContainerExecInspect: %v", err)
	}
	return stdoutBuf.String(), stderrBuf.String(), inspect.ExitCode
}
