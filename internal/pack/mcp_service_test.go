package pack

import (
	"strings"
	"testing"
)

func TestValidateMCPServiceConfig_Valid(t *testing.T) {
	tests := []struct {
		name  string
		agent *AgentYAML
	}{
		{
			name: "full valid config",
			agent: &AgentYAML{
				Name: "feedback-tools",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport:      "streamable_http",
					Tools:          []string{"lookup_feedback", "list_accounts"},
					MaxConcurrency: 4,
				},
			},
		},
		{
			name: "default concurrency (0)",
			agent: &AgentYAML{
				Name: "feedback-tools",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport:      "streamable_http",
					Tools:          []string{"lookup_feedback"},
					MaxConcurrency: 0,
				},
			},
		},
		{
			name: "max concurrency 32",
			agent: &AgentYAML{
				Name: "feedback-tools",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport:      "streamable_http",
					Tools:          []string{"t1"},
					MaxConcurrency: 32,
				},
			},
		},
		{
			name: "tool names with dots and underscores",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"lookup.feedback_v2", "list-accounts"},
				},
			},
		},
		{
			name:    "nil agent",
			agent:   nil,
		},
		{
			name: "legacy worker (no kind, no mcp_service)",
			agent: &AgentYAML{
				Name: "worker",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPServiceConfig(tt.agent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMCPServiceConfig_Errors(t *testing.T) {
	tests := []struct {
		name      string
		agent     *AgentYAML
		errSubstr string
	}{
		{
			name: "kind=mcp_service without transport",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Tools: []string{"t1"},
				},
			},
			errSubstr: "transport is required",
		},
		{
			name: "unsupported transport",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "stdio",
					Tools:     []string{"t1"},
				},
			},
			errSubstr: "streamable_http",
		},
		{
			name: "empty tools list",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{},
				},
			},
			errSubstr: "non-empty",
		},
		{
			name: "empty tool name",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"good", ""},
				},
			},
			errSubstr: "empty tool name",
		},
		{
			name: "invalid tool name starting with digit",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"1bad"},
				},
			},
			errSubstr: "invalid tool name",
		},
		{
			name: "invalid tool name with spaces",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"bad tool"},
				},
			},
			errSubstr: "invalid tool name",
		},
		{
			name: "duplicate tool name",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"dup", "dup"},
				},
			},
			errSubstr: "duplicate tool name",
		},
		{
			name: "negative concurrency",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport:      "streamable_http",
					Tools:          []string{"t1"},
					MaxConcurrency: -1,
				},
			},
			errSubstr: "max_concurrency must be >= 0",
		},
		{
			name: "concurrency too high",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "mcp_service",
				MCPService: MCPServiceConfig{
					Transport:      "streamable_http",
					Tools:          []string{"t1"},
					MaxConcurrency: 33,
				},
			},
			errSubstr: "max_concurrency must be <= 32",
		},
		{
			name: "mcp_service block without kind",
			agent: &AgentYAML{
				Name: "svc",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"t1"},
				},
			},
			errSubstr: "requires kind: mcp_service",
		},
		{
			name: "mcp_service block with kind=worker",
			agent: &AgentYAML{
				Name: "svc",
				Kind: "worker",
				MCPService: MCPServiceConfig{
					Transport: "streamable_http",
					Tools:     []string{"t1"},
				},
			},
			errSubstr: "requires kind: mcp_service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCPServiceConfig(tt.agent)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestLoadAgentYAML_ParsesMCPServiceBlock(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", `name: feedback-tools
kind: mcp_service
mcp_service:
  transport: streamable_http
  tools:
    - lookup_feedback
    - list_accounts
  max_concurrency: 4
`)

	agent, err := LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML error: %v", err)
	}
	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.Kind != "mcp_service" {
		t.Fatalf("Kind = %q, want mcp_service", agent.Kind)
	}
	if agent.MCPService.Transport != "streamable_http" {
		t.Fatalf("Transport = %q, want streamable_http", agent.MCPService.Transport)
	}
	if len(agent.MCPService.Tools) != 2 {
		t.Fatalf("Tools len = %d, want 2", len(agent.MCPService.Tools))
	}
	if agent.MCPService.MaxConcurrency != 4 {
		t.Fatalf("MaxConcurrency = %d, want 4", agent.MCPService.MaxConcurrency)
	}
}
