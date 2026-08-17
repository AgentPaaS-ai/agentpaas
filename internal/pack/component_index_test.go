package pack

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildComponentIndex_MCPFullCard(t *testing.T) {
	agent := &AgentYAML{
		Name:        "demo-mcp",
		Version:     "0.2.0",
		Description: "GitHub helpers",
		Kind:        "mcp_service",
		Egress:      []string{"api.github.com"},
		MCPService: MCPServiceConfig{
			Transport: "streamable_http",
			Tools:     []string{"list_repos"},
		},
		ComponentIndex: map[string]interface{}{
			"title": "GitHub helpers",
			"mcp": map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{"listChanged": false}},
				"tools": []interface{}{
					map[string]interface{}{
						"name":         "list_repos",
						"title":        "List repos",
						"description":  "List repositories",
						"inputSchema":  map[string]interface{}{"type": "object"},
						"outputSchema": map[string]interface{}{"type": "object"},
					},
				},
				"prompts": []interface{}{
					map[string]interface{}{
						"name":        "review",
						"title":       "Review",
						"description": "PR review",
						"arguments": []interface{}{
							map[string]interface{}{"name": "pr", "required": true},
						},
					},
				},
				"resources": []interface{}{
					map[string]interface{}{
						"uri":         "repo://main",
						"name":        "main",
						"mimeType":    "text/plain",
						"description": "Default branch",
					},
				},
			},
		},
	}

	idx := BuildComponentIndex(agent, ComponentIndexProvenance{
		ImageDigest:  "sha256:" + digestString("image"),
		SBOMDigest:   digestString("sbom"),
		PolicyDigest: digestString("policy"),
		Signature:    "cosign://demo",
		Publisher:    "alice",
	})
	if idx == nil {
		t.Fatal("BuildComponentIndex returned nil")
	}
	if idx.SchemaVersion != ComponentIndexSchemaV1 {
		t.Fatalf("schema_version = %q, want %q", idx.SchemaVersion, ComponentIndexSchemaV1)
	}
	if idx.Kind != "mcp" {
		t.Fatalf("kind = %q, want mcp", idx.Kind)
	}
	if idx.Name != "demo-mcp" || idx.Title != "GitHub helpers" || idx.Version != "0.2.0" {
		t.Fatalf("identity = %+v", idx)
	}
	if len(idx.Egress) != 1 || idx.Egress[0] != "api.github.com" {
		t.Fatalf("egress = %#v", idx.Egress)
	}
	if idx.MCP == nil {
		t.Fatal("mcp block missing")
	}
	if idx.MCP.ProtocolVersion != "2025-06-18" {
		t.Fatalf("protocolVersion = %q", idx.MCP.ProtocolVersion)
	}
	if idx.MCP.Transport == nil || idx.MCP.Transport.Type != "streamable-http" {
		t.Fatalf("transport = %+v", idx.MCP.Transport)
	}
	if len(idx.MCP.Tools) != 1 || idx.MCP.Tools[0].Title != "List repos" {
		t.Fatalf("tools = %#v", idx.MCP.Tools)
	}
	if !isJSONObject(idx.MCP.Tools[0].InputSchema) || !isJSONObject(idx.MCP.Tools[0].OutputSchema) {
		t.Fatalf("tool schemas = input %#v output %#v", idx.MCP.Tools[0].InputSchema, idx.MCP.Tools[0].OutputSchema)
	}
	if len(idx.MCP.Prompts) != 1 || len(idx.MCP.Prompts[0].Arguments) != 1 || !idx.MCP.Prompts[0].Arguments[0].Required {
		t.Fatalf("prompts = %#v", idx.MCP.Prompts)
	}
	if len(idx.MCP.Resources) != 1 || idx.MCP.Resources[0].MimeType != "text/plain" {
		t.Fatalf("resources = %#v", idx.MCP.Resources)
	}
	if idx.Invoke.Auth != "inv_token" {
		t.Fatalf("invoke.auth = %q", idx.Invoke.Auth)
	}
	if idx.Provenance.Publisher != "alice" || idx.Provenance.ImageDigest == "" {
		t.Fatalf("provenance = %+v", idx.Provenance)
	}
}

func TestBuildComponentIndex_HonestEmptyWhenUndeclared(t *testing.T) {
	agent := &AgentYAML{
		Name:    "empty-mcp",
		Version: "0.0.1",
		Kind:    "mcp_service",
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if idx.MCP == nil {
		t.Fatal("mcp kind must emit mcp block")
	}
	if idx.MCP.Tools == nil {
		t.Fatal("mcp.tools must be empty-declared, not omitted")
	}
	if len(idx.MCP.Tools) != 0 {
		t.Fatalf("tools = %#v, want empty slice", idx.MCP.Tools)
	}
	if idx.MCP.ProtocolVersion != "" {
		t.Fatalf("undeclared protocolVersion = %q, want empty", idx.MCP.ProtocolVersion)
	}
	if idx.Description != "" {
		t.Fatalf("undeclared description = %q", idx.Description)
	}
	if idx.Egress == nil {
		t.Fatal("egress must be empty-declared, not omitted")
	}
}

func TestBuildComponentIndex_MCPNameOnlyToolsHonestEmptySchemas(t *testing.T) {
	agent := &AgentYAML{
		Name: "names-only",
		Kind: "mcp_service",
		MCPService: MCPServiceConfig{
			Transport: "streamable_http",
			Tools:     []string{"ping"},
		},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if len(idx.MCP.Tools) != 1 || idx.MCP.Tools[0].Name != "ping" {
		t.Fatalf("tools = %#v", idx.MCP.Tools)
	}
	if !isNotDeclared(idx.MCP.Tools[0].InputSchema) {
		t.Fatalf("inputSchema = %#v, want not declared", idx.MCP.Tools[0].InputSchema)
	}
	if !isNotDeclared(idx.MCP.Tools[0].OutputSchema) {
		t.Fatalf("outputSchema = %#v, want not declared", idx.MCP.Tools[0].OutputSchema)
	}
}

func TestBuildComponentIndex_MetaPassThrough(t *testing.T) {
	agent := &AgentYAML{
		Name: "meta-mcp",
		Kind: "mcp_service",
		ComponentIndex: map[string]interface{}{
			"mcp": map[string]interface{}{
				"protocolVersion": "2025-11-25",
				"tools":           []interface{}{},
				"futureWidget":    map[string]interface{}{"enabled": true},
				"_meta": map[string]interface{}{
					"unmodeled_newer_fields": "keep-me",
				},
			},
		},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if idx.MCP.ProtocolVersion != "2025-11-25" {
		t.Fatalf("protocolVersion = %q", idx.MCP.ProtocolVersion)
	}
	if idx.MCP.Meta == nil {
		t.Fatal("_meta missing")
	}
	if idx.MCP.Meta["unmodeled_newer_fields"] != "keep-me" {
		t.Fatalf("_meta declared = %#v", idx.MCP.Meta)
	}
	if _, ok := idx.MCP.Meta["futureWidget"]; !ok {
		t.Fatalf("unknown field not passed through _meta: %#v", idx.MCP.Meta)
	}
}

func TestBuildComponentIndex_AllKinds(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"", "agent"},
		{"worker", "agent"},
		{"agent", "agent"},
		{"mcp_service", "mcp"},
		{"mcp", "mcp"},
		{"tool", "tool"},
		{"sandbox", "sandbox"},
	}
	for _, tc := range cases {
		agent := &AgentYAML{Name: "k", Kind: tc.kind}
		if tc.kind == "agent" || tc.kind == "" || tc.kind == "worker" {
			agent.LLM = LLMConfig{Provider: "xai", Model: "grok-4", Credential: "XAI_API_KEY"}
		}
		idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
		if idx.Kind != tc.want {
			t.Fatalf("kind %q → %q, want %q", tc.kind, idx.Kind, tc.want)
		}
		switch tc.want {
		case "agent":
			if idx.Agent == nil || len(idx.Agent.LLM) != 1 || idx.Agent.LLM[0].Provider != "xai" {
				t.Fatalf("agent block = %+v", idx.Agent)
			}
			if idx.MCP != nil || idx.HTTP != nil || idx.Sandbox != nil {
				t.Fatalf("agent should not emit other kind blocks: %+v", idx)
			}
		case "mcp":
			if idx.MCP == nil {
				t.Fatal("mcp block missing")
			}
		case "tool":
			if idx.HTTP == nil || idx.HTTP.Routes == nil {
				t.Fatalf("http block = %+v", idx.HTTP)
			}
		case "sandbox":
			if idx.Sandbox == nil || idx.Sandbox.Capabilities == nil {
				t.Fatalf("sandbox block = %+v", idx.Sandbox)
			}
		}
	}
}

func TestBuildComponentIndex_ToolRoutesAndSandbox(t *testing.T) {
	tool := &AgentYAML{
		Name: "http-tool",
		Kind: "tool",
		ComponentIndex: map[string]interface{}{
			"http": map[string]interface{}{
				"routes": []interface{}{
					map[string]interface{}{
						"method":       "POST",
						"path":         "/invoke",
						"inputSchema":  map[string]interface{}{"type": "object"},
						"outputSchema": map[string]interface{}{"type": "object"},
					},
				},
			},
		},
	}
	idx := BuildComponentIndex(tool, ComponentIndexProvenance{})
	if idx.Kind != "tool" || idx.HTTP == nil || len(idx.HTTP.Routes) != 1 {
		t.Fatalf("tool index = %+v", idx)
	}
	if idx.HTTP.Routes[0].Path != "/invoke" || !isJSONObject(idx.HTTP.Routes[0].InputSchema) {
		t.Fatalf("route = %+v", idx.HTTP.Routes[0])
	}

	box := &AgentYAML{
		Name: "devbox",
		Kind: "sandbox",
		ComponentIndex: map[string]interface{}{
			"sandbox": map[string]interface{}{
				"capabilities": []interface{}{"exec", "fs"},
				"transport":    map[string]interface{}{"type": "streamable-http"},
			},
		},
	}
	sidx := BuildComponentIndex(box, ComponentIndexProvenance{})
	if sidx.Sandbox == nil || !reflect.DeepEqual(sidx.Sandbox.Capabilities, []string{"exec", "fs"}) {
		t.Fatalf("sandbox = %+v", sidx.Sandbox)
	}
}

func TestComponentIndex_DifferentToolsNewDigest(t *testing.T) {
	base := func(tools []string) *AgentLock {
		lock, err := NewSignedTestLock("same-name", nil)
		if err != nil {
			t.Fatalf("NewSignedTestLock: %v", err)
		}
		lock.AgentYAML = &AgentYAML{
			Name: "same-name",
			Kind: "mcp_service",
			MCPService: MCPServiceConfig{
				Transport: "streamable_http",
				Tools:     tools,
			},
		}
		lock.ComponentIndex = BuildComponentIndex(lock.AgentYAML, ComponentIndexProvenance{
			ImageDigest:  lock.ImageDigest,
			SBOMDigest:   lock.SBOMDigest,
			PolicyDigest: lock.PolicyDigest,
		})
		return lock
	}
	a := base([]string{"alpha"})
	b := base([]string{"beta"})
	da := LockDigest(a)
	db := LockDigest(b)
	if da == "" || db == "" {
		t.Fatal("empty lock digest")
	}
	if da == db {
		t.Fatal("same name + different tools produced the same digest")
	}
}

func TestComponentIndex_CanonicalRoundTrip(t *testing.T) {
	agent := &AgentYAML{
		Name:    "round",
		Version: "1.0.0",
		Kind:    "mcp_service",
		MCPService: MCPServiceConfig{
			Transport: "streamable_http",
			Tools:     []string{"ping"},
		},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{ImageDigest: "sha256:abc"})
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ComponentIndex
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.SchemaVersion != ComponentIndexSchemaV1 || decoded.Kind != "mcp" || decoded.Name != "round" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if len(decoded.MCP.Tools) != 1 || decoded.MCP.Tools[0].Name != "ping" {
		t.Fatalf("decoded tools = %#v", decoded.MCP.Tools)
	}
}

func isJSONObject(v interface{}) bool {
	_, ok := v.(map[string]interface{})
	return ok
}

func isNotDeclared(v interface{}) bool {
	s, ok := v.(string)
	return ok && s == SchemaNotDeclared
}
