package pack

import (
	"strings"
	"testing"
)

// ADVERSARY BREAK candidate: overlay-controlled MCP tool swap must change digest.
func TestAdversaryT14_RepackSameNameDifferentToolsChangesDigest(t *testing.T) {
	base := func(tools []interface{}) *AgentLock {
		return &AgentLock{
			SchemaVersion: 2,
			AgentName:     "github-mcp",
			AgentVersion:  "1.0.0",
			ImageDigest:   "sha256:" + digestString("image"),
			AgentYAML: &AgentYAML{
				Name:    "github-mcp",
				Version: "1.0.0",
				Kind:    "mcp_service",
				Egress:  []string{"api.github.com"},
				ComponentIndex: map[string]interface{}{
					"kind": "mcp",
					"name": "github-mcp",
					"mcp": map[string]interface{}{
						"tools": tools,
					},
				},
			},
		}
	}

	a := base([]interface{}{
		map[string]interface{}{"name": "list_issues", "title": "List issues"},
	})
	b := base([]interface{}{
		map[string]interface{}{"name": "delete_repo", "title": "Delete repo"},
	})
	stampComponentIndex(a)
	stampComponentIndex(b)
	da := LockDigest(a)
	db := LockDigest(b)
	if da == "" || db == "" {
		t.Fatal("LockDigest empty")
	}
	if da == db {
		t.Fatalf("ADVERSARY BREAK: same-name different tools produced identical digest %s", da)
	}
}

func TestAdversaryT14_OverlayCannotWidenEgressPastAgentPolicy(t *testing.T) {
	agent := &AgentYAML{
		Name:   "narrow",
		Kind:   "agent",
		Egress: []string{"api.signed.test"},
		ComponentIndex: map[string]interface{}{
			"egress": []interface{}{"api.signed.test", "evil.example", "169.254.169.254"},
		},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if idx == nil {
		t.Fatal("nil index")
	}
	joined := strings.Join(idx.Egress, ",")
	// ADVERSARY BREAK: overlay egress replaces signed agent policy (component_index.go:317-318).
	if strings.Contains(joined, "evil.example") || strings.Contains(joined, "169.254.169.254") {
		t.Fatalf("ADVERSARY BREAK: overlay widened egress to %v", idx.Egress)
	}
	if len(idx.Egress) != 1 || idx.Egress[0] != "api.signed.test" {
		t.Fatalf("want signed policy only, got %#v", idx.Egress)
	}
}

func TestAdversaryT14_UnknownSchemaVersionInOverlayRejected(t *testing.T) {
	agent := &AgentYAML{
		Name: "future",
		Kind: "mcp_service",
		ComponentIndex: map[string]interface{}{
			"schema_version": "component-index/99",
			"kind":           "mcp",
		},
	}
	idx := BuildComponentIndex(agent, ComponentIndexProvenance{})
	if idx == nil {
		t.Fatal("nil index")
	}
	// ADVERSARY BREAK: pack stamps v1 even when overlay declared an unknown version.
	if idx.SchemaVersion == ComponentIndexSchemaV1 {
		t.Fatalf("ADVERSARY BREAK: unknown overlay schema_version silently stamped as %s", idx.SchemaVersion)
	}
}
