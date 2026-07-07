package install

import (
	"os"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ComputeLocallyVerifiedHops marks fork hops whose parent lock digest matches a local install.
func ComputeLocallyVerifiedHops(report *bundle.InspectReport, state InstallStateStore) map[int]bool {
	if report == nil || report.Provenance == nil || len(report.Provenance.Entries) <= 1 {
		return nil
	}
	fs, ok := state.(*FileInstallState)
	if !ok || fs.StateRoot == "" {
		return nil
	}
	entries := report.Provenance.Entries
	out := make(map[int]bool)
	for i := 1; i < len(entries); i++ {
		e := entries[i]
		if e.ParentLockDigest == "" {
			continue
		}
		digest, found := readInstalledLockDigest(fs.StateRoot, e.PublisherFingerprint, e.AgentName)
		if !found || digest != e.ParentLockDigest {
			continue
		}
		out[e.Index] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func readInstalledLockDigest(stateRoot, publisherFingerprint, agentName string) (string, bool) {
	dir, err := InstalledAgentPath(stateRoot, agentName, publisherFingerprint)
	if err != nil {
		return "", false
	}
	lockPath := filepath.Join(dir, installedLockName)
	if _, err := os.Stat(lockPath); err != nil {
		return "", false
	}
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		return "", false
	}
	d := pack.LockDigest(lock)
	if d == "" {
		return "", false
	}
	return d, true
}