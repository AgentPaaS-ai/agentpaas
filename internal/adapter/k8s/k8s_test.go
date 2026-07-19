package k8s

import (
	"context"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	_ port.WorkloadRuntime = (*K8sWorkloadRuntime)(nil)
	_ port.TransactionalStateStore = (*K8sStateStore)(nil)
	_ port.EventStore = (*K8sEventStore)(nil)
	_ port.ArtifactStore = (*K8sArtifactStore)(nil)
	_ port.EgressEnforcer = (*K8sEgressEnforcer)(nil)
	_ port.IngressEnforcer = (*K8sIngressEnforcer)(nil)
	_ port.SecretBroker = (*K8sSecretBroker)(nil)
	_ port.PackageStore = (*K8sPackageStore)(nil)
	_ port.MeteringSink = (*K8sMeteringSink)(nil)
	_ port.Clock = (*K8sClock)(nil)
	_ port.LeaseStore = (*K8sLeaseStore)(nil)
)

func TestNewK8sAdapterInitializesPorts(t *testing.T) {
	a := NewK8sAdapter(K8sAdapterDeps{Clientset: fake.NewSimpleClientset(), Namespace: "default"})
	if a == nil || a.Runtime == nil || a.State == nil || a.Events == nil || a.Artifacts == nil || a.Egress == nil || a.Ingress == nil || a.Secrets == nil || a.Packages == nil || a.Metering == nil || a.Clock == nil || a.Leases == nil {
		t.Fatal("NewK8sAdapter must initialize every port adapter")
	}
}

func TestK8sEgressDefaultsToDenyAndMatchesRules(t *testing.T) {
	a := NewK8sAdapter(K8sAdapterDeps{Clientset: fake.NewSimpleClientset(), Namespace: "default"})
	if got := a.Egress.Check(context.Background(), "workload", "example.com:443"); got.Action != port.CommDeny { t.Fatalf("got %q", got.Action) }
	if err := a.Egress.Apply(context.Background(), "workload", port.CommSnapshot{Rules: []port.CommRule{{Host: "example.com", Port: 443, Action: port.CommAllow}}, Default: port.CommDeny}); err != nil { t.Fatal(err) }
	if got := a.Egress.Check(context.Background(), "workload", "example.com:443"); got.Action != port.CommAllow { t.Fatalf("got %q", got.Action) }
}

func TestK8sPrepareCreatesSecurePod(t *testing.T) {
	client := fake.NewSimpleClientset()
	a := NewK8sAdapter(K8sAdapterDeps{Clientset: client, Namespace: "default"})
	id, err := a.Runtime.Prepare(context.Background(), port.PrepareRequest{TenantID: "tenant", RunID: "run", AttemptID: "attempt", ImageDigest: "busybox:latest", ResourcePolicy: port.ResourcePolicy{MemoryMB: 32}})
	if err != nil { t.Fatal(err) }
	pod, err := client.CoreV1().Pods("default").Get(context.Background(), string(id), metav1.GetOptions{})
	if err != nil { t.Fatal(err) }
	if pod.Labels["agentpaas.tenant"] != "tenant" || pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot { t.Fatal("pod missing tenant/security policy") }
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem == nil || !*pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem { t.Fatal("pod missing readonly root") }
}
