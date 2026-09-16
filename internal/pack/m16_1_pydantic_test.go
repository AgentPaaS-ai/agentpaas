package pack

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func m161DemoDir(t *testing.T) string {
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

func TestM161PydanticGoldenPacks(t *testing.T) {
	dir := m161DemoDir(t)
	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if agent == nil || agent.Name != "pydantic-weather-agent" {
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
}

func TestM161PydanticUsesLoopbackBaseURL(t *testing.T) {
	dir := m161DemoDir(t)
	src, err := os.ReadFile(filepath.Join(dir, "main.py"))
	if err != nil {
		t.Fatalf("read main.py: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "Agent(") {
		t.Fatal("main.py must construct pydantic_ai Agent(...)")
	}
	if !strings.Contains(body, "from pydantic_ai import Agent") {
		t.Fatal("main.py must import pydantic_ai Agent")
	}
	if !strings.Contains(body, "OPENAI_BASE_URL") {
		t.Fatal("main.py must use harness OPENAI_BASE_URL")
	}
	if !strings.Contains(body, "base_url=") && !strings.Contains(body, "base_url =") {
		t.Fatal("main.py must pass base_url= from OPENAI_BASE_URL into the OpenAI provider, not discard the env")
	}
	if strings.Contains(body, "_ = os.environ[\"OPENAI_BASE_URL\"]") {
		t.Fatal("do not discard OPENAI_BASE_URL")
	}
	lower := strings.ToLower(body)
	for _, host := range []string{"api.openai.com", "openrouter.ai", "api.anthropic.com"} {
		if strings.Contains(lower, host) {
			t.Fatalf("main.py must not hardcode provider host %s", host)
		}
	}
}

func TestM161PydanticDummyKeyNoProviderSecret(t *testing.T) {
	dir := m161DemoDir(t)
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
	if !strings.Contains(strings.ToLower(string(req)), "pydantic-ai") {
		t.Fatal("requirements.txt must list pydantic-ai")
	}
}

func TestM161PydanticBypassDenied(t *testing.T) {
	dir := m161DemoDir(t)
	pol, err := os.ReadFile(filepath.Join(dir, "policy.yaml"))
	if err != nil {
		t.Fatalf("read policy.yaml: %v", err)
	}
	body := strings.ToLower(string(pol))
	if !strings.Contains(body, "wttr.in") {
		t.Fatal("policy must allow wttr.in")
	}
	for _, host := range []string{"api.openai.com", "openrouter.ai", "api.anthropic.com", "openai.azure.com"} {
		if strings.Contains(body, host) {
			t.Fatalf("policy must not allow provider host %s (SC3 default-deny)", host)
		}
	}
}
