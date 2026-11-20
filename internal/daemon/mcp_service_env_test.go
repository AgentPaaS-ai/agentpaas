package daemon

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestMcpServiceEnv(t *testing.T) {
	t.Parallel()

	envVal := func(env []string, key string) (string, bool) {
		prefix := key + "="
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				return strings.TrimPrefix(e, prefix), true
			}
		}
		return "", false
	}

	t.Run("mcp_service yields kind addr http capability", func(t *testing.T) {
		t.Parallel()
		env := mcpServiceEnv("mcp_service", []string{"echo", "ping"}, 4)

		kind, ok := envVal(env, "AGENTPAAS_AGENT_KIND")
		if !ok || kind != "mcp_service" {
			t.Fatalf("AGENTPAAS_AGENT_KIND = %q ok=%v, want mcp_service", kind, ok)
		}
		if got, ok := envVal(env, "AGENTPAAS_ADDR"); !ok || got != "127.0.0.1:8090" {
			t.Fatalf("AGENTPAAS_ADDR = %q ok=%v, want 127.0.0.1:8090", got, ok)
		}
		if got, ok := envVal(env, "AGENTPAAS_MCP_HTTP_ADDR"); !ok || got != "0.0.0.0:8080" {
			t.Fatalf("AGENTPAAS_MCP_HTTP_ADDR = %q ok=%v, want 0.0.0.0:8080", got, ok)
		}
		if got, ok := envVal(env, "AGENTPAAS_MCP_DECLARED_TOOLS"); !ok || got != "echo,ping" {
			t.Fatalf("AGENTPAAS_MCP_DECLARED_TOOLS = %q ok=%v, want echo,ping", got, ok)
		}
		if got, ok := envVal(env, "AGENTPAAS_MCP_MAX_CONCURRENCY"); !ok || got != "4" {
			t.Fatalf("AGENTPAAS_MCP_MAX_CONCURRENCY = %q ok=%v, want 4", got, ok)
		}
		capVal, ok := envVal(env, "AGENTPAAS_MCP_CAPABILITY")
		if !ok || capVal == "" {
			t.Fatal("AGENTPAAS_MCP_CAPABILITY missing or empty")
		}
		if len(capVal) != 64 {
			t.Fatalf("AGENTPAAS_MCP_CAPABILITY length = %d, want 64 hex chars", len(capVal))
		}
		if _, err := hex.DecodeString(capVal); err != nil {
			t.Fatalf("AGENTPAAS_MCP_CAPABILITY is not hex: %v", err)
		}
	})

	t.Run("worker kind only sets AGENTPAAS_AGENT_KIND", func(t *testing.T) {
		t.Parallel()
		env := mcpServiceEnv("worker", []string{"echo"}, 4)
		if got, ok := envVal(env, "AGENTPAAS_AGENT_KIND"); !ok || got != "worker" {
			t.Fatalf("AGENTPAAS_AGENT_KIND = %q ok=%v, want worker", got, ok)
		}
		for _, key := range []string{
			"AGENTPAAS_ADDR",
			"AGENTPAAS_MCP_HTTP_ADDR",
			"AGENTPAAS_MCP_DECLARED_TOOLS",
			"AGENTPAAS_MCP_CAPABILITY",
			"AGENTPAAS_MCP_MAX_CONCURRENCY",
		} {
			if _, ok := envVal(env, key); ok {
				t.Fatalf("%s must not be set for worker", key)
			}
		}
	})

	t.Run("empty and invalid kind default to worker", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []string{"", "MCP_SERVICE", "mcp-service", "worker!", strings.Repeat("a", 33)} {
			env := mcpServiceEnv(kind, nil, 0)
			if got, ok := envVal(env, "AGENTPAAS_AGENT_KIND"); !ok || got != "worker" {
				t.Fatalf("kind %q -> AGENTPAAS_AGENT_KIND = %q ok=%v, want worker", kind, got, ok)
			}
			if _, ok := envVal(env, "AGENTPAAS_MCP_CAPABILITY"); ok {
				t.Fatalf("kind %q must not set capability", kind)
			}
		}
	})

	t.Run("drops invalid tools and out-of-range concurrency", func(t *testing.T) {
		t.Parallel()
		longTool := strings.Repeat("a", 65)
		env := mcpServiceEnv("mcp_service", []string{"ok_tool", "bad tool", longTool, "also.ok-1"}, 0)
		if got, ok := envVal(env, "AGENTPAAS_MCP_DECLARED_TOOLS"); !ok || got != "ok_tool,also.ok-1" {
			t.Fatalf("AGENTPAAS_MCP_DECLARED_TOOLS = %q ok=%v, want ok_tool,also.ok-1", got, ok)
		}
		if _, ok := envVal(env, "AGENTPAAS_MCP_MAX_CONCURRENCY"); ok {
			t.Fatal("AGENTPAAS_MCP_MAX_CONCURRENCY must be omitted when maxConc is 0")
		}
		env = mcpServiceEnv("mcp_service", nil, 33)
		if _, ok := envVal(env, "AGENTPAAS_MCP_MAX_CONCURRENCY"); ok {
			t.Fatal("AGENTPAAS_MCP_MAX_CONCURRENCY must be omitted when maxConc is 33")
		}
	})
}
