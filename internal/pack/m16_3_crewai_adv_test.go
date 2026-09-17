package pack

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/llm"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const (
	advM163PackProviderSentinel = "ADV-SYNTHETIC-PACK-PROVIDER-KEY-M163"
	advM163PackOpenRouterSent   = "ADV-SYNTHETIC-PACK-OPENROUTER-KEY-M163"
	advM163PackCrewAISent       = "ADV-SYNTHETIC-PACK-CREWAI-KEY-M163"
)

func advM163PackDemoDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "crewai-weather-agent")
	if _, err := os.Stat(filepath.Join(dir, "agent.yaml")); err != nil {
		t.Fatalf("demo missing: %s: %v", dir, err)
	}
	return dir
}

func advM163Read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func advM163ContainsSentinel(s string, sentinels ...string) bool {
	for _, sent := range sentinels {
		if sent != "" && strings.Contains(s, sent) {
			return true
		}
	}
	return false
}

var advM163LiveKeyShape = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9_-]{8,}|sk-ant-|gsk_|xai-|or-[A-Za-z0-9]{16,}|phc_[A-Za-z0-9]{8,}|https://[^\s]*@)`)

func advM163ScanSecretShapes(t *testing.T, label, body string) {
	t.Helper()
	if advM163ContainsSentinel(body, advM163PackProviderSentinel, advM163PackOpenRouterSent, advM163PackCrewAISent) {
		t.Errorf("ADVERSARY BREAK: SC1 %s contains planted pack sentinel", label)
	}
	if loc := advM163LiveKeyShape.FindStringIndex(body); loc != nil {
		// ADVERSARY BREAK: live-key shape in golden artifact (do not print match)
		t.Errorf("ADVERSARY BREAK: SC1 %s contains live-key shape at offset=%d (value redacted)", label, loc[0])
	}
}

func advM163PackPolicyAllowsHost(pol *policy.Policy, host string) bool {
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

func advM163TelemetryHosts() []string {
	return []string{
		"telemetry.crewai.com",
		"app.posthog.com",
		"telemetry.sentry.io",
		"api.crewai.com",
	}
}

// TestADV_M16_3_SC1_CrewAIGoldenArtifactsOmitProviderSecrets scans the
// crewai golden pack inputs for embedded provider secrets.
func TestADV_M16_3_SC1_CrewAIGoldenArtifactsOmitProviderSecrets(t *testing.T) {
	dir := advM163PackDemoDir(t)
	for _, name := range []string{"main.py", "agent.yaml", "policy.yaml", "requirements.txt"} {
		body := advM163Read(t, dir, name)
		advM163ScanSecretShapes(t, name, body)
	}

	main := advM163Read(t, dir, "main.py")
	if !strings.Contains(main, "OPENAI_API_KEY") {
		t.Errorf("ADVERSARY BREAK: SC1 main.py does not read dummy OPENAI_API_KEY from env")
	}
	if strings.Contains(main, `os.environ.get("OPENAI_API_KEY"`) ||
		strings.Contains(main, `os.environ.get('OPENAI_API_KEY'`) {
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
	if advM163LiveKeyShape.MatchString(agent.LLM.Credential) {
		t.Errorf("ADVERSARY BREAK: SC1 agent.yaml llm.credential looks like a live key value (want a name)")
	}
}

// TestADV_M16_3_SC3_CrewAIPolicyDefaultDenyAndAutoDeclare asserts the
// packed policy cannot allow a direct provider host, and that pack-time
// auto-declare must not punch that deny. Worker TestM163CrewAIBypassDenied
// only greps policy.yaml text; it never calls ensureLLMProviderEgress.
func TestADV_M16_3_SC3_CrewAIPolicyDefaultDenyAndAutoDeclare(t *testing.T) {
	dir := advM163PackDemoDir(t)
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
		"telemetry.crewai.com",
		"app.posthog.com",
		"telemetry.sentry.io",
		"api.crewai.com",
	}
	for _, host := range forbidden {
		if advM163PackPolicyAllowsHost(pol, host) {
			t.Errorf("ADVERSARY BREAK: SC3 crewai policy allows provider/telemetry host %s (default-deny punched)", host)
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
				t.Errorf("ADVERSARY BREAK: SC3 ensureLLMProviderEgress stamped provider host onto crewai lock egress (punches default-deny)")
			}
		}
	}

	if err := ValidateLLMEgress(agent, pol); err == nil && domain != "" && !advM163PackPolicyAllowsHost(pol, domain) {
		// ADVERSARY BREAK: ValidateLLMEgress is a no-op; missing provider host is not a pack error
		t.Errorf("ADVERSARY BREAK: SC3 ValidateLLMEgress returned nil while policy omits provider domain (auto-declare / skip, not deny)")
	}
}

// TestADV_M16_3_SC3_CrewAIMainDoesNotHardcodeProviderHost
func TestADV_M16_3_SC3_CrewAIMainDoesNotHardcodeProviderHost(t *testing.T) {
	dir := advM163PackDemoDir(t)
	main := strings.ToLower(advM163Read(t, dir, "main.py"))
	for _, host := range []string{
		"api.openai.com",
		"openrouter.ai",
		"api.anthropic.com",
		"openai.azure.com",
		"generativelanguage.googleapis.com",
		"telemetry.crewai.com",
		"app.posthog.com",
		"api.crewai.com",
	} {
		if strings.Contains(main, host) {
			t.Errorf("ADVERSARY BREAK: SC3 main.py hardcodes provider/telemetry host")
		}
	}
	body := advM163Read(t, dir, "main.py")
	if !strings.Contains(body, "OPENAI_BASE_URL") {
		t.Errorf("ADVERSARY BREAK: SC3 main.py does not consume harness OPENAI_BASE_URL")
	}
	if !strings.Contains(body, "base_url=") && !strings.Contains(body, "base_url =") {
		t.Errorf("ADVERSARY BREAK: SC3 main.py does not pass base_url into CrewAI LLM() (SDK default host)")
	}
	if strings.Contains(body, `os.environ.get("OPENAI_BASE_URL"`) ||
		strings.Contains(body, `os.environ.get('OPENAI_BASE_URL'`) {
		t.Errorf("ADVERSARY BREAK: SC3 main.py uses environ.get for OPENAI_BASE_URL (empty/missing fall-opens LiteLLM default host)")
	}
}

// TestADV_M16_3_SC5_PackRejectsWildcardCIDRSchemePortAndHomoglyphTelemetry
// Worker TestM163PackRejectsCrewAITelemetryHost only covers exact Domain
// equality. SC5 is "telemetry domains are not in egress" — CIDR, scheme,
// port, wildcard parent, and IDN homoglyphs still punch default-deny.
func TestADV_M16_3_SC5_PackRejectsWildcardCIDRSchemePortAndHomoglyphTelemetry(t *testing.T) {
	cases := []struct {
		name string
		pol  *policy.Policy
	}{
		{"wildcard_parent", &policy.Policy{Egress: []policy.EgressRule{{Domain: "*.crewai.com"}}}},
		{"wildcard_posthog", &policy.Policy{Egress: []policy.EgressRule{{Domain: "*.posthog.com"}}}},
		{"scheme_https", &policy.Policy{Egress: []policy.EgressRule{{Domain: "https://telemetry.crewai.com"}}}},
		{"scheme_path", &policy.Policy{Egress: []policy.EgressRule{{Domain: "https://app.posthog.com/capture"}}}},
		{"port_4318", &policy.Policy{Egress: []policy.EgressRule{{Domain: "telemetry.crewai.com:4318"}}}},
		{"port_4317", &policy.Policy{Egress: []policy.EgressRule{{Domain: "telemetry.crewai.com:4317"}}}},
		{"port_443", &policy.Policy{Egress: []policy.EgressRule{{Domain: "app.posthog.com:443"}}}},
		{"cidr_bypass", &policy.Policy{Egress: []policy.EgressRule{{CIDR: "104.18.0.0/24"}}}},
		{"cyrillic_c", &policy.Policy{Egress: []policy.EgressRule{{Domain: "telemetry.\u0441rewai.com"}}}}, // Cyrillic с
		{"newline_host", &policy.Policy{Egress: []policy.EgressRule{{Domain: "telemetry.crewai.com\nwttr.in"}}}},
		{"null_host", &policy.Policy{Egress: []policy.EgressRule{{Domain: "telemetry.crewai.com\x00.example"}}}},
		{"us_i_posthog", &policy.Policy{Egress: []policy.EgressRule{{Domain: "us.i.posthog.com"}}}},
		{"eu_i_posthog", &policy.Policy{Egress: []policy.EgressRule{{Domain: "eu.i.posthog.com"}}}},
		{"sentry_ingest", &policy.Policy{Egress: []policy.EgressRule{{Domain: "o447951.ingest.sentry.io"}}}},
		{"crewai_bare", &policy.Policy{Egress: []policy.EgressRule{{Domain: "crewai.com"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCrewAITelemetry(t.TempDir(), tc.pol)
			if err == nil {
				// ADVERSARY BREAK: SC5 alias/CIDR/scheme/wildcard telemetry egress accepted
				t.Errorf("ADVERSARY BREAK: SC5 pack accepted telemetry egress alias %s (want reject)", tc.name)
			}
		})
	}
}

// TestADV_M16_3_SC5_NilPolicyAndProjectDirScanFailClosed
// ValidateCrewAITelemetry(nil) returns nil. projectDir is unused, so a
// golden that hardcodes telemetry.crewai.com in main.py still packs.
func TestADV_M16_3_SC5_NilPolicyAndProjectDirScanFailClosed(t *testing.T) {
	if err := ValidateCrewAITelemetry(t.TempDir(), nil); err == nil {
		// ADVERSARY BREAK: missing policy.yaml fail-opens telemetry
		t.Errorf("ADVERSARY BREAK: SC5 ValidateCrewAITelemetry(nil) returned nil (missing policy fail-opens telemetry egress)")
	}

	dir := t.TempDir()
	writeTestFile(t, dir, "main.py", "import urllib.request\nurllib.request.urlopen('https://telemetry.crewai.com/v1/traces')\n")
	writeTestFile(t, dir, "requirements.txt", "crewai==0.80.0\n")
	pol := &policy.Policy{Egress: []policy.EgressRule{{Domain: "wttr.in"}}}
	if err := ValidateCrewAITelemetry(dir, pol); err == nil {
		// ADVERSARY BREAK: projectDir unused; telemetry host in entrypoint not scanned
		t.Errorf("ADVERSARY BREAK: SC5 pack accepted entrypoint that dials telemetry.crewai.com (projectDir unused)")
	}

	dir2 := t.TempDir()
	writeTestFile(t, dir2, "requirements.txt", "crewai==0.80.0\nposthog==3.0.0\nsentry-sdk==2.0.0\n")
	if err := ValidateCrewAITelemetry(dir2, pol); err == nil {
		t.Errorf("ADVERSARY BREAK: SC5 pack accepted requirements.txt that pulls posthog/sentry-sdk (telemetry clients)")
	}
}

// TestADV_M16_3_SC5_DockerfileSetsDisableAndOTelSdkDisabled
// SC5 requires the disable flag at pack. Worker only greps
// CREWAI_DISABLE_TELEMETRY. CrewAI 0.80 OpenTelemetry still exports
// unless OTEL_SDK_DISABLED is also baked into the image.
func TestADV_M16_3_SC5_DockerfileSetsDisableAndOTelSdkDisabled(t *testing.T) {
	cfg := BuildConfig{
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:deadbeef",
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
	}
	out := renderDockerfile(cfg, nil)
	if !strings.Contains(out, "ENV CREWAI_DISABLE_TELEMETRY=true") {
		t.Errorf("ADVERSARY BREAK: SC5 renderDockerfile missing ENV CREWAI_DISABLE_TELEMETRY=true")
	}
	if !strings.Contains(out, "ENV OTEL_SDK_DISABLED=true") {
		// ADVERSARY BREAK: pack image env never disables the OTel SDK
		t.Errorf("ADVERSARY BREAK: SC5 renderDockerfile missing ENV OTEL_SDK_DISABLED=true (CREWAI_DISABLE_TELEMETRY alone does not stop OTel/PostHog)")
	}
	if strings.Contains(out, "CREWAI_DISABLE_TELEMETRY=false") {
		t.Errorf("ADVERSARY BREAK: SC5 renderDockerfile sets CREWAI_DISABLE_TELEMETRY=false")
	}
	lower := strings.ToLower(out)
	for _, host := range advM163TelemetryHosts() {
		if strings.Contains(lower, host) {
			t.Errorf("ADVERSARY BREAK: SC5 Dockerfile names telemetry host %s", host)
		}
	}
}

// TestADV_M16_3_SC6_CrewAIPackRecordsSameLLMEgressContractAsWeather
func TestADV_M16_3_SC6_CrewAIPackRecordsSameLLMEgressContractAsWeather(t *testing.T) {
	pdir := advM163PackDemoDir(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	wdir := filepath.Join(filepath.Dir(file), "..", "..", "demo", "weather-agent")

	pAgent, err := LoadAgentYAML(pdir)
	if err != nil {
		t.Fatalf("crewai LoadAgentYAML: %v", err)
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
		t.Errorf("ADVERSARY BREAK: SC6 crewai llm.provider=%q diverges from weather-agent analog", pAgent.LLM.Provider)
	}
	if pAgent.LLM.Model != wAgent.LLM.Model {
		t.Errorf("ADVERSARY BREAK: SC6 crewai llm.model diverges from weather-agent analog")
	}

	pPol, err := LoadPolicy(pdir)
	if err != nil {
		t.Fatalf("crewai LoadPolicy: %v", err)
	}
	wPol, err := LoadPolicy(wdir)
	if err != nil {
		t.Fatalf("weather LoadPolicy: %v", err)
	}
	if pPol == nil || wPol == nil {
		t.Fatal("missing policy.yaml")
	}
	if !advM163PackPolicyAllowsHost(pPol, "wttr.in") {
		t.Errorf("ADVERSARY BREAK: SC6 crewai policy missing wttr.in egress (weather analog has it)")
	}
	if advM163PackPolicyAllowsHost(pPol, "openrouter.ai") {
		t.Errorf("ADVERSARY BREAK: SC6 crewai policy copied weather analog's openrouter.ai allow (SC3 default-deny)")
	}
	_ = wPol
}

// TestADV_M16_3_IV_UnpinnedCrewAISupplyChain
func TestADV_M16_3_IV_UnpinnedCrewAISupplyChain(t *testing.T) {
	dir := advM163PackDemoDir(t)
	req := strings.TrimSpace(advM163Read(t, dir, "requirements.txt"))
	pinnedOpenAI := false
	pinnedCrewAI := false
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
		if strings.HasPrefix(lower, "crewai") && !strings.Contains(lower, "crewai-") {
			if strings.Contains(line, "==") || strings.Contains(line, "@") {
				pinnedCrewAI = true
			}
			if strings.Contains(lower, "[") {
				t.Errorf("ADVERSARY BREAK: IV crewai extras in requirements.txt (tools/telemetry extra pulls PostHog)")
			}
		}
	}
	if !pinnedOpenAI {
		t.Errorf("ADVERSARY BREAK: IV unpinned openai in requirements.txt (supply-chain / default host drift)")
	}
	if !pinnedCrewAI {
		t.Errorf("ADVERSARY BREAK: IV unpinned crewai in requirements.txt")
	}

	dir2 := t.TempDir()
	writeTestFile(t, dir2, "requirements.txt", "crewai[tools]==0.80.0\n")
	pol := &policy.Policy{Egress: []policy.EgressRule{{Domain: "wttr.in"}}}
	if err := ValidateCrewAITelemetry(dir2, pol); err == nil {
		t.Errorf("ADVERSARY BREAK: IV pack accepted crewai[tools] extra (telemetry/tools extra)")
	}
}

// TestADV_M16_3_IV_CredentialDeclaredButPolicyCredentialsEmpty
func TestADV_M16_3_IV_CredentialDeclaredButPolicyCredentialsEmpty(t *testing.T) {
	dir := advM163PackDemoDir(t)
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

// TestADV_M16_3_IV_SymlinkGoldenDirRejectedByPack
func TestADV_M16_3_IV_SymlinkGoldenDirRejectedByPack(t *testing.T) {
	dir := advM163PackDemoDir(t)
	tmp := symlinkSafeTempDir(t)
	link := filepath.Join(tmp, "crewai-weather-agent")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := LoadAgentYAML(link)
	if err == nil {
		t.Errorf("ADVERSARY BREAK: IV LoadAgentYAML followed symlink project dir (pack path traversal)")
	}
	if err := ValidateCrewAITelemetry(link, &policy.Policy{Egress: []policy.EgressRule{{Domain: "wttr.in"}}}); err == nil {
		// projectDir unused; symlink is not even inspected
		t.Errorf("ADVERSARY BREAK: IV ValidateCrewAITelemetry followed symlink project dir without rejecting it")
	}
}

// TestADV_M16_3_IV_InjectionUnicodeHomoglyphTelemetryHost
func TestADV_M16_3_IV_InjectionUnicodeHomoglyphTelemetryHost(t *testing.T) {
	hosts := []string{
		"telemetry.crewai.com\n",
		"telemetry.crewai.com\r",
		"telemetry.crewai.com\t",
		" telemetry.crewai.com",
		"telemetry.crewai.com ",
		"telemetry.crewai.com.",
		"TELEMETRY.CREWAI.COM",
	}
	for _, host := range hosts {
		pol := &policy.Policy{Egress: []policy.EgressRule{{Domain: host}}}
		err := ValidateCrewAITelemetry(t.TempDir(), pol)
		// trailing space/dot/case are normalized today; newline/tab must still reject.
		if strings.ContainsAny(host, "\n\r\t") && err == nil {
			t.Errorf("ADVERSARY BREAK: IV pack accepted injected-control telemetry domain")
		}
		_ = err
	}
}
