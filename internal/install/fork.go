package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

const forkLineageFileName = "lineage.json"

// ErrForkRefused is returned when fork preconditions fail before materialization.
var ErrForkRefused = errors.New("fork refused")

// ForkLineage is the advisory fork lineage record written beside the project.
type ForkLineage struct {
	Version  int               `json:"version"`
	Parent   ForkLineageParent `json:"parent"`
	ForkedAt string            `json:"forked_at"`
}

// ForkLineageParent captures the installed parent lock metadata for pack (B24-T02).
type ForkLineageParent struct {
	AgentName            string                 `json:"agent_name"`
	AgentVersion         string                 `json:"agent_version"`
	PublisherFingerprint string                 `json:"publisher_fingerprint"`
	PublisherName        string                 `json:"publisher_name"`
	LockDigest           string                 `json:"lock_digest"`
	BundleDigest         string                 `json:"bundle_digest"`
	PolicyDigest         string                 `json:"policy_digest"`
	PolicyYAMLB64        string                 `json:"policy_yaml_b64"`
	Provenance           []pack.ProvenanceEntry `json:"provenance"`
}

// ForkInstalled materializes an installed agent into an editable project directory.
func ForkInstalled(stateRoot, ref, targetDir string, auditAppender audit.AuditAppender) error {
	resolved, err := ResolveAgentRef(ResolveRefOpts{StateRoot: stateRoot, Input: ref})
	if err != nil {
		return fmt.Errorf("fork installed: %w", err)
	}
	if resolved == nil || !resolved.Installed {
		return fmt.Errorf("%w: no installed agent at %q", ErrForkRefused, ref)
	}
	parentRef := resolved.Ref

	if err := VerifyInstalledAgent(stateRoot, parentRef, auditAppender); err != nil {
		return fmt.Errorf("fork installed: %w", err)
	}

	targetAbs, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("%w: target dir: %w", ErrForkRefused, err)
	}
	if err := assertForkTargetEmpty(targetAbs); err != nil {
		return fmt.Errorf("fork installed: %w", err)
	}

	name, pub8, err := naming.ParseAgentRef(parentRef)
	if err != nil {
		return fmt.Errorf("fork installed: %w", err)
	}
	installedDir, err := findInstalledDirByRef(stateRoot, name, pub8)
	if err != nil {
		return fmt.Errorf("fork installed: %w", err)
	}
	if installedDir == "" {
		return fmt.Errorf("%w: no installed agent at %q", ErrForkRefused, ref)
	}

	lockPath := filepath.Join(installedDir, installedLockName)
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		return fmt.Errorf("%w: read lock: %w", ErrForkRefused, err)
	}

	policyPath := filepath.Join(installedDir, installedPolicyName)
	policyYAML, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("%w: read policy: %w", ErrForkRefused, err)
	}

	parentBundlePath := filepath.Join(installedDir, installedParentBundleRef)
	parentBundleRaw, err := os.ReadFile(parentBundlePath)
	if err != nil {
		return fmt.Errorf("%w: read parent bundle ref: %w", ErrForkRefused, err)
	}
	var parentBundle ParentBundleRef
	if err := json.Unmarshal(parentBundleRaw, &parentBundle); err != nil {
		return fmt.Errorf("%w: parse parent bundle ref: %w", ErrForkRefused, err)
	}

	lineage := ForkLineage{
		Version: 1,
		Parent: ForkLineageParent{
			AgentName:     lock.AgentName,
			AgentVersion:  lock.AgentVersion,
			LockDigest:    pack.LockDigest(lock),
			BundleDigest:  parentBundle.Digest,
			PolicyDigest:  lock.PolicyDigest,
			PolicyYAMLB64: base64.StdEncoding.EncodeToString(policyYAML),
			Provenance:    append([]pack.ProvenanceEntry(nil), lock.Provenance...),
		},
		ForkedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if lock.Publisher != nil {
		lineage.Parent.PublisherFingerprint = lock.Publisher.Fingerprint
		lineage.Parent.PublisherName = lock.Publisher.Name
	}

	if err := os.MkdirAll(targetAbs, 0o700); err != nil {
		return fmt.Errorf("%w: mkdir target: %w", ErrForkRefused, err)
	}

	sourceRoot := filepath.Join(installedDir, installedSourceDir)
	if err := copyForkSourceTree(sourceRoot, targetAbs); err != nil {
		_ = os.RemoveAll(targetAbs) // best-effort remove
		return fmt.Errorf("%w: copy source: %w", ErrForkRefused, err)
	}
	if err := os.WriteFile(filepath.Join(targetAbs, installedPolicyName), policyYAML, 0o600); err != nil {
		_ = os.RemoveAll(targetAbs) // best-effort remove
		return fmt.Errorf("%w: write policy: %w", ErrForkRefused, err)
	}
	lineageRaw, err := json.Marshal(lineage)
	if err != nil {
		_ = os.RemoveAll(targetAbs) // best-effort remove
		return fmt.Errorf("%w: marshal lineage: %w", ErrForkRefused, err)
	}
	if err := os.WriteFile(filepath.Join(targetAbs, forkLineageFileName), lineageRaw, 0o600); err != nil {
		_ = os.RemoveAll(targetAbs) // best-effort remove
		return fmt.Errorf("%w: write lineage: %w", ErrForkRefused, err)
	}

	record := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeAgentForked,
		DeploymentMode: "local",
		Actor:          "agentpaas",
		Payload: map[string]interface{}{
			"parent_ref":         parentRef,
			"parent_lock_digest": lineage.Parent.LockDigest,
			"target_dir":         targetAbs,
		},
	}
	if auditAppender != nil {
		if err := auditAppender.Append(record); err != nil {
			log.Printf("install: audit append (%s): %v", "audit", err)
		}
	}
	return nil
}

func assertForkTargetEmpty(targetAbs string) error {
	info, err := os.Lstat(targetAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("%w: target dir: %w", ErrForkRefused, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: target dir %q is a symlink", ErrForkRefused, targetAbs)
	}
	entries, err := os.ReadDir(targetAbs)
	if err != nil {
		return fmt.Errorf("%w: target dir: %w", ErrForkRefused, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: target dir %q is not empty", ErrForkRefused, targetAbs)
	}
	return nil
}

func copyForkSourceTree(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("copy fork source tree: %w", err)
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(targetRoot, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", path)
		}
		if d.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("copy fork source tree: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("copy fork source tree: %w", err)
		}
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			mode = 0o600
		}
		if err := os.WriteFile(dest, data, mode); err != nil {
			return fmt.Errorf("copy fork source tree: %w", err)
		}
		return nil
	})
}

// ReadForkLineage parses lineage.json from a project directory.
func ReadForkLineage(projectDir string) (*ForkLineage, error) {
	raw, err := os.ReadFile(filepath.Join(projectDir, forkLineageFileName))
	if err != nil {
		return nil, fmt.Errorf("read fork lineage: %w", err)
	}
	var lineage ForkLineage
	if err := json.Unmarshal(raw, &lineage); err != nil {
		return nil, fmt.Errorf("read fork lineage: %w", err)
	}
	if lineage.Version != 1 {
		return nil, fmt.Errorf("unsupported lineage version %d", lineage.Version)
	}
	return &lineage, nil
}

// ForkPublisherWarning returns a stderr hint when the parent lock has no publisher block.
func ForkPublisherWarning(stateRoot, ref string) string {
	resolved, err := ResolveAgentRef(ResolveRefOpts{StateRoot: stateRoot, Input: ref})
	if err != nil || resolved == nil || !resolved.Installed {
		return ""
	}
	name, pub8, err := naming.ParseAgentRef(resolved.Ref)
	if err != nil {
		return ""
	}
	installedDir, err := findInstalledDirByRef(stateRoot, name, pub8)
	if err != nil || installedDir == "" {
		return ""
	}
	lock, err := pack.ReadAgentLock(filepath.Join(installedDir, installedLockName))
	if err != nil || lock.Publisher != nil {
		return ""
	}
	return "Warning: parent install has no publisher identity; fork-packing will require publisher identity init."
}

// ForkAgentNameFromRef extracts the agent name from an installed ref for display.
func ForkAgentNameFromRef(ref string) (string, error) {
	name, _, err := naming.ParseAgentRef(ref) // intentionally ignored (reviewed)
	if err != nil {
		if strings.Contains(ref, "@") {
			return "", fmt.Errorf("fork agent name from ref: %w", err)
		}
		return strings.TrimSpace(ref), nil
	}
	return name, nil
}
