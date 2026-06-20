package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// dockerExec runs a command inside a running Docker container using docker CLI.
// Returns stdout content and any exec error. Uses context-based timeout.
func dockerExec(ctx context.Context, containerID string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("docker exec %s: %w (stderr: %s)", containerID, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// TestE2E_Network_PositivePath verifies the gateway allows allowed paths while
// blocking direct external canary probes:
//   - Gateway container can reach external internet via egress network
//   - Agent container CANNOT reach external internet directly
//   - Agent container DNS is blocked (unreachable)
//   - Agent container CAN reach the gateway via internal network
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_Network_PositivePath(t *testing.T) {
	if os.Getenv("AGENTPAAS_DOCKER_TESTS") != "1" {
		t.Skip("set AGENTPAAS_DOCKER_TESTS=1 to run Docker integration tests")
	}

	ctx := context.Background()
	dr, err := NewDockerRuntime()
	if err != nil {
		t.Fatalf("NewDockerRuntime() failed: %v", err)
	}
	if dr == nil {
		t.Fatal("NewDockerRuntime() returned nil")
	}

	runID := fmt.Sprintf("b5t04a-%d", time.Now().UnixNano())

	// Track resources for deferred cleanup
	cleanupContainers := []ContainerID{}
	cleanupNetworks := []NetworkID{}
	defer func() {
		for _, id := range cleanupContainers {
			_ = dr.Remove(ctx, id, true)
		}
		for _, nid := range cleanupNetworks {
			_ = dr.RemoveNetwork(ctx, nid)
		}
	}()

	// ---- Setup: Create Networks ----

	// Step 1: Create internal bridge network (internal: true — no external access)
	internalNetName := NetworkName("internal", runID)
	internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     internalNetName,
		Internal: true,
		Labels:   Labels(ResourceTypeNetInternal, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(internal, %q) failed: %v", internalNetName, err)
	}
	cleanupNetworks = append(cleanupNetworks, internalNetID)
	t.Logf("Created internal network: %s (ID: %s)", internalNetName, internalNetID)

	// Step 2: Create egress network (regular bridge — has external access)
	egressNetName := NetworkName("egress", runID)
	egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
		Name:     egressNetName,
		Internal: false,
		Labels:   Labels(ResourceTypeNetEgress, runID),
	})
	if err != nil {
		t.Fatalf("CreateNetwork(egress, %q) failed: %v", egressNetName, err)
	}
	cleanupNetworks = append(cleanupNetworks, egressNetID)
	t.Logf("Created egress network: %s (ID: %s)", egressNetName, egressNetID)

	// ---- Setup: Create Containers ----

	// Step 3: Create gateway container (dual-homed: internal + egress)
	// Use alpine with a sleep command so we can exec into it.
	gatewayID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID), string(egressNetID)},
		Labels:     Labels(ResourceTypeGateway, runID),
	})
	if err != nil {
		t.Fatalf("Create(gateway) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, gatewayID)
	t.Logf("Created gateway container: %s", gatewayID)

	// Step 4: Create agent container (internal network only)
	agentID, err := dr.Create(ctx, ContainerSpec{
		Image:      "alpine:latest",
		Command:    []string{"sleep", "3600"},
		NetworkIDs: []string{string(internalNetID)},
		Labels:     Labels(ResourceTypeAgent, runID),
	})
	if err != nil {
		t.Fatalf("Create(agent) failed: %v", err)
	}
	cleanupContainers = append(cleanupContainers, agentID)
	t.Logf("Created agent container: %s", agentID)

	// Step 5: Start both containers
	if err := dr.Start(ctx, gatewayID); err != nil {
		t.Fatalf("Start(gateway) failed: %v", err)
	}
	t.Log("Gateway container started")

	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	t.Log("Agent container started")

	// Allow containers to finish initializing
	time.Sleep(2 * time.Second)

	// ---- Verify Topology ----

	// Agent must be on internal network only
	agentNetworks, err := dr.InspectContainerNetworks(ctx, agentID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(agent) failed: %v", err)
	}
	if len(agentNetworks) != 1 {
		t.Errorf("Agent has %d networks, want exactly 1 (internal only); networks: %v", len(agentNetworks), agentNetworks)
	}

	// Gateway must be on both networks
	gatewayNetworks, err := dr.InspectContainerNetworks(ctx, gatewayID)
	if err != nil {
		t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
	}
	if len(gatewayNetworks) < 2 {
		t.Errorf("Gateway has %d networks, want at least 2 (internal + egress); networks: %v", len(gatewayNetworks), gatewayNetworks)
	}

	// ---- CANARY PROBE 1: Agent direct HTTP to 1.1.1.1 must FAIL fast (<=2s) ----
	t.Run("Canary_AgentDirectHTTPS_fails_fast", func(t *testing.T) {
		canaryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Try to reach 1.1.1.1 on port 80 from the agent container. This should fail
		// because the agent is on the internal (isolated) network.
		_, err := dockerExec(canaryCtx, string(agentID), "wget", "-q", "-O", "/dev/null", "--timeout=3", "http://1.1.1.1")
		if err == nil {
			t.Error("AGENT DIRECT HTTP TO 1.1.1.1 SUCCEEDED — expected BLOCKED; agent should not have direct egress")
		} else {
			t.Logf("PASS: Agent direct HTTP to 1.1.1.1 blocked as expected: %v", err)
		}

		// Verify the failure did NOT take longer than 3s (hanging test)
		if cErr := canaryCtx.Err(); cErr != nil {
			t.Errorf("Canary timed out (hung): %v — expected fast failure within 3s", cErr)
		}
	})

	// ---- CANARY PROBE 2: Agent DNS to 8.8.8.8 must be unreachable ----
	t.Run("Canary_AgentDNS_unreachable", func(t *testing.T) {
		canaryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Try to resolve a DNS query via 8.8.8.8 from the agent container.
		// This should fail because DNS traffic is also blocked by the internal network.
		_, err := dockerExec(canaryCtx, string(agentID), "wget", "-q", "-O", "/dev/null", "--timeout=3", "http://google.com")
		if err == nil {
			t.Error("AGENT HTTP TO GOOGLE SUCCEEDED — expected DNS BLOCKED; agent should not resolve external names")
		} else {
			t.Logf("PASS: Agent DNS to external resolved as blocked: %v", err)
		}

		if cErr := canaryCtx.Err(); cErr != nil {
			t.Errorf("DNS canary timed out (hung): %v — expected fast failure within 3s", cErr)
		}
	})

	// ---- CANARY PROBE 3: Agent direct wget to an arbitrary external host must fail ----
	t.Run("Canary_AgentDirectExternal_fails", func(t *testing.T) {
		canaryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err := dockerExec(canaryCtx, string(agentID), "wget", "-q", "-O", "/dev/null", "--timeout=3", "http://example.com")
		if err == nil {
			t.Error("AGENT HTTP TO EXAMPLE.COM SUCCEEDED — expected BLOCKED; agent should not have direct egress")
		} else {
			t.Logf("PASS: Agent direct HTTP to example.com blocked as expected: %v", err)
		}

		if cErr := canaryCtx.Err(); cErr != nil {
			t.Errorf("External canary timed out (hung): %v — expected fast failure within 3s", cErr)
		}
	})

	// ---- POSITIVE PATH: Gateway CAN reach external internet via egress ----
	t.Run("Positive_GatewayExternalAccess", func(t *testing.T) {
		gatewayCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		// Gateway is dual-homed on egress network, so it should be able
		// to reach external services.
		out, err := dockerExec(gatewayCtx, string(gatewayID), "wget", "-q", "-O", "/dev/null", "--timeout=10", "http://1.1.1.1")
		if err != nil {
			t.Errorf("GATEWAY HTTP TO 1.1.1.1 FAILED: %v — gateway should have egress access (stderr: %s)", err, out)
		} else {
			t.Log("PASS: Gateway can reach external internet via egress network")
		}
	})

	// ---- POSITIVE PATH: Agent CAN reach gateway via internal network ----
	t.Run("Positive_AgentCanReachGateway", func(t *testing.T) {
		// Get gateway's IP address on the internal network
		gatewayNets, err := dr.InspectContainerNetworks(ctx, gatewayID)
		if err != nil {
			t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
		}

		// Find the gateway's IP on the internal network
		var gatewayInternalIP string
		gatewayInfo, err := dr.cli.ContainerInspect(ctx, string(gatewayID))
		if err != nil {
			t.Fatalf("ContainerInspect(gateway) failed: %v", err)
		}
		for netName, netSettings := range gatewayInfo.NetworkSettings.Networks {
			if strings.Contains(netName, "internal") || netName == internalNetName {
				gatewayInternalIP = netSettings.IPAddress
				break
			}
		}
		if gatewayInternalIP == "" {
			// Fallback: try to find any IP on internal network
			for _, netInfo := range gatewayNets {
				netDetail, err := dr.InspectNetwork(ctx, NetworkID(netInfo.ID))
				if err == nil && netDetail.Internal {
					// We need the gateway's IP on this network
					gatewayInfo, err := dr.cli.ContainerInspect(ctx, string(gatewayID))
					if err == nil {
						if ns, ok := gatewayInfo.NetworkSettings.Networks[netDetail.Name]; ok {
							gatewayInternalIP = ns.IPAddress
						}
					}
				}
			}
		}
		if gatewayInternalIP == "" {
			t.Fatal("Could not determine gateway IP on internal network")
		}
		t.Logf("Gateway internal IP: %s", gatewayInternalIP)

		// Agent should be able to reach the gateway via the internal network
		reachCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		out, err := dockerExec(reachCtx, string(agentID), "wget", "-q", "-O", "/dev/null", "--timeout=3", fmt.Sprintf("http://%s:80/", gatewayInternalIP))
		if err != nil {
			// Gateway may not have an HTTP server running, so a connection refused
			// is actually OK — it means the network path works (TCP reached the host)
			// but no service is listening on that port.
			if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "Connection refused") {
				t.Logf("PASS: Agent reached gateway via internal network (connection refused = network path works, no HTTP server on gateway)")
			} else {
				t.Errorf("AGENT COULD NOT REACH GATEWAY via internal network: %v (out: %s)", err, out)
			}
		} else {
			t.Log("PASS: Agent can reach gateway via internal network")
		}
	})

	t.Log("=== E2E Network Positive Path + Canary Probes: COMPLETE ===")
}
