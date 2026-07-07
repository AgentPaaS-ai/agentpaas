package install

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DockerContainerStopper stops containers labeled agentpaas.agent-ref=<ref>.
type DockerContainerStopper struct{}

// StopByAgentRef implements ContainerStopper.
func (DockerContainerStopper) StopByAgentRef(ctx context.Context, agentRef string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	filter := fmt.Sprintf("label=%s=%s", installedAgentRefLabel, agentRef)
	out, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", filter).Output()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	for _, id := range ids {
		if _, err := exec.CommandContext(ctx, "docker", "stop", id).CombinedOutput(); err != nil {
			return fmt.Errorf("docker stop %s: %w", id, err)
		}
	}
	return nil
}

// FakeContainerStopper records stop calls for tests.
type FakeContainerStopper struct {
	Stopped []string
}

// StopByAgentRef implements ContainerStopper.
func (f *FakeContainerStopper) StopByAgentRef(ctx context.Context, agentRef string) error {
	f.Stopped = append(f.Stopped, agentRef)
	return nil
}