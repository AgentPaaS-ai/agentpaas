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
	"strconv"
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
	br := bufio.NewReader(in)
	if err := discardLeadingWS(br); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	b, err := br.Peek(1)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	if b[0] == '{' {
		return serveNDJSON(br, out)
	}
	return serveLSP(br, out)
}

func discardLeadingWS(br *bufio.Reader) error {
	for {
		b, err := br.Peek(1)
		if err != nil {
			return err
		}
		switch b[0] {
		case ' ', '	', '\n', '\r':
			if _, err := br.ReadByte(); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func serveNDJSON(br *bufio.Reader, out io.Writer) error {
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := dispatch(line, func(msg rpc) error { return enc.Encode(msg) }); err != nil {
			return err
		}
	}
	return sc.Err()
}

func serveLSP(br *bufio.Reader, out io.Writer) error {
	for {
		body, err := readLSP(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(body))) == 0 {
			continue
		}
		if err := dispatch(body, func(msg rpc) error { return writeLSP(out, msg) }); err != nil {
			return err
		}
	}
}

func readLSP(br *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if !strings.EqualFold(key, "Content-Length") {
			continue
		}
		parsed, err := strconv.Atoi(val)
		if err != nil || parsed < 0 || parsed > 1024*1024 {
			return nil, fmt.Errorf("bad Content-Length")
		}
		n = parsed
	}
	if n < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(br, buf)
	return buf, err
}

func writeLSP(out io.Writer, msg rpc) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = out.Write(body)
	return err
}

func dispatch(raw []byte, write func(rpc) error) error {
	var req rpc
	if err := json.Unmarshal(raw, &req); err != nil {
		return write(rpc{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	note := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "notifications/initialized", "notifications/cancelled":
		return nil
	case "initialize":
		if note {
			return nil
		}
		return write(rpc{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "agentpaas-mcp", "version": serverVersion},
			},
		})
	case "ping", "tools/list", "tools/call":
		if note {
			return nil
		}
		res, err := handle(req)
		if err != nil {
			return write(rpc{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32603, Message: err.Error()}})
		}
		return write(rpc{JSONRPC: "2.0", ID: req.ID, Result: res})
	default:
		if note {
			return nil
		}
		return write(rpc{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
	}
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
