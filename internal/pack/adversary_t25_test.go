package pack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func slice2DelegatesJSONHasKey(raw []byte, path ...string) bool {
	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		return false
	}
	cur := interface{}(top)
	for _, p := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return false
		}
		next, ok := m[p]
		if !ok {
			return false
		}
		cur = next
	}
	return true
}

func slice2WriteYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write agent.yaml: %v", err)
	}
	return dir
}

func TestAdversaryT25_YAMLEmptySequenceOmitsBothSites(t *testing.T) {
	dir := slice2WriteYAML(t, `name: old-agent
version: 1.0.0
runtime: python3.12
entry: main.py
delegates: []
`)
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if ay == nil {
		t.Fatal("LoadAgentYAML nil")
	}
	if len(ay.Delegates) != 0 {
		t.Fatalf("parsed Delegates = %#v, want empty", ay.Delegates)
	}
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["delegates"]; ok {
		// ADVERSARY BREAK: empty yaml sequence must omit canonical agent_yaml.delegates
		t.Fatalf("ADVERSARY BREAK: empty yaml sequence present in agent_yaml canonical: %#v", m["delegates"])
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	ci := componentIndexCanonicalMap(idx)
	if _, ok := ci["delegates"]; ok {
		// ADVERSARY BREAK: empty yaml sequence must omit canonical component_index.delegates
		t.Fatalf("ADVERSARY BREAK: empty yaml sequence present in component_index canonical: %#v", ci["delegates"])
	}
	lock := &AgentLock{
		SchemaVersion:  LockSchemaVersion,
		AgentName:      "old-agent",
		AgentYAML:      ay,
		ComponentIndex: idx,
	}
	raw, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if slice2DelegatesJSONHasKey(raw, "agent_yaml", "delegates") {
		t.Fatalf("ADVERSARY BREAK: lock agent_yaml.delegates key present for empty yaml sequence: %s", raw)
	}
	if slice2DelegatesJSONHasKey(raw, "component_index", "delegates") {
		t.Fatalf("ADVERSARY BREAK: lock component_index.delegates key present for empty yaml sequence: %s", raw)
	}
	if slice2DelegatesJSONHasKey(raw, "delegates") {
		t.Fatalf("ADVERSARY BREAK: top-level delegates key present: %s", raw)
	}
}

func TestAdversaryT25_YAMLNullOmitsBothSites(t *testing.T) {
	dir := slice2WriteYAML(t, `name: old-agent
version: 1.0.0
runtime: python3.12
entry: main.py
delegates: null
`)
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["delegates"]; ok {
		t.Fatalf("ADVERSARY BREAK: yaml null delegates present in agent_yaml canonical: %#v", m["delegates"])
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	ci := componentIndexCanonicalMap(idx)
	if _, ok := ci["delegates"]; ok {
		t.Fatalf("ADVERSARY BREAK: yaml null delegates present in component_index canonical: %#v", ci["delegates"])
	}
}

func TestAdversaryT25_AbsentEqualsEmptyCanonicalBytes(t *testing.T) {
	absent, err := LoadAgentYAML(slice2WriteYAML(t, `name: old-agent
version: 1.0.0
runtime: python3.12
entry: main.py
`))
	if err != nil {
		t.Fatalf("absent LoadAgentYAML: %v", err)
	}
	empty, err := LoadAgentYAML(slice2WriteYAML(t, `name: old-agent
version: 1.0.0
runtime: python3.12
entry: main.py
delegates: []
`))
	if err != nil {
		t.Fatalf("empty LoadAgentYAML: %v", err)
	}
	aJSON, err := json.Marshal(agentYAMLCanonicalMap(absent))
	if err != nil {
		t.Fatalf("marshal absent: %v", err)
	}
	eJSON, err := json.Marshal(agentYAMLCanonicalMap(empty))
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if !reflect.DeepEqual(aJSON, eJSON) {
		t.Fatalf("ADVERSARY BREAK: absent vs empty yaml sequence canonical bytes differ:\nabsent=%s\nempty=%s", aJSON, eJSON)
	}
	idxA := componentIndexCanonicalMap(BuildComponentIndex(absent, ComponentIndexProvenance{}))
	idxE := componentIndexCanonicalMap(BuildComponentIndex(empty, ComponentIndexProvenance{}))
	ja, err := json.Marshal(idxA)
	if err != nil {
		t.Fatalf("marshal idx absent: %v", err)
	}
	je, err := json.Marshal(idxE)
	if err != nil {
		t.Fatalf("marshal idx empty: %v", err)
	}
	if !reflect.DeepEqual(ja, je) {
		t.Fatalf("ADVERSARY BREAK: absent vs empty component_index canonical bytes differ:\nabsent=%s\nempty=%s", ja, je)
	}
}

func TestAdversaryT25_PreFieldDecodeToMapOmitsKey(t *testing.T) {
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
	raw, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if slice2DelegatesJSONHasKey(raw, "agent_yaml", "delegates") {
		t.Fatalf("ADVERSARY BREAK: pre-field agent_yaml has delegates key: %s", raw)
	}
	if slice2DelegatesJSONHasKey(raw, "component_index", "delegates") {
		t.Fatalf("ADVERSARY BREAK: pre-field component_index has delegates key: %s", raw)
	}
	if slice2DelegatesJSONHasKey(raw, "delegates") {
		t.Fatalf("ADVERSARY BREAK: pre-field top-level delegates key: %s", raw)
	}
}

func TestAdversaryT25_NonemptyIncludesBothNestedNotTopLevel(t *testing.T) {
	ay := &AgentYAML{Name: "weather-agent", Version: "0.1.0", Delegates: []string{"phone_call"}}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	lock := &AgentLock{
		SchemaVersion:  LockSchemaVersion,
		AgentName:      "weather-agent",
		AgentYAML:      ay,
		ComponentIndex: idx,
	}
	raw, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !slice2DelegatesJSONHasKey(raw, "agent_yaml", "delegates") {
		t.Fatalf("ADVERSARY BREAK: nonempty delegates missing from agent_yaml: %s", raw)
	}
	if !slice2DelegatesJSONHasKey(raw, "component_index", "delegates") {
		t.Fatalf("ADVERSARY BREAK: nonempty delegates missing from component_index: %s", raw)
	}
	if slice2DelegatesJSONHasKey(raw, "delegates") {
		t.Fatalf("ADVERSARY BREAK: delegates leaked to lock top-level: %s", raw)
	}
}

func TestAdversaryT25_StampCopiesNotAliasAndNoSharedBacking(t *testing.T) {
	src := []string{"phone_call"}
	ay := &AgentYAML{Name: "weather-agent", Version: "0.1.0", Delegates: src}
	lock := &AgentLock{AgentYAML: ay}
	stampComponentIndex(lock)
	if lock.ComponentIndex == nil {
		t.Fatal("stampComponentIndex left ComponentIndex nil")
	}
	if !reflect.DeepEqual(lock.ComponentIndex.Delegates, []string{"phone_call"}) {
		t.Fatalf("stamped card Delegates = %#v", lock.ComponentIndex.Delegates)
	}
	ay.Delegates[0] = "mutated-src"
	if lock.ComponentIndex.Delegates[0] != "phone_call" {
		t.Fatalf("ADVERSARY BREAK: stamp aliased AgentYAML.Delegates onto card: %#v", lock.ComponentIndex.Delegates)
	}
	lock.ComponentIndex.Delegates[0] = "mutated-card"
	if ay.Delegates[0] != "mutated-src" {
		t.Fatalf("ADVERSARY BREAK: card mutation wrote through to AgentYAML: %#v", ay.Delegates)
	}
}

func TestAdversaryT25_VerbatimNoTrimLowerDedupe(t *testing.T) {
	want := []string{" phone_call", "PHONE_CALL", "not_a_real_delegate", "phone_call"}
	dir := slice2WriteYAML(t, `name: weather-agent
version: 0.1.0
runtime: python3.12
entry: main.py
delegates:
  - " phone_call"
  - PHONE_CALL
  - not_a_real_delegate
  - phone_call
`)
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	if !reflect.DeepEqual(ay.Delegates, want) {
		t.Fatalf("ADVERSARY BREAK: LoadAgentYAML mutated delegates: got %#v want %#v", ay.Delegates, want)
	}
	m := agentYAMLCanonicalMap(ay)
	got, _ := m["delegates"].([]string)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ADVERSARY BREAK: canonical map mutated delegates: got %#v want %#v", got, want)
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	if !reflect.DeepEqual(idx.Delegates, want) {
		t.Fatalf("ADVERSARY BREAK: BuildComponentIndex mutated delegates: got %#v want %#v", idx.Delegates, want)
	}
}

func TestAdversaryT25_OverlayCannotInjectDelegates(t *testing.T) {
	ay := &AgentYAML{
		Name: "weather-agent",
		ComponentIndex: map[string]interface{}{
			"delegates": []interface{}{"phone_call", "sms"},
		},
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	if len(idx.Delegates) != 0 {
		t.Fatalf("ADVERSARY BREAK: overlay injected delegates onto card: %#v", idx.Delegates)
	}
	raw, err := json.Marshal(componentIndexCanonicalMap(idx))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if slice2DelegatesJSONHasKey(raw, "delegates") {
		t.Fatalf("ADVERSARY BREAK: overlay-only delegates key on card JSON: %s", raw)
	}
}

func TestAdversaryT25_CapabilitiesRemainDistinct(t *testing.T) {
	ay := &AgentYAML{
		Name: "weather-agent",
		Capabilities: []DeclaredCapability{
			{ID: "phone_call", Description: "call"},
		},
	}
	m := agentYAMLCanonicalMap(ay)
	if _, ok := m["delegates"]; ok {
		t.Fatalf("ADVERSARY BREAK: capabilities leaked into delegates: %#v", m["delegates"])
	}
	if _, ok := m["capabilities"]; !ok {
		t.Fatal("capabilities missing from canonical map")
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	if len(idx.Delegates) != 0 {
		t.Fatalf("ADVERSARY BREAK: capabilities inferred onto card.delegates: %#v", idx.Delegates)
	}
}

func TestAdversaryT25_DemoWeatherAgentStampsPhoneCall(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	demo := filepath.Clean(filepath.Join(wd, "..", "..", "demo", "weather-agent"))
	ay, err := LoadAgentYAML(demo)
	if err != nil {
		t.Fatalf("LoadAgentYAML demo %s: %v", demo, err)
	}
	if ay == nil {
		t.Fatal("demo agent.yaml missing")
	}
	want := []string{"phone_call"}
	if !reflect.DeepEqual(ay.Delegates, want) {
		t.Fatalf("ADVERSARY BREAK: demo delegates = %#v, want %#v", ay.Delegates, want)
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	if !reflect.DeepEqual(idx.Delegates, want) {
		t.Fatalf("ADVERSARY BREAK: demo card delegates = %#v, want %#v", idx.Delegates, want)
	}
}

func TestAdversaryT25_FlowStyleParsesVerbatim(t *testing.T) {
	dir := slice2WriteYAML(t, `name: weather-agent
version: 0.1.0
runtime: python3.12
entry: main.py
delegates: [phone_call, sms]
`)
	ay, err := LoadAgentYAML(dir)
	if err != nil {
		t.Fatalf("LoadAgentYAML: %v", err)
	}
	want := []string{"phone_call", "sms"}
	if !reflect.DeepEqual(ay.Delegates, want) {
		t.Fatalf("ADVERSARY BREAK: flow-style delegates = %#v, want %#v", ay.Delegates, want)
	}
}

func TestAdversaryT25_NonStringYAMLRejected(t *testing.T) {
	dir := slice2WriteYAML(t, `name: weather-agent
version: 0.1.0
runtime: python3.12
entry: main.py
delegates: phone_call
`)
	ay, err := LoadAgentYAML(dir)
	if err == nil {
		if ay != nil && len(ay.Delegates) > 0 {
			t.Fatalf("ADVERSARY BREAK: scalar delegates coerced into list: %#v", ay.Delegates)
		}
		t.Fatal("ADVERSARY BREAK: scalar delegates parsed without error")
	}
}

func TestAdversaryT25_WriteReadEmptyStillOmits(t *testing.T) {
	lock := &AgentLock{
		SchemaVersion: LockSchemaVersion,
		AgentName:     "old-agent",
		AgentVersion:  "1.0.0",
		Runtime:       "python",
		AgentYAML:     &AgentYAML{Name: "old-agent", Version: "1.0.0", Delegates: []string{}},
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
	if got.AgentYAML != nil && len(got.AgentYAML.Delegates) != 0 {
		t.Fatalf("ADVERSARY BREAK: empty slice roundtripped as %#v", got.AgentYAML.Delegates)
	}
	raw, err := json.Marshal(lockCanonicalMap(got, true))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if slice2DelegatesJSONHasKey(raw, "agent_yaml", "delegates") || slice2DelegatesJSONHasKey(raw, "component_index", "delegates") {
		t.Fatalf("ADVERSARY BREAK: second marshal reintroduced delegates key: %s", raw)
	}
}

func TestAdversaryT25_NewlineAndNullByteStampedVerbatim(t *testing.T) {
	want := []string{"phone\ncall", "phone\x00call"}
	ay := &AgentYAML{Name: "weather-agent", Delegates: append([]string{}, want...)}
	m := agentYAMLCanonicalMap(ay)
	got, _ := m["delegates"].([]string)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical mutated injection tokens: got %#v want %#v", got, want)
	}
	idx := BuildComponentIndex(ay, ComponentIndexProvenance{})
	if !reflect.DeepEqual(idx.Delegates, want) {
		t.Fatalf("card mutated injection tokens: got %#v want %#v", idx.Delegates, want)
	}
}
