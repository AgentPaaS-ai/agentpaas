package dashboard

import (
	"encoding/json"
	"net/http"
)

type resourcesResponse struct {
	Agents     []AgentResource     `json:"agents"`
	Gateways   []GatewayResource   `json:"gateways"`
	MCPServers []MCPServerResource `json:"mcp_servers"`
}

func (s *Server) handleCSRF(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": s.csrfToken})
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request) {
	agents, err := s.listAgents(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list agents failed")
		return
	}
	gateways, err := s.listGateways(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list gateways failed")
		return
	}
	mcpServers, err := s.listMCPServers(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list mcp servers failed")
		return
	}
	writeJSON(w, http.StatusOK, resourcesResponse{
		Agents:     agents,
		Gateways:   gateways,
		MCPServers: mcpServers,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.listAgents(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list agents failed")
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) handleGateways(w http.ResponseWriter, r *http.Request) {
	gateways, err := s.listGateways(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list gateways failed")
		return
	}
	writeJSON(w, http.StatusOK, gateways)
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	mcpServers, err := s.listMCPServers(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list mcp servers failed")
		return
	}
	writeJSON(w, http.StatusOK, mcpServers)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listAgents(r *http.Request) ([]AgentResource, error) {
	if s.resourceMgr == nil {
		return []AgentResource{}, nil
	}
	agents, err := s.resourceMgr.ListAgents(r.Context())
	if err != nil {
		return nil, err
	}
	if agents == nil {
		return []AgentResource{}, nil
	}
	return agents, nil
}

func (s *Server) listGateways(r *http.Request) ([]GatewayResource, error) {
	if s.resourceMgr == nil {
		return []GatewayResource{}, nil
	}
	gateways, err := s.resourceMgr.ListGateways(r.Context())
	if err != nil {
		return nil, err
	}
	if gateways == nil {
		return []GatewayResource{}, nil
	}
	return gateways, nil
}

func (s *Server) listMCPServers(r *http.Request) ([]MCPServerResource, error) {
	if s.resourceMgr == nil {
		return []MCPServerResource{}, nil
	}
	mcpServers, err := s.resourceMgr.ListMCPServers(r.Context())
	if err != nil {
		return nil, err
	}
	if mcpServers == nil {
		return []MCPServerResource{}, nil
	}
	return mcpServers, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
