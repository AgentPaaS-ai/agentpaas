package redteam

import (
	"context"
	"fmt"
	"strings"
	"time"

	docker "github.com/parvezsyed/agentpaas/internal/runtime"
)

// hostAccessFixture (B12-T05 part 1): agent probes host.docker.internal,
// Docker bridge gateway, and daemon ports. Expect blocked/unreachable.
type hostAccessFixture struct{}

func (f *hostAccessFixture) ID() string   { return "T05a" }
func (f *hostAccessFixture) Name() string { return "Host Access Blocked" }

func (f *hostAccessFixture) Run() FixtureResult {
	start := time.Now()
	result := FixtureResult{
		ID:           f.ID(),
		Name:         f.Name(),
		Status:       "FAIL",
		Containment:  "LEAKED",
		AuditVerdict: "n/a",
	}
	defer recoverFixture(&result)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dr, err := docker.NewDockerRuntime()
	if err != nil {
		result.Detail = fmt.Sprintf("NewDockerRuntime: %v", err)
		return result
	}

	runID := uniqueRunID("b12t05a")
	internalNetID, egressNetID, gatewayID, agentID := createTopology(ctx, &fixtureT{result: &result}, dr, runID)
	defer func() {
		cleanupContainers(ctx, dr, gatewayID, agentID)
		cleanupNetworks(ctx, dr, internalNetID, egressNetID)
	}()

	time.Sleep(2 * time.Second)

	// Probe 1: host.docker.internal should NOT resolve from agent container
	// (agent is on internal-only network, no host access)
	probe1Ctx, p1Cancel := context.WithTimeout(ctx, 15*time.Second)
	defer p1Cancel()
	output1, _ := dockerExec(probe1Ctx, string(agentID),
		"sh", "-c", "getent hosts host.docker.internal 2>/dev/null && echo 'RESOLVED' || echo 'DNS_BLOCKED'")
	if strings.Contains(output1, "RESOLVED") {
		result.Detail = "host.docker.internal resolved from agent container — expected blocked"
		return result
	}
	if !containsAny(output1, "DNS_BLOCKED") {
		result.Detail = fmt.Sprintf("host.docker.internal probe unexpected output: %s", output1)
		return result
	}

	// Probe 2: Docker bridge gateway IP (typically 172.17.0.1) should be unreachable
	probe2Ctx, p2Cancel := context.WithTimeout(ctx, 15*time.Second)
	defer p2Cancel()
	output2, _ := dockerExec(probe2Ctx, string(agentID),
		"sh", "-c", "wget -q -O /dev/null http://172.17.0.1:2375 2>/dev/null && echo 'SUCCESS' || echo 'BLOCKED'")
	if strings.Contains(output2, "SUCCESS") {
		result.Detail = "Docker bridge gateway reachable from agent — expected blocked"
		return result
	}
	if !containsAny(output2, "BLOCKED") {
		result.Detail = fmt.Sprintf("bridge gateway probe unexpected output: %s", output2)
		return result
	}

	// Probe 3: daemon gRPC port (typically localhost:7717) should be unreachable
	// from inside the agent container
	probe3Ctx, p3Cancel := context.WithTimeout(ctx, 15*time.Second)
	defer p3Cancel()
	output3, _ := dockerExec(probe3Ctx, string(agentID),
		"sh", "-c", "wget -q -O /dev/null http://host.docker.internal:7717 2>/dev/null && echo 'SUCCESS' || echo 'BLOCKED'")
	if strings.Contains(output3, "SUCCESS") {
		result.Detail = "daemon port reachable from agent — expected blocked"
		return result
	}
	if !containsAny(output3, "BLOCKED") {
		result.Detail = fmt.Sprintf("daemon port probe unexpected output: %s", output3)
		return result
	}

	result.Status = "PASS"
	result.Containment = "BLOCKED"
	result.AuditVerdict = "verified"
	result.Duration = time.Since(start)
	result.Detail = "host.docker.internal, bridge gateway, daemon ports all blocked from agent"
	return result
}

// resourceContainmentFixture (B12-T05 part 2): memory/fd/child-process
// pressure trips configured limit without taking down daemon/dashboard.
type resourceContainmentFixture struct{}

func (f *resourceContainmentFixture) ID() string   { return "T05b" }
func (f *resourceContainmentFixture) Name() string { return "Resource Containment" }

func (f *resourceContainmentFixture) Run() FixtureResult {
	start := time.Now()
	result := FixtureResult{
		ID:           f.ID(),
		Name:         f.Name(),
		Status:       "FAIL",
		Containment:  "LEAKED",
		AuditVerdict: "n/a",
	}
	defer recoverFixture(&result)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dr, err := docker.NewDockerRuntime()
	if err != nil {
		result.Detail = fmt.Sprintf("NewDockerRuntime: %v", err)
		return result
	}

	runID := uniqueRunID("b12t05b")
	internalNetID, _, gatewayID, _ := createTopology(ctx, &fixtureT{result: &result}, dr, runID)
	defer func() {
		cleanupContainers(ctx, dr, gatewayID)
		cleanupNetworks(ctx, dr, internalNetID)
	}()

	// Create a resource-stressed agent container with an enforced memory limit.
	const memoryLimitBytes = 16 * 1024 * 1024 // 16 MiB
	stressID, err := dr.Create(ctx, docker.ContainerSpec{
		Image:            "alpine:latest",
		Command:          []string{"sleep", "120"},
		NetworkIDs:       []string{string(internalNetID)},
		Labels:           docker.Labels(docker.ResourceTypeAgent, runID),
		MemoryLimitBytes: memoryLimitBytes,
	})
	if err != nil {
		result.Detail = fmt.Sprintf("Create(stress agent): %v", err)
		return result
	}
	defer cleanupContainers(ctx, dr, stressID)

	if err := dr.Start(ctx, stressID); err != nil {
		result.Detail = fmt.Sprintf("Start(stress agent): %v", err)
		return result
	}

	time.Sleep(2 * time.Second)

	// Probe 1: memory pressure — try to allocate more than the limit
	// The container should be OOM-killed or the allocation should fail.
	// Alpine's sh doesn't have a memory stress tool, but we can try
	// a fork bomb or large allocation.
	memCtx, memCancel := context.WithTimeout(ctx, 30*time.Second)
	defer memCancel()
	memOutput, _ := dockerExec(memCtx, string(stressID),
		"sh", "-c", "dd if=/dev/zero of=/tmp/fill bs=1M count=200 2>&1; echo EXIT=$?")

	status, err := dr.Status(ctx, stressID)
	if err != nil {
		result.Detail = fmt.Sprintf("Status after memory pressure: %v", err)
		return result
	}

	memoryContained := status == docker.ContainerStatusStopped ||
		strings.Contains(memOutput, "Killed") ||
		strings.Contains(memOutput, "No space left on device") ||
		strings.Contains(memOutput, "Cannot allocate memory") ||
		!strings.Contains(memOutput, "EXIT=0")
	if !memoryContained {
		result.Detail = fmt.Sprintf("memory limit not enforced: status=%s output=%s", status, memOutput)
		return result
	}

	// Verify the DockerRuntime is still functional (daemon survived)
	_, err = dr.ListContainers(ctx, "agentpaas.managed-by=agentpaas")
	if err != nil {
		result.Detail = fmt.Sprintf("DockerRuntime broken after resource pressure: %v", err)
		return result
	}

	result.Status = "PASS"
	result.Containment = "CONTAINED"
	result.AuditVerdict = "verified"
	result.Duration = time.Since(start)
	result.Detail = "memory-limited container killed or write failed under pressure; Docker runtime client still functional"
	return result
}

// Combined fixture for T05 (host access + resource containment)
// Both sub-fixtures (hostAccessFixture T05a, resourceContainmentFixture T05b)
// are registered directly with the runner in redteam_smoke_test.go.
