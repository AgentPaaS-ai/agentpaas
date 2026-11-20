package runtime

import (
	"strings"
	"testing"
	"unicode"
)

// TestAdversaryT01_NameCollision tests for deterministic name collisions
// across different (role, id) pairs. This attacks ownership and reconciliation
// because colliding names could cause the wrong container to be targeted.
func TestAdversaryT01_NameCollision(t *testing.T) {
	// ADVERSARY BREAK: HIGH - ContainerName is not collision-resistant.
	// ("agent", "foo-bar") and ("agent-foo", "bar") both resolve to
	// "agentpaas-agent-foo-bar". Different logical resources get the same
	// Docker name, breaking deterministic ownership and reconciliation safety.
	n1 := ContainerName("agent", "foo-bar")
	n2 := ContainerName("agent-foo", "bar")
	if n1 == n2 {
		t.Errorf("HIGH name collision: %q == %q for distinct (role,id) pairs", n1, n2)
	}
	// Also test gateway vs crafted role
	n3 := ContainerName("gateway", "x-y")
	n4 := ContainerName("gateway-x", "y")
	if n3 == n4 {
		t.Errorf("HIGH name collision on gateway: %q == %q", n3, n4)
	}
}

// TestAdversaryT01_NameDeterminism verifies same inputs always produce
// identical names (positive property, but tested under adversarial inputs).
func TestAdversaryT01_NameDeterminism(t *testing.T) {
	inputs := []struct{ role, id string }{
		{"agent", "run-123"},
		{"gateway", "run-abc"},
		{"internal", "net-1"}, // falls to default format
		{"", ""},
		{"agent", ""},
	}
	for _, in := range inputs {
		n1 := ContainerName(in.role, in.id)
		n2 := ContainerName(in.role, in.id)
		if n1 != n2 {
			t.Errorf("determinism broken for (%q,%q): %q != %q", in.role, in.id, n1, n2)
		}
	}
}

// TestAdversaryT01_LabelNewlineInjection tests newlines and control chars
// in runID/role flowing into label values and names (injection vector).
func TestAdversaryT01_LabelNewlineInjection(t *testing.T) {
	// ADVERSARY BREAK: MEDIUM - no sanitization of runID/role.
	// Newlines, nulls, and other control characters are accepted into
	// deterministic names and label values. This can inject newlines into
	// Docker label filters or container names during reconciliation.
	runID := "run\nid\x00with\nnewline"
	l := Labels("agent", runID)
	if !strings.Contains(l[LabelRunID], "\n") || !strings.Contains(l[LabelRunID], "\x00") {
		t.Error("newline/null not preserved in label value")
	}
	name := ContainerName("agent", runID)
	if !strings.Contains(name, "\n") {
		t.Error("newline not present in container name")
	}
	// IsOwned still true because managed-by is clean
	if !IsOwned(l) {
		t.Error("IsOwned should still recognize our own label map")
	}
}

// TestAdversaryT01_UnicodeHomoglyphBypass tests homoglyphs that visually
// mimic ASCII and could bypass naive [a-z0-9] name validation (future).
func TestAdversaryT01_UnicodeHomoglyphBypass(t *testing.T) {
	// Cyrillic 'а' (U+0430) looks like Latin 'a'
	homoglyphID := "run-\u0430bc" // contains homoglyph
	n := ContainerName("agent", homoglyphID)
	if !strings.ContainsRune(n, '\u0430') {
		t.Error("homoglyph not preserved in name")
	}
	// Different from ASCII equivalent
	asciiID := "run-abc"
	n2 := ContainerName("agent", asciiID)
	if n == n2 {
		t.Error("homoglyph name collided with ASCII name (unexpected)")
	}
	// No [a-z0-9] enforcement exists, allowing bypass of future ASCII-only rules
	for _, r := range n {
		if unicode.IsLetter(r) && r > unicode.MaxASCII {
			// homoglyph present — acceptable for now but potential future attack surface
			break
		}
	}
}

// TestAdversaryT01_LabelOwnershipBypass tests IsOwned under forged or
// partial labels (resource ownership confusion / reconciliation safety).
func TestAdversaryT01_LabelOwnershipBypass(t *testing.T) {
	// Exact match (our Labels func) must be owned
	owned := Labels(ResourceTypeAgent, "r1")
	if !IsOwned(owned) {
		t.Error("IsOwned false for our own Labels() output")
	}

	// Case variation on VALUE is accepted (EqualFold)
	caseVar := map[string]string{
		LabelManagedBy:    "AGENTPAAS",
		LabelResourceType: "agent",
		LabelRunID:        "r1",
	}
	if !IsOwned(caseVar) {
		t.Error("IsOwned should accept case-folded managed-by value")
	}

	// Wrong value must NOT be owned (prevents simple forgery)
	wrongVal := map[string]string{LabelManagedBy: "not-agentpaas"}
	if IsOwned(wrongVal) {
		t.Errorf("IsOwned true for wrong managed-by value — ownership bypass")
	}

	// Key case variation must NOT match (map keys are case-sensitive)
	keyCase := map[string]string{"Agentpaas.Managed-By": "agentpaas"}
	if IsOwned(keyCase) {
		t.Error("IsOwned true for case-variant key — potential injection")
	}

	// Nil, empty, and completely foreign labels must be false (reconciliation safety)
	if IsOwned(nil) || IsOwned(map[string]string{}) || IsOwned(map[string]string{"com.example.foo": "bar"}) {
		t.Error("IsOwned true for non-AgentPaaS labels — reconciliation would touch foreign containers")
	}

	// Partial AgentPaaS-looking but missing managed-by
	partial := map[string]string{LabelResourceType: "agent", LabelRunID: "r1"}
	if IsOwned(partial) {
		t.Error("IsOwned true without managed-by label")
	}
}

// TestAdversaryT01_NetworkNameCollision mirrors container test for networks.
func TestAdversaryT01_NetworkNameCollision(t *testing.T) {
	n1 := NetworkName("internal", "net-foo-bar")
	n2 := NetworkName("internal-foo", "bar")
	if n1 == n2 {
		t.Errorf("HIGH network name collision: %q == %q", n1, n2)
	}
}

// TestAdversaryT01_ReconciliationSafety explicitly verifies that
// non-AgentPaaS containers (various real-world label patterns) are never owned.
func TestAdversaryT01_ReconciliationSafety(t *testing.T) {
	foreignLabels := []map[string]string{
		{"com.docker.compose.config-hash": "abc123"},
		{"io.kubernetes.container.name": "nginx"},
		{"org.opencontainers.image.title": "myimage"},
		{"maintainer": "someone@example.com"},
		nil,
		{},
	}
	for i, lbl := range foreignLabels {
		if IsOwned(lbl) {
			t.Errorf("reconciliation safety broken: foreign label set #%d marked owned: %+v", i, lbl)
		}
	}
}