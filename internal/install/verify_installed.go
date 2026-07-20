package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ErrInstallVerifyFailed is returned when post-install verification fails (rollback required).
var ErrInstallVerifyFailed = errors.New("post-install verification failed")

// VerifyInstalledAgent runs the B20 T03-equivalent checks for materialized installs.
// Image digest is checked against manifest.LocalImageDigest; source/policy against the signed lock.
func VerifyInstalledAgent(stateRoot, agentRef string, auditAppender audit.AuditAppender) error {
	name, pub8, err := parseInstalledRef(agentRef)
	if err != nil {
		return fmt.Errorf("verify installed agent: %w", err)
	}
	dir, err := findInstalledDirByRef(stateRoot, name, pub8)
	if err != nil {
		return fmt.Errorf("verify installed agent: %w", err)
	}
	if dir == "" {
		return fmt.Errorf("%w: no installed agent at %q", ErrInstallVerifyFailed, agentRef)
	}
	return verifyInstalledAtDir(dir, auditAppender, name)
}

func verifyInstalledAtDir(installedDir string, auditAppender audit.AuditAppender, agentName string) error {
	if err := rejectInstalledSymlinks(installedDir); err != nil {
		return fmt.Errorf("%w: %v", ErrInstallVerifyFailed, err)
	}
	lockPath := filepath.Join(installedDir, installedLockName)
	lock, err := pack.ReadAgentLock(lockPath)
	if err != nil {
		return fmt.Errorf("%w: read lock: %w", ErrInstallVerifyFailed, err)
	}
	if err := pack.VerifyLockfileSignature(lock); err != nil {
		return immutableInstalled(auditAppender, agentName, "agent.lock_signature", err)
	}
	manifestPath := filepath.Join(installedDir, installedManifestName)
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("%w: read install manifest: %w", ErrInstallVerifyFailed, err)
	}
	var manifest InstallManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return fmt.Errorf("%w: parse install manifest: %w", ErrInstallVerifyFailed, err)
	}
	if strings.TrimSpace(manifest.LocalImageDigest) == "" {
		return fmt.Errorf("%w: local_image_digest missing in manifest", ErrInstallVerifyFailed)
	}
	digestPath := filepath.Join(installedDir, installedLocalImageDigestFile)
	digestFile, err := os.ReadFile(digestPath)
	if err != nil {
		return fmt.Errorf("%w: read %s: %w", ErrInstallVerifyFailed, installedLocalImageDigestFile, err)
	}
	fileDigest := normalizeImageDigest(strings.TrimSpace(string(digestFile)))
	manifestDigest := normalizeImageDigest(manifest.LocalImageDigest)
	if fileDigest != manifestDigest {
		return immutableInstalled(auditAppender, agentName, "local_image_digest", fmt.Errorf("manifest %s file %s", manifestDigest, fileDigest))
	}
	policyPath := filepath.Join(installedDir, installedPolicyName)
	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("%w: read policy: %w", ErrInstallVerifyFailed, err)
	}
	if lock.PolicyDigest != "" {
		computed, err := pack.ComputePolicyDigest(policyData)
		if err != nil {
			return fmt.Errorf("%w: compute policy digest: %w", ErrInstallVerifyFailed, err)
		}
		if computed != lock.PolicyDigest {
			return immutableInstalled(auditAppender, agentName, "policy_digest", fmt.Errorf("expected %s got %s", lock.PolicyDigest, computed))
		}
	}
	sourceDir := filepath.Join(installedDir, installedSourceDir)
	buildDigest, err := pack.ComputeBuildInputDigest(sourceDir, nil)
	if err != nil {
		return fmt.Errorf("%w: compute source digest: %w", ErrInstallVerifyFailed, err)
	}
	if buildDigest != lock.BuildInputDigest {
		return immutableInstalled(auditAppender, agentName, "source_digest", fmt.Errorf("expected %s got %s", lock.BuildInputDigest, buildDigest))
	}
	// Image: local manifest digest only (rebuilds differ from lock.ImageDigest).
	localWant := normalizeImageDigest(manifest.LocalImageDigest)
	if localWant == "" {
		return fmt.Errorf("%w: invalid local_image_digest", ErrInstallVerifyFailed)
	}
	_ = lock.ImageDigest // signed publisher digest; not used for local rebuild verification.
	return nil
}

func immutableInstalled(auditAppender audit.AuditAppender, agentName, field string, cause error) error {
	record := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeImmutableViolation,
		DeploymentMode: "local",
		Actor:          "agentpaas",
		Payload: map[string]interface{}{
			"agent_name": agentName,
			"field":      field,
			"detail":     cause.Error(),
		},
	}
	if auditAppender != nil {
		if err := auditAppender.Append(record); err != nil {
			log.Printf("install: audit append (%s): %v", "audit", err)
		}
	}
	return fmt.Errorf("%w: %s: %v", ErrInstallVerifyFailed, field, cause)
}

func rejectInstalledSymlinks(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("reject installed symlinks: %w", err)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not allowed: %s", path)
		}
		return nil
	})
}

func parseInstalledRef(ref string) (name, pub8 string, err error) {
	name, pub8, err = naming.ParseAgentRef(ref)
	if err != nil {
		return "", "", fmt.Errorf("parse installed ref: %w", err)
	}
	if pub8 == "" {
		return "", "", fmt.Errorf("installed ref %q requires name@pub8", ref)
	}
	return name, pub8, nil
}
