// Package main is a stdio MCP dummy so host skins can prove connect.
// D72 name: agentpaas-mcp. Not a per-host runtime. Not pack/deploy.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const protocolVersion = "2024-11-05"

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "agentpaas-mcp: %v\n", err)
		os.Exit(1)
	}
}

func serve(in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpc
		if err := json.Unmarshal(line, &req); err != nil {
			if werr := enc.Encode(rpc{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}); werr != nil {
				return werr
			}
			continue
		}
		if req.JSONRPC == "" {
			req.JSONRPC = "2.0"
		}
		note := len(req.ID) == 0 || string(req.ID) == "null"
		switch req.Method {
		case "notifications/initialized", "notifications/cancelled":
			continue
		case "initialize":
			if note {
				continue
			}
			if err := enc.Encode(rpc{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "agentpaas-mcp", "version": "0.0.1-dummy"},
				},
			}); err != nil {
				return err
			}
		case "ping", "tools/list", "tools/call":
			if note {
				continue
			}
			res, err := handle(req)
			if err != nil {
				if werr := enc.Encode(rpc{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}}); werr != nil {
					return werr
				}
				continue
			}
			if err := enc.Encode(rpc{JSONRPC: "2.0", ID: req.ID, Result: res}); err != nil {
				return err
			}
		default:
			if note {
				continue
			}
			if err := enc.Encode(rpc{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func handle(req rpc) (any, error) {
	switch req.Method {
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{
			"tools": []map[string]any{
				{
					"name":        "ping",
					"description": "Dummy connectivity check for host MCP skins.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		}, nil
	case "tools/call":
		var p struct {
			Name string `json:"name"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, err
			}
		}
		if p.Name != "" && p.Name != "ping" {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "unknown tool: " + p.Name}},
				"isError": true,
			}, nil
		}
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": "pong"}},
		}, nil
	default:
		return nil, fmt.Errorf("unhandled %s", req.Method)
	}
}
