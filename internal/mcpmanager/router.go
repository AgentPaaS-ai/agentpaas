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

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// ErrServerCrashed indicates a stopped MCP server has crash context.
var ErrServerCrashed = errors.New("mcp server crashed")

// Error codes for typed MCP failures (B33-T05).
const (
	ErrCodeProtocolError   = "mcp_protocol_error"
	ErrCodeServiceNotFound = "mcp_service_not_found"
	ErrCodeServiceNotReady = "mcp_service_not_ready"
	ErrCodeLeaseExpired    = "mcp_lease_expired"
	ErrCodePolicyDenied    = "mcp_policy_denied"
	ErrCodeTimeout         = "mcp_timeout"
	ErrCodeCancelled       = "mcp_cancelled"
	ErrCodeServiceCrashed  = "mcp_service_crashed"
	ErrCodeRouterUnavail   = "mcp_router_unavailable"
)

// TypedError wraps an error with a stable machine-readable code.
type TypedError struct {
	Code    string
	Message string
	Err     error
}

func (e *TypedError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

func (e *TypedError) Unwrap() error { return e.Err }

// newTypedError creates a TypedError with the given code and message.
func newTypedError(code, message string) *TypedError {
	return &TypedError{Code: code, Message: message}
}

const (
	maxBodySize          int64 = 1 << 20
	stdioResponseTimeout       = 5 * time.Second
)

// Router forwards MCP tool calls from the agent to the appropriate local MCP
// server. Stdio servers receive JSON-RPC over stdin/stdout. HTTP servers
// receive POST requests to their declared endpoint. AgentPaaS-managed services
// are resolved through the ManagedServiceResolver.
type Router struct {
	mu         sync.Mutex
	manager    *Manager
	lifecycle  *Lifecycle
	gateway    HTTPDoer
	audit      audit.AuditAppender
	stdioLocks map[string]*sync.Mutex
	requestSeq int64

	// managedResolver resolves AgentPaaS-managed MCP service bindings.
	// When non-nil and server.Transport is "agentpaas-service", the router
	// dispatches through the ServiceRegistry instead of local stdio/HTTP.
	managedResolver *ManagedServiceResolver
	// managedWorkflowID is the workflow ID for managed service resolution.
	managedWorkflowID string
}

// HTTPDoer is an interface for making HTTP requests. http.Client satisfies
// this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewRouter creates a Router bound to the given Manager and Lifecycle.
func NewRouter(manager *Manager, lifecycle *Lifecycle, gateway HTTPDoer, audit audit.AuditAppender) *Router {
	return &Router{
		manager:    manager,
		lifecycle:  lifecycle,
		gateway:    gateway,
		audit:      audit,
		stdioLocks: make(map[string]*sync.Mutex),
	}
}

// SetManagedResolver installs the resolver for AgentPaaS-managed service
// bindings and sets the workflow ID for binding resolution.
func (r *Router) SetManagedResolver(resolver *ManagedServiceResolver, workflowID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.managedResolver = resolver
	r.managedWorkflowID = workflowID
}

// CallTool routes an MCP tool call to the appropriate server.
func (r *Router) CallTool(ctx context.Context, serverID, tool string, input any, agentID, runID string) (any, error) {
	if r == nil || r.manager == nil {
		return nil, errors.New("mcp router manager is nil")
	}
	start := time.Now()
	if !r.manager.IsToolAllowed(serverID, tool) {
		AuditToolDenied(r.audit, serverID, tool, agentID, runID, "mcp server/tool not allowed", "undeclared", "", hashRouterJSON(input), time.Since(start).Milliseconds())
		return nil, errors.New("mcp server/tool not allowed")
	}
	if r.manager.RequiresConfirmation(serverID, tool) {
		AuditToolDenied(r.audit, serverID, tool, agentID, runID, "host-affecting tool requires confirmation", "host_affecting_unconfirmed", "", hashRouterJSON(input), time.Since(start).Milliseconds())
		return nil, errors.New("host-affecting tool requires confirmation: call manager.ConfirmTool first")
	}
	server, ok := r.server(serverID)
	if !ok {
		AuditToolDenied(r.audit, serverID, tool, agentID, runID, "mcp server/tool not allowed", "undeclared", "", hashRouterJSON(input), time.Since(start).Milliseconds())
		return nil, errors.New("mcp server/tool not allowed")
	}

	var (
		result any
		err    error
	)
	switch server.Transport {
	case "stdio":
		result, err = r.routeStdio(ctx, serverID, tool, input)
	case "http":
		result, err = r.routeHTTP(ctx, server, tool, input)
	case "agentpaas-service":
		result, err = r.routeManagedService(ctx, serverID, tool, input, agentID, runID, start)
	default:
		err = fmt.Errorf("mcp server %q has unsupported transport %q", serverID, server.Transport)
	}
	if err != nil {
		// Bare return preserves exact MCP error strings asserted by tests
		// (e.g. "mcp error N: ...", "mcp server/tool not allowed").
		return nil, err
	}
	AuditToolCall(r.audit, serverID, tool, agentID, runID, "allowed", "", "", hashRouterJSON(input), RedactToolOutputHash(result), time.Since(start).Milliseconds())
	return redactToolOutputValue(result), nil
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
	stdin, stdoutLines, err := r.lifecycle.StdioPipes(serverID)
	if err != nil {
		return nil, fmt.Errorf("router route stdio: %w", err)
	}
	request, err := buildMCPRequest(tool, input, r.nextRequestID())
	if err != nil {
		return nil, fmt.Errorf("router route stdio: %w", err)
	}

	stdioLock := r.stdioLock(serverID)
	stdioLock.Lock()
	defer stdioLock.Unlock()
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		return nil, fmt.Errorf("write stdio MCP request for %q: %w", serverID, err)
	}
	response, err := decodeMCPResponse(ctx, stdoutLines, request.ID)
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
	request, err := buildMCPRequest(tool, input, r.nextRequestID())
	if err != nil {
		return nil, fmt.Errorf("router route http: %w", err)
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
	defer func() { _ = resp.Body.Close() }() // best-effort close
	responseBody, err := readLimitedHTTPResponseBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read http MCP response: %w", err)
	}
	var response mcpResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode http MCP response: %w", err)
	}
	return responseResult(response)
}

// routeManagedService dispatches a tool call through the ManagedServiceResolver
// for AgentPaaS-managed MCP service bindings. Fails closed when no resolver is
// configured.
func (r *Router) routeManagedService(ctx context.Context, serverID, tool string, input any, agentID, runID string, start time.Time) (any, error) {
	if r.managedResolver == nil {
		return nil, newTypedError(ErrCodeRouterUnavail, "managed service resolver not configured")
	}
	if r.managedWorkflowID == "" {
		return nil, newTypedError(ErrCodeRouterUnavail, "managed service workflow ID not set")
	}
	result, err := r.managedResolver.ResolveToolCall(ctx, r.managedWorkflowID, serverID, tool, input)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func readLimitedHTTPResponseBody(body io.Reader) ([]byte, error) {
	responseBody, err := io.ReadAll(io.LimitReader(body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("read limited httpresponse body: %w", err)
	}
	if int64(len(responseBody)) < maxBodySize {
		return responseBody, nil
	}

	var extra [1]byte
	n, err := body.Read(extra[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n > 0 {
		return nil, errors.New("mcp http response exceeds 1MiB limit")
	}
	return responseBody, nil
}

func (r *Router) stdioLock(serverID string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stdioLocks == nil {
		r.stdioLocks = make(map[string]*sync.Mutex)
	}
	lock, ok := r.stdioLocks[serverID]
	if !ok {
		lock = &sync.Mutex{}
		r.stdioLocks[serverID] = lock
	}
	return lock
}

func (r *Router) nextRequestID() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestSeq++
	return r.requestSeq
}

func buildMCPRequest(tool string, input any, id int64) (mcpRequest, error) {
	arguments, err := mcpArguments(input)
	if err != nil {
		return mcpRequest{}, fmt.Errorf("build mcprequest: %w", err)
	}
	request := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
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

func decodeMCPResponse(ctx context.Context, lines <-chan stdioLine, expectedID int64) (mcpResponse, error) {
	// Derive timeout from context deadline when available; fall back to
	// stdioResponseTimeout for legacy callers without explicit deadlines.
	timeout := stdioResponseTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		var line stdioLine
		select {
		case <-ctx.Done():
			return mcpResponse{}, ctx.Err()
		case <-timer.C:
			return mcpResponse{}, errors.New("timed out waiting for MCP response")
		case readLine, ok := <-lines:
			if !ok {
				return mcpResponse{}, io.EOF
			}
			line = readLine
		}
		if line.err != nil {
			return mcpResponse{}, line.err
		}
		var response mcpResponse
		if err := json.Unmarshal(line.data, &response); err != nil {
			return mcpResponse{}, fmt.Errorf("decode mcpresponse: %w", err)
		}
		if response.ID != expectedID {
			continue
		}
		return response, nil
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
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

// mcpResponse is the JSON-RPC 2.0 response from an MCP server.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
