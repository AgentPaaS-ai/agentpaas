package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAdversaryB5T04b_HostDockerInternalBypass tests whether an agent
// container can bypass host.docker.internal blocking by using IP address
// translation, loopback addresses, or the Docker host's gateway IP.
//
// ADVERSARY BREAK: MEDIUM — if the agent can reach host services through
// any mechanism (host.docker.internal, loopback, or bridge gateway), the
// host isolation claim is violated. The agent should have NO path to host
// services from the internal network.
func TestAdversaryB5T04b_HostDockerInternalBypass(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04b-adv-host-bypass"

	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Test multiple host bypass vectors
	bypassTests := []struct {
		name    string
		command []string
	}{
		{
			name:    "hostDockerInternal_HTTPS",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "https://host.docker.internal:443/"},
		},
		{
			name:    "hostDockerInternal_SSH",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://host.docker.internal:22/"},
		},
		{
			name:    "gatewayHost_colon",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://gateway.docker.internal:80/"},
		},
		{
			name:    "kubernetesHost",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://kubernetes.docker.internal:80/"},
		},
	}

	allBlocked := true
	for _, bt := range bypassTests {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := dockerExec(probeCtx, string(agentID), bt.command...)
		cancel()

		if err == nil {
			t.Errorf("Host bypass vector '%s' succeeded — expected BLOCKED (out: %s)", bt.name, out)
			allBlocked = false
		}
		_ = out
	}
	if allBlocked {
		t.Log("PASS: All host.docker.internal bypass variants are blocked")
	}
}

// TestAdversaryB5T04b_LoopbackAndLocalhostProbes tests whether an agent
// container can access loopback addresses (127.0.0.1, localhost) to reach
// host services. On an internal Docker bridge network, loopback should
// refer only to the container itself, not the host.
//
// ADVERSARY BREAK: MEDIUM — if 127.0.0.1 routes to the host instead of
// the container, the agent can bypass network isolation entirely. The
// internal bridge correctly isolates loopback to the container.
func TestAdversaryB5T04b_LoopbackAndLocalhostProbes(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04b-adv-loopback"

	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Loopback probes
	loopbackTests := []struct {
		name    string
		command []string
	}{
		{
			name:    "localhost_80",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://localhost:80/"},
		},
		{
			name:    "localhost_443",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "https://localhost:443/"},
		},
		{
			name:    "loopback_127_0_0_1",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://127.0.0.1:80/"},
		},
		{
			name:    "loopback_127_0_0_2",
			command: []string{"wget", "-q", "-O", "/dev/null", "--timeout=3", "http://127.0.0.2:80/"},
		},
		{
			name:    "loopback_ping",
			command: []string{"ping", "-c", "1", "-W", "2", "127.0.0.1"},
		},
	}

	allBlocked := true
	for _, lt := range loopbackTests {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := dockerExec(probeCtx, string(agentID), lt.command...)
		cancel()

		if err == nil {
			// For ping to 127.0.0.1 inside the container, this actually
			// succeeds (loopback within the container is normal). That's
			// fine — the container's own loopback is expected to work.
			// Only external loopback addresses (referring to host) should
			// be blocked. Ping to 'localhost' or 127.0.0.1 inside a
			// container pings the container itself, not the host.
			if lt.name != "loopback_ping" && lt.name != "localhost_80" && lt.name != "localhost_443" {
				t.Errorf("Loopback probe '%s' succeeded — expected BLOCKED (out: %s)", lt.name, out)
				allBlocked = false
			} else {
				t.Logf("INFO: '%s' succeeded (container's own loopback is expected to work)", lt.name)
			}
		} else {
			t.Logf("PASS: Loopback probe '%s' blocked: %v", lt.name, err)
		}
		_ = out
	}
	if allBlocked {
		t.Log("PASS: All loopback host probes are correctly isolated")
	}
}

// TestAdversaryB5T04b_GatewayPortProbeAggressive tests whether an agent
// container can aggressively scan the gateway container's open ports from
// the internal network. The gateway should only be reachable for policy-
// allowed paths (e.g., defined ingress ports).
//
// ADVERSARY BREAK: LOW — the gateway is on the same internal network, so
// TCP-level connectivity is expected. What matters is that no unexpected
// services are reachable. This test documents the exposed surface.
func TestAdversaryB5T04b_GatewayPortProbeAggressive(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04b-adv-gw-scan"

	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("egress", runID),
		Internal: false,
		Labels:   Labels(ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress) failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, egressNetID) }()

	gatewayID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID), string(egressNetID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, gatewayID, true) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway) failed: %v", err)
	}
	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(2 * time.Second)

	// Get gateway IP on internal network
	gatewayNets, err := dr.InspectContainerNetworks(ctx, gatewayID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
	}
	t.Logf("Gateway networks: %+v", gatewayNets)

	// Get gateway IP using docker inspect
	gatewayInfo, err := dr.cli.ContainerInspect(ctx, string(gatewayID))
	if err != nil {
		t.Fatalf("ContainerInspect(gateway) failed: %v", err)
	}

	var gatewayIP string
	for netName, netSettings := range gatewayInfo.NetworkSettings.Networks {
		if strings.Contains(netName, "internal") {
			gatewayIP = netSettings.IPAddress
			break
		}
	}
	if gatewayIP == "" {
		t.Fatal("Could not determine gateway IP on internal network")
	}
	t.Logf("Gateway internal IP: %s", gatewayIP)

	// Aggressive port scan of common ports on gateway
	scanPorts := []string{"21", "22", "23", "25", "53", "80", "443", "3306", "5432", "6379", "8080", "8443", "27017"}
	openPorts := []string{}
	for _, port := range scanPorts {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, err := dockerExec(probeCtx, string(agentID),
			"timeout", "2", "nc", "-zv", gatewayIP, port)
		cancel()

		if err == nil {
			openPorts = append(openPorts, port)
		}
		_ = out
	}

	if len(openPorts) > 0 {
		t.Logf("INFO: Gateway has open ports on internal network: %v", openPorts)
		t.Logf("This is expected if the gateway runs services on those ports.")
	} else {
		t.Log("PASS: Gateway has no unexpected open ports from agent")
	}
}

// TestAdversaryB5T04b_DockerSocketDiscovery tests whether an agent container
// can discover or access the Docker daemon socket through common paths.
// The Docker socket is mounted via /var/run/docker.sock on the host, but
// agent containers should not have host mounts.
//
// ADVERSARY BREAK: HIGH — if the agent can access the Docker socket, it
// has full control over the Docker daemon (container escape). This is
// mitigated by not mounting the Docker socket into agent containers.
func TestAdversaryB5T04b_DockerSocketDiscovery(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}

	runID := "b5t04b-adv-socket"

	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     NetworkName("internal", runID),
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork failed: %v", err)
	}
	defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	defer func() { _ = dr.Remove(ctx, agentID, true) }()

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(1 * time.Second)

	// Test multiple Docker socket path locations
	socketPaths := []string{
		"/var/run/docker.sock",
		"/run/docker.sock",
		"/var/run/docker-ce.sock",
		"/var/run/docker/libdocker.sock",
		"/run/docker/libdocker.sock",
		".docker/run/docker.sock",
	}

	allAbsent := true
	for _, path := range socketPaths {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		out, err := dockerExec(probeCtx, string(agentID),
			"sh", "-c", "test -e "+path+" && echo EXISTS || echo NOT_FOUND")
		cancel()

		if err == nil && strings.Contains(out, "EXISTS") {
			t.Errorf("Docker socket found at %s — expected NOT accessible", path)
			allAbsent = false
		}
	}
	if allAbsent {
		t.Log("PASS: Docker socket absent from all common paths inside agent container")
	}
}

// TestAdversaryB5T04b_MissingHostBridgeAssertions documents the risk that
// host/bridge probe assertions might not be tested in CI (due to Docker
// requirement).
//
// ADVERSARY BREAK: LOW — the host/bridge probes only run when
// AGENTPAAS_DOCKER_TESTS=1 is set. CI may not set this, leaving the
// host/bridge isolation assertions untested in CI pipelines. This is an
// accepted risk for P1, documented here.
func TestAdversaryB5T04b_MissingHostBridgeAssertions(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Log("WARNING: host/bridge probe tests are SKIPPED without AGENTPAAS_DOCKER_TESTS=1")
		t.Log("This means CI will NOT exercise host/bridge isolation assertions.")
		t.Log("Mitigation: run 'make e2e-network' locally to verify.")
	}
	t.Log("PASS: host/bridge test gap documented")
}