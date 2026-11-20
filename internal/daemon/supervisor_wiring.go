package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
	"github.com/AgentPaaS-ai/agentpaas/internal/supervisor"
)

// supervisorInit initializes the durable supervisor backed by the daemon's
// LocalStore (DurableStore), a filesystem result store, and a control-journal
// factory rooted at the daemon's state directory.
//
// Called once during daemon Start after initRoutedStores.
func (s *controlServer) supervisorInit(stateRoot string) error {
	if s == nil || s.localStore == nil {
		return fmt.Errorf("daemon: cannot init supervisor: routed store not initialized")
	}
	if stateRoot == "" {
		return fmt.Errorf("daemon: cannot init supervisor: empty state root")
	}

	// Create the result store directory.
	resultDir := filepath.Join(stateRoot, "results")
	if err := os.MkdirAll(resultDir, 0o700); err != nil {
		return fmt.Errorf("daemon: create result store dir: %w", err)
	}

	// Journal factory uses the routed store root.
	journalDir := filepath.Join(stateRoot, "journals")
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return fmt.Errorf("daemon: create journal dir: %w", err)
	}

	results := &daemonResultStore{dir: resultDir}
	journals := &daemonJournalFactory{root: journalDir}

	sup, err := supervisor.NewSupervisor(
		s.localStore, // DurableStore
		results,      // ResultStore
		journals,     // ControlJournalFactory
		routedrun.SystemClock{},
		stateRoot,
	)
	if err != nil {
		return fmt.Errorf("daemon: create supervisor: %w", err)
	}

	s.supervisor = sup
	return nil
}

// supervisorReconcile runs the supervisor's Reconcile method for all known
// in-flight runs. Called on daemon startup after supervisor is initialized.
func (s *controlServer) supervisorReconcile(ctx context.Context) {
	if s.supervisor == nil || s.localStore == nil {
		return
	}
	// Get all workflows, then list runs for each.
	workflows, err := s.localStore.ListWorkflows(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: supervisor reconcile: list workflows: %v\n", err)
		return
	}
	for _, wf := range workflows {
		runs, err := s.localStore.ListRuns(ctx, wf.WorkflowID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "daemon: supervisor reconcile: list runs for workflow %s: %v\n", wf.WorkflowID, err)
			continue
		}
		for _, run := range runs {
			if run.Status.IsTerminal() {
				continue
			}
			if err := s.supervisor.Reconcile(ctx, run.RunID); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: supervisor reconcile run %s: %v\n", run.RunID, err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// daemonResultStore — filesystem-backed ResultStore
// ---------------------------------------------------------------------------

type daemonResultStore struct {
	dir string
}

func (s *daemonResultStore) SaveInvokeJobResult(_ context.Context, result *routedrun.InvokeJobResult) error {
	if result == nil {
		return fmt.Errorf("nil result")
	}
	rpath := filepath.Join(s.dir, "result-"+string(result.RunID)+".json")
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return os.WriteFile(rpath, data, 0o644)
}

func (s *daemonResultStore) GetInvokeJobResult(_ context.Context, runID routedrun.RunID) (*routedrun.InvokeJobResult, error) {
	rpath := filepath.Join(s.dir, "result-"+string(runID)+".json")
	data, err := os.ReadFile(rpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("result for run %s: %w", runID, routedrun.ErrNotFound)
		}
		return nil, fmt.Errorf("read result: %w", err)
	}
	var result routedrun.InvokeJobResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// daemonJournalFactory — wraps routedrun.NewControlJournal
// ---------------------------------------------------------------------------

type daemonJournalFactory struct {
	root string
}

func (f *daemonJournalFactory) OpenControlJournal(runID routedrun.RunID, attemptID routedrun.AttemptID) (supervisor.ControlJournalHandle, error) {
	return routedrun.NewControlJournal(f.root, string(runID), string(attemptID))
}