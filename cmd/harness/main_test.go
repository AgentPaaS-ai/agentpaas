package main

import (
	"testing"
	"time"
)

func TestBuildConfig_AgentKindFromEnv(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "mcp_service")
	t.Setenv("AGENTPAAS_MCP_DECLARED_TOOLS", "echo,ping")
	t.Setenv("AGENTPAAS_MCP_MAX_CONCURRENCY", "4")

	cfg := buildConfig()

	if cfg.AgentKind != "mcp_service" {
		t.Fatalf("AgentKind = %q, want mcp_service", cfg.AgentKind)
	}
	if cfg.MCPDeclaredTools != "echo,ping" {
		t.Fatalf("MCPDeclaredTools = %q, want echo,ping", cfg.MCPDeclaredTools)
	}
	if cfg.MCPMaxConcurrency != 4 {
		t.Fatalf("MCPMaxConcurrency = %d, want 4", cfg.MCPMaxConcurrency)
	}
}

func TestBuildConfig_AgentKindDefaults(t *testing.T) {
	cfg := buildConfig()

	if cfg.AgentKind != "" {
		t.Fatalf("AgentKind = %q, want empty (worker default)", cfg.AgentKind)
	}
	if cfg.MCPDeclaredTools != "" {
		t.Fatalf("MCPDeclaredTools = %q, want empty", cfg.MCPDeclaredTools)
	}
	if cfg.MCPMaxConcurrency != 0 {
		t.Fatalf("MCPMaxConcurrency = %d, want 0", cfg.MCPMaxConcurrency)
	}
}

func TestEnvOrDefault_ReturnsEnvValue(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_KEY", "custom-value")
	if got := envOrDefault("AGENTPAAS_TEST_KEY", "default"); got != "custom-value" {
		t.Fatalf("envOrDefault() = %q, want %q", got, "custom-value")
	}
}

func TestEnvOrDefault_ReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_KEY", "")
	if got := envOrDefault("AGENTPAAS_TEST_KEY", "default"); got != "default" {
		t.Fatalf("envOrDefault() = %q, want %q", got, "default")
	}
}

func TestEnvDuration_ValidDuration(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "30s")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != 30*time.Second {
		t.Fatalf("envDuration() = %v, want %v", got, 30*time.Second)
	}
}

func TestEnvDuration_InvalidReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "garbage")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != time.Minute {
		t.Fatalf("envDuration() = %v, want %v", got, time.Minute)
	}
}

func TestEnvDuration_MissingReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != time.Minute {
		t.Fatalf("envDuration() = %v, want %v", got, time.Minute)
	}
}