package mcpmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
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
	mu        sync.RWMutex
	resources map[string]*Resource
	servers   map[string]policy.MCPServer
}

// NewManager creates a new MCP manager.
func NewManager() *Manager {
	return &Manager{
		resources: make(map[string]*Resource),
		servers:   make(map[string]policy.MCPServer),
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
		if s.Transport != "stdio" && s.Transport != "http" {
			return fmt.Errorf("MCP server %q: invalid transport %q (must be stdio or http)", s.Name, s.Transport)
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

// DenyToolCall records a denied tool call and emits an audit event.
func (m *Manager) DenyToolCall(appender audit.AuditAppender, serverID, tool, agentID, runID, policyRuleID string) {
	if appender == nil {
		return
	}
	_ = appender.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeMCPToolDenied,
		DeploymentMode: "local",
		Actor:          agentID,
		Payload: map[string]interface{}{
			"agent_id":       agentID,
			"policy_rule_id": policyRuleID,
			"run_id":         runID,
			"server_id":      serverID,
			"tool":           tool,
		},
	})
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
	data, _ := json.Marshal(canonical)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func stripURLUserinfo(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.User = nil
	return parsed.String()
}
