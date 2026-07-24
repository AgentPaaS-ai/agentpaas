package mcpmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/urlutil"
)

// Readiness states for MCP servers.
const (
	ReadinessStopped   = "stopped"
	ReadinessStarting  = "starting"
	ReadinessReady     = "ready"
	ReadinessUnhealthy = "unhealthy"
)

// Health states for MCP servers.
const (
	HealthUnknown = "unknown"
	HealthHealthy = "healthy"
	HealthFailed  = "failed"
)

// Resource represents a managed MCP server resource.
type Resource struct {
	ResourceType string   `json:"resource_type"`
	AgentID      string   `json:"agent_id,omitempty"`
	RunID        string   `json:"run_id,omitempty"`
	ServerID     string   `json:"server_id"`
	Transport    string   `json:"transport"`
	AllowedTools []string `json:"allowed_tools"`
	Readiness    string   `json:"readiness"`
	Health       string   `json:"health"`
	LastError    string   `json:"last_error,omitempty"`
	PolicyDigest string   `json:"policy_digest"`
}

// Manager manages declared MCP server resources.
type Manager struct {
	mu             sync.RWMutex
	resources      map[string]*Resource
	servers        map[string]policy.MCPServer
	confirmedTools map[string]bool // serverID+":"+tool -> confirmed
}

// NewManager creates a new MCP manager.
func NewManager() *Manager {
	return &Manager{
		resources:      make(map[string]*Resource),
		servers:        make(map[string]policy.MCPServer),
		confirmedTools: make(map[string]bool),
	}
}

// Validate validates declared MCP servers.
func (m *Manager) Validate(servers []policy.MCPServer) error {
	seen := make(map[string]bool)
	for _, s := range servers {
		if s.Name == "" {
			return fmt.Errorf("mcp server with empty name")
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate MCP server ID %q", s.Name)
		}
		seen[s.Name] = true
		if s.Transport == "" {
			return fmt.Errorf("MCP server %q: empty transport", s.Name)
		}
		if s.Transport != "stdio" && s.Transport != "http" && s.Transport != "agentpaas-service" {
			return fmt.Errorf("MCP server %q: invalid transport %q (must be stdio, http, or agentpaas-service)", s.Name, s.Transport)
		}
		if s.Transport == "stdio" && s.Command == "" {
			return fmt.Errorf("MCP server %q: stdio transport requires command", s.Name)
		}
		if s.Transport == "http" && s.URL == "" && s.Endpoint == "" {
			return fmt.Errorf("MCP server %q: http transport requires url or endpoint", s.Name)
		}
	}
	return nil
}

// Register registers declared MCP servers as managed resources.
func (m *Manager) Register(servers []policy.MCPServer, agentID, runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources = make(map[string]*Resource)
	m.servers = make(map[string]policy.MCPServer)
	for _, s := range servers {
		tools := make([]string, len(s.AllowedTools))
		copy(tools, s.AllowedTools)
		m.servers[s.Name] = s
		m.resources[s.Name] = &Resource{
			ResourceType: "mcp_server",
			AgentID:      agentID,
			RunID:        runID,
			ServerID:     s.Name,
			Transport:    s.Transport,
			AllowedTools: tools,
			Readiness:    ReadinessStopped,
			Health:       HealthUnknown,
			PolicyDigest: computePolicyDigest(s),
		}
	}
}

// IsToolAllowed returns true if the server exists and the tool is in its
// allowed list. Empty allowed list denies all tools.
func (m *Manager) IsToolAllowed(serverID, tool string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.resources[serverID]
	if !ok {
		return false
	}
	for _, t := range r.AllowedTools {
		if t == tool {
			return true
		}
	}
	return false
}

// ConfirmTool marks a host-affecting tool as confirmed for use.
// This is the confirmation protocol: host-affecting tools cannot be
// called until confirmed.
func (m *Manager) ConfirmTool(serverID, tool string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.confirmedTools[serverID+":"+tool] = true
}

// IsToolConfirmed returns true if a host-affecting tool has been confirmed.
func (m *Manager) IsToolConfirmed(serverID, tool string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.confirmedTools[serverID+":"+tool]
}

// RequiresConfirmation returns true if a tool is host-affecting and
// has not yet been confirmed.
func (m *Manager) RequiresConfirmation(serverID, tool string) bool {
	return IsHostAffecting(tool) && !m.IsToolConfirmed(serverID, tool)
}

// DenyToolCall records a denied tool call and emits an audit event.
func (m *Manager) DenyToolCall(appender audit.AuditAppender, serverID, tool, agentID, runID, policyRuleID string) {
	AuditToolDenied(appender, serverID, tool, agentID, runID, policyRuleID, policyRuleID, "", "", int64(0))
}

// Status returns all managed MCP resources.
func (m *Manager) Status() []Resource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Resource, 0, len(m.resources))
	for _, r := range m.resources {
		result = append(result, *r)
	}
	return result
}

func (m *Manager) server(serverID string) (policy.MCPServer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	server, ok := m.servers[serverID]
	return server, ok
}

func (m *Manager) setReadiness(serverID, readiness string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.resources[serverID]; ok {
		r.Readiness = readiness
		if readiness == ReadinessReady {
			r.Health = HealthHealthy
			r.LastError = ""
		}
	}
}

func (m *Manager) setFailure(serverID, readiness, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.resources[serverID]; ok {
		r.Readiness = readiness
		r.Health = HealthFailed
		r.LastError = lastError
	}
}

func computePolicyDigest(s policy.MCPServer) string {
	tools := append([]string(nil), s.AllowedTools...)
	sort.Strings(tools)
	canonical := map[string]interface{}{
		"allowed_tools": tools,
		"args":          s.Args,
		"auth_mode":     s.AuthMode,
		"command":       s.Command,
		"endpoint":      s.Endpoint,
		"name":          s.Name,
		"transport":     s.Transport,
		"url":           stripURLUserinfo(s.URL),
	}
	data, _ := json.Marshal(canonical) // best-effort marshal
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func stripURLUserinfo(rawURL string) string {
	return urlutil.StripUserinfo(rawURL)
}
