package main

import (
	"bytes"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLogEnvSummary_LogsKeysAndLengthsNotValues(t *testing.T) {
	// Secret-looking values must never appear in log output.
	const secretBindings = `[{"credential_id":"gmail","host_pattern":"gmail.googleapis.com","consent_url":"https://example/oauth"}]`
	const secretCreds = `[{"id":"k","header":"Authorization","value":"Bearer SUPER_SECRET_NEVER_LOG"}]`

	t.Setenv("AGENTPAAS_OAUTH_BINDINGS_JSON", secretBindings)
	t.Setenv("AGENTPAAS_CREDENTIALS_JSON", secretCreds)
	t.Setenv("AGENTPAAS_CREDENTIALS_PATH", "")
	t.Setenv("AGENTPAAS_AGENT_KIND", "worker")
	t.Setenv("AGENTPAAS_MCP_DECLARED_TOOLS", "")
	t.Setenv("AGENTPAAS_MCP_CAPABILITY", "")
	t.Setenv("AGENTPAAS_DURABLE_PATH", "")
	t.Setenv("AGENTPAAS_CPU_QUOTA_SECONDS", "")
	t.Setenv("AGENTPAAS_MAX_PIDS", "")
	t.Setenv("AGENTPAAS_ADDR", "127.0.0.1:9090")
	t.Setenv("AGENTPAAS_MCP_HTTP_ADDR", "")
	t.Setenv("AGENTPAAS_MCP_BINDINGS_JSON", secretBindings)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logEnvSummary()

	out := buf.String()
	wantKeys := []string{
		"AGENTPAAS_OAUTH_BINDINGS_JSON",
		"AGENTPAAS_CREDENTIALS_JSON",
		"AGENTPAAS_CREDENTIALS_PATH",
		"AGENTPAAS_AGENT_KIND",
		"AGENTPAAS_MCP_DECLARED_TOOLS",
		"AGENTPAAS_MCP_CAPABILITY",
		"AGENTPAAS_DURABLE_PATH",
		"AGENTPAAS_CPU_QUOTA_SECONDS",
		"AGENTPAAS_MAX_PIDS",
		"AGENTPAAS_ADDR",
		"AGENTPAAS_MCP_HTTP_ADDR",
		"AGENTPAAS_MCP_BINDINGS_JSON",
	}
	for _, k := range wantKeys {
		if !strings.Contains(out, "harness: env "+k) {
			t.Errorf("log missing key %s; out=%q", k, out)
		}
	}
	if !strings.Contains(out, "AGENTPAAS_OAUTH_BINDINGS_JSON = "+strconv.Itoa(len(secretBindings))+" chars") {
		t.Errorf("want bindings length log; out=%q", out)
	}
	if !strings.Contains(out, "AGENTPAAS_CREDENTIALS_JSON = "+strconv.Itoa(len(secretCreds))+" chars") {
		t.Errorf("want credentials length log; out=%q", out)
	}
	if !strings.Contains(out, "AGENTPAAS_CREDENTIALS_PATH = (empty)") {
		t.Errorf("want empty credentials path; out=%q", out)
	}
	if !strings.Contains(out, "AGENTPAAS_ADDR = "+strconv.Itoa(len("127.0.0.1:9090"))+" chars") {
		t.Errorf("want addr length log; out=%q", out)
	}
	if !strings.Contains(out, "AGENTPAAS_MCP_BINDINGS_JSON = "+strconv.Itoa(len(secretBindings))+" chars") {
		t.Errorf("want MCP bindings length log; out=%q", out)
	}
	// Never log values.
	for _, leak := range []string{secretBindings, secretCreds, "SUPER_SECRET", "Bearer ", "gmail.googleapis.com", "127.0.0.1:9090"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaked value fragment %q; out=%q", leak, out)
		}
	}
}

func TestLogEnvSummary_EmptyDoesNotPanic(t *testing.T) {
	keys := []string{
		"AGENTPAAS_OAUTH_BINDINGS_JSON",
		"AGENTPAAS_CREDENTIALS_JSON",
		"AGENTPAAS_CREDENTIALS_PATH",
		"AGENTPAAS_AGENT_KIND",
		"AGENTPAAS_MCP_DECLARED_TOOLS",
		"AGENTPAAS_MCP_CAPABILITY",
		"AGENTPAAS_DURABLE_PATH",
		"AGENTPAAS_CPU_QUOTA_SECONDS",
		"AGENTPAAS_MAX_PIDS",
		"AGENTPAAS_ADDR",
		"AGENTPAAS_MCP_HTTP_ADDR",
		"AGENTPAAS_MCP_BINDINGS_JSON",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	logEnvSummary()
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("expected empty markers; out=%q", buf.String())
	}
}

// B5: expectation marker vs bindings cross-check.
const oauthExpectedWarn = "WARN: harness: AGENTPAAS_OAUTH_EXPECTED"

func TestLogEnvSummary_OAuthExpectedEmptyBindingsWarns(t *testing.T) {
	t.Setenv("AGENTPAAS_OAUTH_EXPECTED", "1")
	t.Setenv("AGENTPAAS_OAUTH_BINDINGS_JSON", "")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logEnvSummary()

	out := buf.String()
	if !strings.Contains(out, oauthExpectedWarn) {
		t.Fatalf("want WARN when expected=1 and bindings empty; out=%q", out)
	}
	// Never log env values (bindings empty anyway; still guard expected marker value path).
	if strings.Contains(out, "SUPER_SECRET") {
		t.Fatalf("log leaked secret fragment; out=%q", out)
	}
}

func TestLogEnvSummary_OAuthExpectedWithBindingsNoWarn(t *testing.T) {
	const bindings = `[{"credential_id":"gmail","host_pattern":"gmail.googleapis.com"}]`
	t.Setenv("AGENTPAAS_OAUTH_EXPECTED", "1")
	t.Setenv("AGENTPAAS_OAUTH_BINDINGS_JSON", bindings)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logEnvSummary()

	out := buf.String()
	if strings.Contains(out, oauthExpectedWarn) {
		t.Fatalf("must not WARN when bindings present; out=%q", out)
	}
	if !strings.Contains(out, "harness: OAuth expected and present") {
		t.Fatalf("want present confirmation log; out=%q", out)
	}
	if !strings.Contains(out, strconv.Itoa(len(bindings))+" chars") {
		t.Fatalf("want bindings char count in present log; out=%q", out)
	}
	// Never log binding values.
	for _, leak := range []string{bindings, "gmail.googleapis.com", "credential_id"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaked value fragment %q; out=%q", leak, out)
		}
	}
}

func TestLogEnvSummary_OAuthExpectedUnsetNoWarn(t *testing.T) {
	t.Setenv("AGENTPAAS_OAUTH_EXPECTED", "")
	t.Setenv("AGENTPAAS_OAUTH_BINDINGS_JSON", "")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logEnvSummary()

	out := buf.String()
	if strings.Contains(out, oauthExpectedWarn) {
		t.Fatalf("must not WARN when EXPECTED unset; out=%q", out)
	}
	if strings.Contains(out, "OAuth expected and present") {
		t.Fatalf("must not log present confirmation when EXPECTED unset; out=%q", out)
	}
}

func TestMainUsesHTTPDefaultClient(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "http.DefaultClient") {
		t.Fatal("main.go must pass http.DefaultClient to NewRouter")
	}
	if strings.Contains(src, "NewRouter(manager, lifecycle, nil, cfg.Audit)") {
		t.Fatal("main.go still passes nil HTTP gateway to NewRouter")
	}
}

func TestBuildConfig_MCPBindingsJSON(t *testing.T) {
	const raw = `[{"name":"ext","url":"http://127.0.0.1:9/mcp"}]`
	t.Setenv("AGENTPAAS_MCP_BINDINGS_JSON", raw)
	cfg := buildConfig()
	if cfg.MCPBindingsJSON != raw {
		t.Fatalf("MCPBindingsJSON = %q, want %q", cfg.MCPBindingsJSON, raw)
	}
}

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