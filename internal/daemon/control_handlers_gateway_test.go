package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/pack"
	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
	"gopkg.in/yaml.v3"
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

func TestRun_DefaultDenyGatewayWhenNoPolicy(t *testing.T) {
	var containerSpecs []runtime.ContainerSpec

	mock := defaultMockRuntimeDriver()
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		containerSpecs = append(containerSpecs, spec)
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		return runtime.ContainerID("container-test"), nil
	}

	server, hp := testServerWithMockRuntime(t, mock)

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()

	var gatewaySpec *runtime.ContainerSpec
	for i := range containerSpecs {
		if containerSpecs[i].Labels[runtime.LabelResourceType] == runtime.ResourceTypeGateway {
			gatewaySpec = &containerSpecs[i]
			break
		}
	}
	if gatewaySpec == nil {
		t.Fatal("gateway container not created")
	}
	if gatewaySpec.User != "64000" {
		t.Fatalf("gateway User = %q, want 64000", gatewaySpec.User)
	}
	if len(gatewaySpec.Binds) != 1 {
		t.Fatalf("gateway Binds = %v, want one bind", gatewaySpec.Binds)
	}
	if !strings.HasSuffix(gatewaySpec.Binds[0], ":/config.yaml:ro") {
		t.Fatalf("gateway Binds[0] = %q, want host path mounted as /config.yaml:ro", gatewaySpec.Binds[0])
	}

	wantConfigDir := filepath.Join(hp.State, "runs", runID, "gateway-config")
	wantConfigPath := filepath.Join(wantConfigDir, "config.yaml")
	gotHostPath := strings.TrimSuffix(gatewaySpec.Binds[0], ":/config.yaml:ro")
	if gotHostPath != wantConfigPath {
		t.Fatalf("gateway config host path = %q, want %q", gotHostPath, wantConfigPath)
	}

	data, err := os.ReadFile(wantConfigPath)
	if err != nil {
		t.Fatalf("read default-deny config: %v", err)
	}
	var decoded struct {
		Config struct {
			DNS struct {
				LookupFamily string `yaml:"lookupFamily"`
			} `yaml:"dns"`
		} `yaml:"config"`
		Binds []any `yaml:"binds"`
	}
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("default-deny config is not valid YAML: %v\n%s", err, string(data))
	}
	if decoded.Config.DNS.LookupFamily != "V4Only" {
		t.Fatalf("lookupFamily = %q, want V4Only", decoded.Config.DNS.LookupFamily)
	}
	if len(decoded.Binds) != 0 {
		t.Fatalf("binds = %v, want empty (deny-all)", decoded.Binds)
	}
	if strings.Contains(string(data), "backends:") {
		t.Fatalf("default-deny config must not contain allow backends:\n%s", string(data))
	}

	server.runMu.Lock()
	tracked, ok := server.runs[runID]
	server.runMu.Unlock()
	if !ok {
		t.Fatalf("run %q not tracked", runID)
	}
	if tracked.GatewayConfigDir != wantConfigDir {
		t.Fatalf("tracked GatewayConfigDir = %q, want %q", tracked.GatewayConfigDir, wantConfigDir)
	}

	_, err = server.Stop(context.Background(), &controlv1.StopRequest{RunId: runID})
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := os.Stat(wantConfigDir); !os.IsNotExist(err) {
		t.Fatalf("gateway config dir %q still exists after Stop, err = %v", wantConfigDir, err)
	}
}

func TestRun_CompilesGatewayConfigFromDeployedPolicy(t *testing.T) {
	agentPolicyYAML := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
  - domain: "cdn.example.com"
    ports: [443]
`

	var containerSpecs []runtime.ContainerSpec

	mock := defaultMockRuntimeDriver()
	mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
		containerSpecs = append(containerSpecs, spec)
		if spec.Image == runtime.GatewayImage {
			return runtime.ContainerID("gateway-test"), nil
		}
		return runtime.ContainerID("container-test"), nil
	}

	server, hp := testServerWithMockRuntime(t, mock)

	deployedDir := pack.DeployedAgentPath(hp.Home, "test-agent")
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), []byte(agentPolicyYAML), 0o600); err != nil {
		t.Fatalf("WriteFile(policy.yaml): %v", err)
	}

	globalGatewayPath := filepath.Join(hp.Config, "gateway.yaml")
	globalConfig := []byte("config:\n  dns:\n    lookupFamily: V4Only\nbinds: []\n# GLOBAL_SHOULD_NOT_BE_USED\n")
	if err := os.WriteFile(globalGatewayPath, globalConfig, 0o600); err != nil {
		t.Fatalf("WriteFile(global gateway.yaml): %v", err)
	}

	resp, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runID := resp.GetRunId()

	var gatewaySpec *runtime.ContainerSpec
	for i := range containerSpecs {
		if containerSpecs[i].Labels[runtime.LabelResourceType] == runtime.ResourceTypeGateway {
			gatewaySpec = &containerSpecs[i]
			break
		}
	}
	if gatewaySpec == nil {
		t.Fatal("gateway container not created")
	}

	wantConfigDir := filepath.Join(hp.State, "runs", runID, "gateway-config")
	wantConfigPath := filepath.Join(wantConfigDir, "config.yaml")
	gotHostPath := strings.TrimSuffix(gatewaySpec.Binds[0], ":/config.yaml:ro")
	if gotHostPath != wantConfigPath {
		t.Fatalf("gateway config host path = %q, want per-run %q (not global gateway.yaml)", gotHostPath, wantConfigPath)
	}

	data, err := os.ReadFile(wantConfigPath)
	if err != nil {
		t.Fatalf("read compiled gateway config: %v", err)
	}
	if strings.Contains(string(data), "GLOBAL_SHOULD_NOT_BE_USED") {
		t.Fatalf("gateway config was read from global gateway.yaml:\n%s", string(data))
	}
	for _, domain := range []string{"api.example.com", "cdn.example.com"} {
		if !strings.Contains(string(data), domain) {
			t.Fatalf("compiled gateway config missing domain %q:\n%s", domain, string(data))
		}
	}

	parsedPolicy, err := policy.ParsePolicy(strings.NewReader(agentPolicyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	wantCompiled, err := policy.CompileGatewayConfig(parsedPolicy)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	if string(data) != string(wantCompiled) {
		t.Fatalf("compiled gateway config mismatch:\ngot:\n%s\nwant:\n%s", string(data), string(wantCompiled))
	}

	server.runMu.Lock()
	tracked, ok := server.runs[runID]
	server.runMu.Unlock()
	if !ok {
		t.Fatalf("run %q not tracked", runID)
	}
	if tracked.GatewayConfigDir != wantConfigDir {
		t.Fatalf("tracked GatewayConfigDir = %q, want %q", tracked.GatewayConfigDir, wantConfigDir)
	}
}