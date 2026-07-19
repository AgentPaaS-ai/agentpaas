package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sPrepareCreatesSecurePod(t *testing.T) {
	client := fake.NewSimpleClientset()
	a := NewK8sAdapter(K8sAdapterDeps{Clientset: client, Namespace: "default"})
	id, err := a.Runtime.Prepare(context.Background(), port.PrepareRequest{TenantID: "tenant", RunID: "run", AttemptID: "attempt", ImageDigest: "busybox:latest", ResourcePolicy: port.ResourcePolicy{MemoryMB: 32}})
	if err != nil {
		t.Fatal(err)
	}
	pod, err := client.CoreV1().Pods("default").Get(context.Background(), string(id), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Labels["agentpaas.tenant"] != "tenant" || pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
		t.Fatal("pod missing tenant/security policy")
	}
	if pod.Spec.Containers[0].SecurityContext == nil || pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem == nil || !*pod.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("pod missing readonly root")
	}
}

func TestK8sStartWaitsForRunningPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	a := NewK8sAdapter(K8sAdapterDeps{Clientset: client, Namespace: "default"})
	id, err := a.Runtime.Prepare(context.Background(), port.PrepareRequest{RunID: "run", AttemptID: "attempt", ImageDigest: "busybox"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Runtime.Start(ctx, id); err == nil {
		t.Fatal("expected timeout before pod runs")
	}
}
