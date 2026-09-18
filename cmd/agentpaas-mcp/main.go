// Package main is a stdio MCP adapter that execs the agentpaas CLI.
// D72 name: agentpaas-mcp. Not a per-host runtime. Not pack/deploy.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	protocolVersion = "2024-11-05"
	serverVersion   = "0.5.0-dev"
	execTimeout     = 60 * time.Second
)

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

type mappedTool struct {
	name string
	argv []string
}

var mappedTools = []mappedTool{
	{"doctor", []string{"doctor"}},
	{"version", []string{"version"}},
	{"init", []string{"init"}},
	{"validate", []string{"validate"}},
	{"pack", []string{"pack"}},
	{"policy_show", []string{"policy", "show"}},
	{"policy_validate", []string{"policy", "validate"}},
	{"policy_init", []string{"policy", "init"}},
	{"cloud_whoami", []string{"cloud", "whoami"}},
	{"cloud_push", []string{"cloud", "push"}},
	{"cloud_deploy", []string{"cloud", "deploy"}},
	{"cloud_invoke", []string{"cloud", "invoke"}},
	{"cloud_result", []string{"cloud", "result"}},
	{"cloud_logs", []string{"cloud", "logs"}},
	{"cloud_status", []string{"cloud", "status"}},
	{"audit", []string{"audit"}},
}

var toolPrefixByName = func() map[string][]string {
	m := make(map[string][]string, len(mappedTools))
	for _, t := range mappedTools {
		m[t.name] = t.argv
	}
	return m
}()

var toolInputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"args": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Extra argv after the fixed command prefix",
		},
	},
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
					"serverInfo":      map[string]any{"name": "agentpaas-mcp", "version": serverVersion},
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
		return map[string]any{"tools": listTools()}, nil
	case "tools/call":
		return callTool(req.Params), nil
	default:
		return nil, fmt.Errorf("unhandled %s", req.Method)
	}
}

func listTools() []map[string]any {
	tools := make([]map[string]any, 0, len(mappedTools))
	for _, t := range mappedTools {
		tools = append(tools, map[string]any{
			"name":        t.name,
			"description": "Run agentpaas " + strings.Join(t.argv, " "),
			"inputSchema": toolInputSchema,
		})
	}
	return tools
}

func callTool(params json.RawMessage) map[string]any {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return errorContent("invalid params: " + err.Error())
		}
	}
	if forbiddenTool(p.Name) {
		return errorContent("refused: MCP does not collect passwords, tokens, or secret values")
	}
	prefix, ok := toolPrefixByName[p.Name]
	if !ok {
		return errorContent("unknown tool: " + p.Name)
	}
	extra, err := extraArgs(p.Arguments)
	if err != nil {
		return errorContent("invalid arguments: " + err.Error())
	}
	bin, err := resolveBin()
	if err != nil {
		return errorContent("agentpaas binary not found: " + err.Error())
	}
	argv := make([]string, 0, len(prefix)+len(extra))
	argv = append(argv, prefix...)
	argv = append(argv, extra...)
	return runCLI(bin, argv)
}

func extraArgs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var args struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return args.Args, nil
}

func resolveBin() (string, error) {
	if bin := os.Getenv("AGENTPAAS_BIN"); bin != "" {
		return bin, nil
	}
	return exec.LookPath("agentpaas")
}

func runCLI(bin string, argv []string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, argv...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if ctx.Err() == context.DeadlineExceeded {
		if strings.TrimSpace(text) == "" {
			text = "command timed out after 60s"
		} else {
			text += "\ncommand timed out after 60s"
		}
		return errorContent(text)
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return errorContent(text)
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func errorContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

func forbiddenTool(name string) bool {
	switch name {
	case "login", "logout", "secret", "secret_add", "secret_set", "cloud_login", "cloud_logout":
		return true
	}
	for _, part := range strings.Split(name, "_") {
		switch part {
		case "login", "logout", "secret":
			return true
		}
	}
	return false
}
