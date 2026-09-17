package pack

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const (
	advM162PackProviderSentinel = "ADV-SYNTHETIC-PACK-PROVIDER-KEY-M162"
	advM162PackOpenRouterSent   = "ADV-SYNTHETIC-PACK-OPENROUTER-KEY-M162"
)

func advM162PackDemoDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "langgraph-weather-agent")
	if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
		t.Fatalf("demo missing: %s: %v", dir, err)
	}
	return dir
}

func advM162Read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func advM162ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

var advM162LiveKeyShape = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9_-]{8,}|sk-ant-|gsk_|xai-|or-[A-Za-z0-9]{16,}|lsv2_[A-Za-z0-9_-]{8,})`)

func advM162ScanSecretShapes(t *testing.T, label, body string) {
	t.Helper()
	if advM162ContainsSentinel(body, advM162PackProviderSentinel, advM162PackOpenRouterSent) {
		t.Errorf("ADVERSARY BREAK: SC1 %s contains planted pack sentinel", label)
	}
	if loc := advM162LiveKeyShape.FindStringIndex(body); loc != nil {
		// ADVERSARY BREAK: live-key shape in golden artifact (do not print match)
		t.Errorf("ADVERSARY BREAK: SC1 %s contains live-key shape at offset=%d (value redacted)", label, loc[0])
	}
}

func advM162PackPolicyAllowsHost(pol *policy.Policy, host string) bool {
	if pol == nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(host))
	for _, rule := range pol.Egress {
		d := strings.ToLower(strings.TrimSpace(rule.Domain))
		if d == h {
			return true
		}
		if strings.HasPrefix(d, "*.") && strings.HasSuffix(h, d[1:]) {
			return true
		}
	}
	return false
}

// TestADV_M16_2_SC1_LangGraphGoldenArtifactsOmitProviderSecrets scans the
// langgraph golden pack inputs for embedded provider secrets.
func TestADV_M16_2_SC1_LangGraphGoldenArtifactsOmitProviderSecrets(t *testing.T) {
	dir := advM162PackDemoDir(t)
	for _, name := range []string{"main.py", "agent.yaml", "policy.yaml", "requirements.txt"} {
		body := advM162Read(t, dir, name)
		advM162ScanSecretShapes(t, name, body)
	}

	main := advM162Read(t, dir, "main.py")
	if !strings.Contains(main, "OPENAI_API_KEY") {
		t.Errorf("ADVERSARY BREAK: SC1 main.py does not read dummy OPENAI_API_KEY from env")
	}

	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if agent == nil {
		t.Fatal("LoadAgentYAML returned nil")
	}
	if agent.LLM.Credential == "" {
		t.Errorf("ADVERSARY BREAK: SC1 agent.yaml llm.credential empty (gateway has nothing to inject; SDK may use env secret)")
	}
	if advM162LiveKeyShape.MatchString(agent.LLM.Credential) {
		t.Errorf("ADVERSARY BREAK: SC1 agent.yaml llm.credential looks like a live key value (want a name)")
	}
}

// TestADV_M16_2_SC3_LangGraphPolicyDefaultDenyAndAutoDeclare asserts the
// packed policy cannot allow a direct provider host, and that pack-time
// auto-declare must not punch that deny. Worker TestM162LangGraphBypassDenied
// only greps policy.yaml text; it never calls ensureLLMProviderEgress.
func TestADV_M16_2_SC3_LangGraphPolicyDefaultDenyAndAutoDeclare(t *testing.T) {
	dir := advM162PackDemoDir(t)
	pol, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if pol == nil {
		t.Fatal("policy.yaml missing")
	}

	forbidden := []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"generativelanguage.googleapis.com",
		"api.x.ai",
		"api.smith.langchain.com",
		"api.langchain.com",
		"smith.langchain.com",
		"api.fireworks.ai",
		"api.together.xyz",
	}
	for _, host := range forbidden {
		if advM162PackPolicyAllowsHost(pol, host) {
			t.Errorf("ADVERSARY BREAK: SC3 langgraph policy allows provider/telemetry host %s (default-deny punched)", host)
		}
	}

	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	domain := llm.ProviderDomain(agent.LLM.Provider)
	if domain == "" {
		t.Errorf("ADVERSARY BREAK: SC3 golden llm.provider=%q has no ProviderDomain (cannot default-deny it)", agent.LLM.Provider)
	}
	ensureLLMProviderEgress(agent)
	for _, h := range agent.Egress {
		for _, host := range forbidden {
			if strings.EqualFold(h, host) && host == domain {
				// ADVERSARY BREAK: pack auto-declares the LLM provider host onto lock egress
				t.Errorf("ADVERSARY BREAK: SC3 ensureLLMProviderEgress stamped provider host onto langgraph lock egress (punches default-deny)")
			}
		}
	}

	if err := ValidateLLMEgress(agent, pol); err == nil && domain != "" && !advM162PackPolicyAllowsHost(pol, domain) {
		// ADVERSARY BREAK: ValidateLLMEgress is a no-op; missing provider host is not a pack error
		t.Errorf("ADVERSARY BREAK: SC3 ValidateLLMEgress returned nil while policy omits provider domain (auto-declare / skip, not deny)")
	}
}

// TestADV_M16_2_SC4_PackRejectsUnderscoreAndPostgresRedisAliases
// Worker TestM162PackRejects* only covers hyphenated langgraph-server /
// langgraph-checkpoint / langgraph-checkpoint-sqlite. SC4 is "image contains
// neither langgraph-server nor a checkpointer package" — underscore PEP 503
// names and postgres/redis extras are still checkpointers/servers.
func TestADV_M16_2_SC4_PackRejectsUnderscoreAndPostgresRedisAliases(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
		want string
	}{
		{"underscore_server", "requirements.txt", "langgraph==0.2.28\nlanggraph_server==0.2.0\n", "server"},
		{"underscore_api", "requirements.txt", "langgraph==0.2.28\nlanggraph_api==0.1.0\n", "api"},
		{"underscore_checkpoint", "requirements.txt", "langgraph==0.2.28\nlanggraph_checkpoint==1.0.0\n", "checkpoint"},
		{"postgres", "requirements.txt", "langgraph==0.2.28\nlanggraph-checkpoint-postgres==1.0.0\n", "checkpoint"},
		{"redis", "requirements.txt", "langgraph==0.2.28\nlanggraph-checkpoint-redis==1.0.0\n", "checkpoint"},
		{"aiosqlite", "requirements.txt", "langgraph==0.2.28\nlanggraph-checkpoint-aiosqlite==1.0.0\n", "checkpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, dir, tc.file, tc.body)
			err := ValidateLangGraphLibraryDeps(dir)
			if err == nil {
				// ADVERSARY BREAK: SC4 alias/extra checkpointer or server distribution accepted
				t.Errorf("ADVERSARY BREAK: SC4 pack accepted %s (want reject containing %s)", tc.name, tc.want)
				return
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Errorf("ADVERSARY BREAK: SC4 error for %s = %v, want %s", tc.name, err, tc.want)
			}
		})
	}
}

// TestADV_M16_2_SC4_PackRejectsPyprojectExtrasCliAndPoetry
// langgraph[server] / langgraph[checkpoint] extras and langgraph-cli pull
// server/checkpointer wheels. Worker only greps four filenames for literal
// package names; extras and cli are untested.
func TestADV_M16_2_SC4_PackRejectsPyprojectExtrasCliAndPoetry(t *testing.T) {
	cases := []struct {
		name string
		file string
		body string
	}{
		{"cli", "requirements.txt", "langgraph==0.2.28\nlanggraph-cli==0.1.0\n"},
		{"pyproject_server_extra", "pyproject.toml", "[project]\ndependencies = [\"langgraph[server]==0.2.28\"]\n"},
		{"pyproject_checkpoint_extra", "pyproject.toml", "[project]\ndependencies = [\"langgraph[checkpoint]==0.2.28\"]\n"},
		{"poetry_server", "poetry.lock", "name = \"langgraph-cli\"\nversion = \"0.1.0\"\n"},
		{"pipfile_server", "Pipfile", "langgraph-server = \"==0.2.0\"\n"},
		{"setup_py_checkpoint", "setup.py", "install_requires=['langgraph-checkpoint-postgres']\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, dir, tc.file, tc.body)
			err := ValidateLangGraphLibraryDeps(dir)
			if err == nil {
				// ADVERSARY BREAK: SC4 extras/cli/unscanned manifest accepted
				t.Errorf("ADVERSARY BREAK: SC4 pack accepted %s (langgraph-server/checkpointer extra or unscanned manifest)", tc.name)
			}
		})
	}
}

// TestADV_M16_2_SC4_PackRejectsCheckpointImportInEntrypoint
// Core langgraph ships langgraph.checkpoint / MemorySaver. SC4 is an image
// claim, not a requirements.txt substring. Worker never scans main.py.
func TestADV_M16_2_SC4_PackRejectsCheckpointImportInEntrypoint(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "requirements.txt", "langgraph==0.2.28\n")
	writeTestFile(t, dir, "main.py", "from langgraph.checkpoint.memory import MemorySaver\nfrom langgraph.graph import StateGraph\ng = StateGraph(dict)\ng.compile(checkpointer=MemorySaver())\n")
	err := ValidateLangGraphLibraryDeps(dir)
	if err == nil {
		// ADVERSARY BREAK: SC4 MemorySaver / langgraph.checkpoint in entrypoint not rejected
		t.Errorf("ADVERSARY BREAK: SC4 pack accepted entrypoint that imports langgraph.checkpoint.memory.MemorySaver (core checkpointer; D150)")
	}

	golden := advM162PackDemoDir(t)
	main := strings.ToLower(advM162Read(t, golden, "main.py"))
	for _, banned := range []string{
		"memorysaver",
		"inmemorysaver",
		"langgraph.checkpoint",
		"checkpointer=",
		"from langgraph.checkpoint",
	} {
		if strings.Contains(main, banned) {
			t.Errorf("ADVERSARY BREAK: SC4 golden main.py contains checkpointer surface %s", banned)
		}
	}
}

// TestADV_M16_2_SC6_LangGraphPackRecordsSameLLMEgressContractAsWeather
func TestADV_M16_2_SC6_LangGraphPackRecordsSameLLMEgressContractAsWeather(t *testing.T) {
	pdir := advM162PackDemoDir(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	wdir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "weather-agent")

	pAgent, err := LoadAgentYAML(pdir)
	if err != nil {
		t.Fatalf("langgraph LoadAgentYAML: %v", err)
	}
	wAgent, err := LoadAgentYAML(wdir)
	if err != nil {
		t.Fatalf("weather LoadAgentYAML: %v", err)
	}
	if pAgent == nil || wAgent == nil {
		t.Fatal("missing agent.yaml")
	}
	if pAgent.LLM.Provider == "" || wAgent.LLM.Provider == "" {
		t.Errorf("ADVERSARY BREAK: SC6 llm.provider missing on a weather-class golden")
	}
	if pAgent.LLM.Provider != wAgent.LLM.Provider {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph llm.provider=%q diverges from weather-agent analog", pAgent.LLM.Provider)
	}
	if pAgent.LLM.Model != wAgent.LLM.Model {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph llm.model diverges from weather-agent analog")
	}

	pPol, err := LoadPolicy(pdir)
	if err != nil {
		t.Fatalf("langgraph LoadPolicy: %v", err)
	}
	wPol, err := LoadPolicy(wdir)
	if err != nil {
		t.Fatalf("weather LoadPolicy: %v", err)
	}
	if pPol == nil || wPol == nil {
		t.Fatal("missing policy.yaml")
	}
	if !advM162PackPolicyAllowsHost(pPol, "wttr.in") {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph policy missing wttr.in egress (weather analog has it)")
	}
	if advM162PackPolicyAllowsHost(pPol, "openrouter.ai") {
		t.Errorf("ADVERSARY BREAK: SC6 langgraph policy copied weather analog's openrouter.ai allow (SC3 default-deny)")
	}
	_ = wPol
}

// TestADV_M16_2_IV_UnpinnedOpenAISupplyChain
func TestADV_M16_2_IV_UnpinnedOpenAISupplyChain(t *testing.T) {
	dir := advM162PackDemoDir(t)
	req := strings.TrimSpace(advM162Read(t, dir, "requirements.txt"))
	pinnedOpenAI := false
	pinnedLangGraph := false
	for _, line := range strings.Split(req, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "openai") {
			if strings.Contains(line, "==") || strings.Contains(line, "@") {
				pinnedOpenAI = true
			}
		}
		if strings.HasPrefix(lower, "langgraph") && !strings.Contains(lower, "langgraph-") {
			if strings.Contains(line, "==") || strings.Contains(line, "@") {
				pinnedLangGraph = true
			}
		}
	}
	if !pinnedOpenAI {
		t.Errorf("ADVERSARY BREAK: IV unpinned openai in requirements.txt (supply-chain / default host drift)")
	}
	if !pinnedLangGraph {
		t.Errorf("ADVERSARY BREAK: IV unpinned langgraph in requirements.txt")
	}
}

// TestADV_M16_2_IV_CredentialDeclaredButPolicyCredentialsEmpty
func TestADV_M16_2_IV_CredentialDeclaredButPolicyCredentialsEmpty(t *testing.T) {
	dir := advM162PackDemoDir(t)
	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	pol, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if agent.LLM.Credential != "" && pol != nil && len(pol.Credentials) == 0 {
		t.Errorf("ADVERSARY BREAK: IV agent.yaml llm.credential set but policy.yaml credentials: [] (brokered secret not policy-bound)")
	}
}

// TestADV_M16_2_IV_LangSmithTelemetryHostsNotInPolicy
func TestADV_M16_2_IV_LangSmithTelemetryHostsNotInPolicy(t *testing.T) {
	dir := advM162PackDemoDir(t)
	pol, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	for _, host := range []string{
		"api.smith.langchain.com",
		"api.langchain.com",
		"smith.langchain.com",
		"langchain.com",
	} {
		if advM162PackPolicyAllowsHost(pol, host) {
			t.Errorf("ADVERSARY BREAK: IV langsmith/langgraph-cloud telemetry host allowed in policy")
		}
	}
	main := strings.ToLower(advM162Read(t, dir, "main.py"))
	if strings.Contains(main, "langsmith") || strings.Contains(main, "langchain_callback") || strings.Contains(main, "tracing_v2") {
		t.Errorf("ADVERSARY BREAK: IV main.py enables langsmith/langchain tracing (exfil channel)")
	}
}
