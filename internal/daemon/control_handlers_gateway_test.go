package daemon

import (
	"context"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

func TestRun_CreatesGatewayAndEgressNetwork(t *testing.T) {
	var networkSpecs []runtime.NetworkSpec
	var containerSpecs []runtime.ContainerSpec

	mock := defaultMockRuntimeDriver()
	mock.createNetworkFunc = func(_ context.Context, spec runtime.NetworkSpec) (runtime.NetworkID, error) {
		networkSpecs = append(networkSpecs, spec)
		if spec.Internal {
			return runtime.NetworkID("network-internal"), nil
		}
		return runtime.NetworkID("network-egress"), nil
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		containerSpecs = append(containerSpecs, spec)
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()
	if runID == "" {
		t.Fatal("Run() returned empty run_id")
	}

	if len(networkSpecs) != 2 {
		t.Fatalf("CreateNetwork call count = %d, want 2", len(networkSpecs))
	}

	var internalNet, egressNet *runtime.NetworkSpec
	for i := range networkSpecs {
		switch networkSpecs[i].Labels[runtime.LabelResourceType] {
		case runtime.ResourceTypeNetInternal:
			internalNet = &networkSpecs[i]
		case runtime.ResourceTypeNetEgress:
			egressNet = &networkSpecs[i]
		}
	}
	if internalNet == nil {
		t.Fatal("internal network not created")
	}
	if !internalNet.Internal {
		t.Fatal("internal network must have Internal=true")
	}
	if egressNet == nil {
		t.Fatal("egress network not created")
	}
	if egressNet.Internal {
		t.Fatal("egress network must have Internal=false")
	}

	if len(containerSpecs) != 2 {
		t.Fatalf("Create container call count = %d, want 2 (gateway + agent)", len(containerSpecs))
	}

	var gatewaySpec, agentSpec *runtime.ContainerSpec
	for i := range containerSpecs {
		switch containerSpecs[i].Labels[runtime.LabelResourceType] {
		case runtime.ResourceTypeGateway:
			gatewaySpec = &containerSpecs[i]
		case runtime.ResourceTypeAgent:
			agentSpec = &containerSpecs[i]
		}
	}
	if gatewaySpec == nil {
		t.Fatal("gateway container not created")
	}
	if gatewaySpec.Image != runtime.GatewayImage {
		t.Fatalf("gateway Image = %q, want %q", gatewaySpec.Image, runtime.GatewayImage)
	}
	if gatewaySpec.Labels[runtime.LabelResourceType] != runtime.ResourceTypeGateway {
		t.Fatalf("gateway resource-type label = %q, want %q",
			gatewaySpec.Labels[runtime.LabelResourceType], runtime.ResourceTypeGateway)
	}
	wantGatewayNetworks := []string{"network-internal", "network-egress"}
	if len(gatewaySpec.NetworkIDs) != len(wantGatewayNetworks) {
		t.Fatalf("gateway NetworkIDs = %v, want %v", gatewaySpec.NetworkIDs, wantGatewayNetworks)
	}
	for _, want := range wantGatewayNetworks {
		found := false
		for _, got := range gatewaySpec.NetworkIDs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("gateway NetworkIDs = %v, missing %q", gatewaySpec.NetworkIDs, want)
		}
	}

	if agentSpec == nil {
		t.Fatal("agent container not created")
	}
	if len(agentSpec.NetworkIDs) != 1 || agentSpec.NetworkIDs[0] != "network-internal" {
		t.Fatalf("agent NetworkIDs = %v, want [network-internal]", agentSpec.NetworkIDs)
	}

	server.runMu.Lock()
	tracked, ok := server.runs[runID]
	server.runMu.Unlock()
	if !ok {
		t.Fatalf("run %q not tracked", runID)
	}
	if tracked.Gateway != "gateway-test" {
		t.Fatalf("tracked Gateway = %q, want gateway-test", tracked.Gateway)
	}
	if tracked.Network != "network-internal" {
		t.Fatalf("tracked Network = %q, want network-internal", tracked.Network)
	}
	if tracked.EgressNetwork != "network-egress" {
		t.Fatalf("tracked EgressNetwork = %q, want network-egress", tracked.EgressNetwork)
	}
}