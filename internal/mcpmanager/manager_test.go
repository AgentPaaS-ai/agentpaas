package mcpmanager

import (
	"testing"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
)

type recordingAuditAppender struct {
	records []audit.AuditRecord
}

func (a *recordingAuditAppender) Append(record audit.AuditRecord) error {
	a.records = append(a.records, record)
	return nil
}

func TestValidateRejectsDuplicateIDs(t *testing.T) {
	m := NewManager()
	err := m.Validate([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"},
		{Name: "filesystem", Transport: "http", URL: "https://example.com/mcp"},
	})
	if err == nil {
		t.Fatal("expected duplicate server ID error")
	}
}

func TestValidateRejectsEmptyTransport(t *testing.T) {
	m := NewManager()
	err := m.Validate([]policy.MCPServer{{Name: "filesystem", Command: "mcp-fs"}})
	if err == nil {
		t.Fatal("expected empty transport error")
	}
}

func TestValidateRejectsStdioWithoutCommand(t *testing.T) {
	m := NewManager()
	err := m.Validate([]policy.MCPServer{{Name: "filesystem", Transport: "stdio"}})
	if err == nil {
		t.Fatal("expected missing stdio command error")
	}
}

func TestValidateRejectsHTTPWithoutURLOrEndpoint(t *testing.T) {
	m := NewManager()
	err := m.Validate([]policy.MCPServer{{Name: "remote", Transport: "http"}})
	if err == nil {
		t.Fatal("expected missing http URL or endpoint error")
	}
}

func TestRegisterCreatesResources(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs", AllowedTools: []string{"read_file"}},
	}, "agent-1", "run-1")

	resources := m.Status()
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	resource := resources[0]
	if resource.AgentID != "agent-1" || resource.RunID != "run-1" || resource.ServerID != "filesystem" {
		t.Fatalf("unexpected resource identity: %#v", resource)
	}
	if resource.Transport != "stdio" {
		t.Fatalf("expected stdio transport, got %q", resource.Transport)
	}
	if len(resource.AllowedTools) != 1 || resource.AllowedTools[0] != "read_file" {
		t.Fatalf("unexpected allowed tools: %#v", resource.AllowedTools)
	}
	if resource.PolicyDigest == "" {
		t.Fatal("expected policy digest")
	}
}

func TestIsToolAllowedDenyAll(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}}, "", "")
	if m.IsToolAllowed("filesystem", "read_file") {
		t.Fatal("expected empty allowed tools to deny all tools")
	}
}

func TestIsToolAllowedExactMatch(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs", AllowedTools: []string{"read_file"}},
	}, "", "")
	if !m.IsToolAllowed("filesystem", "read_file") {
		t.Fatal("expected exact tool match to be allowed")
	}
	if m.IsToolAllowed("filesystem", "read") {
		t.Fatal("expected partial tool match to be denied")
	}
}

func TestIsToolAllowedUndeclaredServer(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs", AllowedTools: []string{"read_file"}},
	}, "", "")
	if m.IsToolAllowed("unknown", "read_file") {
		t.Fatal("expected undeclared server to deny tool")
	}
}

func TestPolicyDigestDeterministic(t *testing.T) {
	server := policy.MCPServer{
		Name:         "filesystem",
		Transport:    "stdio",
		Command:      "mcp-fs",
		Args:         []string{"--root", "/tmp"},
		AllowedTools: []string{"write_file", "read_file"},
		AuthMode:     "none",
	}
	digest1 := computePolicyDigest(server)
	digest2 := computePolicyDigest(server)
	if digest1 == "" {
		t.Fatal("expected digest")
	}
	if digest1 != digest2 {
		t.Fatalf("expected deterministic digest, got %q and %q", digest1, digest2)
	}
}

func TestPolicyDigestDifferentEntries(t *testing.T) {
	server1 := policy.MCPServer{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}
	server2 := policy.MCPServer{Name: "filesystem", Transport: "stdio", Command: "mcp-other"}
	if computePolicyDigest(server1) == computePolicyDigest(server2) {
		t.Fatal("expected different policy entries to have different digests")
	}
}

func TestStatusReturnsResources(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{
		{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"},
		{Name: "remote", Transport: "http", URL: "https://example.com/mcp"},
	}, "", "")
	if got := len(m.Status()); got != 2 {
		t.Fatalf("expected 2 resources, got %d", got)
	}
}

func TestStatusResourceTypeIsMcpServer(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}}, "", "")
	resources := m.Status()
	if resources[0].ResourceType != "mcp_server" {
		t.Fatalf("expected mcp_server resource type, got %q", resources[0].ResourceType)
	}
}

func TestStatusInitialReadinessStopped(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}}, "", "")
	resources := m.Status()
	if resources[0].Readiness != ReadinessStopped {
		t.Fatalf("expected stopped readiness, got %q", resources[0].Readiness)
	}
}

func TestStatusInitialHealthUnknown(t *testing.T) {
	m := NewManager()
	m.Register([]policy.MCPServer{{Name: "filesystem", Transport: "stdio", Command: "mcp-fs"}}, "", "")
	resources := m.Status()
	if resources[0].Health != HealthUnknown {
		t.Fatalf("expected unknown health, got %q", resources[0].Health)
	}
}

func TestDenyToolCallEmitsAuditEvent(t *testing.T) {
	appender := &recordingAuditAppender{}
	m := NewManager()
	m.DenyToolCall(appender, "filesystem", "read_file", "agent-1", "run-1", "rule-1")

	if len(appender.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(appender.records))
	}
	record := appender.records[0]
	if record.EventType != audit.EventTypeMCPToolDenied {
		t.Fatalf("expected MCP tool denied event, got %q", record.EventType)
	}
	if record.Actor != "agent-1" {
		t.Fatalf("expected actor agent-1, got %q", record.Actor)
	}
	if record.Payload["server_id"] != "filesystem" {
		t.Fatalf("expected server_id payload, got %#v", record.Payload)
	}
	if record.Payload["tool"] != "read_file" {
		t.Fatalf("expected tool payload, got %#v", record.Payload)
	}
	if record.Payload["run_id"] != "run-1" {
		t.Fatalf("expected run_id payload, got %#v", record.Payload)
	}
	if record.Payload["policy_rule_id"] != "rule-1" {
		t.Fatalf("expected policy_rule_id payload, got %#v", record.Payload)
	}
}
