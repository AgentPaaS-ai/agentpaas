package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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