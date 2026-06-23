package dashboard

import "context"

// MockResourceManager returns canned data for dashboard tests.
type MockResourceManager struct {
	Agents     []AgentResource
	Gateways   []GatewayResource
	MCPServers []MCPServerResource
	Err        error
}

func (m *MockResourceManager) ListAgents(context.Context) ([]AgentResource, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Agents == nil {
		return []AgentResource{}, nil
	}
	return m.Agents, nil
}

func (m *MockResourceManager) ListGateways(context.Context) ([]GatewayResource, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Gateways == nil {
		return []GatewayResource{}, nil
	}
	return m.Gateways, nil
}

func (m *MockResourceManager) ListMCPServers(context.Context) ([]MCPServerResource, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if m.MCPServers == nil {
		return []MCPServerResource{}, nil
	}
	return m.MCPServers, nil
}
