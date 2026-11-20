package mcpmanager

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// MCPBindingSidecar is the JSON schema for the trusted MCP binding sidecar file
// written by the daemon and read by the harness at startup. It contains only
// READY bindings that the harness should register and wire into the Router.
//
// The file is owned by root:root with mode 0600 — capability tokens inside
// are trusted root-only material and never reach the Python environment.
type MCPBindingSidecar struct {
	WorkflowID string                   `json:"workflow_id"`
	Bindings   []MCPBindingSidecarEntry `json:"bindings"`
}

// MCPBindingSidecarEntry is a single binding entry for a managed MCP service.
// Only entries with State == "READY" are installed by the harness.
type MCPBindingSidecarEntry struct {
	BindingID     string   `json:"binding_id"`
	ServiceRunID  string   `json:"service_run_id,omitempty"`
	Endpoint      string   `json:"endpoint"`
	Capability    string   `json:"capability"`
	AllowedTools  []string `json:"allowed_tools"`
	PackageDigest string   `json:"package_digest,omitempty"`
	NetworkAlias  string   `json:"network_alias,omitempty"`
	State         string   `json:"state"`
}

// WriteMCPBindingSidecar writes a sidecar file with mode 0600.
func WriteMCPBindingSidecar(path string, sc MCPBindingSidecar) error {
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sidecar: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write sidecar file: %w", err)
	}
	return nil
}

// ReadMCPBindingSidecar reads and unmarshals a sidecar file.
func ReadMCPBindingSidecar(path string) (MCPBindingSidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MCPBindingSidecar{}, fmt.Errorf("read sidecar file: %w", err)
	}
	var sc MCPBindingSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return MCPBindingSidecar{}, fmt.Errorf("unmarshal sidecar: %w", err)
	}
	return sc, nil
}

// ServiceRegistryFromSidecar builds an in-memory ServiceRegistry containing
// only entries with State == "READY". Non-READY entries are silently skipped.
// The returned registry has no driver/checker/probe — it is a snapshot for
// route resolution only.
func ServiceRegistryFromSidecar(sc MCPBindingSidecar) (*ServiceRegistry, error) {
	reg := &ServiceRegistry{
		instances: make(map[string]*ServiceInstance),
	}
	for _, b := range sc.Bindings {
		if b.State != "READY" {
			continue
		}
		inst := NewServiceInstance(sc.WorkflowID, b.BindingID,
			"", "", b.PackageDigest, b.AllowedTools)
		inst.State = StateReady
		inst.Endpoint = b.Endpoint
		inst.Capability = b.Capability
		inst.NetworkAlias = b.NetworkAlias
		inst.RunID = b.ServiceRunID
		key := sc.WorkflowID + "/" + b.BindingID
		reg.instances[key] = inst
	}
	return reg, nil
}

// InstallSidecarOnRouter builds a ServiceRegistry from the sidecar, creates
// a ManagedServiceResolver, wires it onto the Router, and registers each
// binding on the Manager with transport=agentpaas-service.
func InstallSidecarOnRouter(router *Router, manager *Manager, sc MCPBindingSidecar) error {
	reg, err := ServiceRegistryFromSidecar(sc)
	if err != nil {
		return fmt.Errorf("install sidecar: %w", err)
	}

	resolver := NewManagedServiceResolver(reg, nil)
	router.SetManagedResolver(resolver, sc.WorkflowID)

	// Register each binding on the Manager so tools/list returns them.
	var servers []policy.MCPServer
	for _, b := range sc.Bindings {
		if b.State != "READY" {
			continue
		}
		servers = append(servers, policy.MCPServer{
			Name:         b.BindingID,
			Transport:    "agentpaas-service",
			Endpoint:     b.Endpoint,
			AllowedTools: b.AllowedTools,
		})
	}
	if len(servers) > 0 {
		manager.Register(servers, "", "")
	}

	return nil
}

// WriteBindingSidecar dumps all READY instances for the given workflowID
// to the sidecar file. Used by the daemon to produce the trusted input for
// the harness. Capability and endpoint are written as-is — the file is
// trusted root-only material.
func (r *ServiceRegistry) WriteBindingSidecar(path, workflowID string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var entries []MCPBindingSidecarEntry
	prefix := workflowID + "/"
	for key, inst := range r.instances {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// Read instance fields under its own lock.
		inst.mu.RLock()
		state := inst.State
		if state != StateReady {
			inst.mu.RUnlock()
			continue
		}
		entry := MCPBindingSidecarEntry{
			BindingID:     inst.ServiceBindingID,
			ServiceRunID:  inst.RunID,
			Endpoint:      inst.Endpoint,
			Capability:    inst.Capability,
			AllowedTools:  append([]string(nil), inst.DeclaredTools...),
			PackageDigest: inst.BundleDigest,
			NetworkAlias:  inst.NetworkAlias,
			State:         string(inst.State),
		}
		inst.mu.RUnlock()
		entries = append(entries, entry)
	}

	sc := MCPBindingSidecar{
		WorkflowID: workflowID,
		Bindings:   entries,
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal binding sidecar: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write binding sidecar file: %w", err)
	}
	return nil
}