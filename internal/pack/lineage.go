package pack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const (
	lineageFileName          = "lineage.json"
	maxLineageFileBytes      = 1 << 20 // 1 MiB
	maxProvenanceChainLength = 32
	errLineageCorrupt        = "lineage.json corrupt; re-fork or delete to pack as original"
	errForkPackNeedsIdentity = "fork pack requires publisher identity; run 'agent identity init' first"
	errProvenanceChainCap    = "provenance chain exceeds 32-entry cap; publish as original with attribution"
)

// ErrLineageNotFound indicates lineage.json is absent from the project directory.
var ErrLineageNotFound = errors.New("lineage.json not found")

// LineageFile is the fork lineage record written beside an editable project.
type LineageFile struct {
	Version  int           `json:"version"`
	Parent   LineageParent `json:"parent"`
	ForkedAt string        `json:"forked_at"`
}

// LineageParent captures installed parent lock metadata for fork-aware pack.
type LineageParent struct {
	AgentName            string            `json:"agent_name"`
	AgentVersion         string            `json:"agent_version"`
	PublisherFingerprint string            `json:"publisher_fingerprint"`
	PublisherName        string            `json:"publisher_name"`
	LockDigest           string            `json:"lock_digest"`
	BundleDigest         string            `json:"bundle_digest"`
	PolicyDigest         string            `json:"policy_digest"`
	PolicyYAMLB64        string            `json:"policy_yaml_b64"`
	Provenance           []ProvenanceEntry `json:"provenance"`
}

// ReadLineage parses lineage.json from projectDir when present.
func ReadLineage(projectDir string) (*LineageFile, error) {
	if strings.TrimSpace(projectDir) == "" {
		return nil, ErrLineageNotFound
	}
	path := filepath.Join(projectDir, lineageFileName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrLineageNotFound
		}
		return nil, err
	}
	if info.Size() > maxLineageFileBytes {
		return nil, fmt.Errorf("lineage file exceeds %d byte cap", maxLineageFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var lineage LineageFile
	if err := dec.Decode(&lineage); err != nil {
		return nil, fmt.Errorf("decode lineage: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("lineage file contains trailing JSON values")
		}
		return nil, fmt.Errorf("decode lineage: %w", err)
	}
	if lineage.Version != 1 {
		return nil, fmt.Errorf("unsupported lineage version %d", lineage.Version)
	}
	return &lineage, nil
}

// VerifyLineageParentProvenance checks structural rules and signatures for the
// embedded parent provenance chain. The last entry signer must match
// parent.PublisherFingerprint (parent lock tail rule, not the new lock).
func VerifyLineageParentProvenance(parent *LineageParent) error {
	if parent == nil {
		return errors.New("lineage parent is nil")
	}
	entries := parent.Provenance
	if len(entries) == 0 {
		return errors.New("parent provenance is empty")
	}
	if entries[0].Action != "created" {
		return fmt.Errorf("entry[0]: action must be \"created\", got %q", entries[0].Action)
	}
	if entries[0].ParentLockDigest != "" || entries[0].ParentBundleDigest != "" || entries[0].ParentPolicyDigest != "" {
		return errors.New("entry[0]: created entry must have empty parent digests")
	}
	for i := 1; i < len(entries); i++ {
		e := &entries[i]
		if e.Action != "forked" {
			return fmt.Errorf("entry[%d]: action must be \"forked\", got %q", i, e.Action)
		}
		if e.ParentLockDigest == "" {
			return fmt.Errorf("entry[%d]: forked entry must have non-empty parent_lock_digest", i)
		}
	}
	lastIdx := len(entries) - 1
	if parent.PublisherFingerprint != "" {
		if entries[lastIdx].PublisherFingerprint != parent.PublisherFingerprint {
			return fmt.Errorf("entry[%d]: last signer fingerprint %q does not match parent publisher fingerprint %q",
				lastIdx, entries[lastIdx].PublisherFingerprint, parent.PublisherFingerprint)
		}
	}
	for i := range entries {
		if err := verifyEntrySignatureAndFingerprint(&entries[i]); err != nil {
			return fmt.Errorf("entry[%d]: %w", i, err)
		}
	}
	return nil
}

func policyDeltaFromPolicy(d *policy.PolicyDelta) *PolicyDelta {
	if d == nil {
		return nil
	}
	return &PolicyDelta{
		EgressAdded:        d.EgressAdded,
		EgressRemoved:      d.EgressRemoved,
		CredentialsAdded:   d.CredentialsAdded,
		CredentialsRemoved: d.CredentialsRemoved,
		MCPToolsAdded:      d.MCPToolsAdded,
		MCPToolsRemoved:    d.MCPToolsRemoved,
		ModelRoutesAdded:   d.ModelRoutesAdded,
		ModelRoutesRemoved: d.ModelRoutesRemoved,
		RoutedRunChanged:   d.RoutedRunChanged,
	}
}