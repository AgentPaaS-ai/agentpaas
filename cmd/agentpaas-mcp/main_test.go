package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeInitializeAndPing(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ping","arguments":{}}}`,
		"",
	}, "\n"))
	var out bytes.Buffer
	if err := serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("got %d replies: %s", len(lines), out.String())
	}
	var init rpc
	if err := json.Unmarshal(lines[0], &init); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(init.Result)
	if !bytes.Contains(raw, []byte(`"agentpaas-mcp"`)) {
		t.Fatalf("initialize: %s", lines[0])
	}
	var call rpc
	if err := json.Unmarshal(lines[2], &call); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(call.Result)
	if !bytes.Contains(raw, []byte("pong")) {
		t.Fatalf("tools/call: %s", lines[2])
	}
}
