package mcpmanager

import (
	"net/http"
)

// TestServiceRegistry creates a ServiceRegistry pre-populated with the given
// instances. This bypasses Declare+Start and is intended for unit/integration
// tests that need a ready registry without real containers.
func TestServiceRegistry(instances []*ServiceInstance) *ServiceRegistry {
	r := &ServiceRegistry{
		instances: make(map[string]*ServiceInstance),
	}
	for _, inst := range instances {
		key := inst.WorkflowID + "/" + inst.ServiceBindingID
		r.instances[key] = inst
	}
	return r
}

// TestServiceInstance builds a ServiceInstance in the given state, useful
// for constructing test registries.
func TestServiceInstance(workflowID, bindingID string, state ServiceState, endpoint, capability string, declaredTools []string) *ServiceInstance {
	inst := NewServiceInstance(workflowID, bindingID, "test-pkg", "1.0.0", "test-digest", declaredTools)
	inst.State = state
	inst.Endpoint = endpoint
	inst.Capability = capability
	return inst
}

// TestManagedResolverHTTPClient creates a ManagedServiceResolver with a custom
// HTTP client. Primarily for tests.
func TestManagedResolverHTTPClient(registry *ServiceRegistry, client HTTPDoer) *ManagedServiceResolver {
	if client == nil {
		client = &http.Client{}
	}
	return &ManagedServiceResolver{
		registry:   registry,
		httpClient: client,
		authorizer: &ServiceRouteAuthorizer{},
	}
}
