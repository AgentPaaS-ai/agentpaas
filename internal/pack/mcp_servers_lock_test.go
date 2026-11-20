package pack

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCreateAgentLock_StampsAgentYAMLMCPServers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	installFakeTool(t, "syft", `#!/bin/sh
printf '%s' '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-mcp"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-mcp"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML: &AgentYAML{
			Name:    "t12a-agent-mcp",
			Version: "0.1.0",
			MCPServers: []MCPServerDecl{
				{
					Name:         "t12a-mcp-m",
					Transport:    "http",
					AllowedTools: []string{"fetch"},
				},
			},
		},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-mcp"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	raw, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		t.Fatalf("json.Marshal lock: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal lock: %v", err)
	}
	ay, ok := parsed["agent_yaml"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml missing or not an object: %s", raw)
	}
	servers, ok := ay["mcp_servers"].([]interface{})
	if !ok || len(servers) != 1 {
		t.Fatalf("agent_yaml.mcp_servers = %v, want 1 stamped server: %s", ay["mcp_servers"], raw)
	}
	server, ok := servers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp_servers[0] not an object: %v", servers[0])
	}
	if server["name"] != "t12a-mcp-m" {
		t.Fatalf("mcp_servers[0].name = %v, want t12a-mcp-m", server["name"])
	}
	if server["transport"] != "http" {
		t.Fatalf("mcp_servers[0].transport = %v, want http", server["transport"])
	}
	if _, hasURL := server["url"]; hasURL {
		t.Fatalf("hosted stamp must omit url so bind can fill it: %v", server)
	}
	tools, ok := server["allowed_tools"].([]interface{})
	if !ok || len(tools) != 1 || tools[0] != "fetch" {
		t.Fatalf("mcp_servers[0].allowed_tools = %v, want [fetch]", server["allowed_tools"])
	}
}

func TestCreateAgentLock_OmitsEmptyMCPServers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake shell tools require a POSIX shell")
	}
	installFakeTool(t, "syft", `#!/bin/sh
printf '%s' '{"spdxVersion":"SPDX-2.3","name":"agentpaas-test"}'
`)
	installFakeTool(t, "cosign", fakeCosignScript())
	key, _ := testKeyPair(t)
	store := testStoreForKey(t, key)
	pubKS, _ := publisherTestStore(t)

	lock, err := CreateAgentLock(context.Background(), LockConfig{
		BuildResult: &BuildResult{
			ImageDigest:      digestString("image-empty"),
			ImageRef:         "agentpaas-test:latest",
			BuildInputDigest: digestString("input-empty"),
			DepsLocked:       []string{"dep==1.0.0"},
		},
		AgentYAML:         &AgentYAML{Name: "no-mcp-agent"},
		Runtime:           RuntimeType("python"),
		BaseImageDigest:   "gcr.io/distroless/python3-debian12@sha256:" + digestString("base-empty"),
		HarnessVersion:    "test",
		Platform:          "linux/arm64",
		SourceDateEpoch:   testTime(),
		KeyStore:          store,
		KeyID:             store.keyID,
		PublisherKeyStore: pubKS,
	})
	if err != nil {
		t.Fatalf("CreateAgentLock: %v", err)
	}

	raw, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		t.Fatalf("json.Marshal lock: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("json.Unmarshal lock: %v", err)
	}
	ay, ok := parsed["agent_yaml"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent_yaml missing or not an object: %s", raw)
	}
	if _, present := ay["mcp_servers"]; present {
		t.Fatalf("empty mcp_servers must be absent from lock agent_yaml: %s", raw)
	}
}

func TestAgentYAMLCanonicalMap_IncludesMCPServers(t *testing.T) {
	ay := &AgentYAML{
		Name: "t12a-agent-mcp",
		MCPServers: []MCPServerDecl{
			{Name: "t12a-mcp-m", Transport: "http", AllowedTools: []string{"fetch"}},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["mcp_servers"]
	if !ok {
		t.Fatal("canonical map missing mcp_servers")
	}
	got, ok := raw.([]map[string]interface{})
	if !ok {
		t.Fatalf("mcp_servers type %T, want []map[string]interface{}", raw)
	}
	if len(got) != 1 {
		t.Fatalf("len(mcp_servers) = %d, want 1", len(got))
	}
	if got[0]["name"] != "t12a-mcp-m" {
		t.Fatalf("name = %v, want t12a-mcp-m", got[0]["name"])
	}
	tools, ok := got[0]["allowed_tools"].([]string)
	if !ok || len(tools) != 1 || tools[0] != "fetch" {
		t.Fatalf("allowed_tools = %v, want [fetch]", got[0]["allowed_tools"])
	}
	tools[0] = "mutated"
	if ay.MCPServers[0].AllowedTools[0] != "fetch" {
		t.Fatal("canonical map must copy allowed_tools")
	}
}

func TestAgentYAML_MCPServersUnmarshalNameOnly(t *testing.T) {
	var ay AgentYAML
	err := yaml.Unmarshal([]byte("name: t12a-agent-mcp\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    allowed_tools:\n      - fetch\n"), &ay)
	if err != nil {
		t.Fatalf("unmarshal agent.yaml mcp_servers: %v", err)
	}
	if len(ay.MCPServers) != 1 {
		t.Fatalf("len(MCPServers) = %d, want 1", len(ay.MCPServers))
	}
	if ay.MCPServers[0].Name != "t12a-mcp-m" || ay.MCPServers[0].Transport != "http" {
		t.Fatalf("MCPServers[0] = %+v", ay.MCPServers[0])
	}
	if ay.MCPServers[0].URL != "" {
		t.Fatalf("URL = %q, want empty so hosted bind can fill", ay.MCPServers[0].URL)
	}
}

func TestLoadAgentYAML_PersistsDeclaredMCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	content := []byte("name: t12a-agent-mcp\nversion: 0.1.0\nruntime: python3.12\nentry: main.py\nmcp_servers:\n  - name: t12a-mcp-m\n    transport: http\n    allowed_tools:\n      - fetch\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if ay == nil {
		t.Fatal("LoadAgentYAML returned nil")
	}
	if len(ay.MCPServers) != 1 || ay.MCPServers[0].Name != "t12a-mcp-m" {
		t.Fatalf("MCPServers = %+v, want t12a-mcp-m", ay.MCPServers)
	}
}
