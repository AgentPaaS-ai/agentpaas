package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

type harnessRPCServer struct {
	listener *net.UnixListener
	addr     string
	socket   string
	done     chan struct{}

	mu     sync.RWMutex
	invoke *rpcInvokeState
	audit  AuditAppender
}

type rpcInvokeState struct {
	budget      *BudgetEnforcer
	payload     map[string]any
	terminate   func()
	credentials map[string]rpcCredential
	mcpAllowed  map[string]map[string]bool
}

type rpcCredential struct {
	Header string
	Value  string
}

type rpcRequest struct {
	ID     string         `json:"id,omitempty"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     string `json:"id,omitempty"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"`
}

func startHarnessRPCServer(appender AuditAppender) (*harnessRPCServer, error) {
	dir, err := os.MkdirTemp("", "agentpaas-rpc-*")
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, "rpc.sock")
	addr, err := net.ResolveUnixAddr("unix", socket)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	s := &harnessRPCServer{
		listener: listener,
		addr:     socket,
		socket:   socket,
		done:     make(chan struct{}),
		audit:    appender,
	}
	go s.serve()
	return s, nil
}

func (s *harnessRPCServer) Addr() string {
	return s.addr
}

func (s *harnessRPCServer) Close() error {
	err := s.listener.Close()
	<-s.done
	return errors.Join(err, os.RemoveAll(filepath.Dir(s.socket)))
}

func (s *harnessRPCServer) SetInvoke(payload map[string]any, budget *BudgetEnforcer, terminate func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoke = &rpcInvokeState{
		budget:      budget,
		payload:     payload,
		terminate:   terminate,
		credentials: credentialsFromPayload(payload),
		mcpAllowed:  mcpAllowlistFromPayload(payload),
	}
}

func (s *harnessRPCServer) ClearInvoke() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoke = nil
}

func (s *harnessRPCServer) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *harnessRPCServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(rpcResponse{OK: false, Error: err.Error(), Code: "invalid_json"})
			continue
		}
		_ = encoder.Encode(s.handleRequest(req))
	}
}

func (s *harnessRPCServer) handleRequest(req rpcRequest) rpcResponse {
	state := s.currentInvoke()
	if state == nil {
		return rpcError(req.ID, "no active invoke", "no_active_invoke")
	}
	switch req.Method {
	case "llm":
		return s.handleLLM(req, state)
	case "record_iteration":
		return s.handleRecordIteration(req, state)
	case "http":
		return s.handleHTTP(req, state, false)
	case "http_with_credential":
		return s.handleHTTP(req, state, true)
	case "mcp":
		return s.handleMCP(req, state)
	default:
		return rpcError(req.ID, fmt.Sprintf("unknown method %q", req.Method), "unknown_method")
	}
}

func (s *harnessRPCServer) currentInvoke() *rpcInvokeState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.invoke
}

func (s *harnessRPCServer) handleLLM(req rpcRequest, state *rpcInvokeState) rpcResponse {
	prompt := stringParam(req.Params, "prompt")
	tokens := int64(len(strings.Fields(prompt)))
	if tokens == 0 && prompt != "" {
		tokens = 1
	}
	if err := state.budget.RecordTokens(tokens); err != nil {
		if errors.Is(err, ErrBudgetExceeded) && state.terminate != nil {
			go state.terminate()
			return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
		}
		return rpcError(req.ID, err.Error(), "llm_failed")
	}
	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"text":   "agentpaas fake llm response",
			"tokens": tokens,
		},
	}
}

func (s *harnessRPCServer) handleRecordIteration(req rpcRequest, state *rpcInvokeState) rpcResponse {
	if err := state.budget.RecordIteration(); err != nil {
		if errors.Is(err, ErrBudgetExceeded) && state.terminate != nil {
			go state.terminate()
			return rpcError(req.ID, err.Error(), StatusBudgetExceeded)
		}
		return rpcError(req.ID, err.Error(), "iteration_failed")
	}
	return rpcResponse{ID: req.ID, OK: true, Result: map[string]any{"recorded": true}}
}

func (s *harnessRPCServer) handleHTTP(req rpcRequest, state *rpcInvokeState, withCredential bool) rpcResponse {
	method := strings.ToUpper(defaultString(stringParam(req.Params, "method"), http.MethodGet))
	url := stringParam(req.Params, "url")
	if url == "" {
		return rpcError(req.ID, "url is required", "invalid_http_request")
	}
	body := stringParam(req.Params, "body")
	httpReq, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	if err != nil {
		return rpcError(req.ID, err.Error(), "invalid_http_request")
	}
	for key, value := range stringMapParam(req.Params, "headers") {
		httpReq.Header.Set(key, value)
	}
	if withCredential {
		credID := stringParam(req.Params, "credential_id")
		cred, ok := state.credentials[credID]
		if !ok {
			return rpcError(req.ID, "credential is not declared", "credential_denied")
		}
		header := defaultString(cred.Header, "Authorization")
		httpReq.Header.Set(header, cred.Value)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return rpcError(req.ID, err.Error(), "http_failed")
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return rpcError(req.ID, err.Error(), "http_failed")
	}
	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"status":  resp.StatusCode,
			"headers": redactedHeaders(resp.Header),
			"body":    string(respBody),
		},
	}
}

func (s *harnessRPCServer) handleMCP(req rpcRequest, state *rpcInvokeState) rpcResponse {
	serverID := stringParam(req.Params, "server_id")
	tool := stringParam(req.Params, "tool")
	if !state.mcpAllowed[serverID][tool] {
		s.auditMCPDenied(serverID, tool, "undeclared")
		return rpcError(req.ID, "mcp server/tool is not declared", "mcp_denied")
	}
	return rpcResponse{
		ID: req.ID,
		OK: true,
		Result: map[string]any{
			"server_id": serverID,
			"tool":      tool,
			"result":    map[string]any{"ok": true},
		},
	}
}

func (s *harnessRPCServer) auditMCPDenied(serverID, tool, reason string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      "mcp_denied",
		DeploymentMode: "local",
		Actor:          "harness",
		Payload: map[string]interface{}{
			"server_id": serverID,
			"tool":      tool,
			"reason":    reason,
		},
	})
}

func rpcError(id, message, code string) rpcResponse {
	return rpcResponse{ID: id, OK: false, Error: message, Code: code}
}

func credentialsFromPayload(payload map[string]any) map[string]rpcCredential {
	out := make(map[string]rpcCredential)
	for _, item := range listParam(payload, "credentials") {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := firstString(obj, "id", "credential_id")
		if id == "" {
			continue
		}
		out[id] = rpcCredential{
			Header: defaultString(firstString(obj, "header"), "Authorization"),
			Value:  firstString(obj, "value", "secret"),
		}
	}
	return out
}

func mcpAllowlistFromPayload(payload map[string]any) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	for _, item := range listParam(payload, "mcp_servers") {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		serverID := firstString(obj, "server_id", "id", "name")
		if serverID == "" {
			continue
		}
		tools := make(map[string]bool)
		for _, rawTool := range listParam(obj, "tools") {
			switch v := rawTool.(type) {
			case string:
				tools[v] = true
			case map[string]any:
				name := firstString(v, "name", "tool")
				if name != "" {
					tools[name] = true
				}
			}
		}
		out[serverID] = tools
	}
	return out
}

func redactedHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key := range headers {
		out[key] = "[redacted]"
	}
	return out
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, _ := params[key].(string)
	return value
}

func stringMapParam(params map[string]any, key string) map[string]string {
	out := make(map[string]string)
	raw, ok := params[key].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		if text, ok := v.(string); ok {
			out[k] = text
		}
	}
	return out
}

func listParam(params map[string]any, key string) []any {
	if params == nil {
		return nil
	}
	items, _ := params[key].([]any)
	return items
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
