package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
)

// Promote marks an installed package as promoted in the local registry.
// It is idempotent: promoting an already-promoted package is a no-op success.
// An audit event (package_promoted) is emitted with the agent ref, publisher
// fingerprint, package digest, and actor.
func Promote(stateRoot, ref, actor string) error {
	// Resolve the ref (name@pub8 or alias) to find the installed agent.
	resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: stateRoot,
		Input:     ref,
	})
	if err != nil {
		return fmt.Errorf("promote: %w", err)
	}
	if !resolved.Installed {
		return fmt.Errorf("promote: ref %q resolves to non-installed agent", ref)
	}
	return promoteResolved(stateRoot, resolved.Ref, actor)
}

// promoteResolved sets the promoted flag on an already-resolved agent ref.
func promoteResolved(stateRoot, canonicalRef, actor string) error {
	manifestPath := filepath.Join(stateRoot, "agents", canonicalRef, "install-manifest.json")

	// Symlink protection: reject if the manifest path or any parent component
	// up to stateRoot/agents is a symlink. This prevents write escape via
	// symlink redirection.
	if err := validatePathNoSymlinks(manifestPath, stateRoot); err != nil {
		return fmt.Errorf("promote: %w", err)
	}

	// File locking for concurrent safety: lock the manifest, read, modify,
	// write, unlock. Uses a lock file adjacent to the manifest so that
	// atomic rename (which changes inodes) doesn't break lock coverage.
	lockPath := manifestPath + ".lock"
	m, unlock, err := lockAndLoadManifest(manifestPath, lockPath)
	if err != nil {
		return fmt.Errorf("promote: %w", err)
	}
	defer unlock()

	// Idempotent: if already promoted, don't change timestamp or actor.
	if m.Promoted {
		return nil
	}

	now := time.Now().UTC()
	m.Promoted = true
	m.PromotedAt = &now
	m.PromotedBy = actor

	// Determine the digest for the audit event. Prefer LocalImageDigest,
	// but if empty, fall back to the lockfile's ImageDigest.
	digest := m.LocalImageDigest
	if digest == "" {
		lock, err := loadLock(filepath.Join(stateRoot, "agents"), canonicalRef)
		if err == nil && lock != nil {
			digest = lock.ImageDigest
		}
	}

	// Audit event MUST be written BEFORE the manifest is saved.
	// If audit fails, the manifest is never modified (atomicity: audit-first).
	if err := emitPromotionAudit(stateRoot, audit.EventTypePackagePromoted, canonicalRef, m.PublisherFingerprint, digest, actor); err != nil {
		return fmt.Errorf("promote: audit: %w", err)
	}

	// Now save the manifest (under the same lock file protection).
	if err := saveManifestAtomic(manifestPath, m); err != nil {
		return fmt.Errorf("promote: %w", err)
	}

	return nil
}

// Demote clears the promoted flag on an installed package.
// It is idempotent: demoting an already-not-promoted package is a no-op success.
// An audit event (package_demoted) is emitted.
func Demote(stateRoot, ref string) error {
	// Resolve the ref (name@pub8 or alias) to find the installed agent.
	resolved, err := install.ResolveAgentRef(install.ResolveRefOpts{
		StateRoot: stateRoot,
		Input:     ref,
	})
	if err != nil {
		return fmt.Errorf("demote: %w", err)
	}
	if !resolved.Installed {
		return fmt.Errorf("demote: ref %q resolves to non-installed agent", ref)
	}
	return demoteResolved(stateRoot, resolved.Ref)
}

// demoteResolved clears the promoted flag on an already-resolved agent ref.
func demoteResolved(stateRoot, canonicalRef string) error {
	manifestPath := filepath.Join(stateRoot, "agents", canonicalRef, "install-manifest.json")

	// Symlink protection.
	if err := validatePathNoSymlinks(manifestPath, stateRoot); err != nil {
		return fmt.Errorf("demote: %w", err)
	}

	// File locking for concurrent safety.
	lockPath := manifestPath + ".lock"
	m, unlock, err := lockAndLoadManifest(manifestPath, lockPath)
	if err != nil {
		return fmt.Errorf("demote: %w", err)
	}
	defer unlock()

	// Idempotent: if not promoted, do nothing (no audit event either).
	if !m.Promoted {
		return nil
	}

	// Determine the digest for the audit event.
	digest := m.LocalImageDigest
	if digest == "" {
		lock, err := loadLock(filepath.Join(stateRoot, "agents"), canonicalRef)
		if err == nil && lock != nil {
			digest = lock.ImageDigest
		}
	}

	m.Promoted = false
	m.PromotedAt = nil
	m.PromotedBy = ""

	// Audit-first approach for demote as well.
	if err := emitPromotionAudit(stateRoot, audit.EventTypePackageDemoted, canonicalRef, m.PublisherFingerprint, digest, ""); err != nil {
		return fmt.Errorf("demote: audit: %w", err)
	}

	if err := saveManifestAtomic(manifestPath, m); err != nil {
		return fmt.Errorf("demote: %w", err)
	}

	return nil
}

// lockAndLoadManifest acquires a POSIX advisory lock on the lock file,
// then reads and parses the manifest. The lock file (separate from the
// manifest) allows atomic rename to replace the manifest while the lock
// remains valid on the same inode.
// Returns the parsed manifest and an unlock function.
func lockAndLoadManifest(manifestPath, lockPath string) (*install.InstallManifest, func(), error) {
	// Create/open the lock file.
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open lock file: %w", err)
	}

	// Acquire exclusive lock (blocking) on the lock file.
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return nil, nil, fmt.Errorf("lock manifest: %w", err)
	}

	unlock := func() {
		_ = lockFile.Close()
	}

	// Read the manifest while holding the lock.
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		unlock()
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) == 0 {
		unlock()
		return nil, nil, fmt.Errorf("empty manifest")
	}

	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		unlock()
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}

	return &m, unlock, nil
}

// saveManifestAtomic writes the manifest to a temp file and atomically
// renames it over the target. The lock file (separate inode) ensures
// mutual exclusion even across renames. Readers using os.ReadFile see
// either the old or new content, never a partial write.
func saveManifestAtomic(path string, m *install.InstallManifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	// Write to a temp file on the same filesystem, then rename atomically.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write manifest tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
}

// validatePathNoSymlinks verifies that neither the target file nor any
// parent directory component within stateRoot is a symlink, and that the
// resolved path after symlink evaluation stays within the stateRoot bounds.
func validatePathNoSymlinks(path, stateRoot string) error {
	clean := filepath.Clean(path)
	stateRootClean := filepath.Clean(stateRoot) + string(filepath.Separator)

	// Check each path component from leaf up to (but not including) stateRoot.
	for p := clean; strings.HasPrefix(p, stateRootClean); p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("lstat %q: %w", p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains symlink: %q", p)
		}
		if p == stateRootClean || p == filepath.Clean(stateRoot) {
			break
		}
	}

	// Verify that EvalSymlinks doesn't escape the expected tree.
	// Resolve stateRoot first (handles macOS /var -> /private/var).
	resolvedStateRoot, err := filepath.EvalSymlinks(stateRootClean)
	if err != nil {
		// stateRoot might not exist in all code paths; that's ok.
		resolvedStateRoot = stateRootClean
	}
	resolvedStateRoot = filepath.Clean(resolvedStateRoot) + string(filepath.Separator)

	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if os.IsNotExist(err) {
			resolved, err = filepath.EvalSymlinks(filepath.Dir(clean))
			if err != nil {
				return fmt.Errorf("eval symlinks parent: %w", err)
			}
		} else {
			return fmt.Errorf("eval symlinks: %w", err)
		}
	}
	resolved = filepath.Clean(resolved)

	if !strings.HasPrefix(resolved, resolvedStateRoot) && resolved != filepath.Clean(resolvedStateRoot) {
		return fmt.Errorf("resolved path %q escapes state root %q", resolved, resolvedStateRoot)
	}

	return nil
}

// emitPromotionAudit opens the audit JSONL at stateRoot/audit.jsonl and appends
// a package_promoted or package_demoted event.
func emitPromotionAudit(stateRoot, eventType, agentRef, fingerprint, digest, actor string) error {
	auditPath := filepath.Join(stateRoot, "audit.jsonl")
	w, err := audit.NewAuditWriter(auditPath)
	if err != nil {
		return fmt.Errorf("open audit writer: %w", err)
	}
	defer func() { _ = w.Close() }()

	rec := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          actor,
		Payload: map[string]interface{}{
			"agent_ref":   agentRef,
			"fingerprint": fingerprint,
			"digest":      digest,
			"actor":       actor,
		},
	}

	if err := w.Append(rec); err != nil {
		return fmt.Errorf("append audit record: %w", err)
	}

	return nil
}
