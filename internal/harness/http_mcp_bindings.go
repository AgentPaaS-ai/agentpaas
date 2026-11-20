package harness

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type httpMCPBindingJSON struct {
	Name         string            `json:"name"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers"`
	AllowedTools []string          `json:"allowed_tools"`
}

// InstallHTTPMCPBindingsJSON parses HTTP MCP client bindings and merges
// them onto the Manager without wiping existing servers.
func (s *Server) InstallHTTPMCPBindingsJSON(raw string) error {
	var entries []httpMCPBindingJSON
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return fmt.Errorf("harness: parse MCP bindings JSON: %w", err)
	}

	s.mu.RLock()
	manager := s.managedManager
	s.mu.RUnlock()
	if manager == nil {
		return fmt.Errorf("harness: InstallHTTPMCPBindingsJSON: manager must be installed via SetRouter first")
	}

	servers := make([]policy.MCPServer, 0, len(entries))
	for _, e := range entries {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		servers = append(servers, policy.MCPServer{
			Name:         name,
			Transport:    "http",
			URL:          e.URL,
			Headers:      e.Headers,
			AllowedTools: e.AllowedTools,
		})
	}
	manager.AddServers(servers, "", "")
	return nil
}
