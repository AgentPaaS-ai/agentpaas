package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var requiredTools = []string{
	"doctor",
	"version",
	"init",
	"validate",
	"pack",
	"policy_show",
	"policy_init",
	"cloud_whoami",
	"cloud_push",
	"cloud_deploy",
	"cloud_invoke",
	"cloud_result",
	"cloud_logs",
	"cloud_status",
	"audit",
}

func TestMain(m *testing.M) {
	if os.Getenv("AGENTPAAS_MCP_STUB") == "1" {
		os.Exit(runStub())
	}
	os.Exit(m.Run())
}

func runStub() int {
	if p := os.Getenv("AGENTPAAS_MCP_STUB_RAN"); p != "" {
		_ = os.WriteFile(p, []byte("1\n"), 0o600)
	}
	if p := os.Getenv("AGENTPAAS_MCP_STUB_ARGV"); p != "" {
		var b strings.Builder
		for _, a := range os.Args[1:] {
			b.WriteString(a)
			b.WriteByte('\n')
		}
		_ = os.WriteFile(p, []byte(b.String()), 0o600)
	}
	fmt.Fprint(os.Stdout, os.Getenv("AGENTPAAS_MCP_STUB_STDOUT"))
	code, _ := strconv.Atoi(os.Getenv("AGENTPAAS_MCP_STUB_EXIT"))
	return code
}

func TestInitializeServerInfo(t *testing.T) {
	replies := rpcServe(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	name, ver := serverInfo(t, replies[0])
	if name != "agentpaas-mcp" {
		t.Fatalf("serverInfo.name=%q", name)
	}
	if ver == "" || ver == "0.0.1-dummy" {
		t.Fatalf("serverInfo.version=%q", ver)
	}
}

func TestToolsListContainsRequiredNames(t *testing.T) {
	replies := rpcServe(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	names := toolNames(t, replies[0])
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	var missing []string
	for _, n := range requiredTools {
		if !have[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("tools/list missing %v; got %v", missing, names)
	}
}

func TestUnknownToolNoExec(t *testing.T) {
	_, ran := stubEnv(t, "should-not-run", 0)
	replies := rpcServe(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"not_a_tool","arguments":{}}}`)
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	text, isErr := callResult(t, replies[0])
	if !isErr {
		t.Fatalf("expected isError, got %s", text)
	}
	assertNoExec(t, ran)
}

func TestRejectedAuthToolsNoExec(t *testing.T) {
	for _, name := range []string{"login", "secret_add", "cloud_login"} {
		t.Run(name, func(t *testing.T) {
			_, ran := stubEnv(t, "should-not-run", 0)
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, name)
			replies := rpcServe(t, req)
			if len(replies) != 1 {
				t.Fatalf("got %d replies", len(replies))
			}
			text, isErr := callResult(t, replies[0])
			if !isErr {
				t.Fatalf("expected isError, got %s", text)
			}
			assertNoExec(t, ran)
		})
	}
}

func TestDoctorAndVersionArgv(t *testing.T) {
	cases := []struct {
		tool string
		argv []string
		out  string
	}{
		{tool: "doctor", argv: []string{"doctor"}, out: "doctor-ok\n"},
		{tool: "version", argv: []string{"version"}, out: "agentpaas 0.5.0-dev\n"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			argvFile, ran := stubEnv(t, tc.out, 0)
			req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, tc.tool)
			replies := rpcServe(t, req)
			if len(replies) != 1 {
				t.Fatalf("got %d replies", len(replies))
			}
			text, isErr := callResult(t, replies[0])
			if isErr {
				t.Fatalf("unexpected isError: %s", text)
			}
			if !strings.Contains(text, strings.TrimSpace(tc.out)) {
				t.Fatalf("text %q missing %q", text, tc.out)
			}
			assertArgv(t, argvFile, tc.argv)
			if _, err := os.Stat(ran); err != nil {
				t.Fatalf("expected exec: %v", err)
			}
		})
	}
}

func TestPolicyShowArgvPlusArgs(t *testing.T) {
	argvFile, _ := stubEnv(t, "policy-ok\n", 0)
	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"policy_show","arguments":{"args":["--compiled"]}}}`
	replies := rpcServe(t, req)
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	text, isErr := callResult(t, replies[0])
	if isErr {
		t.Fatalf("unexpected isError: %s", text)
	}
	if !strings.Contains(text, "policy-ok") {
		t.Fatalf("text %q", text)
	}
	assertArgv(t, argvFile, []string{"policy", "show", "--compiled"})
}

func TestNonZeroStubExitIsError(t *testing.T) {
	_, ran := stubEnv(t, "boom from stub\n", 7)
	replies := rpcServe(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"doctor","arguments":{}}}`)
	if len(replies) != 1 {
		t.Fatalf("got %d replies", len(replies))
	}
	text, isErr := callResult(t, replies[0])
	if !isErr {
		t.Fatalf("expected isError, got %s", text)
	}
	if !strings.Contains(text, "boom from stub") {
		t.Fatalf("output not included: %q", text)
	}
	if _, err := os.Stat(ran); err != nil {
		t.Fatalf("expected exec: %v", err)
	}
}

func TestServeStdioContract(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		"",
	}, "\n"))
	var out bytes.Buffer
	if err := serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("got %d replies: %s", len(lines), out.String())
	}
	var init rpc
	if err := json.Unmarshal(lines[0], &init); err != nil {
		t.Fatal(err)
	}
	name, ver := serverInfo(t, init)
	if name != "agentpaas-mcp" {
		t.Fatalf("initialize name=%q", name)
	}
	if ver == "0.0.1-dummy" {
		t.Fatalf("initialize still dummy version")
	}
	var listed rpc
	if err := json.Unmarshal(lines[1], &listed); err != nil {
		t.Fatal(err)
	}
	names := toolNames(t, listed)
	if len(names) == 0 {
		t.Fatal("tools/list empty")
	}
}

func rpcServe(t *testing.T, reqs ...string) []rpc {
	t.Helper()
	in := strings.NewReader(strings.Join(append(append([]string{}, reqs...), ""), "\n"))
	var out bytes.Buffer
	if err := serve(in, &out); err != nil {
		t.Fatal(err)
	}
	raw := bytes.TrimSpace(out.Bytes())
	if len(raw) == 0 {
		return nil
	}
	lines := bytes.Split(raw, []byte("\n"))
	replies := make([]rpc, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var r rpc
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("unmarshal %s: %v", line, err)
		}
		replies = append(replies, r)
	}
	return replies
}

func serverInfo(t *testing.T, r rpc) (name, version string) {
	t.Helper()
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", r.Result)
	}
	info, _ := m["serverInfo"].(map[string]any)
	name, _ = info["name"].(string)
	version, _ = info["version"].(string)
	return name, version
}

func toolNames(t *testing.T, r rpc) []string {
	t.Helper()
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", r.Result)
	}
	tools, _ := m["tools"].([]any)
	var names []string
	for _, raw := range tools {
		tm, _ := raw.(map[string]any)
		if n, ok := tm["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}

func callResult(t *testing.T, r rpc) (text string, isError bool) {
	t.Helper()
	raw, err := json.Marshal(r.Result)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("call result %s: %v", raw, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String(), res.IsError
}

func stubEnv(t *testing.T, stdout string, exit int) (argvFile, ranFile string) {
	t.Helper()
	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	ranFile = filepath.Join(dir, "ran")
	t.Setenv("AGENTPAAS_BIN", os.Args[0])
	t.Setenv("AGENTPAAS_MCP_STUB", "1")
	t.Setenv("AGENTPAAS_MCP_STUB_ARGV", argvFile)
	t.Setenv("AGENTPAAS_MCP_STUB_RAN", ranFile)
	t.Setenv("AGENTPAAS_MCP_STUB_STDOUT", stdout)
	t.Setenv("AGENTPAAS_MCP_STUB_EXIT", strconv.Itoa(exit))
	return argvFile, ranFile
}

func assertNoExec(t *testing.T, ranFile string) {
	t.Helper()
	if _, err := os.Stat(ranFile); err == nil {
		t.Fatal("expected zero execs")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertArgv(t *testing.T, path string, want []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimSuffix(string(b), "\n")
	var got []string
	if s != "" {
		got = strings.Split(s, "\n")
	}
	if len(got) != len(want) {
		t.Fatalf("argv=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv=%v want %v", got, want)
		}
	}
}
