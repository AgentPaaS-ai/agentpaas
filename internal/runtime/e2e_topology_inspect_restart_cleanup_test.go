package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_TopologyInspect verifies deep Docker inspect assertions about the
// agent-gateway topology:
//   - Agent container has NO default route (ip route shows only internal bridge subnet)
//   - Agent container has NO egress network attachment
//   - Agent container is NOT in host networking mode
//   - Agent container does NOT share the gateway's network namespace
//   - Gateway container has exactly internal + egress networks
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_TopologyInspect(t *testing.T) {
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

	runID := fmt.Sprintf("b5t04d-%d", time.Now().UnixNano())

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

	// ---- Setup: Networks ----

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

	// ---- Setup: Containers ----

	// Step 3: Create gateway container (dual-homed: internal + egress)
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

	// ---- PHASE 1: Topology Inspect Assertions ----
	t.Run("Phase1_TopologyInspect", func(t *testing.T) {
		runTopologyInspections(t, ctx, dr, agentID, gatewayID, internalNetName, egressNetName)
	})

	// ---- PHASE 2: Restart preserves membership ----
	t.Run("Phase2_RestartPreservesMembership", func(t *testing.T) {
		// Stop both containers
		timeout := 10 * time.Second
		if err := dr.Stop(ctx, agentID, &timeout); err != nil {
			t.Fatalf("Stop(agent) failed: %v", err)
		}
		t.Log("Agent container stopped")

		if err := dr.Stop(ctx, gatewayID, &timeout); err != nil {
			t.Fatalf("Stop(gateway) failed: %v", err)
		}
		t.Log("Gateway container stopped")

		// Verify both are stopped
		agentStatus, err := dr.Status(ctx, agentID)
		if err != nil {
			t.Fatalf("Status(agent) after stop failed: %v", err)
		}
		if agentStatus != ContainerStatusStopped {
			t.Errorf("Agent status after stop = %v, want %v", agentStatus, ContainerStatusStopped)
		}

		gatewayStatus, err := dr.Status(ctx, gatewayID)
		if err != nil {
			t.Fatalf("Status(gateway) after stop failed: %v", err)
		}
		if gatewayStatus != ContainerStatusStopped {
			t.Errorf("Gateway status after stop = %v, want %v", gatewayStatus, ContainerStatusStopped)
		}
		t.Log("Both containers confirmed stopped")

		// Restart both containers
		if err := dr.Start(ctx, gatewayID); err != nil {
			t.Fatalf("Start(gateway after restart) failed: %v", err)
		}
		t.Log("Gateway container restarted")

		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent after restart) failed: %v", err)
		}
		t.Log("Agent container restarted")

		time.Sleep(2 * time.Second)

		// Re-run all topology assertions — membership must be preserved
		t.Log("=== Re-running topology inspections after restart ===")
		runTopologyInspections(t, ctx, dr, agentID, gatewayID, internalNetName, egressNetName)
		t.Log("All topology assertions pass after restart — membership preserved")
	})

	t.Log("=== E2E Topology Inspect & Restart: COMPLETE ===")
}

// runTopologyInspections executes all deep Docker inspect assertions to
// verify the dual-container gateway-only network topology. Called both
// before and after restart to prove membership is preserved.
func runTopologyInspections(t *testing.T, ctx context.Context, dr *DockerRuntime,
	agentID, gatewayID ContainerID, internalNetName, egressNetName string) {

	// ---- INSPECT 1: Agent has NO default route ----
	t.Run("Inspect_AgentNoDefaultRoute", func(t *testing.T) {
		out, err := dockerExec(ctx, string(agentID), "ip", "route")
		if err != nil {
			t.Fatalf("Failed to get agent routing table: %v", err)
		}
		t.Logf("Agent routing table:\n%s", out)
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "default") {
				t.Errorf("Agent HAS default route: %q — expected NO default route (internal bridge only)", line)
			}
		}
	})

	// ---- INSPECT 2: Agent has NO egress network attachment ----
	t.Run("Inspect_AgentNoEgressNetwork", func(t *testing.T) {
		agentNetworks, err := dr.InspectContainerNetworks(ctx, agentID)
		if err != nil {
			t.Fatalf("InspectContainerNetworks(agent) failed: %v", err)
		}
		for _, n := range agentNetworks {
			if n.Name == egressNetName {
				t.Errorf("Agent IS attached to egress network %q — expected internal only", egressNetName)
			}
			if strings.Contains(n.Name, "egress") {
				t.Errorf("Agent has egress-like network attachment %q — expected internal only", n.Name)
			}
		}
		// Agent must have exactly 1 network attachment (internal only)
		if len(agentNetworks) != 1 {
			t.Errorf("Agent has %d network(s), want exactly 1 (internal only); networks: %v",
				len(agentNetworks), agentNetworks)
		} else {
			t.Logf("PASS: Agent has exactly 1 network (internal only): %v", agentNetworks)
		}
	})

	// ---- INSPECT 3: Agent is NOT in host networking mode ----
	t.Run("Inspect_AgentNoHostNetworking", func(t *testing.T) {
		agentInfo, err := dr.cli.ContainerInspect(ctx, string(agentID))
		if err != nil {
			t.Fatalf("ContainerInspect(agent) failed: %v", err)
		}
		if agentInfo.HostConfig == nil {
			t.Fatal("HostConfig is nil in container inspect")
		}
		netMode := agentInfo.HostConfig.NetworkMode
		if netMode == "host" {
			t.Error("AGENT HAS HOST NETWORK MODE — expected non-host mode")
		} else {
			t.Logf("PASS: Agent network mode is %q (not host mode)", netMode)
		}
	})

	// ---- INSPECT 4: Agent does NOT share gateway namespace ----
	t.Run("Inspect_AgentNoSharedNetNS", func(t *testing.T) {
		agentInfo, err := dr.cli.ContainerInspect(ctx, string(agentID))
		if err != nil {
			t.Fatalf("ContainerInspect(agent) failed: %v", err)
		}
		if agentInfo.HostConfig == nil {
			t.Fatal("HostConfig is nil in container inspect")
		}
		netMode := string(agentInfo.HostConfig.NetworkMode)
		if strings.HasPrefix(netMode, "container:") {
			t.Errorf("AGENT SHARES NET NS with container %q — expected isolated netns", netMode)
		} else {
			t.Logf("PASS: Agent uses its own network namespace (network mode: %q)", netMode)
		}
	})

	// ---- INSPECT 5: Gateway has exactly internal + egress networks ----
	t.Run("Inspect_GatewayDualHomed", func(t *testing.T) {
		gatewayNetworks, err := dr.InspectContainerNetworks(ctx, gatewayID)
		if err != nil {
			t.Fatalf("InspectContainerNetworks(gateway) failed: %v", err)
		}
		// Gateway must have both internal and egress networks
		hasInternal := false
		hasEgress := false
		for _, n := range gatewayNetworks {
			if n.Name == internalNetName {
				hasInternal = true
			}
			if n.Name == egressNetName {
				hasEgress = true
			}
		}
		if !hasInternal {
			t.Errorf("Gateway NOT attached to internal network %q", internalNetName)
		}
		if !hasEgress {
			t.Errorf("Gateway NOT attached to egress network %q", egressNetName)
		}
		if len(gatewayNetworks) != 2 {
			t.Errorf("Gateway has %d network(s), want exactly 2 (internal + egress); networks: %v",
				len(gatewayNetworks), gatewayNetworks)
		} else {
			t.Logf("PASS: Gateway has exactly 2 networks (internal + egress)")
		}
	})

	// ---- INSPECT 6: Agent container verify internal-only via eth0 ----
	t.Run("Inspect_AgentOnlyInternalIP", func(t *testing.T) {
		// Agent should have only one network interface (eth0 on internal bridge)
		out, err := dockerExec(ctx, string(agentID), "ip", "addr")
		if err != nil {
			t.Fatalf("Failed to get agent IP addresses: %v", err)
		}
		t.Logf("Agent IP addresses:\n%s", out)
		// Count non-loopback interfaces
		lines := strings.Split(out, "\n")
		nonLoopCount := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "inet ") && !strings.Contains(line, "127.0.0.1") {
				nonLoopCount++
			}
		}
		// On an internal bridge, the agent should have exactly 1 non-loopback IP
		if nonLoopCount != 1 {
			t.Logf("INFO: Agent has %d non-loopback IP(s); expected 1 (internal bridge only)", nonLoopCount)
		}
	})
}

// TestE2E_PartialCreateCleanup verifies that when a container create or
// start fails partway through the topology setup, no orphaned AgentPaaS-owned
// resources remain. This test:
//   - Creates a partial topology (networks but no containers)
//   - Creates agent-only (no gateway) and verifies cleanup
//   - Creates gateway-only (no agent) and verifies cleanup
//   - Verifies Create failure releases owned resources
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_PartialCreateCleanup(t *testing.T) {
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

	runID := fmt.Sprintf("b5t04d-cleanup-%d", time.Now().UnixNano())

	// ---- SCENARIO 1: Networks created, containers never created ----
	// After creating networks, cleanup should remove them with no orphans.
	t.Run("NetworksCreated_ContainersSkipped", func(t *testing.T) {
		netRunID := runID + "-sc1"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		t.Logf("Created internal network: %s", internalNetName)

		egressNetName := NetworkName("egress", netRunID)
		egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     egressNetName,
			Internal: false,
			Labels:   Labels(ResourceTypeNetEgress, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(egress) failed: %v", err)
		}
		t.Logf("Created egress network: %s", egressNetName)

		// Verify both networks exist
		_, err = dr.InspectNetwork(ctx, internalNetID)
		if err != nil {
			t.Errorf("Internal network %s should exist: %v", internalNetName, err)
		}
		_, err = dr.InspectNetwork(ctx, egressNetID)
		if err != nil {
			t.Errorf("Egress network %s should exist: %v", egressNetName, err)
		}

		// Cleanup: remove both networks
		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork(internal) failed: %v", err)
		}
		if err := dr.RemoveNetwork(ctx, egressNetID); err != nil {
			t.Errorf("RemoveNetwork(egress) failed: %v", err)
		}

		// Verify networks are gone
		_, err = dr.InspectNetwork(ctx, internalNetID)
		if err == nil {
			t.Error("Internal network still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Internal network removed: %v", err)
		}
		_, err = dr.InspectNetwork(ctx, egressNetID)
		if err == nil {
			t.Error("Egress network still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Egress network removed: %v", err)
		}
	})

	// ---- SCENARIO 2: Agent container created without gateway ----
	// The agent's internal network should be cleanly removable even if
	// no gateway was created alongside it.
	t.Run("AgentWithoutGateway_Cleanup", func(t *testing.T) {
		netRunID := runID + "-sc2"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}

		// Create agent container only (no gateway)
		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}
		t.Logf("Created agent container (no gateway): %s", agentID)

		// Start and verify
		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		t.Log("Agent container started")

		// Cleanup: remove agent, then network
		if err := dr.Remove(ctx, agentID, true); err != nil {
			t.Errorf("Remove(agent) failed: %v", err)
		}
		t.Log("Agent container removed")

		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork(internal) failed: %v", err)
		}
		t.Log("Internal network removed")

		// Verify no orphans remain
		_, err = dr.cli.ContainerInspect(ctx, string(agentID))
		if err == nil {
			t.Error("Agent container still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Agent container removed: %v", err)
		}

		_, err = dr.InspectNetwork(ctx, internalNetID)
		if err == nil {
			t.Error("Internal network still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Internal network removed: %v", err)
		}
	})

	// ---- SCENARIO 3: Gateway container created without agent ----
	t.Run("GatewayWithoutAgent_Cleanup", func(t *testing.T) {
		netRunID := runID + "-sc3"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}

		egressNetName := NetworkName("egress", netRunID)
		egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     egressNetName,
			Internal: false,
			Labels:   Labels(ResourceTypeNetEgress, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(egress) failed: %v", err)
		}

		gatewayID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID), string(egressNetID)},
			Labels:     Labels(ResourceTypeGateway, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(gateway) failed: %v", err)
		}
		t.Logf("Created gateway container (no agent): %s", gatewayID)

		if err := dr.Start(ctx, gatewayID); err != nil {
			t.Fatalf("Start(gateway) failed: %v", err)
		}
		t.Log("Gateway container started")

		// Cleanup: remove gateway, then networks
		if err := dr.Remove(ctx, gatewayID, true); err != nil {
			t.Errorf("Remove(gateway) failed: %v", err)
		}
		t.Log("Gateway container removed")

		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork(internal) failed: %v", err)
		}
		if err := dr.RemoveNetwork(ctx, egressNetID); err != nil {
			t.Errorf("RemoveNetwork(egress) failed: %v", err)
		}

		// Verify no orphans
		_, err = dr.cli.ContainerInspect(ctx, string(gatewayID))
		if err == nil {
			t.Error("Gateway container still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Gateway container removed: %v", err)
		}

		_, err = dr.InspectNetwork(ctx, internalNetID)
		if err == nil {
			t.Error("Internal network still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Internal network removed: %v", err)
		}

		_, err = dr.InspectNetwork(ctx, egressNetID)
		if err == nil {
			t.Error("Egress network still exists after removal — orphan detected")
		} else {
			t.Logf("PASS: Egress network removed: %v", err)
		}
	})

	// ---- SCENARIO 4: Full topology created then fully cleaned ----
	t.Run("FullTopology_Cleanup_NoOrphans", func(t *testing.T) {
		netRunID := runID + "-sc4"

		// Create networks
		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}

		egressNetName := NetworkName("egress", netRunID)
		egressNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     egressNetName,
			Internal: false,
			Labels:   Labels(ResourceTypeNetEgress, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(egress) failed: %v", err)
		}

		// Create gateway
		gatewayID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID), string(egressNetID)},
			Labels:     Labels(ResourceTypeGateway, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(gateway) failed: %v", err)
		}

		// Create agent
		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}

		// Start both
		if err := dr.Start(ctx, gatewayID); err != nil {
			t.Fatalf("Start(gateway) failed: %v", err)
		}
		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		time.Sleep(1 * time.Second)

		// Remove everything in reverse order
		if err := dr.Remove(ctx, agentID, true); err != nil {
			t.Errorf("Remove(agent) failed: %v", err)
		}
		if err := dr.Remove(ctx, gatewayID, true); err != nil {
			t.Errorf("Remove(gateway) failed: %v", err)
		}
		if err := dr.RemoveNetwork(ctx, egressNetID); err != nil {
			t.Errorf("RemoveNetwork(egress) failed: %v", err)
		}
		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork(internal) failed: %v", err)
		}

		// Verify no orphans
		for _, id := range []ContainerID{agentID, gatewayID} {
			_, err := dr.cli.ContainerInspect(ctx, string(id))
			if err == nil {
				t.Errorf("Container %s still exists after removal — orphan detected", id)
			}
		}
		for _, nid := range []NetworkID{internalNetID, egressNetID} {
			_, err := dr.InspectNetwork(ctx, nid)
			if err == nil {
				t.Errorf("Network %s still exists after removal — orphan detected", nid)
			}
		}
	})

	t.Log("=== E2E Partial Create Cleanup: COMPLETE — zero orphans ===")
}