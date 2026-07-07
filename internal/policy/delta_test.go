package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

const deltaBasePolicy = `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.example.com"
    ports: [443]
`

func TestComputeDelta_Table(t *testing.T) {
	tests := []struct {
		name     string
		parent   string
		child    string
		wantNil  bool
		want     *PolicyDelta
		wantErr  bool
	}{
		{
			name:    "no change",
			parent:  deltaBasePolicy,
			child:   deltaBasePolicy,
			wantNil: true,
		},
		{
			name:   "add egress",
			parent: deltaBasePolicy,
			child: deltaBasePolicy + `  - domain: "api.slack.com"
    ports: [443]
`,
			want: &PolicyDelta{
				EgressAdded: []string{"api.slack.com:443"},
			},
		},
		{
			name: "remove egress",
			parent: deltaBasePolicy + `  - domain: "api.slack.com"
    ports: [443]
`,
			child: deltaBasePolicy,
			want: &PolicyDelta{
				EgressRemoved: []string{"api.slack.com:443"},
			},
		},
		{
			name: "change ports on same domain",
			parent: `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.slack.com"
    ports: [443]
`,
			child: `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.slack.com"
    ports: [443, 80]
`,
			want: &PolicyDelta{
				EgressRemoved: []string{"api.slack.com:443"},
				EgressAdded:   []string{"api.slack.com:80,443"},
			},
		},
		{
			name:   "add credential",
			parent: deltaBasePolicy,
			child: deltaBasePolicy + `credentials:
  - id: slack-token
    type: header
    header: Authorization
    value: secret
`,
			want: &PolicyDelta{
				CredentialsAdded: []string{"slack-token"},
			},
		},
		{
			name:   "add MCP server",
			parent: deltaBasePolicy,
			child: deltaBasePolicy + `mcp_servers:
  - name: filesystem
    url: "https://mcp.example.com/fs"
`,
			want: &PolicyDelta{
				MCPToolsAdded: []string{"filesystem"},
			},
		},
		{
			name:   "add hook",
			parent: deltaBasePolicy,
			child: deltaBasePolicy + `hooks:
  - name: deploy-hook
    url: "https://hooks.example.com/deploy"
`,
			want: &PolicyDelta{
				HooksAdded: []string{"deploy-hook"},
			},
		},
		{
			name:   "add ingress",
			parent: deltaBasePolicy,
			child: deltaBasePolicy + `ingress:
  - path: /webhook
    port: 8080
`,
			want: &PolicyDelta{
				IngressAdded: []string{"/webhook:8080"},
			},
		},
		{
			name:    "invalid parent yaml",
			parent:  "not: [valid",
			child:   deltaBasePolicy,
			wantErr: true,
		},
		{
			name:    "invalid child yaml",
			parent:  deltaBasePolicy,
			child:   "{{",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeDelta([]byte(tc.parent), []byte(tc.child))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ComputeDelta: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil delta, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil delta")
			}
			assertPolicyDeltaEqual(t, tc.want, got)
		})
	}
}

func TestComputeDelta_NilMarshalsToNull(t *testing.T) {
	var d *PolicyDelta
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("expected null, got %s", data)
	}

	got, err := ComputeDelta([]byte(deltaBasePolicy), []byte(deltaBasePolicy))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	data, err = json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("expected null for no-change delta, got %s", data)
	}
}

func TestComputeDelta_DeterministicShuffledYAML(t *testing.T) {
	parent := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "a.example.com"
    ports: [443]
credentials:
  - id: cred-b
    type: header
    header: X-B
    value: b
mcp_servers:
  - name: server-b
    url: "https://b.example.com"
`

	childShuffled := `version: "1.0"
agent:
  name: test-agent
mcp_servers:
  - name: server-a
    url: "https://a.example.com"
  - name: server-b
    url: "https://b.example.com"
credentials:
  - id: cred-a
    type: header
    header: X-A
    value: a
  - id: cred-b
    type: header
    header: X-B
    value: b
egress:
  - domain: "a.example.com"
    ports: [443]
  - domain: "api.slack.com"
    ports: [443]
`

	childOrdered := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "a.example.com"
    ports: [443]
  - domain: "api.slack.com"
    ports: [443]
credentials:
  - id: cred-a
    type: header
    header: X-A
    value: a
  - id: cred-b
    type: header
    header: X-B
    value: b
mcp_servers:
  - name: server-a
    url: "https://a.example.com"
  - name: server-b
    url: "https://b.example.com"
`

	d1, err := ComputeDelta([]byte(parent), []byte(childShuffled))
	if err != nil {
		t.Fatalf("ComputeDelta shuffled: %v", err)
	}
	d2, err := ComputeDelta([]byte(parent), []byte(childOrdered))
	if err != nil {
		t.Fatalf("ComputeDelta ordered: %v", err)
	}

	j1, err := json.Marshal(d1)
	if err != nil {
		t.Fatalf("marshal d1: %v", err)
	}
	j2, err := json.Marshal(d2)
	if err != nil {
		t.Fatalf("marshal d2: %v", err)
	}
	if string(j1) != string(j2) {
		t.Fatalf("delta JSON differs:\n%s\nvs\n%s", j1, j2)
	}
}

func TestComputeDelta_CanonicalDomainNormalization(t *testing.T) {
	parent := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "API.SLACK.COM"
    ports: [443]
`
	child := `version: "1.0"
agent:
  name: test-agent
egress:
  - domain: "api.slack.com"
    ports: [443]
`

	got, err := ComputeDelta([]byte(parent), []byte(child))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil delta for case-normalized domain, got %+v", got)
	}
}

func assertPolicyDeltaEqual(t *testing.T, want, got *PolicyDelta) {
	t.Helper()
	assertStringSliceEqual(t, "EgressAdded", want.EgressAdded, got.EgressAdded)
	assertStringSliceEqual(t, "EgressRemoved", want.EgressRemoved, got.EgressRemoved)
	assertStringSliceEqual(t, "CredentialsAdded", want.CredentialsAdded, got.CredentialsAdded)
	assertStringSliceEqual(t, "CredentialsRemoved", want.CredentialsRemoved, got.CredentialsRemoved)
	assertStringSliceEqual(t, "MCPToolsAdded", want.MCPToolsAdded, got.MCPToolsAdded)
	assertStringSliceEqual(t, "MCPToolsRemoved", want.MCPToolsRemoved, got.MCPToolsRemoved)
	assertStringSliceEqual(t, "IngressAdded", want.IngressAdded, got.IngressAdded)
	assertStringSliceEqual(t, "IngressRemoved", want.IngressRemoved, got.IngressRemoved)
	assertStringSliceEqual(t, "HooksAdded", want.HooksAdded, got.HooksAdded)
	assertStringSliceEqual(t, "HooksRemoved", want.HooksRemoved, got.HooksRemoved)
}

func assertStringSliceEqual(t *testing.T, field string, want, got []string) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if len(want) != len(got) {
		t.Fatalf("%s: len %d vs %d (want %v got %v)", field, len(want), len(got), want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("%s[%d]: want %q got %q", field, i, want[i], got[i])
		}
	}
}

func TestComputeDelta_EmptyDeltaNotEmptyObject(t *testing.T) {
	got, err := ComputeDelta([]byte(deltaBasePolicy), []byte(deltaBasePolicy))
	if err != nil {
		t.Fatalf("ComputeDelta: %v", err)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "egress_added") {
		t.Fatalf("unexpected fields in no-change marshal: %s", data)
	}
}