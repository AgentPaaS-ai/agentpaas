package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
)

// PriorInstallRecord is the stored install state for update/downgrade/diff logic.
type PriorInstallRecord struct {
	Manifest   InstallManifest
	PolicyYAML []byte
}

// InstallStateStore persists per-(publisher fingerprint, agent name) install records.
type InstallStateStore interface {
	GetPriorInstall(publisherFingerprint, agentName string) (*PriorInstallRecord, error)
	SaveApprovedInstall(manifest InstallManifest, policyYAML []byte) error
	GetInstallByRef(ref string) (*PriorInstallRecord, error)
}

// FileInstallState stores install manifests under StateRoot/installs/<fp>/<agent>/.
type FileInstallState struct {
	StateRoot string
}

// GetPriorInstall loads the prior manifest and policy bytes if present.
func (s *FileInstallState) GetPriorInstall(publisherFingerprint, agentName string) (*PriorInstallRecord, error) {
	dir, err := s.installDir(publisherFingerprint, agentName)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	policyPath := filepath.Join(dir, "policy.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m InstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	pol, err := os.ReadFile(policyPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read policy: %w", err)
	}
	return &PriorInstallRecord{Manifest: m, PolicyYAML: pol}, nil
}

// SaveApprovedInstall writes manifest and policy after successful policy approval.
func (s *FileInstallState) SaveApprovedInstall(manifest InstallManifest, policyYAML []byte) error {
	dir, err := s.installDir(manifest.PublisherFingerprint, manifest.AgentName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir install state: %w", err)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	policyPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(policyPath, policyYAML, 0o600); err != nil {
		return fmt.Errorf("write policy: %w", err)
	}
	return nil
}

// GetInstallByRef loads an install record by agent reference name@pub8.
func (s *FileInstallState) GetInstallByRef(ref string) (*PriorInstallRecord, error) {
	name, pub8, err := naming.ParseAgentRef(ref)
	if err != nil {
		return nil, fmt.Errorf("install ref: %w", err)
	}
	if pub8 == "" {
		return nil, fmt.Errorf("install ref %q requires name@pub8", ref)
	}
	installsRoot := filepath.Join(s.StateRoot, "installs")
	entries, err := os.ReadDir(installsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, fpEnt := range entries {
		if !fpEnt.IsDir() {
			continue
		}
		agentDir := filepath.Join(installsRoot, fpEnt.Name(), sanitizePathSegment(name))
		manifestPath := filepath.Join(agentDir, "manifest.json")
		if _, err := os.Stat(manifestPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("read manifest: %w", err)
		}
		var m InstallManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		if !strings.EqualFold(m.AgentName, name) {
			continue
		}
		if !MatchPublisherPub8(m.PublisherFingerprint, pub8) {
			continue
		}
		pol, err := os.ReadFile(filepath.Join(agentDir, "policy.yaml"))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read policy: %w", err)
		}
		return &PriorInstallRecord{Manifest: m, PolicyYAML: pol}, nil
	}
	return nil, nil
}

func (s *FileInstallState) installDir(publisherFingerprint, agentName string) (string, error) {
	if s.StateRoot == "" {
		return "", fmt.Errorf("install state: empty StateRoot")
	}
	fp := sanitizePathSegment(publisherFingerprint)
	name := sanitizePathSegment(agentName)
	return filepath.Join(s.StateRoot, "installs", fp, name), nil
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, string(os.PathSeparator), "_")
	s = strings.ReplaceAll(s, "..", "_")
	if s == "" {
		return "_"
	}
	return s
}