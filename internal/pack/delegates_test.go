package pack

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadAgentYAML_ParsesDelegates(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, "agent.yaml", `name: weather-agent
version: 0.1.0
runtime: python3.12
entry: main.py
delegates:
  - phone_call
`)

	got, err := LoadAgentYAML(projectDir)
	if err != nil {
		t.Fatalf("LoadAgentYAML() error = %v", err)
	}
	if got == nil {
		t.Fatal("LoadAgentYAML() = nil, want value")
	}
	want := []string{"phone_call"}
	if !reflect.DeepEqual(got.Delegates, want) {
		t.Fatalf("Delegates = %#v, want %#v", got.Delegates, want)
	}
}

func TestAgentYAMLCanonicalMap_IncludesDelegatesWhenSet(t *testing.T) {
	src := []string{"phone_call", "sms"}
	ay := &AgentYAML{Name: "weather-agent", Delegates: src}
	m := agentYAMLCanonicalMap(ay)
	raw, ok := m["delegates"]
	if !ok {
		t.Fatal("canonical map missing delegates")
	}
	got, ok := raw.([]string)
	if !ok {
		t.Fatalf("delegates type %T, want []string", raw)
	}
	if !reflect.DeepEqual(got, src) {
		t.Fatalf("delegates = %#v, want %#v", got, src)
	}
	got[0] = "mutated"
	if ay.Delegates[0] != "phone_call" {
		t.Fatal("canonical map must copy delegates strings")
	}
}

func TestAgentYAMLCanonicalMap_OmitsEmptyDelegates(t *testing.T) {
	cases := []struct {
		name string
		ay   *AgentYAML
	}{
		{name: "nil", ay: &AgentYAML{Name: "old-agent"}},
		{name: "explicit nil slice", ay: &AgentYAML{Name: "old-agent", Delegates: nil}},
		{name: "empty slice", ay: &AgentYAML{Name: "old-agent", Delegates: []string{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := agentYAMLCanonicalMap(tc.ay)
			if _, ok := m["delegates"]; ok {
				t.Fatalf("delegates present for %s: %#v", tc.name, m["delegates"])
			}
		})
	}
}

func TestBuildComponentIndex_CopiesDelegates(t *testing.T) {
	agent := &AgentYAML{
		Name:      "weather-agent",
		Version:   "0.1.0",
		Delegates: []string{"phone_call"},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if idx == nil {
		t.Fatal("BuildComponentIndex returned nil")
	}
	want := []string{"phone_call"}
	if !reflect.DeepEqual(idx.Delegates, want) {
		t.Fatalf("Delegates = %#v, want %#v", idx.Delegates, want)
	}
	idx.Delegates[0] = "mutated"
	if agent.Delegates[0] != "phone_call" {
		t.Fatal("BuildComponentIndex must copy delegates strings")
	}
}

func TestBuildComponentIndex_OmitsEmptyDelegates(t *testing.T) {
	idx := BuildComponentIndex(&AgentYAML{Name: "old-agent"}, ComponentIndexProvenance{})
	if len(idx.Delegates) != 0 {
		t.Fatalf("Delegates = %#v, want empty", idx.Delegates)
	}
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(raw), `"delegates"`) {
		t.Fatalf("empty delegates must be omitted from card JSON: %s", raw)
	}
}

func TestWriteReadAgentLock_DelegatesRoundtrip(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: LockSchemaVersion,
		AgentName:     "weather-agent",
		AgentVersion:  "0.1.0",
		Runtime:       "python",
		AgentYAML: &AgentYAML{
			Name:      "weather-agent",
			Version:   "0.1.0",
			Runtime:   "python3.12",
			Entry:     "main.py",
			Delegates: []string{"phone_call"},
		},
	}
	lock.ComponentIndex = BuildComponentIndex(lock.AgentYAML, ComponentIndexProvenance{})

	path := filepath.Join(testSecureTempDir(t), "agent.lock")
	if err := WriteAgentLock(lock, path); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}
	got, err := ReadAgentLock(path)
	if err != nil {
		t.Fatalf("ReadAgentLock: %v", err)
	}
	if got.AgentYAML == nil {
		t.Fatal("AgentYAML is nil after roundtrip")
	}
	want := []string{"phone_call"}
	if !reflect.DeepEqual(got.AgentYAML.Delegates, want) {
		t.Fatalf("agent_yaml.delegates = %#v, want %#v", got.AgentYAML.Delegates, want)
	}
	if got.ComponentIndex == nil {
		t.Fatal("ComponentIndex is nil after roundtrip")
	}
	if !reflect.DeepEqual(got.ComponentIndex.Delegates, want) {
		t.Fatalf("component_index.delegates = %#v, want %#v", got.ComponentIndex.Delegates, want)
	}
}

func TestLockCanonical_OldLockWithoutDelegatesOmitsField(t *testing.T) {
	// A lock signed before delegates existed has neither field. After the
	// struct gained Delegates, omitempty must keep canonical JSON identical.
	lock := &AgentLock{
		SchemaVersion: LockSchemaVersion,
		AgentName:     "old-agent",
		AgentYAML:     &AgentYAML{Name: "old-agent", Version: "1.0.0"},
		ComponentIndex: &ComponentIndex{
			SchemaVersion: ComponentIndexSchemaV1,
			Kind:          "agent",
			Name:          "old-agent",
		},
	}

	ay := agentYAMLCanonicalMap(lock.AgentYAML)
	if _, ok := ay["delegates"]; ok {
		t.Fatal("agent_yaml canonical map must omit empty delegates")
	}
	ayJSON, err := json.Marshal(ay)
	if err != nil {
		t.Fatalf("json.Marshal agent_yaml: %v", err)
	}
	if strings.Contains(string(ayJSON), "delegates") {
		t.Fatalf("agent_yaml canonical JSON must not mention delegates: %s", ayJSON)
	}

	ci := componentIndexCanonicalMap(lock.ComponentIndex)
	if _, ok := ci["delegates"]; ok {
		t.Fatal("component_index canonical map must omit empty delegates")
	}
	ciJSON, err := json.Marshal(ci)
	if err != nil {
		t.Fatalf("json.Marshal component_index: %v", err)
	}
	if strings.Contains(string(ciJSON), "delegates") {
		t.Fatalf("component_index canonical JSON must not mention delegates: %s", ciJSON)
	}

	m := lockCanonicalMap(lock, true)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal lock canonical: %v", err)
	}
	if strings.Contains(string(raw), "delegates") {
		t.Fatalf("lock canonical JSON must not mention delegates: %s", raw)
	}
}
