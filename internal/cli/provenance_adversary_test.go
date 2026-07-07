package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestProvenanceShow_AdversaryTamperedInstalledLock(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	dir, err := installResolveDir(fix.homeDir, fix.ref)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "agent.lock")
	if err := os.WriteFile(lockPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeProvenanceShow(t, fix.homeDir, fix.ref)
	if err == nil {
		t.Fatalf("want error, out=%q", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("must not render on tampered lock: %q", out)
	}
}

func TestProvenanceShow_AdversaryForgedProvenanceEntry(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	dir, err := installResolveDir(fix.homeDir, fix.ref)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "agent.lock")
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Provenance) == 0 {
		t.Fatal("want provenance entry")
	}
	lock.Provenance[0].EntrySignature = "AAAA"
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeProvenanceShow(t, fix.homeDir, fix.ref)
	if err == nil {
		t.Fatalf("want error, out=%q", out)
	}
	if strings.Contains(out, "Provenance:") {
		t.Fatalf("must not render: %q", out)
	}
}

func TestProvenanceShow_AdversaryTamperedLockJSONMode(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	dir, err := installResolveDir(fix.homeDir, fix.ref)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "agent.lock")
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.Provenance[0].EntrySignature = "invalid"
	raw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := executeProvenanceShow(t, fix.homeDir, fix.ref, "--json")
	if err == nil {
		t.Fatalf("want error, out=%q", out)
	}
	trim := strings.TrimSpace(out)
	if trim != "" && strings.HasPrefix(trim, "{") {
		t.Fatalf("must not emit JSON on failure: %q", out)
	}
}

func installResolveDir(homeDir, ref string) (string, error) {
	stateRoot := filepath.Join(homeDir, "state")
	return install.ResolveInstalledAgentDir(stateRoot, ref)
}