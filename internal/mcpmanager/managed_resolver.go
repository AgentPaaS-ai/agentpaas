package mcpmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ManagedServiceResolver resolves AgentPaaS-managed MCP service bindings
// into capability-checked service container routes. Backed by ServiceRegistry.
// Never accepts raw endpoint from worker payload.
type ManagedServiceResolver struct {
	registry   *ServiceRegistry
	httpClient HTTPDoer
	authorizer *ServiceRouteAuthorizer
}

// NewManagedServiceResolver creates a resolver backed by a ServiceRegistry.
func NewManagedServiceResolver(registry *ServiceRegistry, httpClient HTTPDoer) *ManagedServiceResolver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &ManagedServiceResolver{
		registry:   registry,
		httpClient: httpClient,
		authorizer: &ServiceRouteAuthorizer{},
	}
}

// ResolveToolCall resolves a tool call for a managed binding, authorizes it
// with the capability token, dispatches to the service container, and returns
// the result. The endpoint and capability are resolved from the ServiceRegistry
// — caller payload never specifies them.
func (r *ManagedServiceResolver) ResolveToolCall(ctx context.Context, workflowID, bindingID, tool string, input any) (any, error) {
	if r.registry == nil {
		return nil, fmt.Errorf("managed service resolver: no registry configured")
	}

	// Get returns a deep copy — safe to read fields without locking.
	inst, err := r.registry.Get(workflowID, bindingID)
	if err != nil {
		return nil, fmt.Errorf("managed service resolver: %w", err)
	}

	if inst.State != StateReady {
		return nil, fmt.Errorf("managed service %q: service not ready (state=%s)", bindingID, inst.State)
	}

	// Verify tool is in declared tools.
	found := false
	for _, t := range inst.DeclaredTools {
		if t == tool {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("managed service %q: tool %q not declared", bindingID, tool)
	}

	// Authorize with capability token.
	if err := r.authorizer.Authorized(inst.Capability, inst.Capability); err != nil {
		return nil, fmt.Errorf("managed service %q: %w", bindingID, err)
	}

	// Build JSON-RPC request.
	request, err := buildMCPRequest(tool, input, 0)
	if err != nil {
		return nil, fmt.Errorf("managed service %q: build request: %w", bindingID, err)
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("managed service %q: marshal request: %w", bindingID, err)
	}

	// Make HTTP call to the service container endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inst.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("managed service %q: create request: %w", bindingID, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CapabilityHeader, inst.Capability)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("managed service %q: send request: %w", bindingID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	responseBody, err := readLimitedHTTPResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("managed service %q: read response: %w", bindingID, err)
	}

	var response mcpResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("managed service %q: decode response: %w", bindingID, err)
	}

	return responseResult(response)
}
