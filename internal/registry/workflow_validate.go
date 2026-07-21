package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ValidateWorkflowPromotedPackages checks that every named package referenced in
// a workflow.yaml (service bindings, pipeline stages, child allowlist) is
// promoted in the local registry. If stateRoot is empty, the check is skipped
// (graceful degradation).
//
// Design decision (B31 adversary fixes #2-#5):
// - Bare name resolution uses install.ListInstalledAgents (not ResolveAgentRef)
//   to prevent alias shadowing. The workflow gate matches by agent name, not alias.
// - Both package_name and package_version must match an installed, promoted agent.
// - A promoted=true flag without a corresponding package_promoted audit event is
//   treated as un-promoted. The audit trail is the authoritative trust signal;
//   the manifest flag alone is not sufficient for gate decisions.
//
// Returns a list of actionable error messages, one per un-promoted package.
func ValidateWorkflowPromotedPackages(stateRoot string, wf *pack.WorkflowYAML) []string {
	if wf == nil || stateRoot == "" {
		return nil
	}

	var errs []string

	// Collect unique package name+version pairs to check.
	refs := collectWorkflowPackageRefs(wf)
	for _, pkg := range refs {
		promoted, err := isPackagePromoted(stateRoot, pkg.name, pkg.version)
		if err != nil {
			// Resolution failure for an installed-adjacent name (e.g. ambiguous
			// resolution, version mismatch across installed agents) must fail
			// the gate, not silently skip it.
			errs = append(errs, fmt.Sprintf(
				"package %q version %q is not promoted: %v; promote it first: `agentpaas registry promote %s@%s`",
				pkg.name, pkg.version, err, pkg.name, pkg.version,
			))
			continue
		}
		if !promoted {
			errs = append(errs, fmt.Sprintf(
				"package %q version %q is not promoted; promote it first: `agentpaas registry promote %s@%s`",
				pkg.name, pkg.version, pkg.name, pkg.version,
			))
		}
	}

	return errs
}

// packageRef holds a package name and version from a workflow reference.
type packageRef struct {
	name    string
	version string
}

// collectWorkflowPackageRefs extracts all unique package name+version pairs from a workflow.
func collectWorkflowPackageRefs(wf *pack.WorkflowYAML) []packageRef {
	seen := make(map[string]bool)
	var refs []packageRef

	add := func(name, version string) {
		name = strings.TrimSpace(name)
		version = strings.TrimSpace(version)
		if name == "" || version == "" {
			return
		}
		key := name + "@" + version
		if seen[key] {
			return
		}
		seen[key] = true
		refs = append(refs, packageRef{name: name, version: version})
	}

	// Service bindings.
	for _, s := range wf.Services {
		add(s.PackageName, s.PackageVersion)
	}

	// Pipeline stages.
	if wf.Pipeline != nil {
		for _, s := range wf.Pipeline.Stages {
			add(s.PackageName, s.PackageVersion)
		}
	}

	// Child allowlist (parent identity and children).
	if wf.ParentChild != nil {
		add(wf.ParentChild.ParentIdentity.PackageName, wf.ParentChild.ParentIdentity.PackageVersion)
		for _, c := range wf.ParentChild.ChildAllowlist {
			add(c.PackageName, c.PackageVersion)
		}
	}

	return refs
}

// isPackagePromoted checks whether a package with the given name and version
// is promoted. It uses install.ListInstalledAgents (not ResolveAgentRef) to
// prevent alias shadowing, and verifies that a matching package_promoted audit
// event exists before accepting the promoted flag.
func isPackagePromoted(stateRoot, name, version string) (bool, error) {
	installed, err := install.ListInstalledAgents(stateRoot)
	if err != nil {
		return false, fmt.Errorf("list installed agents: %w", err)
	}

	// Find installed agents whose name matches the requested package name.
	var matching []install.InstalledAgentEntry
	for _, inst := range installed {
		agentName, _, ok := parseRef(inst.Ref)
		if !ok {
			continue
		}
		if agentName == name {
			matching = append(matching, inst)
		}
	}

	// If no installed agent has this package name, skip gracefully.
	// The package may be locally-owned or handled outside the registry;
	// the promotion gate only applies to installed packages.
	if len(matching) == 0 {
		return true, nil
	}

	// Among agents with matching name, find those with matching version.
	var versionMatches []install.InstalledAgentEntry
	for _, inst := range matching {
		if inst.Version == version {
			versionMatches = append(versionMatches, inst)
		}
	}

	if len(versionMatches) == 0 {
		return false, fmt.Errorf("agent %q is installed but not at version %s", name, version)
	}

	// Check if any version-matching agent is promoted with a valid audit trail.
	for _, inst := range versionMatches {
		manifest, err := install.LoadManifestByRef(stateRoot, inst.Ref)
		if err != nil || manifest == nil {
			continue
		}
		if !manifest.Promoted {
			continue
		}
		// DESIGN DECISION: promoted=true without a package_promoted audit event
		// is treated as un-promoted. The audit trail is the authoritative trust
		// signal; hand-edited manifest flags are not sufficient for gate decisions.
		if !hasPromotionAudit(stateRoot, inst.Ref) {
			continue
		}
		return true, nil
	}

	return false, nil
}

// hasPromotionAudit checks the full audit log for the given agent ref and
// returns true only if the LAST promotion-relevant event (package_promoted or
// package_demoted) is package_promoted. A promote-then-demote sequence (or
// demoted after promote) must not pass the gate. Only the last event matters.
func hasPromotionAudit(stateRoot, agentRef string) bool {
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		return false
	}
	// Track the last promotion-relevant event for this ref.
	var lastEventType string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec audit.AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.EventType != audit.EventTypePackagePromoted && rec.EventType != audit.EventTypePackageDemoted {
			continue
		}
		ref, _ := rec.Payload["agent_ref"].(string)
		if ref == agentRef {
			lastEventType = rec.EventType
		}
	}
	return lastEventType == audit.EventTypePackagePromoted
}
