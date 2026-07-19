package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// K8sWorkloadRuntime manages workloads as Kubernetes Pods.
type K8sWorkloadRuntime struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.Mutex
	pods      map[port.WorkloadID]string
	states    map[port.WorkloadID]port.WorkloadState
}

var _ port.WorkloadRuntime = (*K8sWorkloadRuntime)(nil)

func (w *K8sWorkloadRuntime) Prepare(ctx context.Context, r port.PrepareRequest) (port.WorkloadID, error) {
	if w.client == nil {
		return "", fmt.Errorf("kubernetes client unavailable")
	}
	name := fmt.Sprintf("agentpaas-%s-%s", r.RunID, r.AttemptID)
	nonroot, readonly := true, true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{GenerateName: name + "-", Labels: map[string]string{"agentpaas.tenant": r.TenantID, "agentpaas.run": r.RunID, "agentpaas.attempt": r.AttemptID}}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonroot}, Containers: []corev1.Container{{Name: "workload", Image: r.ImageDigest, Command: []string{"sleep", "30"}, SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readonly, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(r.ResourcePolicy.CPUShares, resource.DecimalSI), corev1.ResourceMemory: *resource.NewQuantity(r.ResourcePolicy.MemoryMB*1024*1024, resource.BinarySI)}}}}}}
	created, err := w.client.CoreV1().Pods(w.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	id := port.WorkloadID(created.Name)
	created.Labels["agentpaas.workload"] = created.Name
	if _, err = w.client.CoreV1().Pods(w.namespace).Update(ctx, created, metav1.UpdateOptions{}); err != nil {
		return "", err
	}
	w.mu.Lock()
	if w.pods == nil {
		w.pods = map[port.WorkloadID]string{}
	}
	w.pods[id] = created.Name
	if w.states == nil {
		w.states = map[port.WorkloadID]port.WorkloadState{}
	}
	w.states[id] = port.WorkloadPrepared
	w.mu.Unlock()
	return id, nil
}
func (w *K8sWorkloadRuntime) pod(id port.WorkloadID) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, ok := w.pods[id]
	if !ok {
		return "", port.ErrNotFound
	}
	return n, nil
}
func (w *K8sWorkloadRuntime) Start(ctx context.Context, id port.WorkloadID) error {
	n, e := w.pod(id)
	if e != nil {
		return e
	}
	for {
		p, e := w.client.CoreV1().Pods(w.namespace).Get(ctx, n, metav1.GetOptions{})
		if e != nil {
			return e
		}
		if p.Status.Phase == corev1.PodRunning {
			break
		}
		if p.Status.Phase == corev1.PodFailed || p.Status.Phase == corev1.PodSucceeded {
			return fmt.Errorf("pod %s is %s", n, p.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	w.mu.Lock()
	w.states[id] = port.WorkloadRunning
	w.mu.Unlock()
	return nil
}
func (w *K8sWorkloadRuntime) Signal(ctx context.Context, id port.WorkloadID, _ port.WorkloadSignal) error {
	return w.Stop(ctx, id, nil)
}
func (w *K8sWorkloadRuntime) Fence(ctx context.Context, id port.WorkloadID) error {
	return (&K8sEgressEnforcer{policy: newK8sPolicyEnforcer(w.client, w.namespace, "egress")}).Apply(ctx, string(id), port.CommSnapshot{Default: port.CommDeny})
}
func (w *K8sWorkloadRuntime) Stop(ctx context.Context, id port.WorkloadID, d *time.Duration) error {
	n, e := w.pod(id)
	if e != nil {
		return e
	}
	opts := metav1.DeleteOptions{}
	if d != nil {
		s := int64(d.Seconds())
		opts.GracePeriodSeconds = &s
	}
	e = w.client.CoreV1().Pods(w.namespace).Delete(ctx, n, opts)
	if e == nil {
		w.mu.Lock()
		w.states[id] = port.WorkloadStopped
		w.mu.Unlock()
	}
	return e
}
func (w *K8sWorkloadRuntime) Inspect(ctx context.Context, id port.WorkloadID) (port.WorkloadStatus, error) {
	n, e := w.pod(id)
	if e != nil {
		return port.WorkloadStatus{}, e
	}
	p, e := w.client.CoreV1().Pods(w.namespace).Get(ctx, n, metav1.GetOptions{})
	if e != nil {
		return port.WorkloadStatus{}, e
	}
	state := port.WorkloadStopped
	if p.Status.Phase == corev1.PodRunning {
		state = port.WorkloadRunning
	}
	w.mu.Lock()
	if s := w.states[id]; s != "" {
		state = s
	}
	w.mu.Unlock()
	return port.WorkloadStatus{ID: id, State: state, IP: p.Status.PodIP}, nil
}
func (w *K8sWorkloadRuntime) Cleanup(ctx context.Context, id port.WorkloadID) error {
	n, e := w.pod(id)
	if e != nil {
		return e
	}
	e = w.client.CoreV1().Pods(w.namespace).Delete(ctx, n, metav1.DeleteOptions{})
	if e == nil {
		w.mu.Lock()
		delete(w.pods, id)
		w.states[id] = port.WorkloadCleaned
		w.mu.Unlock()
	}
	return e
}
