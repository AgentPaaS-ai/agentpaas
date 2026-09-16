package pack

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func m162DemoDir(t *testing.T) string {
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

func TestM162LangGraphGoldenPacks(t *testing.T) {
	dir := m162DemoDir(t)
	agent, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if agent == nil || agent.Name != "langgraph-weather-agent" {
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
	if err := ValidateLangGraphLibraryDeps(dir); err != nil {
		t.Fatalf("golden must pack: %v", err)
	}
}

func TestM162LangGraphUsesLoopbackBaseURL(t *testing.T) {
	dir := m162DemoDir(t)
	src, err := os.ReadFile(filepath.Join(dir, "main.py"))
	if err != nil {
		t.Fatalf("read main.py: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "StateGraph(") {
		t.Fatal("main.py must construct langgraph StateGraph(...)")
	}
	if !strings.Contains(body, "from langgraph.graph import") || !strings.Contains(body, "StateGraph") {
		t.Fatal("main.py must import langgraph StateGraph")
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
	if !strings.Contains(body, "compile(") {
		t.Fatal("main.py must compile() the StateGraph")
	}
	if strings.Contains(body, "checkpointer") {
		t.Fatal("compile() must not pass a checkpointer")
	}
	lower := strings.ToLower(body)
	for _, banned := range []string{
		"langgraph.checkpoint",
		"langgraph_checkpoint",
		"langgraph_api",
		"langgraph.server",
	} {
		if strings.Contains(lower, banned) {
			t.Fatalf("main.py must not import %s", banned)
		}
	}
	for _, host := range []string{"api.openai.com", "openrouter.ai", "api.anthropic.com"} {
		if strings.Contains(lower, host) {
			t.Fatalf("main.py must not hardcode provider host %s", host)
		}
	}
}

func TestM162LangGraphDummyKeyNoProviderSecret(t *testing.T) {
	dir := m162DemoDir(t)
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
	if !strings.Contains(reqBody, "langgraph") {
		t.Fatal("requirements.txt must list langgraph")
	}
	for _, banned := range []string{
		"agentpaas-sdk",
		"langgraph-server",
		"langgraph-api",
		"langgraph-cli",
		"langgraph-checkpoint",
	} {
		if strings.Contains(reqBody, banned) {
			t.Fatalf("requirements.txt must not list %s", banned)
		}
	}
}

func TestM162LangGraphBypassDenied(t *testing.T) {
	dir := m162DemoDir(t)
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

func TestM162PackRejectsLanggraphServer(t *testing.T) {
	for _, dep := range []string{"langgraph-server==0.2.0", "langgraph-api==0.1.0", "langgraph.server"} {
		dir := t.TempDir()
		writeTestFile(t, dir, "requirements.txt", "langgraph==0.2.1\n"+dep+"\n")
		err := ValidateLangGraphLibraryDeps(dir)
		if err == nil {
			t.Fatalf("pack must reject %s", dep)
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "langgraph-server") &&
			!strings.Contains(lower, "langgraph-api") &&
			!strings.Contains(lower, "langgraph.server") {
			t.Fatalf("error = %v, want langgraph-server/api", err)
		}
	}
}

func TestM162PackRejectsCheckpointer(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
	}{
		{"requirements", "requirements.txt", "langgraph==0.2.1\nlanggraph-checkpoint==1.0.0\n"},
		{"sqlite", "requirements.txt", "langgraph==0.2.1\nlanggraph-checkpoint-sqlite==1.0.0\n"},
		{"uv.lock", "uv.lock", "[[package]]\nname = \"langgraph-checkpoint\"\nversion = \"1.0.0\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, dir, tc.file, tc.content)
			err := ValidateLangGraphLibraryDeps(dir)
			if err == nil {
				t.Fatal("pack must reject checkpointer package")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "checkpoint") {
				t.Fatalf("error = %v, want checkpoint", err)
			}
		})
	}
}
