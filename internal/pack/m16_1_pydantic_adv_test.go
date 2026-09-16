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
	advM161PackProviderSentinel = "ADV-SYNTHETIC-PACK-PROVIDER-KEY-M161"
	advM161PackOpenRouterSent   = "ADV-SYNTHETIC-PACK-OPENROUTER-KEY-M161"
)

func advM161PackDemoDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "pydantic-weather-agent")
	if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
		t.Fatalf("demo missing: %s: %v", dir, err)
	}
	return dir
}

func advM161Read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func advM161ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

var advM161LiveKeyShape = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9_-]{8,}|sk-ant-|gsk_|xai-|or-[A-Za-z0-9]{16,})`)

func advM161ScanSecretShapes(t *testing.T, label, body string) {
	t.Helper()
	if advM161ContainsSentinel(body, advM161PackProviderSentinel, advM161PackOpenRouterSent) {
		t.Errorf("ADVERSARY BREAK: SC1 %s contains planted pack sentinel", label)
	}
	if loc := advM161LiveKeyShape.FindStringIndex(body); loc != nil {
		// ADVERSARY BREAK: live-key shape in golden artifact (do not print match)
		t.Errorf("ADVERSARY BREAK: SC1 %s contains live-key shape at offset=%d (value redacted)", label, loc[0])
	}
}

// TestADV_M16_1_SC1_PydanticGoldenArtifactsOmitProviderSecrets scans the
// pydantic golden pack inputs for embedded provider secrets.
func TestADV_M16_1_SC1_PydanticGoldenArtifactsOmitProviderSecrets(t *testing.T) {
	dir := advM161PackDemoDir(t)
	for _, name := range []string{"main.py", "agent.yaml", "policy.yaml", "requirements.txt"} {
		body := advM161Read(t, dir, name)
		advM161ScanSecretShapes(t, name, body)
	}

	main := advM161Read(t, dir, "main.py")
	if !strings.Contains(main, "OPENAI_API_KEY") {
		t.Errorf("ADVERSARY BREAK: SC1 main.py does not read dummy OPENAI_API_KEY from env")
	}
	if strings.Contains(main, "os.environ.get(\"OPENAI_API_KEY\"") &&
		!strings.Contains(main, "os.environ[\"OPENAI_API_KEY\"]") {
		// get() with a default can bake a fallback secret; fail if a default is a key shape
		t.Errorf("ADVERSARY BREAK: SC1 main.py uses OPENAI_API_KEY get() fallback (may hide a default secret)")
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
	if advM161LiveKeyShape.MatchString(agent.LLM.Credential) {
		t.Errorf("ADVERSARY BREAK: SC1 agent.yaml llm.credential looks like a live key value (want a name)")
	}
}

// TestADV_M16_1_SC3_PydanticPolicyDefaultDenyProviderHosts asserts the
// packed policy cannot allow a direct provider host, and that pack-time
// auto-declare must not punch that deny.
func TestADV_M16_1_SC3_PydanticPolicyDefaultDenyProviderHosts(t *testing.T) {
	dir := advM161PackDemoDir(t)
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
		"logfire.pydantic.dev",
		"logfire-api.pydantic.dev",
	}
	for _, host := range forbidden {
		if packPolicyAllowsHost(pol, host) {
			t.Errorf("ADVERSARY BREAK: SC3 pydantic policy allows provider/telemetry host %s (default-deny punched)", host)
		}
	}

	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	before := append([]string(nil), agent.Egress...)
	ensureLLMProviderEgress(agent)
	domain := llm.ProviderDomain(agent.LLM.Provider)
	if domain == "" {
		t.Errorf("ADVERSARY BREAK: SC3 golden llm.provider=%q has no ProviderDomain (cannot default-deny it)", agent.LLM.Provider)
	}
	got := false
	for _, h := range agent.Egress {
		if strings.EqualFold(h, domain) {
			got = true
		}
		for _, host := range forbidden {
			if strings.EqualFold(h, host) && host == domain {
				// ADVERSARY BREAK: pack auto-declares the LLM provider host onto lock egress
				t.Errorf("ADVERSARY BREAK: SC3 ensureLLMProviderEgress stamped provider host onto pydantic lock egress (punches default-deny)")
			}
		}
	}
	if got {
		t.Errorf("ADVERSARY BREAK: SC3 pack auto-declared provider domain onto AgentYAML.Egress (policy.yaml only allows wttr.in)")
	}
	_ = before

	if err := ValidateLLMEgress(agent, pol); err == nil && domain != "" && !packPolicyAllowsHost(pol, domain) {
		// ADVERSARY BREAK: ValidateLLMEgress is a no-op; missing provider host is not a pack error
		t.Errorf("ADVERSARY BREAK: SC3 ValidateLLMEgress returned nil while policy omits provider domain (auto-declare / skip, not deny)")
	}
}

func packPolicyAllowsHost(pol *policy.Policy, host string) bool {
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

// TestADV_M16_1_SC3_PydanticMainDoesNotHardcodeProviderHost
func TestADV_M16_1_SC3_PydanticMainDoesNotHardcodeProviderHost(t *testing.T) {
	dir := advM161PackDemoDir(t)
	main := strings.ToLower(advM161Read(t, dir, "main.py"))
	for _, host := range []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"generativelanguage.googleapis.com",
	} {
		if strings.Contains(main, host) {
			t.Errorf("ADVERSARY BREAK: SC3 main.py hardcodes provider host")
		}
	}
	if !strings.Contains(main, "openai_base_url") && !strings.Contains(advM161Read(t, dir, "main.py"), "OPENAI_BASE_URL") {
		t.Errorf("ADVERSARY BREAK: SC3 main.py does not consume harness OPENAI_BASE_URL")
	}
	body := advM161Read(t, dir, "main.py")
	if !strings.Contains(body, "base_url=") && !strings.Contains(body, "base_url =") {
		t.Errorf("ADVERSARY BREAK: SC3 main.py does not pass base_url into OpenAIProvider (SDK default host)")
	}
}

// TestADV_M16_1_SC3_EmptyBaseURLFallOpenInPackedEntrypoint
func TestADV_M16_1_SC3_EmptyBaseURLFallOpenInPackedEntrypoint(t *testing.T) {
	dir := advM161PackDemoDir(t)
	body := advM161Read(t, dir, "main.py")
	// os.environ["OPENAI_BASE_URL"] raises if unset — that is fail-closed.
	// os.environ.get("OPENAI_BASE_URL") with omitted/empty default fall-opens
	// pydantic OpenAIProvider to api.openai.com.
	if strings.Contains(body, "os.environ.get(\"OPENAI_BASE_URL\"") ||
		strings.Contains(body, "os.environ.get('OPENAI_BASE_URL'") {
		t.Errorf("ADVERSARY BREAK: SC3 main.py uses environ.get for OPENAI_BASE_URL (empty/missing fall-opens SDK default host)")
	}
	if strings.Contains(body, "OpenAIProvider()") && !strings.Contains(body, "base_url") {
		t.Errorf("ADVERSARY BREAK: SC3 OpenAIProvider() constructed without base_url")
	}
}

// TestADV_M16_1_SC6_PydanticPackRecordsSameLLMEgressContractAsWeather
func TestADV_M16_1_SC6_PydanticPackRecordsSameLLMEgressContractAsWeather(t *testing.T) {
	pdir := advM161PackDemoDir(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	wdir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "weather-agent")

	pAgent, err := LoadAgentYAML(pdir)
	if err != nil {
		t.Fatalf("pydantic LoadAgentYAML: %v", err)
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
		t.Errorf("ADVERSARY BREAK: SC6 pydantic llm.provider=%q diverges from weather-agent analog", pAgent.LLM.Provider)
	}
	if pAgent.LLM.Model != wAgent.LLM.Model {
		t.Errorf("ADVERSARY BREAK: SC6 pydantic llm.model diverges from weather-agent analog")
	}

	pPol, err := LoadPolicy(pdir)
	if err != nil {
		t.Fatalf("pydantic LoadPolicy: %v", err)
	}
	wPol, err := LoadPolicy(wdir)
	if err != nil {
		t.Fatalf("weather LoadPolicy: %v", err)
	}
	if pPol == nil || wPol == nil {
		t.Fatal("missing policy.yaml")
	}
	if !packPolicyAllowsHost(pPol, "wttr.in") {
		t.Errorf("ADVERSARY BREAK: SC6 pydantic policy missing wttr.in egress (weather analog has it)")
	}
	// Weather analog still allows openrouter.ai; pydantic golden must NOT copy that hole.
	if packPolicyAllowsHost(pPol, "openrouter.ai") {
		t.Errorf("ADVERSARY BREAK: SC6 pydantic policy copied weather analog's openrouter.ai allow (SC3 default-deny)")
	}
}

// TestADV_M16_1_IV_UnpinnedPydanticAISupplyChain
func TestADV_M16_1_IV_UnpinnedPydanticAISupplyChain(t *testing.T) {
	dir := advM161PackDemoDir(t)
	req := strings.TrimSpace(advM161Read(t, dir, "requirements.txt"))
	lines := []string{}
	for _, line := range strings.Split(req, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	pinned := false
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "pydantic-ai") {
			if strings.Contains(line, "==") || strings.Contains(line, "@") {
				pinned = true
			}
		}
	}
	if !pinned {
		t.Errorf("ADVERSARY BREAK: IV unpinned pydantic-ai in requirements.txt (supply-chain / telemetry host drift)")
	}
}

// TestADV_M16_1_IV_LogfireTelemetryHostsNotInPolicy
func TestADV_M16_1_IV_LogfireTelemetryHostsNotInPolicy(t *testing.T) {
	dir := advM161PackDemoDir(t)
	pol, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	for _, host := range []string{
		"logfire.pydantic.dev",
		"logfire-api.pydantic.dev",
		"pydantic.dev",
		"api.pydantic.ai",
	} {
		if packPolicyAllowsHost(pol, host) {
			t.Errorf("ADVERSARY BREAK: IV pydantic telemetry host allowed in policy")
		}
	}
	main := strings.ToLower(advM161Read(t, dir, "main.py"))
	if strings.Contains(main, "logfire") || strings.Contains(main, "instrument") {
		t.Errorf("ADVERSARY BREAK: IV main.py enables logfire/instrumentation (exfil channel)")
	}
}

// TestADV_M16_1_IV_CredentialDeclaredButPolicyCredentialsEmpty
func TestADV_M16_1_IV_CredentialDeclaredButPolicyCredentialsEmpty(t *testing.T) {
	dir := advM161PackDemoDir(t)
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

// TestADV_M16_1_IV_SymlinkGoldenDirRejectedByPack
func TestADV_M16_1_IV_SymlinkGoldenDirRejectedByPack(t *testing.T) {
	dir := advM161PackDemoDir(t)
	tmp := symlinkSafeTempDir(t)
	link := filepath.Join(tmp, "pydantic-weather-agent")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := LoadAgentYAML(link)
	if err == nil {
		t.Errorf("ADVERSARY BREAK: IV LoadAgentYAML followed symlink project dir (pack path traversal)")
	}
}
