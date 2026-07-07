package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ResolveInstalledAgentDir returns the directory for a materialized install or Phase-1 local agent.
func ResolveInstalledAgentDir(stateRoot, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty agent reference")
	}
	if err := ValidateReferenceInput(ref); err != nil {
		return "", err
	}
	if stateRoot == "" {
		return "", fmt.Errorf("install: empty StateRoot")
	}

	if strings.Contains(ref, "@") {
		_, dir, err := resolveInstalledRef(stateRoot, ref)
		if err != nil {
			return "", err
		}
		if dir == "" {
			return "", fmt.Errorf("no installed agent for reference %q", ref)
		}
		return dir, nil
	}

	_, dir, err := resolveInstalledRef(stateRoot, ref)
	if err != nil {
		return "", err
	}
	if dir != "" {
		return dir, nil
	}

	if isPhase1LocalAgent(stateRoot, ref) {
		return filepath.Join(stateRoot, installedAgentsDirName, ref), nil
	}

	res, err := ResolveAgentRef(ResolveRefOpts{StateRoot: stateRoot, Input: ref})
	if err != nil {
		return "", err
	}
	if res.Installed {
		name, pub8, perr := naming.ParseAgentRef(res.Ref)
		if perr != nil {
			return "", perr
		}
		dir, err := findInstalledDirByRef(stateRoot, name, pub8)
		if err != nil {
			return "", err
		}
		if dir == "" {
			return "", fmt.Errorf("no installed agent for reference %q", ref)
		}
		return dir, nil
	}
	if isPhase1LocalAgent(stateRoot, res.Ref) {
		return filepath.Join(stateRoot, installedAgentsDirName, res.Ref), nil
	}
	return "", fmt.Errorf("no installed agent for reference %q", ref)
}

// ProvenanceReportFromLock validates signatures and provenance, returning a report or error.
func ProvenanceReportFromLock(lock *pack.AgentLock) (*pack.ProvenanceReport, error) {
	if lock == nil {
		return nil, fmt.Errorf("lock must not be nil")
	}
	if err := pack.VerifyLockfileSignature(lock); err != nil {
		return nil, fmt.Errorf("lock signature invalid: %w", err)
	}
	if lock.SchemaVersion >= 2 && lock.Publisher != nil {
		if err := pack.VerifyPublisherSignature(lock); err != nil {
			return nil, fmt.Errorf("publisher signature invalid: %w", err)
		}
		if err := pack.VerifyProvenanceSignatures(lock); err != nil {
			return nil, fmt.Errorf("provenance signatures invalid: %w", err)
		}
	}
	report, err := pack.VerifyProvenance(lock)
	if err != nil {
		return nil, err
	}
	if !report.Verified {
		return nil, fmt.Errorf("provenance chain invalid")
	}
	return report, nil
}

// ReadInstalledProvenanceReport loads agent.lock from an installed ref and validates provenance.
func ReadInstalledProvenanceReport(stateRoot, ref string) (*pack.ProvenanceReport, error) {
	dir, err := ResolveInstalledAgentDir(stateRoot, ref)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, installedLockName)
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("read installed lock: %w", err)
	}
	return ProvenanceReportFromLock(lock)
}

// IsBundleFileArg reports whether arg names an existing regular bundle file path.
func IsBundleFileArg(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return false
	}
	if strings.Contains(arg, "/") || strings.HasSuffix(arg, ".tar") || strings.HasSuffix(arg, ".agentpaas") {
		if st, err := os.Stat(arg); err == nil && st.Mode().IsRegular() {
			return true
		}
	}
	if st, err := os.Stat(arg); err == nil && st.Mode().IsRegular() {
		if strings.HasSuffix(arg, ".agentpaas") || strings.HasSuffix(arg, ".tar") {
			return true
		}
	}
	return false
}