package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const policyShowCanary = "sk-CANARY-SECRET"

const policyShowCompiledYAML = `version: "1.1"
agent:
  name: show-compiled
ingress:
  - path: /webhook
    port: 8080
egress:
  - domain: api.openai.com
    ports: [443]
  - cidr: 10.0.0.0/8
    ports: [443]
credentials:
  - id: openai-key
    type: header
    header: Authorization
    value: sk-CANARY-SECRET
mcp_servers:
  - name: code-exec
    url: http://localhost:9090/mcp
    transport: http
    allowed_tools: [execute, read]
    denied_tools: [shell]
    headers:
      Authorization: sk-CANARY-SECRET
llm_budget:
  max_tokens: 100000
  max_tokens_per_request: 8000
  max_cost_usd: "5.00"
llm_rate_limit:
  requests_per_minute: 60
  tokens_per_minute: 100000
guardrails:
  pii:
    action: mask
    builtins: [Email]
    patterns:
      - "(?i)employee-id-[0-9]+"
model_routes:
  primary:
    pattern: local-first
    cloud_transfer: allowed
    candidates:
      - id: ollama
        role: primary
        provider: ollama
        model: llama3
        location: local
`

const policyShowMinimalYAML = `version: "1.0"
agent:
  name: min
`

func writePolicyShowYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeCLITestFile(t, dir, "policy.yaml", content)
	return dir
}

func execPolicyShow(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return executeCmd(append([]string{"policy", "show"}, args...)...)
}

func mustPolicyShowJSON(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("json unmarshal: %v\nstdout:\n%s", err, stdout)
	}
	return got
}

func jsonMap(t *testing.T, v any, path string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: want object, got %T (%v)", path, v, v)
	}
	return m
}

func jsonBool(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing %q in %v", key, m)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("%s: want bool, got %T (%v)", key, v, v)
	}
	return b
}

func jsonString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing %q in %v", key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s: want string, got %T (%v)", key, v, v)
	}
	return s
}

func TestPolicyShowCompiledJSON(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	stdout, stderr, err := execPolicyShow(t, dir, "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("canary credential value leaked in JSON output")
	}

	got := mustPolicyShowJSON(t, stdout)
	if _, ok := got["policy"]; ok {
		if s, isStr := got["policy"].(string); isStr {
			t.Fatalf("JSON must not put YAML source in policy string field, got %q", s)
		}
	}
	if strings.Contains(stdout, "version: \"1.1\"") {
		t.Fatal("JSON looks like a raw YAML blob")
	}

	if jsonString(t, got, "schema_version") == "" {
		t.Fatal("schema_version empty")
	}
	if jsonString(t, got, "project_dir") != dir {
		t.Fatalf("project_dir = %q, want %q", got["project_dir"], dir)
	}

	digest := jsonString(t, got, "digest")
	if digest == "" {
		t.Fatal("digest empty")
	}
	parsed, err := policy.ParsePolicy(bytes.NewReader([]byte(policyShowCompiledYAML)))
	if err != nil {
		t.Fatalf("ParsePolicy fixture: %v", err)
	}
	wantDigest, err := policy.Digest(parsed)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if digest != wantDigest {
		t.Fatalf("digest = %q, want %q (same family as apply)", digest, wantDigest)
	}

	ingress := jsonMap(t, got["ingress"], "ingress")
	if !jsonBool(t, ingress, "enabled") {
		t.Fatal("ingress.enabled = false, want true")
	}
	rules, ok := ingress["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("ingress.rules = %v, want 1 rule", ingress["rules"])
	}
	rule := jsonMap(t, rules[0], "ingress.rules[0]")
	if jsonString(t, rule, "path") != "/webhook" {
		t.Fatalf("ingress path = %v", rule["path"])
	}
	if !jsonBool(t, rule, "enabled") {
		t.Fatal("ingress rule enabled = false")
	}

	egress := jsonMap(t, got["egress"], "egress")
	if !jsonBool(t, egress, "enabled") {
		t.Fatal("egress.enabled = false")
	}
	hosts, ok := egress["hosts"].([]any)
	if !ok || len(hosts) != 2 {
		t.Fatalf("egress.hosts = %v, want 2", egress["hosts"])
	}
	h0 := jsonMap(t, hosts[0], "egress.hosts[0]")
	h1 := jsonMap(t, hosts[1], "egress.hosts[1]")
	if jsonString(t, h0, "domain") != "api.openai.com" {
		t.Fatalf("egress host 0 domain = %v", h0["domain"])
	}
	if jsonString(t, h1, "cidr") != "10.0.0.0/8" {
		t.Fatalf("egress host 1 cidr = %v", h1["cidr"])
	}
	if !jsonBool(t, h0, "enabled") || !jsonBool(t, h1, "enabled") {
		t.Fatal("egress hosts must be enabled")
	}

	creds := jsonMap(t, got["credentials"], "credentials")
	if !jsonBool(t, creds, "enabled") {
		t.Fatal("credentials.enabled = false")
	}
	items, ok := creds["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("credentials.items = %v", creds["items"])
	}
	cred := jsonMap(t, items[0], "credentials.items[0]")
	if jsonString(t, cred, "id") != "openai-key" {
		t.Fatalf("credential id = %v", cred["id"])
	}
	if jsonString(t, cred, "type") != "header" {
		t.Fatalf("credential type = %v", cred["type"])
	}
	if jsonString(t, cred, "header") != "Authorization" {
		t.Fatalf("credential header = %v", cred["header"])
	}
	if _, exists := cred["value"]; exists {
		t.Fatalf("credential value must be omitted, got %v", cred["value"])
	}

	mcp := jsonMap(t, got["mcp_servers"], "mcp_servers")
	if !jsonBool(t, mcp, "enabled") {
		t.Fatal("mcp_servers.enabled = false")
	}
	mcpItems, ok := mcp["items"].([]any)
	if !ok || len(mcpItems) != 1 {
		t.Fatalf("mcp_servers.items = %v", mcp["items"])
	}
	server := jsonMap(t, mcpItems[0], "mcp_servers.items[0]")
	if jsonString(t, server, "name") != "code-exec" {
		t.Fatalf("mcp name = %v", server["name"])
	}
	allowed, _ := server["allowed_tools"].([]any)
	denied, _ := server["denied_tools"].([]any)
	if len(allowed) != 2 || len(denied) != 1 {
		t.Fatalf("mcp tools allowed=%v denied=%v", allowed, denied)
	}
	if _, exists := server["headers"]; exists {
		t.Fatalf("mcp headers must be omitted, got %v", server["headers"])
	}

	gr := jsonMap(t, got["guardrails"], "guardrails")
	if !jsonBool(t, gr, "enabled") {
		t.Fatal("guardrails.enabled = false")
	}
	pii := jsonMap(t, gr["pii"], "guardrails.pii")
	if jsonString(t, pii, "action") != "mask" {
		t.Fatalf("pii.action = %v", pii["action"])
	}
	builtins, _ := pii["builtins"].([]any)
	if len(builtins) != 1 || builtins[0] != "Email" {
		t.Fatalf("pii.builtins = %v", builtins)
	}

	budget := jsonMap(t, got["llm_budget"], "llm_budget")
	if !jsonBool(t, budget, "enabled") {
		t.Fatal("llm_budget.enabled = false")
	}
	limit := jsonMap(t, got["llm_rate_limit"], "llm_rate_limit")
	if !jsonBool(t, limit, "enabled") {
		t.Fatal("llm_rate_limit.enabled = false")
	}

	routes := jsonMap(t, got["model_routes"], "model_routes")
	if !jsonBool(t, routes, "enabled") {
		t.Fatal("model_routes.enabled = false")
	}
	routeMap := jsonMap(t, routes["routes"], "model_routes.routes")
	primary := jsonMap(t, routeMap["primary"], "model_routes.routes.primary")
	if jsonString(t, primary, "pattern") != "local-first" {
		t.Fatalf("model_routes.primary.pattern = %v (must be as declared)", primary["pattern"])
	}
}

func TestPolicyShow_TerminalNotRawYAML(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	yamlBytes, err := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	stdout, stderr, err := execPolicyShow(t, dir)
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	out := strings.TrimSpace(stdout)
	if out == strings.TrimSpace(string(yamlBytes)) {
		t.Fatal("terminal output is identical to YAML file contents")
	}
	for _, section := range []string{"Ingress", "Egress", "Credentials", "MCP", "Guardrails", "Digest"} {
		if !strings.Contains(stdout, section) {
			t.Fatalf("terminal missing section %q\n%s", section, stdout)
		}
	}
	if strings.Contains(stdout+stderr, policyShowCanary) {
		t.Fatal("canary leaked in terminal output")
	}
}

func TestPolicyShow_CredentialValueStripped(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowCompiledYAML)
	jsonOut, jsonErr, err := execPolicyShow(t, dir, "--json")
	if err != nil {
		t.Fatalf("json Execute() error = %v\n%s\n%s", err, jsonErr, jsonOut)
	}
	textOut, textErr, err := execPolicyShow(t, dir)
	if err != nil {
		t.Fatalf("text Execute() error = %v\n%s\n%s", err, textErr, textOut)
	}
	combined := jsonOut + jsonErr + textOut + textErr
	if strings.Contains(combined, policyShowCanary) {
		t.Fatal("planted credential value appeared in JSON or terminal")
	}
}

func TestPolicyShow_MissingPolicy(t *testing.T) {
	dir := t.TempDir()
	_, _, err := execPolicyShow(t, dir)
	if err == nil {
		t.Fatal("Execute() error = nil, want missing policy.yaml")
	}
	want := "policy.yaml not found in " + dir + "; run 'agentpaas policy init " + dir + "' to create one"
	if err.Error() != want {
		t.Fatalf("error = %q\nwant    %q", err.Error(), want)
	}
}

func TestPolicyShow_InvalidYAML(t *testing.T) {
	dir := writePolicyShowYAML(t, "version: [this is: not: valid: yaml:")
	stdout, stderr, err := execPolicyShow(t, dir)
	if err == nil {
		t.Fatalf("Execute() error = nil, want parse error\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestPolicyShow_RunIDStub(t *testing.T) {
	stdout, stderr, err := execPolicyShow(t, "run-abc123")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	want := "policy show by run-id is not yet implemented; use a project directory instead"
	if !strings.Contains(stdout, want) {
		t.Fatalf("stub message missing\nstdout: %s", stdout)
	}

	jsonOut, jsonErr, err := execPolicyShow(t, "run-abc123", "--json")
	if err != nil {
		t.Fatalf("json Execute() error = %v\n%s\n%s", err, jsonErr, jsonOut)
	}
	got := mustPolicyShowJSON(t, jsonOut)
	if jsonString(t, got, "run_id") != "run-abc123" {
		t.Fatalf("run_id = %v", got["run_id"])
	}
	if jsonString(t, got, "message") != want {
		t.Fatalf("message = %v", got["message"])
	}
}

func TestPolicyShow_EmptyMinimal(t *testing.T) {
	dir := writePolicyShowYAML(t, policyShowMinimalYAML)
	stdout, stderr, err := execPolicyShow(t, dir, "--json")
	if err != nil {
		t.Fatalf("Execute() error = %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
	}
	got := mustPolicyShowJSON(t, stdout)
	if jsonString(t, got, "digest") == "" {
		t.Fatal("digest empty on minimal policy")
	}
	for _, section := range []string{"ingress", "egress", "credentials", "mcp_servers"} {
		m := jsonMap(t, got[section], section)
		if jsonBool(t, m, "enabled") {
			t.Fatalf("%s.enabled = true on empty/minimal policy", section)
		}
	}
	if jsonBool(t, jsonMap(t, got["llm_budget"], "llm_budget"), "enabled") {
		t.Fatal("llm_budget.enabled = true on minimal policy")
	}
	if jsonBool(t, jsonMap(t, got["llm_rate_limit"], "llm_rate_limit"), "enabled") {
		t.Fatal("llm_rate_limit.enabled = true on minimal policy")
	}
	if jsonBool(t, jsonMap(t, got["guardrails"], "guardrails"), "enabled") {
		t.Fatal("guardrails.enabled = true on minimal policy")
	}
	if jsonBool(t, jsonMap(t, got["model_routes"], "model_routes"), "enabled") {
		t.Fatal("model_routes.enabled = true on minimal policy")
	}

	textOut, textErr, err := execPolicyShow(t, dir)
	if err != nil {
		t.Fatalf("text Execute() error = %v\n%s\n%s", err, textErr, textOut)
	}
	if strings.Contains(strings.ToLower(textOut+textErr), "panic") {
		t.Fatalf("panic in terminal output: %s", textOut+textErr)
	}
}
