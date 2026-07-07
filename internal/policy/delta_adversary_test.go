package policy

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const adversaryAgentBlock = `version: "1.0"
agent:
  name: test-agent
`

// TestAdversary_DeltaDomainCaseNoDelta: API.SLACK.COM vs api.slack.com → nil delta.
func TestAdversary_DeltaDomainCaseNoDelta(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "API.SLACK.COM"
    ports: [443]
`
	child := adversaryAgentBlock + `egress:
  - domain: "api.slack.com"
    ports: [443]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("BREAK: case-only domain change produced delta %+v", got)
	}
}

// TestAdversary_DeltaPunycodeIDNNoDelta: Unicode IDN vs punycode label → nil delta.
func TestAdversary_DeltaPunycodeIDNNoDelta(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "münchen.de"
    ports: [443]
`
	child := adversaryAgentBlock + `egress:
  - domain: "xn--mnchen-3ya.de"
    ports: [443]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("BREAK: punycode-equivalent domains produced delta %+v", got)
	}
}

// TestAdversary_DeltaTrailingDotNoDelta: FQDN trailing dot stripped → nil delta.
func TestAdversary_DeltaTrailingDotNoDelta(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "api.slack.com."
    ports: [443]
`
	child := adversaryAgentBlock + `egress:
  - domain: "api.slack.com"
    ports: [443]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("BREAK: trailing-dot domain produced delta %+v", got)
	}
}

func marshalDeltaJSON(t *testing.T, d *PolicyDelta) string {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

// TestAdversary_DeltaEgressShuffleByteIdentical: list order must not affect delta JSON.
func TestAdversary_DeltaEgressShuffleByteIdentical(t *testing.T) {
	base := adversaryAgentBlock
	parent := base + `egress:
  - domain: "a.example.com"
    ports: [443]
  - domain: "b.example.com"
    ports: [80]
`
	orderA := base + `egress:
  - domain: "a.example.com"
    ports: [443]
  - domain: "b.example.com"
    ports: [80]
  - domain: "c.example.com"
    ports: [443]
`
	orderB := base + `egress:
  - domain: "c.example.com"
    ports: [443]
  - domain: "b.example.com"
    ports: [80]
  - domain: "a.example.com"
    ports: [443]
`
	d1, err := ComputeDelta([]byte(parent), []byte(orderA))
	if err != nil {
		t.Fatalf("orderA: %v", err)
	}
	d2, err := ComputeDelta([]byte(parent), []byte(orderB))
	if err != nil {
		t.Fatalf("orderB: %v", err)
	}
	j1, j2 := marshalDeltaJSON(t, d1), marshalDeltaJSON(t, d2)
	if j1 != j2 {
		t.Fatalf("BREAK: shuffled egress YAML produced different delta JSON:\n%s\nvs\n%s", j1, j2)
	}
}

// TestAdversary_DeltaCredentialsShuffleIdentical
func TestAdversary_DeltaCredentialsShuffleIdentical(t *testing.T) {
	parent := adversaryAgentBlock + `credentials:
  - id: cred-b
    type: header
    header: X-B
    value: b
`
	childA := adversaryAgentBlock + `credentials:
  - id: cred-a
    type: header
    header: X-A
    value: a
  - id: cred-b
    type: header
    header: X-B
    value: b
`
	childB := adversaryAgentBlock + `credentials:
  - id: cred-b
    type: header
    header: X-B
    value: b
  - id: cred-a
    type: header
    header: X-A
    value: a
`
	d1, _ := ComputeDelta([]byte(parent), []byte(childA))
	d2, _ := ComputeDelta([]byte(parent), []byte(childB))
	if marshalDeltaJSON(t, d1) != marshalDeltaJSON(t, d2) {
		t.Fatal("BREAK: shuffled credentials changed delta JSON")
	}
}

// TestAdversary_DeltaMCPShuffleIdentical
func TestAdversary_DeltaMCPShuffleIdentical(t *testing.T) {
	parent := adversaryAgentBlock + `mcp_servers:
  - name: server-b
    url: "https://b.example.com"
`
	childA := adversaryAgentBlock + `mcp_servers:
  - name: server-a
    url: "https://a.example.com"
  - name: server-b
    url: "https://b.example.com"
`
	childB := adversaryAgentBlock + `mcp_servers:
  - name: server-b
    url: "https://b.example.com"
  - name: server-a
    url: "https://a.example.com"
`
	d1, _ := ComputeDelta([]byte(parent), []byte(childA))
	d2, _ := ComputeDelta([]byte(parent), []byte(childB))
	if marshalDeltaJSON(t, d1) != marshalDeltaJSON(t, d2) {
		t.Fatal("BREAK: shuffled MCP list changed delta JSON")
	}
}

// TestAdversary_DeltaHooksShuffleIdentical
func TestAdversary_DeltaHooksShuffleIdentical(t *testing.T) {
	parent := adversaryAgentBlock
	childA := adversaryAgentBlock + `hooks:
  - name: hook-a
    url: "https://a.example.com/h"
  - name: hook-b
    url: "https://b.example.com/h"
`
	childB := adversaryAgentBlock + `hooks:
  - name: hook-b
    url: "https://b.example.com/h"
  - name: hook-a
    url: "https://a.example.com/h"
`
	d1, _ := ComputeDelta([]byte(parent), []byte(childA))
	d2, _ := ComputeDelta([]byte(parent), []byte(childB))
	if marshalDeltaJSON(t, d1) != marshalDeltaJSON(t, d2) {
		t.Fatal("BREAK: shuffled hooks changed delta JSON")
	}
}

// TestAdversary_DeltaIngressShuffleIdentical
func TestAdversary_DeltaIngressShuffleIdentical(t *testing.T) {
	parent := adversaryAgentBlock + `ingress:
  - path: /a
    port: 8080
`
	childA := adversaryAgentBlock + `ingress:
  - path: /a
    port: 8080
  - path: /b
    port: 9090
`
	childB := adversaryAgentBlock + `ingress:
  - path: /b
    port: 9090
  - path: /a
    port: 8080
`
	d1, _ := ComputeDelta([]byte(parent), []byte(childA))
	d2, _ := ComputeDelta([]byte(parent), []byte(childB))
	if marshalDeltaJSON(t, d1) != marshalDeltaJSON(t, d2) {
		t.Fatal("BREAK: shuffled ingress changed delta JSON")
	}
}

// TestAdversary_DeltaThreeRulesReversePlusExtra: deterministic added when child reorders + adds.
func TestAdversary_DeltaThreeRulesReversePlusExtra(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "one.example.com"
    ports: [1]
  - domain: "two.example.com"
    ports: [2]
  - domain: "three.example.com"
    ports: [3]
`
	child := adversaryAgentBlock + `egress:
  - domain: "three.example.com"
    ports: [3]
  - domain: "two.example.com"
    ports: [2]
  - domain: "one.example.com"
    ports: [1]
  - domain: "four.example.com"
    ports: [4]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	want := &PolicyDelta{EgressAdded: []string{"four.example.com:4"}}
	assertPolicyDeltaEqual(t, want, got)
}

// TestAdversary_DeltaEmptyParentNonemptyChild
func TestAdversary_DeltaEmptyParentNonemptyChild(t *testing.T) {
	parent := adversaryAgentBlock
	child := adversaryAgentBlock + `egress:
  - domain: "only.example.com"
    ports: [443]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	want := &PolicyDelta{EgressAdded: []string{"only.example.com:443"}}
	assertPolicyDeltaEqual(t, want, got)
}

// TestAdversary_DeltaNonemptyParentEmptyChild
func TestAdversary_DeltaNonemptyParentEmptyChild(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "gone.example.com"
    ports: [443]
`
	child := adversaryAgentBlock
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	want := &PolicyDelta{EgressRemoved: []string{"gone.example.com:443"}}
	assertPolicyDeltaEqual(t, want, got)
}

// TestAdversary_DeltaIdenticalWhitespaceMarshalsNull
func TestAdversary_DeltaIdenticalWhitespaceMarshalsNull(t *testing.T) {
	parent := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: api.example.com
    ports: [443]
`
	child := `version: "1.0"
agent:
  name:   test-agent

egress:
  - domain: "api.example.com"
    ports:
      - 443
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("BREAK: equivalent policies with whitespace variants produced delta %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("BREAK: empty delta must marshal as null, got %s", data)
	}
}

// TestAdversary_DeltaAllowWildcardChangeIsRemovedPlusAdded
func TestAdversary_DeltaAllowWildcardChangeIsRemovedPlusAdded(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "api.example.com"
    ports: [443]
    allow_wildcard: true
`
	child := adversaryAgentBlock + `egress:
  - domain: "api.example.com"
    ports: [443]
    allow_wildcard: false
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got == nil {
		t.Fatal("BREAK: AllowWildcard change produced nil delta (identity key must differ)")
	}
	if len(got.EgressAdded) != 1 || len(got.EgressRemoved) != 1 {
		t.Fatalf("BREAK: expected exactly one added and one removed, got %+v", got)
	}
	if got.EgressAdded[0] != got.EgressRemoved[0] {
		t.Fatalf("BREAK: labels should match host:ports; added=%q removed=%q",
			got.EgressAdded[0], got.EgressRemoved[0])
	}
}

// TestAdversary_DeltaAllowPrivateNilVsFalseProducesDelta: nil vs *false are distinct keys.
func TestAdversary_DeltaAllowPrivateNilVsFalseProducesDelta(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "api.example.com"
    ports: [443]
`
	child := adversaryAgentBlock + `egress:
  - domain: "api.example.com"
    ports: [443]
    allow_private: false
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got == nil || len(got.EgressAdded) == 0 || len(got.EgressRemoved) == 0 {
		t.Fatalf("BREAK: nil vs explicit false AllowPrivate should be removed+added, got %+v", got)
	}
}

// TestAdversary_DeltaMalformedYAMLReturnsError
func TestAdversary_DeltaMalformedYAMLReturnsError(t *testing.T) {
	_, err := ComputeDelta([]byte("not: [valid"), []byte(adversaryAgentBlock))
	if err == nil {
		t.Fatal("BREAK: malformed parent YAML should error")
	}
	_, err = ComputeDelta([]byte(adversaryAgentBlock), []byte("{{"))
	if err == nil {
		t.Fatal("BREAK: malformed child YAML should error")
	}
}

// TestAdversary_DeltaEmptyByteSlicesBehavior: empty input must not panic.
func TestAdversary_DeltaEmptyByteSlicesBehavior(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("BREAK: ComputeDelta panicked on empty input: %v", r)
		}
	}()
	_, errParent := ComputeDelta(nil, []byte(adversaryAgentBlock))
	_, errChild := ComputeDelta([]byte(adversaryAgentBlock), nil)
	_, errBoth := ComputeDelta([]byte{}, []byte{})
	if errParent == nil && errChild == nil && errBoth == nil {
		t.Log("note: all empty/nil inputs returned nil error — acceptable if parse fails elsewhere")
	}
	if errParent != nil || errChild != nil || errBoth != nil {
		t.Logf("empty inputs returned errors (expected): parent=%v child=%v both=%v",
			errParent, errChild, errBoth)
	}
}

// TestAdversary_DeltaCredentialKeyedByIDOnly: same ID different type → no credential delta.
func TestAdversary_DeltaCredentialKeyedByIDOnly(t *testing.T) {
	parent := adversaryAgentBlock + `credentials:
  - id: tok
    type: header
    header: Authorization
    value: old
`
	child := adversaryAgentBlock + `credentials:
  - id: tok
    type: oauth
    header: Authorization
    value: new
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil && (len(got.CredentialsAdded) > 0 || len(got.CredentialsRemoved) > 0) {
		t.Fatalf("BREAK: credential delta should key on ID only; type change leaked as delta %+v", got)
	}
}

// TestAdversary_DeltaMCPKeyedByNameOnly: same name different URL → no MCP delta.
func TestAdversary_DeltaMCPKeyedByNameOnly(t *testing.T) {
	parent := adversaryAgentBlock + `mcp_servers:
  - name: fs
    url: "https://old.example.com"
`
	child := adversaryAgentBlock + `mcp_servers:
  - name: fs
    url: "https://new.example.com"
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil && (len(got.MCPToolsAdded) > 0 || len(got.MCPToolsRemoved) > 0) {
		t.Fatalf("BREAK: MCP delta should key on name only; URL change leaked as delta %+v", got)
	}
}

// TestAdversary_DeltaPortChangeNoModifiedCategory
func TestAdversary_DeltaPortChangeNoModifiedCategory(t *testing.T) {
	parent := adversaryAgentBlock + `egress:
  - domain: "api.slack.com"
    ports: [443]
`
	child := adversaryAgentBlock + `egress:
  - domain: "api.slack.com"
    ports: [443, 80]
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	want := &PolicyDelta{
		EgressRemoved: []string{"api.slack.com:443"},
		EgressAdded:   []string{"api.slack.com:80,443"},
	}
	assertPolicyDeltaEqual(t, want, got)
	// PolicyDelta has no "modified" fields — structural check
	rt := fmt.Sprintf("%T", got)
	if strings.Contains(rt, "Modified") {
		t.Fatal("BREAK: modified category exists")
	}
}

// TestAdversary_DeltaLargePolicyDeterminism: 100 egress rules, shuffle child order.
func TestAdversary_DeltaLargePolicyDeterminism(t *testing.T) {
	var parentEgress, childEgress strings.Builder
	parentEgress.WriteString(adversaryAgentBlock + "egress:\n")
	childEgress.WriteString(adversaryAgentBlock + "egress:\n")
	for i := 0; i < 50; i++ {
		line := fmt.Sprintf("  - domain: \"host-%02d.example.com\"\n    ports: [%d]\n", i, 1000+i)
		parentEgress.WriteString(line)
	}
	// child: same 50 + 50 new, reverse order in YAML
	for i := 49; i >= 0; i-- {
		line := fmt.Sprintf("  - domain: \"host-%02d.example.com\"\n    ports: [%d]\n", i, 1000+i)
		childEgress.WriteString(line)
	}
	for i := 50; i < 100; i++ {
		line := fmt.Sprintf("  - domain: \"host-%02d.example.com\"\n    ports: [%d]\n", i, 1000+i)
		childEgress.WriteString(line)
	}
	parent := parentEgress.String()
	child := childEgress.String()

	// second child: forward order for original 50 + extras
	var childFwd strings.Builder
	childFwd.WriteString(adversaryAgentBlock + "egress:\n")
	for i := 0; i < 100; i++ {
		line := fmt.Sprintf("  - domain: \"host-%02d.example.com\"\n    ports: [%d]\n", i, 1000+i)
		childFwd.WriteString(line)
	}

	d1, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("shuffled child: %v", err)
	}
	d2, err := ComputeDelta([]byte(parent), []byte(childFwd.String()))
	if err != nil {
		t.Fatalf("ordered child: %v", err)
	}
	if marshalDeltaJSON(t, d1) != marshalDeltaJSON(t, d2) {
		t.Fatal("BREAK: 100-rule policy shuffle changed delta JSON")
	}
	if d1 == nil || len(d1.EgressAdded) != 50 {
		t.Fatalf("expected 50 added egress rules, got %+v", d1)
	}
}

// TestAdversary_DeltaAddedRemovedSorted
func TestAdversary_DeltaAddedRemovedSorted(t *testing.T) {
	parent := adversaryAgentBlock
	child := adversaryAgentBlock + `egress:
  - domain: "z.example.com"
    ports: [1]
  - domain: "m.example.com"
    ports: [2]
  - domain: "a.example.com"
    ports: [3]
credentials:
  - id: z-cred
    type: header
    header: X
    value: z
  - id: a-cred
    type: header
    header: X
    value: a
`
	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	wantAdded := []string{"a.example.com:3", "m.example.com:2", "z.example.com:1"}
	if len(got.EgressAdded) != len(wantAdded) {
		t.Fatalf("egress added len %d want %d: %v", len(got.EgressAdded), len(wantAdded), got.EgressAdded)
	}
	for i := range wantAdded {
		if got.EgressAdded[i] != wantAdded[i] {
			t.Fatalf("BREAK: egress_added not sorted by label key order at [%d]: got %v want %v",
				i, got.EgressAdded, wantAdded)
		}
	}
	wantCreds := []string{"a-cred", "z-cred"}
	if len(got.CredentialsAdded) != 2 {
		t.Fatalf("credentials added: %v", got.CredentialsAdded)
	}
	for i := range wantCreds {
		if got.CredentialsAdded[i] != wantCreds[i] {
			t.Fatalf("BREAK: credentials_added not sorted: got %v want %v", got.CredentialsAdded, wantCreds)
		}
	}
}