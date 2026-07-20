package k8s

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type k8sPolicyEnforcer struct {
	client               kubernetes.Interface
	namespace, direction string
	mu                   sync.RWMutex
	rules                map[string]port.CommSnapshot
}

func newK8sPolicyEnforcer(c kubernetes.Interface, n, d string) *k8sPolicyEnforcer {
	return &k8sPolicyEnforcer{client: c, namespace: n, direction: d, rules: map[string]port.CommSnapshot{}}
}

type K8sEgressEnforcer struct{ policy *k8sPolicyEnforcer }
type K8sIngressEnforcer struct{ policy *k8sPolicyEnforcer }

var _ port.EgressEnforcer = (*K8sEgressEnforcer)(nil)
var _ port.IngressEnforcer = (*K8sIngressEnforcer)(nil)

func (p *k8sPolicyEnforcer) apply(ctx context.Context, id string, s port.CommSnapshot) error {
	if p.client == nil {
		return fmt.Errorf("kubernetes client unavailable")
	}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "agentpaas-" + id, Labels: map[string]string{"agentpaas.workload": id}}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"agentpaas.workload": id}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress, networkingv1.PolicyTypeIngress}}}
	if p.direction == "egress" {
		policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	} else {
		policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{}
	}
	_, err := p.client.NetworkingV1().NetworkPolicies(p.namespace).Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("k8s policy enforcer apply: %w", err)
	}
	p.mu.Lock()
	p.rules[id] = s
	p.mu.Unlock()
	return nil
}
func (p *k8sPolicyEnforcer) check(_ context.Context, id, d string) port.Decision {
	p.mu.RLock()
	s, ok := p.rules[id]
	p.mu.RUnlock()
	if !ok {
		return port.Decision{Action: port.CommDeny, Reason: "no snapshot", RuleIndex: -1}
	}
	for i, r := range s.Rules {
		if r.Host+":"+strconv.Itoa(r.Port) == d {
			return port.Decision{Action: r.Action, Reason: "matched rule", RuleIndex: i}
		}
	}
	return port.Decision{Action: s.Default, Reason: "default policy", RuleIndex: -1}
}
func (p *k8sPolicyEnforcer) remove(ctx context.Context, id string) error {
	if p.client != nil {
		_ = p.client.NetworkingV1().NetworkPolicies(p.namespace).Delete(ctx, "agentpaas-"+id, metav1.DeleteOptions{}) // best-effort cleanup
	}
	p.mu.Lock()
	delete(p.rules, id)
	p.mu.Unlock()
	return nil
}
func (e *K8sEgressEnforcer) Apply(c context.Context, id string, s port.CommSnapshot) error {
	return e.policy.apply(c, id, s)
}
func (e *K8sEgressEnforcer) Check(c context.Context, id, d string) port.Decision {
	return e.policy.check(c, id, d)
}
func (e *K8sEgressEnforcer) Remove(c context.Context, id string) error { return e.policy.remove(c, id) }
func (e *K8sIngressEnforcer) Apply(c context.Context, id string, s port.CommSnapshot) error {
	return e.policy.apply(c, id, s)
}
func (e *K8sIngressEnforcer) Check(c context.Context, id, d string) port.Decision {
	return e.policy.check(c, id, d)
}
func (e *K8sIngressEnforcer) Remove(c context.Context, id string) error {
	return e.policy.remove(c, id)
}
