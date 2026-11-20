package harness

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
)

// ---------------------------------------------------------------------------
// MCP HTTP Bridge — S1a
//
// When the harness starts with AgentKind == "mcp_service", it starts the
// Python service worker as today (stdin protocol) AND optionally starts an
// HTTP JSON-RPC bridge. The bridge listens on 0.0.0.0:8080 (or the
// AGENTPAAS_MCP_HTTP_ADDR env var), checks the X-AgentPaaS-MCP-Capability
// header against the AGENTPAAS_MCP_CAPABILITY env var, and translates
// between JSON-RPC requests and the stdin/stdout protocol.
// ---------------------------------------------------------------------------

// MCPBridgeConfig configures the MCP HTTP bridge.
type MCPBridgeConfig struct {
	// Addr is the listen address (e.g. "0.0.0.0:8080"). Default from env
	// AGENTPAAS_MCP_HTTP_ADDR or "0.0.0.0:8080".
	Addr string
	// Capability is the required capability value for the header check.
	// Default from env AGENTPAAS_MCP_CAPABILITY.
	Capability string
	// Stdin is the writer for the Python worker's stdin.
	Stdin io.Writer
	// Stdout is the reader for the Python worker's stdout (line-delimited JSON).
	Stdout io.Reader
	// ErrorLog is an optional logger for bridge errors. If nil, a default
	// logger writing to os.Stderr is used.
	ErrorLog *log.Logger
	// OnCallStart is invoked at the start of tools/call with the tool name
	// and arguments. Nil is a no-op (existing tests leave it unset).
	OnCallStart func(payload map[string]any)
	// OnCallEnd is deferred at the start of tools/call so it always runs
	// after the call completes. Nil is a no-op.
	OnCallEnd func()
}

// MCPBridge is an HTTP server that translates JSON-RPC MCP requests to the
// internal stdin/stdout protocol used by the Python service worker.
type MCPBridge struct {
	cfg MCPBridgeConfig

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	closed   bool

	// stdinMu serializes access to the worker stdin/stdout protocol.
	stdinMu sync.Mutex
	stdin   io.Writer
	decoder *json.Decoder

	requestSeq atomic.Int64

	errorLog *log.Logger
}

// NewMCPBridge creates a new MCPBridge using the given config.
func NewMCPBridge(cfg MCPBridgeConfig) *MCPBridge {
	if cfg.Addr == "" {
		cfg.Addr = envOrDefault("AGENTPAAS_MCP_HTTP_ADDR", "0.0.0.0:8080")
	}
	if cfg.Capability == "" {
		cfg.Capability = os.Getenv("AGENTPAAS_MCP_CAPABILITY")
	}
	if cfg.ErrorLog == nil {
		cfg.ErrorLog = log.New(os.Stderr, "[mcp-bridge] ", log.LstdFlags)
	}
	return &MCPBridge{
		cfg:      cfg,
		stdin:    cfg.Stdin,
		decoder:  json.NewDecoder(cfg.Stdout),
		errorLog: cfg.ErrorLog,
	}
}

// Start starts the HTTP server. Blocks until the server is listening, then
// returns. The caller must call Close() to shut it down.
func (b *MCPBridge) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("mcp bridge already closed")
	}

	ln, err := net.Listen("tcp", b.cfg.Addr)
	if err != nil {
		return fmt.Errorf("mcp bridge listen: %w", err)
	}
	b.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/", b.ServeHTTP)
	b.server = &http.Server{
		Handler:  mux,
		ErrorLog: b.errorLog,
	}

	go func() {
		if err := b.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			b.errorLog.Printf("mcp bridge serve error: %v", err)
		}
	}()

	return nil
}

// Addr returns the listen address the bridge is bound to. Returns an empty
// string if Start() has not been called yet.
func (b *MCPBridge) Addr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener != nil {
		return b.listener.Addr().String()
	}
	return ""
}

// Close gracefully shuts down the HTTP server. It is safe to call multiple
// times.
func (b *MCPBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	var errs []error
	if b.server != nil {
		if err := b.server.Shutdown(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("mcp bridge close: %v", errs)
	}
	return nil
}

// ServeHTTP handles a single HTTP request. It checks the capability header,
// parses the JSON-RPC body, dispatches to the worker, and writes the JSON-RPC
// response.
func (b *MCPBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Cloud CF containers only expose :8080. When the bridge owns that port,
	// answer health probes so startRunContainer can green without the agent mux.
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/healthz", "/health", "/readyz", "/":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"service":"mcp_bridge"}`))
			return
		}
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32600,"message":"Method not allowed"},"id":null}`, http.StatusMethodNotAllowed)
		return
	}

	// Check capability header with constant-time comparison.
	capValue := r.Header.Get("X-AgentPaaS-MCP-Capability")
	if b.cfg.Capability == "" || subtle.ConstantTimeCompare([]byte(capValue), []byte(b.cfg.Capability)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		// Never include capability in error response.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error":   map[string]any{"code": -32001, "message": "invalid capability header"},
			"id":      nil,
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error":   map[string]any{"code": -32700, "message": "Parse error"},
			"id":      nil,
		})
		return
	}

	var req struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"error":   map[string]any{"code": -32700, "message": "Parse error"},
			"id":      nil,
		})
		return
	}

	switch req.Method {
	case "tools/list":
		b.handleToolsList(w, req.ID)
	case "tools/call":
		b.handleToolsCall(w, req.ID, req.Params.Name, req.Params.Arguments)
	default:
		b.writeJSONRPCError(w, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (b *MCPBridge) handleToolsList(w http.ResponseWriter, id any) {
	b.stdinMu.Lock()
	defer b.stdinMu.Unlock()

	reqID := b.nextReqID()
	if err := json.NewEncoder(b.stdin).Encode(map[string]any{
		"type": "mcp_tools_list",
		"id":   reqID,
	}); err != nil {
		b.errorLog.Printf("mcp bridge stdin write error: %v", err)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}

	var resp struct {
		Type  string   `json:"type"`
		ID    string   `json:"id"`
		OK    bool     `json:"ok"`
		Tools []string `json:"tools,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := b.decoder.Decode(&resp); err != nil {
		b.errorLog.Printf("mcp bridge stdout read error: %v", err)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}
	if resp.ID != reqID {
		b.errorLog.Printf("mcp bridge response id mismatch: got %s, want %s", resp.ID, reqID)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}

	if !resp.OK {
		b.writeJSONRPCError(w, id, -32603, "tools/list failed")
		return
	}

	b.writeJSONRPCResult(w, id, map[string]any{
		"tools": resp.Tools,
	})
}

func (b *MCPBridge) handleToolsCall(w http.ResponseWriter, id any, tool string, args map[string]any) {
	if args == nil {
		args = map[string]any{}
	}
	if b.cfg.OnCallStart != nil {
		b.cfg.OnCallStart(map[string]any{"tool": tool, "arguments": args})
	}
	if b.cfg.OnCallEnd != nil {
		defer b.cfg.OnCallEnd()
	}

	b.stdinMu.Lock()
	defer b.stdinMu.Unlock()

	reqID := b.nextReqID()
	if err := json.NewEncoder(b.stdin).Encode(map[string]any{
		"type":      "mcp_tools_call",
		"id":        reqID,
		"tool":      tool,
		"arguments": args,
	}); err != nil {
		b.errorLog.Printf("mcp bridge stdin write error: %v", err)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}

	var resp struct {
		Type   string         `json:"type"`
		ID     string         `json:"id"`
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result,omitempty"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := b.decoder.Decode(&resp); err != nil {
		b.errorLog.Printf("mcp bridge stdout read error: %v", err)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}
	if resp.ID != reqID {
		b.errorLog.Printf("mcp bridge response id mismatch: got %s, want %s", resp.ID, reqID)
		b.writeJSONRPCError(w, id, -32603, "Internal error")
		return
	}

	if !resp.OK {
		code := "-32603"
		msg := "tools/call failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		b.writeJSONRPCError(w, id, jsonRPCErrorCode(code), msg)
		return
	}

	// Wrap result in MCP content format.
	contentText, _ := json.Marshal(resp.Result)
	b.writeJSONRPCResult(w, id, map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(contentText),
			},
		},
	})
}

func (b *MCPBridge) writeJSONRPCResult(w http.ResponseWriter, id any, result any) {
	b.writeJSONRPC(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func (b *MCPBridge) writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	b.writeJSONRPC(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (b *MCPBridge) writeJSONRPC(w http.ResponseWriter, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		b.errorLog.Printf("mcp bridge response write error: %v", err)
	}
}

func (b *MCPBridge) nextReqID() string {
	seq := b.requestSeq.Add(1)
	return fmt.Sprintf("bridge-%d", seq)
}

func jsonRPCErrorCode(code string) int {
	// Common JSON-RPC error codes.
	switch code {
	case "tool_error":
		return -32000
	case "tool_not_found":
		return -32601
	default:
		return -32603
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}