package daemon

import (
	"fmt"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// routedStoreRoot returns the LocalStore root under the daemon state directory.
// Layout is isolated under state/routed so it does not collide with harness
// audit directories under state/runs.
func routedStoreRoot(paths *home.HomePaths) string {
	if paths == nil {
		return ""
	}
	return filepath.Join(paths.State, "routed")
}

// initRoutedStores opens the protected LocalStore and wires DeploymentStore,
// RunStore, and WorkflowStore onto the control server. Safe to call once at
// daemon Start; tests may call it on a bare controlServer.
func (s *controlServer) initRoutedStores(root string) error {
	if s == nil {
		return fmt.Errorf("daemon: nil control server")
	}
	if root == "" && s.homePaths != nil {
		root = routedStoreRoot(s.homePaths)
	}
	if root == "" {
		return fmt.Errorf("daemon: cannot init routed stores: empty root")
	}
	store, err := routedrun.OpenLocalStore(root)
	if err != nil {
		return fmt.Errorf("daemon: open routed store at %s: %w", root, err)
	}
	s.localStore = store
	s.deploymentStore = store
	s.runStore = store
	s.workflowStore = store
	return nil
}

// DeploymentStore returns the wired deployment store (may be nil if not init).
func (s *controlServer) DeploymentStore() routedrun.DeploymentStore {
	if s == nil {
		return nil
	}
	return s.deploymentStore
}

// RunStore returns the wired run store (may be nil if not init).
func (s *controlServer) RunStore() routedrun.RunStore {
	if s == nil {
		return nil
	}
	return s.runStore
}

// WorkflowStore returns the wired workflow store (may be nil if not init).
func (s *controlServer) WorkflowStore() routedrun.WorkflowStore {
	if s == nil {
		return nil
	}
	return s.workflowStore
}
