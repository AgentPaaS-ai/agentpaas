package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

const (
	installedAgentsDirName   = "agents"
	installedLockName        = "agent.lock"
	installedPolicyName      = "policy.yaml"
	installedSBOMName        = "sbom.spdx.json"
	installedSourceDir       = "source"
	installedManifestName    = "install-manifest.json"
	installedParentBundleRef = "parent-bundle.ref"
)

// InstalledAgentRefDirName returns the state directory segment name@pub8.
func InstalledAgentRefDirName(agentName, publisherFingerprint string) (string, error) {
	if err := naming.ValidateName(agentName); err != nil {
		return "", fmt.Errorf("installed agent ref dir name: %w", err)
	}
	fp := trust.NormalizeFingerprint(publisherFingerprint)
	if len(fp) < 8 {
		return "", fmt.Errorf("publisher fingerprint too short")
	}
	pub8 := strings.ToLower(fp[:8])
	return agentName + "@" + pub8, nil
}

// InstalledAgentPath returns StateRoot/agents/<name>@<pub8>/.
func InstalledAgentPath(stateRoot, agentName, publisherFingerprint string) (string, error) {
	refDir, err := InstalledAgentRefDirName(agentName, publisherFingerprint)
	if err != nil {
		return "", fmt.Errorf("installed agent path: %w", err)
	}
	if stateRoot == "" {
		return "", fmt.Errorf("install: empty StateRoot")
	}
	return filepath.Join(stateRoot, installedAgentsDirName, refDir), nil
}

// ParseInstalledAgentDir reports whether a directory name under state/agents is an installed ref (name@pub8).
func ParseInstalledAgentDir(dirName string) (name, pub8 string, ok bool) {
	parts := strings.SplitN(dirName, "@", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	name, pub8 = parts[0], strings.ToLower(parts[1])
	if err := naming.ValidateName(name); err != nil {
		return "", "", false
	}
	if err := naming.ValidatePub8(pub8); err != nil {
		return "", "", false
	}
	return name, pub8, true
}

// IsPhase1LocalAgentDir reports whether dirName is a Phase-1 local agent path (no @pub8).
func IsPhase1LocalAgentDir(dirName string) bool {
	if strings.Contains(dirName, "@") {
		return false
	}
	return naming.ValidateName(dirName) == nil
}

func ensureAgentsParent(stateRoot string) (string, error) {
	agentsDir := filepath.Join(stateRoot, installedAgentsDirName)
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir agents state: %w", err)
	}
	return agentsDir, nil
}
