package daemon

import (
	"strings"
	"testing"
)

// OpenShell routable names are DNS-1123 labels of at most 19 characters
// (19 + "--" + 19 + "--" + 19 = 61, under the 63-char DNS label max).
const openShellRoutableNameMax = 19

func TestOpenShellSandboxNameLength(t *testing.T) {
	runID := "run-0123456789abcdef"
	if len(runID) != 20 {
		t.Fatalf("fixture run ID len = %d, want 20", len(runID))
	}
	name := openShellSandboxName(runID)
	assertOpenShellSandboxName(t, name)
	if name == runID || name == strings.ReplaceAll(runID, "_", "-") {
		t.Fatalf("sandbox name must not be the 20-char run ID, got %q", name)
	}
	// Keep every entropy character. "run-" is 4 chars; a 1-char prefix
	// leaves the 16 hex digits inside the 19-char limit.
	if got, want := name, "r0123456789abcdef"; got != want {
		t.Fatalf("openShellSandboxName(%q) = %q, want %q", runID, got, want)
	}
	if openShellSandboxName(runID) != name {
		t.Fatal("sandbox name must be stable for a run ID")
	}

	seen := map[string]string{}
	for i := 0; i < 32; i++ {
		id := generateRunID()
		if len(id) < 20 || !strings.HasPrefix(id, "run-") {
			t.Fatalf("generateRunID shortened or changed, got %q (len %d)", id, len(id))
		}
		got := openShellSandboxName(id)
		assertOpenShellSandboxName(t, got)
		if prev, ok := seen[got]; ok {
			t.Fatalf("sandbox name %q collided for %q and %q", got, prev, id)
		}
		seen[got] = id
	}

	// Truncating to 19 collides these. The sandbox name must not.
	longA := "run-1234567890123450001"
	longB := "run-1234567890123450002"
	nameA := openShellSandboxName(longA)
	nameB := openShellSandboxName(longB)
	assertOpenShellSandboxName(t, nameA)
	assertOpenShellSandboxName(t, nameB)
	if nameA == nameB {
		t.Fatalf("distinct long run IDs collapsed to %q", nameA)
	}
	if strings.HasPrefix(nameA, longA[:openShellRoutableNameMax]) {
		t.Fatalf("long run ID was truncated rather than compressed: %q", nameA)
	}
}

func assertOpenShellSandboxName(t *testing.T, name string) {
	t.Helper()
	if name == "" || len(name) > openShellRoutableNameMax {
		t.Fatalf("sandbox name length %d, want 1..%d: %q", len(name), openShellRoutableNameMax, name)
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		t.Fatalf("sandbox name %q has a leading, trailing, or consecutive hyphen", name)
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			t.Fatalf("sandbox name %q is not a DNS-1123 label", name)
		}
	}
}
