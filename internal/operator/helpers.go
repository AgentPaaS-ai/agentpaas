//go:build soak

package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/supervisor"
)

// ---------------------------------------------------------------------------
// soak configuration
// ---------------------------------------------------------------------------

type soakConfig struct {
	MinTurns       int
	MinWallSeconds int
	DaemonRestarts int
	SIGKILLPoints  int
	ShortMode      bool
}

func soakConfigFull() soakConfig {
	return soakConfig{
		MinTurns:       100,
		MinWallSeconds: 1800,
		DaemonRestarts: 3,
		SIGKILLPoints:  5,
	}
}

func soakConfigShort() soakConfig {
	return soakConfig{
		MinTurns:       10,
		MinWallSeconds: 5,
		DaemonRestarts: 1,
		SIGKILLPoints:  2,
		ShortMode:      true,
	}
}

func soakConfigFromEnv() soakConfig {
	if os.Getenv("AGENTPAAS_SOAK_SHORT") == "1" {
		return soakConfigShort()
	}
	return soakConfigFull()
}

// ---------------------------------------------------------------------------
// Docker helpers
// ---------------------------------------------------------------------------

func requireDockerSoak(t *testing.T) {
	t.Helper()
	if dockerInfoOK() {
		return
	}
	repoRoot := findRepoRoot()
	script := filepath.Join(repoRoot, "scripts", "ensure-docker.sh")
	t.Logf("Docker not running — executing: %s", script)
	cmd := exec.Command("bash", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Docker required for soak test (no skip): %v\nRun: make ensure-docker", err)
	}
	if !dockerInfoOK() {
		t.Fatalf("Docker still not available after ensure-docker.sh — cannot run soak test")
	}
}

func dockerInfoOK() bool {
	return exec.Command("docker", "info").Run() == nil
}

func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Fake journal factory
// ---------------------------------------------------------------------------

type fakeJournalFactory struct {
	mu       sync.Mutex
	journals map[string]*fakeJournal
}

func newFakeJournalFactory() *fakeJournalFactory {
	return &fakeJournalFactory{
		journals: make(map[string]*fakeJournal),
	}
}

func (f *fakeJournalFactory) OpenControlJournal(runID routedrun.RunID, attemptID routedrun.AttemptID) (supervisor.ControlJournalHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(runID) + "/" + string(attemptID)
	j, ok := f.journals[key]
	if !ok {
		j = &fakeJournal{}
		f.journals[key] = j
	}
	return j, nil
}

type fakeJournal struct {
	mu     sync.Mutex
	events []routedrun.InvokeJobEvent
}

func (j *fakeJournal) Append(event routedrun.InvokeJobEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, event)
	return nil
}

func (j *fakeJournal) Read(fromSeq int64) ([]routedrun.InvokeJobEvent, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []routedrun.InvokeJobEvent
	for _, e := range j.events {
		if e.Sequence >= fromSeq {
			out = append(out, e)
		}
	}
	return out, nil
}

func (j *fakeJournal) Close() error { return nil }

// ---------------------------------------------------------------------------
// File result store
// ---------------------------------------------------------------------------

type fileResultStore struct {
	dir string
	mu  sync.Mutex
}

func newFileResultStore(dir string) *fileResultStore {
	return &fileResultStore{dir: dir}
}

func (s *fileResultStore) SaveInvokeJobResult(_ context.Context, result *routedrun.InvokeJobResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rpath := filepath.Join(s.dir, "result-"+string(result.RunID)+".json")
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(rpath, data, 0o644)
}

func (s *fileResultStore) GetInvokeJobResult(_ context.Context, runID routedrun.RunID) (*routedrun.InvokeJobResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rpath := filepath.Join(s.dir, "result-"+string(runID)+".json")
	data, err := os.ReadFile(rpath)
	if err != nil {
		return nil, fmt.Errorf("not found: %w", err)
	}
	var result routedrun.InvokeJobResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}