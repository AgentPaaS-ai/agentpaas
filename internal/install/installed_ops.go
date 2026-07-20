package install

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

const installedLocalImageDigestFile = "local_image.digest"
const installedAgentRefLabel = AgentRefLabel

// InstalledAgentEntry is one row for `installed list`.
type InstalledAgentEntry struct {
	Ref         string    `json:"ref"`
	Alias       string    `json:"alias,omitempty"`
	Version     string    `json:"version"`
	Publisher   string    `json:"publisher"`
	InstalledAt time.Time `json:"installed_at"`
	Mode        string    `json:"mode"`
}

// ContainerStopper stops running containers for an installed agent ref.
type ContainerStopper interface {
	StopByAgentRef(ctx context.Context, agentRef string) error
}

// ListInstalledAgents scans a database value into StateRoot/agents/<name>@<pub8>/ only (not Phase-1 bare names).
func ListInstalledAgents(stateRoot string) ([]InstalledAgentEntry, error) {
	agentsDir := filepath.Join(stateRoot, installedAgentsDirName)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []InstalledAgentEntry
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name, pub8, ok := ParseInstalledAgentDir(ent.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(agentsDir, ent.Name())
		manifestPath := filepath.Join(dir, installedManifestName)
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var m InstallManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if !MatchPublisherPub8(m.PublisherFingerprint, pub8) || !strings.EqualFold(m.AgentName, name) {
			continue
		}
		ref := ent.Name()
		out = append(out, InstalledAgentEntry{
			Ref:         ref,
			Alias:       m.Alias,
			Version:     m.AgentVersion,
			Publisher:   m.PublisherName,
			InstalledAt: m.InstalledAt,
			Mode:        m.InstallMode,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

// RemoveInstalledAgent removes materialized state for ref (name@pub8 or alias). Trust pin is retained.
func RemoveInstalledAgent(ctx context.Context, stateRoot, ref string, stopper ContainerStopper, emitAudit func(eventType string, payload map[string]string)) error {
	resolvedRef, dir, err := resolveInstalledRef(stateRoot, ref)
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("no installed agent for reference %q", ref)
	}
	if stopper != nil {
		if err := stopper.StopByAgentRef(ctx, resolvedRef); err != nil {
			return fmt.Errorf("stop containers: %w", err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove installed state: %w", err)
	}
	if emitAudit != nil {
		emitAudit(audit.EventTypeInstallRemoved, map[string]string{
			"agent_ref": resolvedRef,
		})
	}
	return nil
}

func resolveInstalledRef(stateRoot, ref string) (resolvedRef, dir string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("empty install reference")
	}
	// Direct name@pub8
	if strings.Contains(ref, "@") {
		name, pub8, perr := naming.ParseAgentRef(ref)
		if perr != nil {
			return "", "", perr
		}
		dir, err = findInstalledDirByRef(stateRoot, name, pub8)
		if err != nil {
			return "", "", err
		}
		return name + "@" + pub8, dir, nil
	}
	// Alias lookup
	list, err := ListInstalledAgents(stateRoot)
	if err != nil {
		return "", "", err
	}
	var matches []InstalledAgentEntry
	for _, e := range list {
		if e.Alias == ref {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		return "", "", nil
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("ambiguous alias %q matches %d installed agents", ref, len(matches))
	}
	dir = filepath.Join(stateRoot, installedAgentsDirName, matches[0].Ref)
	return matches[0].Ref, dir, nil
}

func findInstalledDirByRef(stateRoot, name, pub8 string) (string, error) {
	pub8 = strings.ToLower(pub8)
	want := name + "@" + pub8
	dir := filepath.Join(stateRoot, installedAgentsDirName, want)
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", nil
	}
	// Confirm fingerprint in manifest matches pub8.
	manifestPath := filepath.Join(dir, installedManifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", nil
	}
	var m InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", nil
	}
	if !strings.EqualFold(m.AgentName, name) || !MatchPublisherPub8(m.PublisherFingerprint, pub8) {
		return "", nil
	}
	_ = trust.NormalizeFingerprint(m.PublisherFingerprint) // intentionally ignored (reviewed)
	return dir, nil
}

// LoadManifestByRef loads the install manifest for an installed agent ref (name@pub8).
func LoadManifestByRef(stateRoot, ref string) (*InstallManifest, error) {
	name, pub8, err := naming.ParseAgentRef(ref)
	if err != nil {
		return nil, err
	}
	dir, err := findInstalledDirByRef(stateRoot, name, pub8)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, fmt.Errorf("no installed agent for reference %q", ref)
	}
	m, err := loadInstalledManifest(dir)
	if err != nil {
		return nil, err
	}
	return &m, nil
}