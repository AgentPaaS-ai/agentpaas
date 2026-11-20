package k8s

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// k8sAvailable returns true if a Kubernetes cluster is reachable.
func k8sAvailable(t *testing.T) (kubernetes.Interface, string, bool) {
	t.Helper()
	if os.Getenv("AGENTPAAS_K8S_TESTS") == "" {
		t.Skip("skipping k8s integration test; set AGENTPAAS_K8S_TESTS=1 to run")
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Logf("k8s config not available: %v", err)
		return nil, "", false
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Logf("k8s client create failed: %v", err)
		return nil, "", false
	}
	ns := "default"
	// Check connectivity
	_, err = clientset.CoreV1().Namespaces().Get(context.Background(), ns, metav1.GetOptions{})
	if err != nil {
		t.Logf("k8s API not reachable: %v", err)
		return nil, "", false
	}
	return clientset, ns, true
}

// TestK8sConformance runs the 10-step portability scenario against a real kind cluster.
func TestK8sConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping k8s integration test in short mode")
	}
	clientset, ns, ok := k8sAvailable(t)
	if !ok {
		t.Skip("kubernetes not available")
	}

	adapter := NewK8sAdapter(K8sAdapterDeps{
		Clientset: clientset,
		Namespace: ns,
	})

	// Step 1: Register tenant A and tenant B.
	t.Run("Step1_RegisterTenants", func(t *testing.T) {
		ctx := context.Background()
		depA := port.DeploymentState{TenantID: "tenant-a", DeploymentID: "dep-1", PackageName: "test", ImageDigest: "sha256:abc"}
		if err := adapter.State.CasDeployment(ctx, depA, 0); err != nil {
			t.Fatalf("cas deployment tenant-a: %v", err)
		}
		depB := port.DeploymentState{TenantID: "tenant-b", DeploymentID: "dep-2", PackageName: "test", ImageDigest: "sha256:def"}
		if err := adapter.State.CasDeployment(ctx, depB, 0); err != nil {
			t.Fatalf("cas deployment tenant-b: %v", err)
		}
		deps, err := adapter.State.ListDeployments(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("list deployments: %v", err)
		}
		for _, d := range deps {
			if d.TenantID != "tenant-a" {
				t.Fatalf("cross-tenant leak: tenant-a list contains %q", d.TenantID)
			}
		}
	})

	// Step 4: Default-deny egress.
	t.Run("Step4_DefaultDenyEgress", func(t *testing.T) {
		ctx := context.Background()
		decision := adapter.Egress.Check(ctx, "workload-k8s-1", "undeclared.example:443")
		if decision.Action != port.CommDeny {
			t.Fatalf("expected deny, got %q", decision.Action)
		}
		snapshot := port.CommSnapshot{
			Digest: "sha256:snap1",
			Rules: []port.CommRule{
				{Host: "allowed.example", Port: 443, Action: port.CommAllow},
			},
			Default: port.CommDeny,
		}
		// Apply creates a real NetworkPolicy in the cluster
		if err := adapter.Egress.Apply(ctx, "workload-k8s-1", snapshot); err != nil {
			t.Fatalf("apply snapshot: %v", err)
		}
		defer func() { _ = adapter.Egress.Remove(ctx, "workload-k8s-1") }()
		decision = adapter.Egress.Check(ctx, "workload-k8s-1", "allowed.example:443")
		if decision.Action != port.CommAllow {
			t.Fatalf("expected allow for declared host, got %q", decision.Action)
		}
		decision = adapter.Egress.Check(ctx, "workload-k8s-1", "undeclared.example:443")
		if decision.Action != port.CommDeny {
			t.Fatalf("expected deny for undeclared host, got %q", decision.Action)
		}
	})

	// Step 7: Ordered events.
	t.Run("Step7_OrderedEvents", func(t *testing.T) {
		ctx := context.Background()
		seq1, err := adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-k8s-1", Type: "progress"})
		if err != nil {
			t.Fatalf("append event 1: %v", err)
		}
		seq2, err := adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-k8s-1", Type: "checkpoint"})
		if err != nil {
			t.Fatalf("append event 2: %v", err)
		}
		if seq2 <= seq1 {
			t.Fatalf("sequences not monotonic: %d then %d", seq1, seq2)
		}
		events, err := adapter.Events.Read(ctx, "tenant-a", "run-k8s-1", 0, 10)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
	})

	// Step 9: Cross-tenant denial.
	t.Run("Step9_CrossTenantDenial", func(t *testing.T) {
		ctx := context.Background()
		_, _ = adapter.Events.Append(ctx, port.Event{TenantID: "tenant-a", RunID: "run-k8s-2", Type: "progress"})
		events, err := adapter.Events.Read(ctx, "tenant-b", "run-k8s-2", 0, 10)
		if err != nil {
			t.Fatalf("read events: %v", err)
		}
		if len(events) != 0 {
			t.Fatalf("cross-tenant leak: tenant-b sees tenant-a events")
		}
	})

	// Step 10: Metering.
	t.Run("Step10_Metering", func(t *testing.T) {
		ctx := context.Background()
		err := adapter.Metering.Record(ctx, port.Measurement{
			TenantID: "tenant-a",
			Type:     port.MeterModel,
			Value:    1000,
			Unit:     "tokens",
		})
		if err != nil {
			t.Fatalf("record measurement: %v", err)
		}
		measurements, err := adapter.Metering.Query(ctx, port.MeasurementFilter{
			TenantID: "tenant-a",
			Type:     port.MeterModel,
		})
		if err != nil {
			t.Fatalf("query measurements: %v", err)
		}
		if len(measurements) < 1 {
			t.Fatalf("expected at least 1 measurement, got %d", len(measurements))
		}
		summary, err := adapter.Metering.Summary(ctx, "tenant-b", time.Time{}, time.Now())
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if summary.TotalModelTokens != 0 {
			t.Fatalf("cross-tenant leak: tenant-b sees tenant-a model tokens")
		}
	})
}
