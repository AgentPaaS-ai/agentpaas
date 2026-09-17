package pack

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func m163DemoDir(t *testing.T) string {
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

func TestM163CrewAIGoldenPacks(t *testing.T) {
	dir := m163DemoDir(t)
	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if agent == nil || agent.Name != "crewai-weather-agent" {
		t.Fatalf("agent = %#v", agent)
	}
	if resolveRuntime(agent.Runtime) != RuntimePython {
		t.Fatalf("runtime = %q, want python", agent.Runtime)
	}
	if agent.LLM.Provider == "" || agent.LLM.Credential == "" {
		t.Fatalf("llm gateway config missing: %#v", agent.LLM)
	}
	pol, err := LoadPolicy(dir)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if pol == nil {
		t.Fatal("policy.yaml missing")
	}
	digest, err := ComputeBuildInputDigest(dir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest: %v", err)
	}
	if digest == "" {
		t.Fatal("empty digest")
	}
	if err := ValidateCrewAITelemetry(dir, pol); err != nil {
		t.Fatalf("golden must pack: %v", err)
	}
	if err := ValidateCrewAITelemetry(dir, nil); err != nil {
		t.Fatalf("ValidateCrewAITelemetry(nil) on golden: %v", err)
	}
}

func TestM163CrewAIUsesLoopbackBaseURL(t *testing.T) {
	dir := m163DemoDir(t)
	src, err := os.ReadFile(filepath.Join(dir, "main.py"))
	if err != nil {
		t.Fatalf("read main.py: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "Crew(") {
		t.Fatal("main.py must construct crewai Crew(")
	}
	if !strings.Contains(body, "kickoff(") {
		t.Fatal("main.py must crew.kickoff() the one Crew (do not construct-and-discard)")
	}
	if strings.Contains(body, "_ = crew") {
		t.Fatal("main.py must not discard Crew")
	}
	if !strings.Contains(body, "from crewai import") {
		t.Fatal("main.py must import crewai")
	}
	if !strings.Contains(body, "Agent") || !strings.Contains(body, "Task") {
		t.Fatal("main.py must import Agent and Task")
	}
	if !strings.Contains(body, "OPENAI_BASE_URL") {
		t.Fatal("main.py must use harness OPENAI_BASE_URL")
	}
	if !strings.Contains(body, "base_url=") && !strings.Contains(body, "base_url =") {
		t.Fatal("main.py must pass base_url= from OPENAI_BASE_URL into the OpenAI provider, not discard the env")
	}
	if strings.Contains(body, `_ = os.environ["OPENAI_BASE_URL"]`) {
		t.Fatal("do not discard OPENAI_BASE_URL")
	}
	lower := strings.ToLower(body)
	for _, banned := range []string{"checkpointer", "langgraph"} {
		if strings.Contains(lower, banned) {
			t.Fatalf("main.py must not contain %s", banned)
		}
	}
	for _, host := range []string{
		"api.openai.com", "openrouter.ai", "api.anthropic.com",
		"telemetry.crewai.com", "app.posthog.com", "api.crewai.com",
	} {
		if strings.Contains(lower, host) {
			t.Fatalf("main.py must not hardcode provider host %s", host)
		}
	}
}

func TestM163CrewAIDummyKeyNoProviderSecret(t *testing.T) {
	dir := m163DemoDir(t)
	src, err := os.ReadFile(filepath.Join(dir, "main.py"))
	if err != nil {
		t.Fatalf("read main.py: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "OPENAI_API_KEY") {
		t.Fatal("main.py must read dummy OPENAI_API_KEY from env")
	}
	if strings.Contains(body, "sk-") {
		t.Fatal("main.py must not embed an sk- provider key")
	}
	req, err := os.ReadFile(filepath.Join(dir, "requirements.txt"))
	if err != nil {
		t.Fatalf("read requirements.txt: %v", err)
	}
	reqBody := strings.ToLower(string(req))
	if !strings.Contains(reqBody, "crewai==") {
		t.Fatal("requirements.txt must list pinned crewai==")
	}
	for _, banned := range []string{
		"agentpaas-sdk",
	} {
		if strings.Contains(reqBody, banned) {
			t.Fatalf("requirements.txt must not list %s", banned)
		}
	}
}

func TestM163CrewAIBypassDenied(t *testing.T) {
	dir := m163DemoDir(t)
	pol, err := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	if err != nil {
		t.Fatalf("read policy.yaml: %v", err)
	}
	body := strings.ToLower(string(pol))
	if !strings.Contains(body, "wttr.in") {
		t.Fatal("policy must allow wttr.in")
	}
	for _, host := range []string{
		"api.openai.com", "openrouter.ai", "api.anthropic.com", "openai.azure.com",
		"telemetry.crewai.com", "app.posthog.com", "telemetry.sentry.io", "api.crewai.com",
	} {
		if strings.Contains(body, host) {
			t.Fatalf("policy must not allow provider/telemetry host %s (SC3/SC5 default-deny)", host)
		}
	}
}

func TestM163PackRejectsCrewAITelemetryHost(t *testing.T) {
	hosts := []string{
		"telemetry.crewai.com",
		"app.posthog.com",
		"telemetry.sentry.io",
		"api.crewai.com",
	}
	for _, host := range hosts {
		pol := &policy.Policy{
			Egress: []policy.EgressRule{{Domain: host}},
		}
		err := ValidateCrewAITelemetry(t.TempDir(), pol)
		if err == nil {
			t.Fatalf("pack must reject %s in egress", host)
		}
		if !strings.Contains(err.Error(), "pack rejects CrewAI telemetry host in egress") {
			t.Fatalf("error = %v, want pack rejects CrewAI telemetry host in egress", err)
		}
	}
}

func TestM163RenderDockerfileDisablesCrewAITelemetry(t *testing.T) {
	cfg := BuildConfig{
		BaseImage:       "gcr.io/distroless/python3-debian12@sha256:deadbeef",
		SourceDateEpoch: time.Unix(0, 0),
		NonRootUID:      64000,
	}
	out := renderDockerfile(cfg, nil)
	if !strings.Contains(out, "ENV CREWAI_DISABLE_TELEMETRY=true") {
		t.Fatalf("renderDockerfile missing ENV CREWAI_DISABLE_TELEMETRY=true:\n%s", out)
	}
}
