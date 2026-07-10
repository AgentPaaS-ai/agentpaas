package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/runtime"
)

func TestRun_SetsProxyEnvWhenGatewayIPAvailable(t *testing.T) {
	const gatewayIP = "172.18.0.42"

	var capturedSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.inspectContainerIPFunc = func(_ context.Context, id runtime.ContainerID, networkID string) (string, error) {
		if id != "gateway-test" {
			t.Fatalf("InspectContainerIP id = %q, want gateway-test", id)
		}
		if networkID != "network-internal" {
			t.Fatalf("InspectContainerIP networkID = %q, want network-internal", networkID)
		}
		return gatewayIP, nil
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		capturedSpec = spec
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.GetRunId() == "" {
		t.Fatal("Run() returned empty run_id")
	}

	wantGateway := []string{
		fmt.Sprintf("AGENTPAAS_GATEWAY_IP=%s", gatewayIP),
		fmt.Sprintf("AGENTPAAS_GATEWAY_URL=http://%s:7799", gatewayIP),
	}
	for _, want := range wantGateway {
		if !containsEnv(capturedSpec.Env, want) {
			t.Fatalf("ContainerSpec.Env = %v, want to contain %q", capturedSpec.Env, want)
		}
	}
	// Forward-proxy CONNECT env vars must not be set (Bug 021).
	for _, env := range capturedSpec.Env {
		if isLegacyProxyEnvVar(env) {
			t.Fatalf("unexpected legacy proxy env %q (gateway-native routing uses AGENTPAAS_GATEWAY_URL)", env)
		}
	}
}

func TestRun_OmitsProxyEnvWhenNoGatewayIP(t *testing.T) {
	var capturedSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.inspectContainerIPFunc = func(context.Context, runtime.ContainerID, string) (string, error) {
		return "", nil
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		capturedSpec = spec
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.GetRunId() == "" {
		t.Fatal("Run() returned empty run_id")
	}

	for _, env := range capturedSpec.Env {
		if isLegacyProxyEnvVar(env) || strings.HasPrefix(env, "AGENTPAAS_GATEWAY_URL=") {
			t.Fatalf("unexpected gateway/proxy env %q when gateway IP unavailable", env)
		}
	}

	wantEnv := []string{
		"AGENTPAAS_AUDIT_PATH=/audit/harness-audit.jsonl",
		"AGENTPAAS_AGENT_PATH=/app/main.py",
	}
	for _, want := range wantEnv {
		if !containsEnv(capturedSpec.Env, want) {
			t.Fatalf("ContainerSpec.Env = %v, want to contain %q", capturedSpec.Env, want)
		}
	}
}

func TestRun_OmitsProxyEnvWhenInspectContainerIPFails(t *testing.T) {
	var capturedSpec runtime.ContainerSpec
	mock := defaultMockRuntimeDriver()
	mock.inspectContainerIPFunc = func(context.Context, runtime.ContainerID, string) (string, error) {
		return "", fmt.Errorf("inspect failed")
	}
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		capturedSpec = spec
		return runtime.ContainerID("container-test"), nil
	}

	server, _ := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if resp.GetRunId() == "" {
		t.Fatal("Run() returned empty run_id")
	}

	for _, env := range capturedSpec.Env {
		if isLegacyProxyEnvVar(env) || strings.HasPrefix(env, "AGENTPAAS_GATEWAY_URL=") {
			t.Fatalf("unexpected gateway/proxy env %q when InspectContainerIP failed", env)
		}
	}
}

// isLegacyProxyEnvVar reports forward-proxy CONNECT env vars (removed in Bug 021).
func isLegacyProxyEnvVar(env string) bool {
	prefixes := []string{
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"http_proxy=",
		"https_proxy=",
		"NO_PROXY=",
		"no_proxy=",
	}
	for _, prefix := range prefixes {
		if len(env) >= len(prefix) && env[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// isProxyEnvVar is kept for adversary suite compatibility; treats legacy
// proxy vars and the new AGENTPAAS_GATEWAY_URL as gateway-related env.
func isProxyEnvVar(env string) bool {
	if isLegacyProxyEnvVar(env) {
		return true
	}
	return strings.HasPrefix(env, "AGENTPAAS_GATEWAY_URL=")
}