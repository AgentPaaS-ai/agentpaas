package policy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// B19-T9: Cost Tracking & Observability
// ---------------------------------------------------------------------------

func TestParsePolicy_WithObservability(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
observability:
  cost_tracking: true
  otel_endpoint: http://localhost:4317
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.Observability == nil {
		t.Fatal("expected Observability to be non-nil")
	}
	if !p.Observability.CostTracking {
		t.Error("expected CostTracking=true")
	}
	if p.Observability.OTelEndpoint != "http://localhost:4317" {
		t.Errorf("expected OTelEndpoint=http://localhost:4317, got %q", p.Observability.OTelEndpoint)
	}
}

func TestParsePolicy_WithoutObservability(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if p.Observability != nil {
		t.Error("expected Observability to be nil")
	}
}

func TestValidateObservability_InvalidEndpoint(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Observability: &Observability{
			CostTracking: true,
			OTelEndpoint: "not-a-url",
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "otel_endpoint must be a valid URL")
}

func TestValidateObservability_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Observability: &Observability{
			CostTracking: true,
			OTelEndpoint: "http://localhost:4317",
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestCompileGatewayConfig_ObservabilityTracing(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		Observability: &Observability{
			CostTracking: true,
			OTelEndpoint: "http://localhost:4317",
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	var decoded any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, outStr)
	}

	if !strings.Contains(outStr, "tracing") {
		t.Errorf("expected tracing in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "otlpEndpoint") {
		t.Errorf("expected otlpEndpoint in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "http://localhost:4317") {
		t.Errorf("expected OTel endpoint in output, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_NoTracingWithoutObservability(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if strings.Contains(outStr, "tracing") {
		t.Errorf("tracing should NOT appear without observability, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_BackwardCompatNoObservability(t *testing.T) {
	p := samplePolicy()
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if strings.Contains(outStr, "tracing") {
		t.Errorf("samplePolicy should not have tracing, got:\n%s", outStr)
	}
}

// ---------------------------------------------------------------------------
// B19-T10: MCP Tool Access Control
// ---------------------------------------------------------------------------

func TestParsePolicy_WithDeniedTools(t *testing.T) {
	yamlStr := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.openai.com
    ports: [443]
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["@modelcontextprotocol/server-filesystem"]
    allowed_tools:
      - read_file
      - list_directory
    denied_tools:
      - write_file
      - delete_file
`
	p, err := ParsePolicy(strings.NewReader(yamlStr))
	if err != nil {
		t.Fatalf("ParsePolicy failed: %v", err)
	}
	if len(p.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(p.MCPServers))
	}
	m := p.MCPServers[0]
	if len(m.AllowedTools) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(m.AllowedTools))
	}
	if len(m.DeniedTools) != 2 {
		t.Errorf("expected 2 denied tools, got %d", len(m.DeniedTools))
	}
}

func TestValidateMCPToolAccess_ToolInBothLists(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		MCPServers: []MCPServer{
			{
				Name:      "filesystem",
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"@modelcontextprotocol/server-filesystem"},
				AllowedTools: []string{"read_file", "write_file"},
				DeniedTools:  []string{"write_file", "delete_file"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireValidationError(t, errs, "error", "is in both allowed_tools and denied_tools")
}

func TestValidateMCPToolAccess_ValidConfig(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		MCPServers: []MCPServer{
			{
				Name:         "filesystem",
				Transport:    "stdio",
				Command:      "npx",
				Args:         []string{"@modelcontextprotocol/server-filesystem"},
				AllowedTools: []string{"read_file", "list_directory"},
				DeniedTools:  []string{"write_file", "delete_file"},
			},
		},
	}
	errs := ValidatePolicy(p)
	requireNoValidationErrors(t, errs, false)
}

func TestCompileGatewayConfig_MCPAllowedToolsInOutput(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		MCPServers: []MCPServer{
			{
				Name:         "filesystem",
				Transport:    "stdio",
				Command:      "npx",
				Args:         []string{"server-filesystem"},
				AllowedTools: []string{"read_file"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "allowedTools") {
		t.Errorf("expected allowedTools in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "read_file") {
		t.Errorf("expected read_file in allowedTools, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_MCPDeniedToolsInOutput(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		MCPServers: []MCPServer{
			{
				Name:        "filesystem",
				Transport:   "stdio",
				Command:     "npx",
				Args:        []string{"server-filesystem"},
				DeniedTools: []string{"delete_file"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if !strings.Contains(outStr, "deniedTools") {
		t.Errorf("expected deniedTools in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "delete_file") {
		t.Errorf("expected delete_file in deniedTools, got:\n%s", outStr)
	}
}

func TestCompileGatewayConfig_MCPNoToolAccessWhenNotConfigured(t *testing.T) {
	p := &Policy{
		Version: "1",
		Agent:   AgentConfig{Name: "test"},
		Egress:  []EgressRule{{Domain: "api.openai.com", Ports: []int{443}}},
		MCPServers: []MCPServer{
			{
				Name:      "filesystem",
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"server-filesystem"},
			},
		},
	}
	out, err := CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	outStr := string(out)

	if strings.Contains(outStr, "allowedTools") {
		t.Errorf("allowedTools should NOT appear when not configured, got:\n%s", outStr)
	}
	if strings.Contains(outStr, "deniedTools") {
		t.Errorf("deniedTools should NOT appear when not configured, got:\n%s", outStr)
	}
}
