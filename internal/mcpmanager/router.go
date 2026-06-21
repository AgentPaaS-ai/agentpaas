package mcpmanager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
)

// ErrServerCrashed indicates a stopped MCP server has crash context.
var ErrServerCrashed = errors.New("mcp server crashed")

// Router forwards MCP tool calls from the agent to the appropriate local MCP
// server. Stdio servers receive JSON-RPC over stdin/stdout. HTTP servers
// receive POST requests to their declared endpoint.
type Router struct {
	mu        sync.Mutex
	manager   *Manager
	lifecycle *Lifecycle
	gateway   HTTPDoer
	audit     audit.AuditAppender
}

// HTTPDoer is an interface for making HTTP requests. http.Client satisfies
// this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewRouter creates a Router bound to the given Manager and Lifecycle.
func NewRouter(manager *Manager, lifecycle *Lifecycle, gateway HTTPDoer, audit audit.AuditAppender) *Router {
	return &Router{
		manager:   manager,
		lifecycle: lifecycle,
		gateway:   gateway,
		audit:     audit,
	}
}

// CallTool routes an MCP tool call to the appropriate server.
func (r *Router) CallTool(ctx context.Context, serverID, tool string, input any, agentID, runID string) (any, error) {
	if r == nil || r.manager == nil {
		return nil, errors.New("mcp router manager is nil")
	}
	if !r.manager.IsToolAllowed(serverID, tool) {
		r.manager.DenyToolCall(r.audit, serverID, tool, agentID, runID, "undeclared")
		return nil, errors.New("mcp server/tool not allowed")
	}
	server, ok := r.server(serverID)
	if !ok {
		r.manager.DenyToolCall(r.audit, serverID, tool, agentID, runID, "undeclared")
		return nil, errors.New("mcp server/tool not allowed")
	}

	start := time.Now()
	var (
		result any
		err    error
	)
	switch server.Transport {
	case "stdio":
		result, err = r.routeStdio(ctx, serverID, tool, input)
	case "http":
		result, err = r.routeHTTP(ctx, server, tool, input)
	default:
		err = fmt.Errorf("mcp server %q has unsupported transport %q", serverID, server.Transport)
	}
	if err != nil {
		return nil, err
	}
	if r.audit != nil {
		_ = r.audit.Append(audit.AuditRecord{
			Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
			EventType:      "mcp_call",
			DeploymentMode: "local",
			Actor:          agentID,
			Payload: map[string]interface{}{
				"agent_id":    agentID,
				"input_hash":  hashRouterJSON(input),
				"output_hash": hashRouterJSON(result),
				"run_id":      runID,
				"server_id":   serverID,
				"timing_ms":   time.Since(start).Milliseconds(),
				"tool":        tool,
			},
		})
	}
	return result, nil
}

func (r *Router) server(serverID string) (policy.MCPServer, bool) {
	r.manager.mu.RLock()
	defer r.manager.mu.RUnlock()
	server, ok := r.manager.servers[serverID]
	return server, ok
}

func (r *Router) routeStdio(ctx context.Context, serverID, tool string, input any) (any, error) {
	if r.lifecycle == nil {
		return nil, errors.New("mcp router lifecycle is nil")
	}
	stdin, stdout, err := r.lifecycle.StdioPipes(serverID)
	if err != nil {
		return nil, err
	}
	request, err := buildMCPRequest(tool, input)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		return nil, fmt.Errorf("write stdio MCP request for %q: %w", serverID, err)
	}
	response, err := decodeMCPResponse(ctx, stdout)
	if err != nil {
		if crash := r.lifecycle.CrashContext(serverID); crash != nil {
			return nil, fmt.Errorf("%w: server_id=%s transport=%s exit_code=%d error=%s", ErrServerCrashed, crash.ServerID, crash.Transport, crash.ExitCode, crash.Error)
		}
		return nil, fmt.Errorf("read stdio MCP response for %q: %w", serverID, err)
	}
	return responseResult(response)
}

func (r *Router) routeHTTP(ctx context.Context, server policy.MCPServer, tool string, input any) (any, error) {
	if r.gateway == nil {
		return nil, errors.New("mcp router http gateway is nil")
	}
	request, err := buildMCPRequest(tool, input)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal http MCP request: %w", err)
	}
	endpoint := server.Endpoint
	if endpoint == "" {
		endpoint = server.URL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http MCP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.gateway.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send http MCP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read http MCP response: %w", err)
	}
	var response mcpResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode http MCP response: %w", err)
	}
	return responseResult(response)
}

func buildMCPRequest(tool string, input any) (mcpRequest, error) {
	arguments, err := mcpArguments(input)
	if err != nil {
		return mcpRequest{}, err
	}
	request := mcpRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
	}
	request.Params.Name = tool
	request.Params.Arguments = arguments
	return request, nil
}

func mcpArguments(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	if arguments, ok := input.(map[string]any); ok {
		return arguments, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal MCP input: %w", err)
	}
	var arguments map[string]any
	if err := json.Unmarshal(data, &arguments); err != nil {
		return nil, fmt.Errorf("MCP input must be a JSON object: %w", err)
	}
	return arguments, nil
}

func decodeMCPResponse(ctx context.Context, reader io.Reader) (mcpResponse, error) {
	responseCh := make(chan mcpResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		var response mcpResponse
		if err := json.NewDecoder(reader).Decode(&response); err != nil {
			errCh <- err
			return
		}
		responseCh <- response
	}()

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	select {
	case response := <-responseCh:
		return response, nil
	case err := <-errCh:
		return mcpResponse{}, err
	case <-ctx.Done():
		return mcpResponse{}, ctx.Err()
	case <-timeout.C:
		return mcpResponse{}, errors.New("timed out waiting for MCP response")
	}
}

func responseResult(response mcpResponse) (any, error) {
	if response.Error != nil {
		return nil, fmt.Errorf("mcp error %d: %s", response.Error.Code, response.Error.Message)
	}
	if len(response.Result) == 0 {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("decode MCP result: %w", err)
	}
	return result, nil
}

func hashRouterJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data = []byte(fmt.Sprintf("%v", value))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mcpRequest is the JSON-RPC 2.0 request for MCP tools/call.
type mcpRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

// mcpResponse is the JSON-RPC 2.0 response from an MCP server.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
