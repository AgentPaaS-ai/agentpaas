// Package registry provides a read API over the local package registry.
// It joins installed-agent state, deployment records, aliases, and lockfile
// capability metadata into deterministic RegistryEntry views.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// MaxListEntries bounds the output of ListEntries to prevent unbounded
// expansion when many agents are installed.
const MaxListEntries = 100

// DeploymentStoreReader is the subset of routedrun.DeploymentStore needed
// by the registry read API.
type DeploymentStoreReader interface {
	ListDeployments(ctx context.Context) ([]*routedrun.DeploymentRecord, error)
	GetDeployment(ctx context.Context, deploymentID routedrun.DeploymentID) (*routedrun.DeploymentRecord, error)
	ListAliases(ctx context.Context) ([]*routedrun.AliasRecord, error)
}

// RegistryEntry is a joined view of installed agent, deployment, alias,
// and capability data. It is NOT a new store; it is built from existing
// B23 installed-agent state and B26 deployment state.
type RegistryEntry struct {
	// Package identity.
	Ref                  string `json:"ref"`                  // name@pub8
	Name                 string `json:"name"`                 // agent name
	Pub8                 string `json:"pub8"`                 // first 8 hex chars of publisher fingerprint
	Version              string `json:"version"`              // agent version
	PublisherName        string `json:"publisher_name"`       // publisher display name
	PublisherFingerprint string `json:"publisher_fingerprint"` // full publisher fingerprint

	// Package digests from the installed lockfile.
	PackageDigest string `json:"package_digest"` // image digest from agent.lock
	PolicyDigest  string `json:"policy_digest"`  // policy digest from agent.lock

	// Install metadata.
	InstallMode      string    `json:"install_mode"`       // prebuilt-image or local-rebuild
	LocalImageDigest string    `json:"local_image_digest"` // local image digest from manifest
	InstalledAt      time.Time `json:"installed_at"`       // installation time
	CredentialIDs    []string  `json:"credential_ids,omitempty"` // credential map keys only (no values)
	Alias            string    `json:"alias,omitempty"`    // display alias

	// Deployment info (joined from B26 deployment store).
	DeploymentID     *string `json:"deployment_id,omitempty"`     // deployment ID if one exists for this package
	DeploymentStatus string  `json:"deployment_status,omitempty"` // ACTIVE or INACTIVE
	Generation       int64   `json:"generation,omitempty"`        // deployment generation
	BundleDigest     string  `json:"bundle_digest,omitempty"`     // bundle digest from deployment
	Aliases          []string `json:"aliases_deployment,omitempty"` // deployment aliases pointing to this package

	// Promotion (B31-T01).
	Promoted   bool       `json:"promoted"`
	PromotedAt *time.Time `json:"promoted_at,omitempty"`
	PromotedBy string     `json:"promoted_by,omitempty"`

	// Declared capabilities from the signed package manifest (verbatim, not schema-matched in v0.3).
	Capabilities []pack.DeclaredCapability `json:"capabilities,omitempty"`
}

// ListEntries returns every installed package with joined deployment,
// alias, and promotion data. Results are sorted by name asc, then version asc.
// Output is bounded to MaxListEntries.
func ListEntries(stateRoot string, store DeploymentStoreReader) ([]RegistryEntry, error) {
	installed, err := install.ListInstalledAgents(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}

	// Build a map of deployment records by package name + version.
	var deps []*routedrun.DeploymentRecord
	if store != nil {
		var err error
		deps, err = store.ListDeployments(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list entries: list deployments: %w", err)
		}
	}
	depMap := make(map[string][]*routedrun.DeploymentRecord) // key: name@version
	for _, d := range deps {
		if d == nil {
			continue
		}
		key := d.PackageName + "@" + d.PackageVersion
		depMap[key] = append(depMap[key], d)
	}

	// Build alias map: alias -> deployment ID.
	var aliasList []*routedrun.AliasRecord
	if store != nil {
		var err error
		aliasList, err = store.ListAliases(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list entries: list aliases: %w", err)
		}
	}
	aliasToDepID := make(map[string]string) // alias -> deployment ID
	depIDToAliases := make(map[string][]string)
	for _, a := range aliasList {
		if a == nil {
			continue
		}
		aliasToDepID[a.Alias] = string(a.TargetDeploymentID)
		depIDToAliases[string(a.TargetDeploymentID)] = append(depIDToAliases[string(a.TargetDeploymentID)], a.Alias)
	}

	var entries []RegistryEntry
	agentsDir := filepath.Join(stateRoot, "agents")

	for _, inst := range installed {
		if inst.Ref == "" {
			continue
		}
		name, pub8, ok := parseRef(inst.Ref)
		if !ok {
			continue
		}

		entry := RegistryEntry{
			Ref:         inst.Ref,
			Name:        name,
			Pub8:        pub8,
			Version:     inst.Version,
			PublisherName: inst.Publisher,
			InstalledAt:   inst.InstalledAt,
			InstallMode:   inst.Mode,
			Alias:         inst.Alias,
		}

		// Load full manifest for additional fields.
		m, err := loadManifest(agentsDir, inst.Ref)
		if err == nil && m != nil {
			entry.PublisherFingerprint = m.PublisherFingerprint
			entry.LocalImageDigest = m.LocalImageDigest
			entry.Promoted = m.Promoted
			entry.PromotedAt = m.PromotedAt
			entry.PromotedBy = m.PromotedBy
			// Credential IDs only — never the secret store values.
			for k := range m.CredentialMap {
				entry.CredentialIDs = append(entry.CredentialIDs, k)
			}
			sort.Strings(entry.CredentialIDs)
		}

		// Load lockfile for package/policy digests and capabilities.
		lock, err := loadLock(agentsDir, inst.Ref)
		if err == nil && lock != nil {
			entry.PackageDigest = lock.ImageDigest
			entry.PolicyDigest = lock.PolicyDigest
			if lock.Publisher != nil {
				if entry.PublisherName == "" {
					entry.PublisherName = lock.Publisher.Name
				}
				if entry.PublisherFingerprint == "" {
					entry.PublisherFingerprint = lock.Publisher.Fingerprint
				}
			}
			entry.Capabilities = lock.Capabilities
		}

		// Join deployment data.
		depKey := name + "@" + inst.Version
		if deps, ok := depMap[depKey]; ok && len(deps) > 0 {
			// Use the first (most recent) deployment.
			d := deps[len(deps)-1]
			depID := string(d.DeploymentID)
			entry.DeploymentID = &depID
			entry.DeploymentStatus = d.Status.String()
			entry.Generation = d.Generation
			entry.BundleDigest = d.BundleDigest

			// Collect aliases pointing to this deployment.
			if aliases, ok := depIDToAliases[depID]; ok {
				sort.Strings(aliases)
				entry.Aliases = aliases
			}
		}

		entries = append(entries, entry)
		if len(entries) >= MaxListEntries {
			break
		}
	}

	// Deterministic ordering: name asc, then version asc.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Version < entries[j].Version
	})

	return entries, nil
}

// ShowEntry returns the full registry entry for a single installed agent,
// looked up by name@pub8 or alias. Returns an error if the ref is not found
// or is ambiguous.
func ShowEntry(stateRoot, ref string, store DeploymentStoreReader) (*RegistryEntry, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("empty reference")
	}

	// Reject refs containing path separators, null bytes, newlines, or ".."
	// to prevent path traversal attacks.
	if err := validateRefSafe(ref); err != nil {
		return nil, err
	}

	agentsDir := filepath.Join(stateRoot, "agents")

	// Try exact ref (name@pub8) first.
	if strings.Contains(ref, "@") {
		return showByExactRef(agentsDir, ref, store)
	}

	// Try alias.
	installed, err := install.ListInstalledAgents(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("show entry: %w", err)
	}

	// Try alias match.
	var aliasMatches []string
	for _, inst := range installed {
		// Load manifest to check alias.
		m, err := loadManifest(agentsDir, inst.Ref)
		if err != nil || m == nil {
			continue
		}
		if m.Alias == ref {
			aliasMatches = append(aliasMatches, inst.Ref)
		}
	}
	if len(aliasMatches) == 1 {
		return showByExactRef(agentsDir, aliasMatches[0], store)
	}
	if len(aliasMatches) > 1 {
		return nil, fmt.Errorf("ambiguous alias %q matches %d installed agents: %s",
			ref, len(aliasMatches), strings.Join(aliasMatches, ", "))
	}

	// Try bare name match.
	var nameMatches []string
	for _, inst := range installed {
		name, _, ok := parseRef(inst.Ref)
		if !ok {
			continue
		}
		if name == ref {
			nameMatches = append(nameMatches, inst.Ref)
		}
	}
	switch len(nameMatches) {
	case 0:
		return nil, fmt.Errorf("no installed agent found for %q", ref)
	case 1:
		return showByExactRef(agentsDir, nameMatches[0], store)
	default:
		return nil, fmt.Errorf("ambiguous name %q; candidates: %s. Use name@pub8 or alias",
			ref, strings.Join(nameMatches, ", "))
	}
}

func showByExactRef(agentsDir, ref string, store DeploymentStoreReader) (*RegistryEntry, error) {
	m, err := loadManifest(agentsDir, ref)
	if err != nil || m == nil {
		return nil, fmt.Errorf("agent %q: %w", ref, err)
	}

	name, pub8, ok := parseRef(ref)
	if !ok {
		return nil, fmt.Errorf("invalid ref %q", ref)
	}

	entry := &RegistryEntry{
		Ref:                  ref,
		Name:                 name,
		Pub8:                 pub8,
		Version:              m.AgentVersion,
		PublisherName:        m.PublisherName,
		PublisherFingerprint: m.PublisherFingerprint,
		InstallMode:          m.InstallMode,
		LocalImageDigest:     m.LocalImageDigest,
		InstalledAt:          m.InstalledAt,
		Alias:                m.Alias,
		Promoted:             m.Promoted,
		PromotedAt:           m.PromotedAt,
		PromotedBy:           m.PromotedBy,
	}

	// Credential IDs only.
	for k := range m.CredentialMap {
		entry.CredentialIDs = append(entry.CredentialIDs, k)
	}
	sort.Strings(entry.CredentialIDs)

	// Load lockfile.
	lock, err := loadLock(agentsDir, ref)
	if err == nil && lock != nil {
		entry.PackageDigest = lock.ImageDigest
		entry.PolicyDigest = lock.PolicyDigest
		if lock.Publisher != nil {
			if entry.PublisherName == "" {
				entry.PublisherName = lock.Publisher.Name
			}
			if entry.PublisherFingerprint == "" {
				entry.PublisherFingerprint = lock.Publisher.Fingerprint
			}
		}
		entry.Capabilities = lock.Capabilities
	}

	// Join deployment data.
	if store != nil {
		deps, err := store.ListDeployments(context.Background())
		if err == nil {
			for _, d := range deps {
				if d == nil {
					continue
				}
				if d.PackageName == name && d.PackageVersion == m.AgentVersion {
					depID := string(d.DeploymentID)
					entry.DeploymentID = &depID
					entry.DeploymentStatus = d.Status.String()
					entry.Generation = d.Generation
					entry.BundleDigest = d.BundleDigest
					break
				}
			}
		}
	}

	// Join aliases if we have a deployment ID.
	if entry.DeploymentID != nil && store != nil {
		aliases, err := store.ListAliases(context.Background())
		if err == nil {
			for _, a := range aliases {
				if a == nil {
					continue
				}
				if string(a.TargetDeploymentID) == *entry.DeploymentID {
					entry.Aliases = append(entry.Aliases, a.Alias)
				}
			}
			sort.Strings(entry.Aliases)
		}
	}

	return entry, nil
}

func loadManifest(agentsDir, ref string) (*install.InstallManifest, error) {
	if err := validateRefSafe(ref); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(agentsDir, ref, "install-manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m install.InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

func loadLock(agentsDir, ref string) (*pack.AgentLock, error) {
	if err := validateRefSafe(ref); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(agentsDir, ref, "agent.lock")
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("read lock: %w", err)
	}
	var lock pack.AgentLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}
	return &lock, nil
}

// validateRefSafe rejects refs containing path separators, null bytes,
// newlines, or ".." components that could enable path traversal.
func validateRefSafe(ref string) error {
	if strings.Contains(ref, "/") || strings.Contains(ref, "\\") {
		return fmt.Errorf("invalid ref %q: contains path separator", ref)
	}
	if strings.Contains(ref, "\x00") {
		return fmt.Errorf("invalid ref %q: contains null byte", ref)
	}
	if strings.Contains(ref, "\n") || strings.Contains(ref, "\r") {
		return fmt.Errorf("invalid ref %q: contains newline", ref)
	}
	// Check for ".." as a path component (not as part of a name like "foo..bar").
	// Since we already reject "/" and "\\", ".." anywhere in the ref is dangerous.
	if strings.Contains(ref, "..") {
		return fmt.Errorf("invalid ref %q: contains parent directory traversal", ref)
	}
	return nil
}

func parseRef(ref string) (name, pub8 string, ok bool) {
	parts := strings.SplitN(ref, "@", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
