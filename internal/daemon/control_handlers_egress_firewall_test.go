package daemon

import (
	"context"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

func TestRun_SetsEgressFirewallOnAgentSpec(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "1")

	var agentSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
		return "172.20.0.2", nil
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		agentSpec = spec
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_ = resp.GetRunId()

	if len(agentSpec.CapAdd) != 1 || agentSpec.CapAdd[0] != "NET_ADMIN" {
		t.Fatalf("agent CapAdd = %v, want [NET_ADMIN]", agentSpec.CapAdd)
	}
	if !containsEnv(agentSpec.Env, "AGENTPAAS_EGRESS_FIREWALL=1") {
		t.Fatalf("agent env missing AGENTPAAS_EGRESS_FIREWALL=1: %v", agentSpec.Env)
	}
	if !containsEnv(agentSpec.Env, "AGENTPAAS_GATEWAY_IP=172.20.0.2") {
		t.Fatalf("agent env missing gateway IP: %v", agentSpec.Env)
	}
}

func TestRun_OmitsNetAdminWhenEgressFirewallDisabled(t *testing.T) {
	t.Setenv("AGENTPAAS_EGRESS_FIREWALL", "0")

	var agentSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
		return "172.20.0.2", nil
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		agentSpec = spec
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	if _, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(agentSpec.CapAdd) != 0 {
		t.Fatalf("agent CapAdd = %v, want empty when firewall disabled", agentSpec.CapAdd)
	}
	if !containsEnv(agentSpec.Env, "AGENTPAAS_EGRESS_FIREWALL=0") {
		t.Fatalf("agent env missing AGENTPAAS_EGRESS_FIREWALL=0: %v", agentSpec.Env)
	}
}