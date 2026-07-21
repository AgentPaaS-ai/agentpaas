package registry

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ValidateWorkflowPromotedPackages checks that every named package referenced in
// a workflow.yaml (service bindings, pipeline stages, child allowlist) is
// promoted in the local registry. If stateRoot is empty, the check is skipped
// (graceful degradation).
//
// Returns a list of actionable error messages, one per un-promoted package.
func ValidateWorkflowPromotedPackages(stateRoot string, wf *pack.WorkflowYAML) []string {
	if wf == nil || stateRoot == "" {
		return nil
	}

	var errs []string

	// Collect unique package names to check.
	names := collectWorkflowPackageNames(wf)
	for _, name := range names {
		promoted, err := isPackagePromoted(stateRoot, name)
		if err != nil {
			// If we can't resolve the package (e.g., not installed), skip —
			// this validation only applies to installed packages.
			continue
		}
		if !promoted {
			errs = append(errs, fmt.Sprintf(
				"package %q is not promoted; promote it first: `agentpaas registry promote %s`",
				name, name,
			))
		}
	}

	return errs
}

// collectWorkflowPackageNames extracts all unique package names from a workflow.
func collectWorkflowPackageNames(wf *pack.WorkflowYAML) []string {
	seen := make(map[string]bool)
	var names []string

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}

	// Service bindings.
	for _, s := range wf.Services {
		add(s.PackageName)
	}

	// Pipeline stages.
	if wf.Pipeline != nil {
		for _, s := range wf.Pipeline.Stages {
			add(s.PackageName)
		}
	}

	// Child allowlist (parent identity and children).
	if wf.ParentChild != nil {
		add(wf.ParentChild.ParentIdentity.PackageName)
		for _, c := range wf.ParentChild.ChildAllowlist {
			add(c.PackageName)
		}
	}

	return names
}

// isPackagePromoted checks whether an installed package is promoted.
// It tries to resolve the name as a ref (name@pub8 or alias).
// Returns (promoted, error). If the package is not installed, returns an error.
func isPackagePromoted(stateRoot, name string) (bool, error) {
	resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: stateRoot,
		Input:     name,
	})
	if err != nil {
		return false, err
	}
	if !resolved.Installed {
		return false, fmt.Errorf("not installed: %s", name)
	}

	m, err := install.LoadManifestByRef(stateRoot, resolved.Ref)
	if err != nil {
		return false, err
	}

	return m.Promoted, nil
}
