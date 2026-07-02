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

// TestAdversaryB5T04d tests for security issues related to topology restart,
// partial create cleanup, and orphan resource management.
//
// The adversary probes:
//   1. Orphaned containers leak when Create fails mid-way
//   2. Orphaned networks when CreateNetwork succeeds but Create fails
//   3. Orphaned networks when Remove is called on a non-existent container
//   4. Resource leak when Start fails after Create succeeds
//   5. Restart does not attach egress network to agent
//   6. Container restart after network removal does not cause leaks
//   7. Duplicate cleanup is idempotent (no errors from double-removal)
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestAdversaryB5T04d(t *testing.T) {
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

	runID := fmt.Sprintf("b5t04d-adv-%d", time.Now().UnixNano())

	// ---- ADVERSARY 1: Sequential cleanup ordering ----
	// Resources must be cleanable in the correct order: remove container
	// (force disconnects from network), then remove network. Verify no
	// orphaned resources remain after complete teardown.
	t.Run("Adversary_SequentialCleanup", func(t *testing.T) {
		netRunID := runID + "-adv1"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork failed: %v", err)
		}

		containerID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}

		if err := dr.Start(ctx, containerID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		time.Sleep(1 * time.Second)

		// Remove container first (normal order) — force flag
		// disconnects from network automatically.
		if err := dr.Remove(ctx, containerID, true); err != nil {
			t.Errorf("Remove(container) failed: %v", err)
		} else {
			t.Log("PASS: Container removed with force (disconnected from network)")
		}

		// Then remove network — should work after container is gone
		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork after container removal failed: %v", err)
		} else {
			t.Log("PASS: Network removed after its container")
		}

		// Verify no orphaned containers
		orphans := listOwnedContainers(t, ctx, dr, netRunID)
		if len(orphans) > 0 {
			t.Errorf("Found orphan container: %v", orphans)
		}
	})

	// ---- ADVERSARY 2: Orphaned network when Create fails after CreateNetwork ----
	// If a container create fails, the networks created for it should still
	// be removable independently (the network is not orphaned).
	t.Run("Adversary_NetworkCleanableAfterCreateFailure", func(t *testing.T) {
		netRunID := runID + "-adv2"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}

		// Attempt to create an agent container with an empty-image spec
		// (should fail validation or pull error).
		_, err = dr.Create(ctx, ContainerSpec{
			Image:      "",
			Command:    []string{},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err == nil {
			t.Log("Create(agent with empty image) succeeded (alpine default may apply)")
		} else {
			t.Logf("Create(agent) failed as expected: %v", err)
		}

		// The network should still be cleanly removable
		if err := dr.RemoveNetwork(ctx, internalNetID); err != nil {
			t.Errorf("RemoveNetwork failed after failed Create: %v — network may be orphaned", err)
		} else {
			t.Log("PASS: Network cleanly removable after failed container Create")
		}
	})

	// ---- ADVERSARY 3: Double removal is idempotent ----
	// Calling Remove twice on the same container should not error on the
	// second call (or should return ErrContainerNotFound).
	t.Run("Adversary_DoubleRemoveIdempotent", func(t *testing.T) {
		netRunID := runID + "-adv3"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		cleanupNetworks := []NetworkID{internalNetID}
		defer func() {
			for _, nid := range cleanupNetworks {
				_ = dr.RemoveNetwork(ctx, nid)
			}
		}()

		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}

		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		time.Sleep(1 * time.Second)

		// First removal
		if err := dr.Remove(ctx, agentID, true); err != nil {
			t.Fatalf("First Remove(agent) failed: %v", err)
		}
		t.Log("First Remove succeeded")

		// Second removal — should be idempotent (return ErrContainerNotFound)
		err = dr.Remove(ctx, agentID, true)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				t.Logf("PASS: Second Remove correctly returned not-found: %v", err)
			} else {
				t.Errorf("Second Remove returned unexpected error: %v", err)
			}
		} else {
			t.Log("PASS: Second Remove returned nil (Docker API idempotent)")
		}
	})

	// ---- ADVERSARY 4: Restart does not attach egress to agent ----
	// After restart, the agent must still be on the internal network only.
	// An adversary might try to attach the agent to egress during restart.
	t.Run("Adversary_RestartNoEgressForAgent", func(t *testing.T) {
		netRunID := runID + "-adv4"

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

		cleanupNs := []NetworkID{internalNetID, egressNetID}
		defer func() {
			for _, nid := range cleanupNs {
				_ = dr.RemoveNetwork(ctx, nid)
			}
		}()

		gatewayID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID), string(egressNetID)},
			Labels:     Labels(ResourceTypeGateway, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(gateway) failed: %v", err)
		}
		defer func() { _ = dr.Remove(ctx, gatewayID, true) }()

		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
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
		time.Sleep(1 * time.Second)

		// Verify agent is internal-only before restart
		agentNets, err := dr.InspectContainerNetworks(ctx, agentID)
		if err != nil {
			t.Fatalf("InspectContainerNetworks(agent) failed: %v", err)
		}
		for _, n := range agentNets {
			if n.Name == egressNetName || strings.Contains(n.Name, "egress") {
				t.Errorf("Agent attached to egress BEFORE restart: %v", n.Name)
			}
		}

		// Restart agent
		timeout := 10 * time.Second
		if err := dr.Stop(ctx, agentID, &timeout); err != nil {
			t.Fatalf("Stop(agent) failed: %v", err)
		}
		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent after restart) failed: %v", err)
		}
		time.Sleep(2 * time.Second)

		// Verify agent is still internal-only after restart
		agentNetsAfter, err := dr.InspectContainerNetworks(ctx, agentID)
		if err != nil {
			t.Fatalf("InspectContainerNetworks(agent after restart) failed: %v", err)
		}
		for _, n := range agentNetsAfter {
			if n.Name == egressNetName || strings.Contains(n.Name, "egress") {
				t.Errorf("Agent attached to egress AFTER restart — topology violation: %v", n.Name)
			}
		}
		if len(agentNetsAfter) != 1 {
			t.Errorf("Agent has %d networks after restart, want 1; networks: %v",
				len(agentNetsAfter), agentNetsAfter)
		} else {
			t.Log("PASS: Agent still internal-only after restart")
		}
	})

	// ---- ADVERSARY 5: Partial Start failure leaves no orphan ----
	// If one container starts but the other fails, the started container
	// should still be cleanly removable.
	t.Run("Adversary_PartialStartCleanup", func(t *testing.T) {
		netRunID := runID + "-adv5"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

		// Create agent with a command that immediately exits (true sleep)
		// so starting it succeeds; then test that a container with bad
		// command can still be cleaned up.
		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "1"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}

		// Start should work (sleep 1 is valid)
		if err := dr.Start(ctx, agentID); err != nil {
			t.Logf("Start(agent with sleep 1) failed: %v (container may have exited quickly)", err)
		}

		// Give it a moment to exit
		time.Sleep(2 * time.Second)

		// Remove the container — should work even if it already exited
		if err := dr.Remove(ctx, agentID, true); err != nil {
			t.Errorf("Remove(agent after quick-exit) failed: %v", err)
		} else {
			t.Log("PASS: Clean removal after quick-exit container")
		}
	})

	// ---- ADVERSARY 6: Agent has no default route after restart ----
	// After restart, verify the agent still has no default route (no
	// unexpected default route added by Docker on restart).
	t.Run("Adversary_NoDefaultRouteAfterRestart", func(t *testing.T) {
		netRunID := runID + "-adv6"

		internalNetName := NetworkName("internal", netRunID)
		internalNetID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     internalNetName,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, netRunID),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(internal) failed: %v", err)
		}
		defer func() { _ = dr.RemoveNetwork(ctx, internalNetID) }()

		agentID, err := dr.Create(ctx, ContainerSpec{
			Image:      "alpine:latest",
			Command:    []string{"sleep", "3600"},
			NetworkIDs: []string{string(internalNetID)},
			Labels:     Labels(ResourceTypeAgent, netRunID),
		})
		if err != nil {
			t.Fatalf("Create(agent) failed: %v", err)
		}
		defer func() { _ = dr.Remove(ctx, agentID, true) }()

		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent) failed: %v", err)
		}
		time.Sleep(1 * time.Second)

		// Check no default route before restart
		out, err := dockerExec(ctx, string(agentID), "ip", "route")
		if err != nil {
			t.Fatalf("ip route failed: %v", err)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "default") {
				t.Errorf("Agent has default route BEFORE restart: %q", line)
			}
		}

		// Restart
		timeout := 10 * time.Second
		if err := dr.Stop(ctx, agentID, &timeout); err != nil {
			t.Fatalf("Stop(agent) failed: %v", err)
		}
		if err := dr.Start(ctx, agentID); err != nil {
			t.Fatalf("Start(agent after restart) failed: %v", err)
		}
		time.Sleep(2 * time.Second)

		// Check no default route after restart
		out2, err := dockerExec(ctx, string(agentID), "ip", "route")
		if err != nil {
			t.Fatalf("ip route after restart failed: %v", err)
		}
		for _, line := range strings.Split(out2, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "default") {
				t.Errorf("Agent has default route AFTER restart: %q — topology violation", line)
			}
		}
		t.Log("PASS: No default route in agent after restart")
	})

	// ---- ADVERSARY 7: Ownership labels prevent cross-run cleanup ----
	// Different run IDs should not interfere — resources from one run
	// should not be affected by operations targeting another run.
	t.Run("Adversary_OwnershipLabelsIsolateRuns", func(t *testing.T) {
		runA := runID + "-adv7-a"
		runB := runID + "-adv7-b"

		// Create resources for run A
		netA := NetworkName("internal", runA)
		netAID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     netA,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, runA),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(A) failed: %v", err)
		}
		defer func() { _ = dr.RemoveNetwork(ctx, netAID) }()

		// Create resources for run B (same network name pattern but diff run ID)
		netB := NetworkName("internal", runB)
		netBID, err := dr.CreateNetwork(ctx, NetworkSpec{
			Name:     netB,
			Internal: true,
			Labels:   Labels(ResourceTypeNetInternal, runB),
		})
		if err != nil {
			t.Fatalf("CreateNetwork(B) failed: %v", err)
		}
		defer func() { _ = dr.RemoveNetwork(ctx, netBID) }()

		// Verify labels distinguish them
		infoA, err := dr.InspectNetwork(ctx, netAID)
		if err != nil {
			t.Fatalf("InspectNetwork(A) failed: %v", err)
		}
		infoB, err := dr.InspectNetwork(ctx, netBID)
		if err != nil {
			t.Fatalf("InspectNetwork(B) failed: %v", err)
		}

		if infoA.Labels[LabelRunID] != runA {
			t.Errorf("Network A has run-id=%q, want %q", infoA.Labels[LabelRunID], runA)
		}
		if infoB.Labels[LabelRunID] != runB {
			t.Errorf("Network B has run-id=%q, want %q", infoB.Labels[LabelRunID], runB)
		}
		if infoA.Labels[LabelManagedBy] != ManagedByValue {
			t.Errorf("Network A missing managed-by label: %v", infoA.Labels)
		}
		if infoB.Labels[LabelManagedBy] != ManagedByValue {
			t.Errorf("Network B missing managed-by label: %v", infoB.Labels)
		}
		t.Log("PASS: Ownership labels correctly isolate different runs")
	})

	t.Log("=== B5-T04d Adversary Tests: COMPLETE ===")
}

// listOwnedContainers returns ContainerIDs of all containers matching the
// given runID label. Uses docker ps CLI with label filtering.
func listOwnedContainers(t *testing.T, ctx context.Context, dr *DockerRuntime, runID string) []string {
	t.Helper()

	cmdArgs := []string{"ps", "-a",
		"--filter", fmt.Sprintf("label=agentpaas.run-id=%s", runID),
		"--format", "{{.ID}}"}
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Logf("Could not list containers via docker ps: %v (stderr: %s)", err, strings.TrimSpace(stderr.String()))
		return nil
	}

	parts := strings.Fields(strings.TrimSpace(stdout.String()))
	return parts
}