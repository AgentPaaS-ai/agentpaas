package docker

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

var (
	_ port.WorkloadRuntime         = (*DockerWorkloadRuntime)(nil)
	_ port.TransactionalStateStore = (*DockerStateStore)(nil)
	_ port.EventStore              = (*DockerEventStore)(nil)
	_ port.ArtifactStore           = (*DockerArtifactStore)(nil)
	_ port.EgressEnforcer          = (*DockerEgressEnforcer)(nil)
	_ port.IngressEnforcer         = (*DockerIngressEnforcer)(nil)
	_ port.SecretBroker            = (*DockerSecretBroker)(nil)
	_ port.PackageStore            = (*DockerPackageStore)(nil)
	_ port.MeteringSink            = (*DockerMeteringSink)(nil)
	_ port.Clock                   = (*DockerClock)(nil)
	_ port.LeaseStore              = (*DockerLeaseStore)(nil)
)

func TestNewDockerAdapterInitializesAllPorts(t *testing.T) {
	a := NewDockerAdapter(DockerAdapterDeps{})
	if a == nil || a.Runtime == nil || a.State == nil || a.Events == nil || a.Artifacts == nil || a.Egress == nil || a.Ingress == nil || a.Secrets == nil || a.Packages == nil || a.Metering == nil || a.Clock == nil || a.Leases == nil {
		t.Fatal("NewDockerAdapter must initialize every port adapter")
	}
}

func TestCommDecisionDefaultsToDeny(t *testing.T) {
	e := NewDockerAdapter(DockerAdapterDeps{}).Egress
	if got := e.Check(context.TODO(), "tenant/workload", "example.com:443"); got.Action != port.CommDeny {
		t.Fatalf("action = %q, want deny", got.Action)
	}
}
