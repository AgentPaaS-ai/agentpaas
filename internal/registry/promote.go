package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	m, err := loadManifestByPath(manifestPath)
	if err != nil {
		return fmt.Errorf("promote: %w", err)
	}

	// Idempotent: if already promoted, don't change timestamp or actor.
	if m.Promoted {
		return nil
	}

	now := time.Now().UTC()
	m.Promoted = true
	m.PromotedAt = &now
	m.PromotedBy = actor

	if err := saveManifest(manifestPath, m); err != nil {
		return fmt.Errorf("promote: %w", err)
	}

	// Emit audit event.
	if err := emitPromotionAudit(stateRoot, audit.EventTypePackagePromoted, canonicalRef, m.PublisherFingerprint, m.LocalImageDigest, actor); err != nil {
		return fmt.Errorf("promote: audit: %w", err)
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

	m, err := loadManifestByPath(manifestPath)
	if err != nil {
		return fmt.Errorf("demote: %w", err)
	}

	// Idempotent: if not promoted, do nothing (no audit event either).
	if !m.Promoted {
		return nil
	}

	m.Promoted = false
	m.PromotedAt = nil
	m.PromotedBy = ""

	if err := saveManifest(manifestPath, m); err != nil {
		return fmt.Errorf("demote: %w", err)
	}

	// Emit audit event (actor is empty for demote — it's an operational action).
	if err := emitPromotionAudit(stateRoot, audit.EventTypePackageDemoted, canonicalRef, m.PublisherFingerprint, m.LocalImageDigest, ""); err != nil {
		return fmt.Errorf("demote: audit: %w", err)
	}

	return nil
}

// loadManifestByPath reads and parses an InstallManifest from the given path.
func loadManifestByPath(path string) (*install.InstallManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// saveManifest writes an InstallManifest back to disk with secure permissions.
func saveManifest(path string, m *install.InstallManifest) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
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
