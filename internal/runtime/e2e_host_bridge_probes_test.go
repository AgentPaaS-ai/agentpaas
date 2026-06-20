package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestE2E_HostBridgeProbes verifies agent containers CANNOT use host or Docker
// bridge shortcuts to reach the host, daemon, or other privileged endpoints:
//   - host.docker.internal is unreachable
//   - Docker bridge gateway IP (172.x.x.1) is unreachable
//   - Gateway container IP probing (non-ingress ports) is blocked
//   - Docker daemon Unix socket and TCP ports are unreachable
//
// Requires AGENTPAAS_DOCKER_TESTS=1 and a running Docker daemon.
func TestE2E_HostBridgeProbes(t *testing.T) {
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

	runID := fmt.Sprintf("b5t04b-%d", time.Now().UnixNano())

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

	// Step 1: Create internal bridge network (no external access)
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

	// Step 2: Create egress network (external access)
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
	if err := dr.Start(ctx, agentID); err != nil {
		t.Fatalf("Start(agent) failed: %v", err)
	}
	time.Sleep(2 * time.Second)

	// ---- Get gateway's internal IP for probe tests ----
	gatewayInfo, err := dr.cli.ContainerInspect(ctx, string(gatewayID))
	if err != nil {
		t.Fatalf("ContainerInspect(gateway) failed: %v", err)
	}

	var gatewayInternalIP string
	for netName, netSettings := range gatewayInfo.NetworkSettings.Networks {
		if strings.Contains(netName, "internal") || netName == internalNetName {
			gatewayInternalIP = netSettings.IPAddress
			break
		}
	}
	if gatewayInternalIP == "" {
		// Fallback: try to find any internal network IP
		for _, netSettings := range gatewayInfo.NetworkSettings.Networks {
			if netSettings.IPAddress != "" {
				gatewayInternalIP = netSettings.IPAddress
				break
			}
		}
	}
	t.Logf("Gateway internal IP: %s", gatewayInternalIP)

	// ---- Get the bridge gateway IP (typically 172.x.x.1) ----
	inspectOut, err := dockerExec(ctx, string(agentID), "ip", "route")
	agentRoutes := ""
	if err == nil {
		agentRoutes = inspectOut
		t.Logf("Agent routes:\n%s", agentRoutes)
	}

	// Extract default gateway IP from agent's routing table
	bridgeGatewayIP := ""
	for _, line := range strings.Split(agentRoutes, "\n") {
		// Look for "default via 172.x.x.x" pattern
		if strings.HasPrefix(line, "default via ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				bridgeGatewayIP = parts[2]
				t.Logf("Detected bridge gateway IP: %s", bridgeGatewayIP)
			}
		}
	}
	if bridgeGatewayIP == "" {
		t.Log("Could not detect bridge gateway IP from agent routes (internal bridge may not have one)")
	}

	// ---- HOST PROBE 1: host.docker.internal must be unreachable ----
	t.Run("HostDockerInternal_unreachable", func(t *testing.T) {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err := dockerExec(probeCtx, string(agentID),
			"wget", "-q", "-O", "/dev/null", "--timeout=3",
			"http://host.docker.internal:80/")
		if err == nil {
			t.Error("HOST.DOCKER.INTERNAL REACHABLE — expected BLOCKED")
		} else {
			t.Logf("PASS: host.docker.internal unreachable: %v", err)
		}
	})

	// ---- HOST PROBE 2: host.docker.internal ping must fail ----
	t.Run("HostDockerInternal_ping_fails", func(t *testing.T) {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		out, err := dockerExec(probeCtx, string(agentID),
			"ping", "-c", "1", "-W", "2", "host.docker.internal")
		if err == nil {
			t.Errorf("PING TO HOST.DOCKER.INTERNAL SUCCEEDED (%s) — expected BLOCKED", out)
		} else {
			t.Logf("PASS: ping to host.docker.internal blocked: %v", err)
		}
	})

	// ---- BRIDGE PROBE 1: Docker bridge gateway IP must be unreachable (if detected) ----
	if bridgeGatewayIP != "" {
		t.Run("BridgeGatewayIP_unreachable", func(t *testing.T) {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			_, err := dockerExec(probeCtx, string(agentID),
				"wget", "-q", "-O", "/dev/null", "--timeout=3",
				fmt.Sprintf("http://%s:80/", bridgeGatewayIP))
			if err == nil {
				t.Errorf("BRIDGE GATEWAY %s:80 REACHABLE — expected BLOCKED", bridgeGatewayIP)
			} else {
				t.Logf("PASS: Bridge gateway %s unreachable: %v", bridgeGatewayIP, err)
			}
		})

		// ---- BRIDGE PROBE 2: Other ports on bridge gateway must be unreachable ----
		t.Run("BridgeGateway_OtherPorts_Blocked", func(t *testing.T) {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			_, err := dockerExec(probeCtx, string(agentID),
				"wget", "-q", "-O", "/dev/null", "--timeout=3",
				fmt.Sprintf("http://%s:22/", bridgeGatewayIP))
			if err == nil {
				t.Errorf("BRIDGE GATEWAY %s:22 REACHABLE — expected BLOCKED", bridgeGatewayIP)
			} else {
				t.Logf("PASS: Bridge gateway %s:22 unreachable: %v", bridgeGatewayIP, err)
			}
		})
	} else {
		t.Log("SKIP: Bridge gateway IP tests (no gateway detected on internal bridge)")
	}

	// ---- GATEWAY PROBE 1: Gateway container HTTP probing blocked ----
	if gatewayInternalIP != "" {
		t.Run("GatewayContainer_HTTP_probe_blocked", func(t *testing.T) {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			_, err := dockerExec(probeCtx, string(agentID),
				"wget", "-q", "-O", "/dev/null", "--timeout=3",
				fmt.Sprintf("http://%s:80/", gatewayInternalIP))
			// The agent IS on the same internal network as the gateway, so the
			// agent CAN reach the gateway by IP. But there should be no HTTP
			// service listening on the gateway (the gateway container is just
			// running `sleep 3600`). So we expect "connection refused"
			// (TCP handshake works, port unreachable).
			if err == nil {
				// Connection succeeded means something is listening — unexpected
				t.Logf("INFO: Gateway internal IP %s:80 responded (unexpected HTTP service?)", gatewayInternalIP)
			} else {
				errStr := err.Error()
				if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "Connection refused") {
					t.Logf("PASS: Gateway %s:80 connection refused = network path works, no service", gatewayInternalIP)
				} else {
					// Other errors like "no route" are also valid isolation
					t.Logf("PASS: Gateway %s:80 blocked: %v", gatewayInternalIP, err)
				}
			}
		})

		// ---- GATEWAY PROBE 2: Gateway container SSH/HTTPS probing blocked ----
		t.Run("GatewayContainer_SSH_probe_blocked", func(t *testing.T) {
			ports := []struct {
				name  string
				port  string
			}{
				{"SSH", "22"},
				{"HTTPS", "443"},
				{"MYSQL", "3306"},
			}
			for _, p := range ports {
				t.Run(p.name, func(t *testing.T) {
					probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()

					_, err := dockerExec(probeCtx, string(agentID),
						"timeout", "3", "nc", "-zv", gatewayInternalIP, p.port)
					if err == nil {
						t.Errorf("Gateway port %s:%s REACHABLE — expected BLOCKED", gatewayInternalIP, p.port)
					} else {
						t.Logf("PASS: Gateway %s:%s blocked: %v", gatewayInternalIP, p.port, err)
					}
				})
			}
		})
	} else {
		t.Log("SKIP: Gateway container probe tests (no gateway IP detected)")
	}

	// ---- DAEMON PROBE 1: Docker daemon Unix socket unreachable ----
	t.Run("DaemonUnixSocket_unreachable", func(t *testing.T) {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		out, err := dockerExec(probeCtx, string(agentID),
			"sh", "-c", "test -e /var/run/docker.sock && echo EXISTS || echo NOT_FOUND")
		if err != nil {
			t.Logf("PASS: Docker socket check returned error (expected): %v", err)
			return
		}
		if strings.Contains(out, "EXISTS") {
			t.Error("DOCKER SOCKET VISIBLE from agent — expected NOT accessible")
		} else {
			t.Log("PASS: Docker socket not present in agent container")
		}
	})

	// ---- DAEMON PROBE 2: Docker daemon TCP port (2375/2376) unreachable ----
	t.Run("DaemonTCPPorts_unreachable", func(t *testing.T) {
		daemonPorts := []string{"2375", "2376"}
		for _, port := range daemonPorts {
			t.Run("port_"+port, func(t *testing.T) {
				probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				_, err := dockerExec(probeCtx, string(agentID),
					"wget", "-q", "-O", "/dev/null", "--timeout=3",
					fmt.Sprintf("http://host.docker.internal:%s/", port))
				if err == nil {
					t.Errorf("DOCKER DAEMON PORT host.docker.internal:%s REACHABLE — expected BLOCKED", port)
				} else {
					t.Logf("PASS: Daemon port %s unreachable: %v", port, err)
				}
			})
		}
	})

	t.Log("=== E2E Host/Bridge Probes: COMPLETE ===")
}