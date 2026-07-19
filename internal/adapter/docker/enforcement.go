package docker

import (
	"context"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"sync"
)

type DockerEgressEnforcer struct{ *policyEnforcer }
type DockerIngressEnforcer struct{ *policyEnforcer }
type policyEnforcer struct {
	mu    sync.RWMutex
	rules map[string]port.CommSnapshot
}

func newDockerEnforcer() *DockerEgressEnforcer {
	return &DockerEgressEnforcer{&policyEnforcer{rules: map[string]port.CommSnapshot{}}}
}

var _ = newDockerEnforcer

func (p *policyEnforcer) apply(_ context.Context, id string, s port.CommSnapshot) error {
	p.mu.Lock()
	p.rules[id] = s
	p.mu.Unlock()
	return nil
}
func (p *policyEnforcer) check(_ context.Context, id, d string) port.Decision {
	p.mu.RLock()
	s, ok := p.rules[id]
	p.mu.RUnlock()
	if !ok {
		return port.Decision{Action: port.CommDeny, Reason: "no snapshot", RuleIndex: -1}
	}
	for i, r := range s.Rules {
		if r.Host+":"+itoa(r.Port) == d {
			return port.Decision{Action: r.Action, RuleIndex: i, Reason: "matched rule"}
		}
	}
	return port.Decision{Action: s.Default, Reason: "default policy", RuleIndex: -1}
}
func (p *policyEnforcer) remove(_ context.Context, id string) error {
	p.mu.Lock()
	delete(p.rules, id)
	p.mu.Unlock()
	return nil
}
func (e *DockerEgressEnforcer) Apply(c context.Context, id string, s port.CommSnapshot) error {
	return e.apply(c, id, s)
}
func (e *DockerEgressEnforcer) Check(c context.Context, id, d string) port.Decision {
	return e.check(c, id, d)
}
func (e *DockerEgressEnforcer) Remove(c context.Context, id string) error { return e.remove(c, id) }
func (i *DockerIngressEnforcer) Apply(c context.Context, id string, s port.CommSnapshot) error {
	return i.apply(c, id, s)
}
func (i *DockerIngressEnforcer) Check(c context.Context, id, d string) port.Decision {
	return i.check(c, id, d)
}
func (i *DockerIngressEnforcer) Remove(c context.Context, id string) error { return i.remove(c, id) }
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
