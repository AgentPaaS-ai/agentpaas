package openshellrt

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEnsureLocalGatewaySmoke(t *testing.T) {
	if os.Getenv("AGENTPAAS_OPENSHELL_SMOKE") != "1" {
		t.Skip("set AGENTPAAS_OPENSHELL_SMOKE=1 to drive the local gateway")
	}
	home := t.TempDir()
	bin := os.Getenv("OPENSHELL_BIN_DIR")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	rt, err := EnsureLocal(ctx, home, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.Close() }()
	ver, err := rt.GatewayVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ver == "" {
		t.Fatal("empty gateway version")
	}
	t.Logf("gateway %s cli %s", ver, rt.CLIVersion())
	sb, err := rt.CreateAgentSandbox(ctx, SandboxRequest{
		Name:        "ap-smoke",
		Image:       "localhost:5001/agentpaas/o02-golden:0.1.0",
		Env:         map[string]string{"AGENTPAAS_OPENSHELL": "1", "AGENTPAAS_EGRESS_FIREWALL": "0"},
		Rules:       []EgressRule{{Domain: "wttr.in", Ports: []int{443}}},
		HarnessPort: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sandbox %s url %s", sb.Name, sb.ServiceURL)
	if err := rt.DeleteSandbox(context.Background(), sb.Name); err != nil {
		t.Fatal(err)
	}
}
