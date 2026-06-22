package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

const (
	deployedLockName         = "agent.lock"
	deployedImageDigestName  = "image.digest"
	deployedAtName           = "deployed_at"
	deployedSourceDigestName = "source_digest"
	deployedLockHashName     = "agent.lock.sha256"
)

// DeployedAgentPath returns the deployed agent state directory.
func DeployedAgentPath(homeDir, agentName string) string {
	return filepath.Join(homeDir, "state", "agents", agentName)
}

// DeployedAgent is the metadata of a deployed agent on disk.
type DeployedAgent struct {
	AgentName    string    `json:"agent_name"`
	ImageDigest  string    `json:"image_digest"`
	SourceDigest string    `json:"source_digest"`
	LockfileSig  string    `json:"lockfile_signature"`
	DeployedAt   time.Time `json:"deployed_at"`
}

// ErrImmutableViolation is returned when an in-place mutation of a deployed
// agent artifact is attempted.
var ErrImmutableViolation = errors.New("immutable violation: deployed agent artifacts cannot be modified in place")

// LoadDeployedAgent reads the deployed agent metadata from disk.
// Returns os.ErrNotExist if the agent was never deployed.
func LoadDeployedAgent(homeDir, agentName string) (*DeployedAgent, error) {
	if err := validateDeployedAgentInput(homeDir, agentName); err != nil {
		return nil, err
	}

	deployedDir := DeployedAgentPath(homeDir, agentName)
	if err := rejectSymlinkPath(deployedDir, false); err != nil {
		return nil, err
	}

	lock, err := readDeployedLock(deployedDir)
	if err != nil {
		return nil, err
	}
	imageDigest, err := readDeployedTextFile(deployedDir, deployedImageDigestName)
	if err != nil {
		return nil, err
	}
	sourceDigest, err := readDeployedTextFile(deployedDir, deployedSourceDigestName)
	if err != nil {
		return nil, err
	}
	deployedAtText, err := readDeployedTextFile(deployedDir, deployedAtName)
	if err != nil {
		return nil, err
	}
	deployedAt, err := time.Parse(time.RFC3339Nano, deployedAtText)
	if err != nil {
		return nil, fmt.Errorf("parse deployed_at: %w", err)
	}

	return &DeployedAgent{
		AgentName:    lock.AgentName,
		ImageDigest:  imageDigest,
		SourceDigest: sourceDigest,
		LockfileSig:  lock.LockfileSignature,
		DeployedAt:   deployedAt,
	}, nil
}

// VerifyDeployedIntegrity checks that the deployed agent.lock and image.digest
// on disk have NOT been modified since deployment.
func VerifyDeployedIntegrity(homeDir, agentName string, auditAppender audit.AuditAppender) error {
	if err := validateDeployedAgentInput(homeDir, agentName); err != nil {
		return err
	}

	deployedDir := DeployedAgentPath(homeDir, agentName)
	deployed, err := LoadDeployedAgent(homeDir, agentName)
	if err != nil {
		return err
	}
	lock, err := readDeployedLock(deployedDir)
	if err != nil {
		return err
	}
	imageDigest, err := readDeployedTextFile(deployedDir, deployedImageDigestName)
	if err != nil {
		return err
	}
	storedLockHash, err := readDeployedTextFile(deployedDir, deployedLockHashName)
	if err != nil {
		return err
	}
	lockHash, err := hashDeployedFile(filepath.Join(deployedDir, deployedLockName))
	if err != nil {
		return err
	}

	if lockHash != storedLockHash {
		return immutableViolation(auditAppender, agentName, "agent.lock_hash", storedLockHash, lockHash)
	}
	if err := VerifyLockfileSignature(lock); err != nil {
		return immutableViolation(auditAppender, agentName, "agent.lock_signature", "valid", err.Error())
	}
	if lock.LockfileSignature != deployed.LockfileSig {
		return immutableViolation(auditAppender, agentName, "lockfile_signature", deployed.LockfileSig, lock.LockfileSignature)
	}
	if lock.ImageDigest != deployed.ImageDigest {
		return immutableViolation(auditAppender, agentName, "agent.lock image_digest", deployed.ImageDigest, lock.ImageDigest)
	}
	if lock.BuildInputDigest != deployed.SourceDigest {
		return immutableViolation(auditAppender, agentName, "agent.lock build_input_digest", deployed.SourceDigest, lock.BuildInputDigest)
	}
	if imageDigest != deployed.ImageDigest {
		return immutableViolation(auditAppender, agentName, "image.digest", deployed.ImageDigest, imageDigest)
	}

	return nil
}

// RecordDeployment writes the deployed agent metadata to disk atomically.
func RecordDeployment(homeDir, agentName string, lock *AgentLock) error {
	if lock == nil {
		return errors.New("lock must not be nil")
	}
	if err := validateDeployedAgentInput(homeDir, agentName); err != nil {
		return err
	}
	if strings.TrimSpace(lock.AgentName) != agentName {
		return fmt.Errorf("lock agent name %q does not match deployed agent %q", lock.AgentName, agentName)
	}

	deployedDir := DeployedAgentPath(homeDir, agentName)
	if err := ensureDeployedDir(homeDir, agentName); err != nil {
		return err
	}

	lockContent, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		return fmt.Errorf("marshal deployed agent.lock: %w", err)
	}
	lockHash := sha256.Sum256(lockContent)

	if err := atomicWriteDeployedFile(filepath.Join(deployedDir, deployedLockName), lockContent, 0o600); err != nil {
		return err
	}
	if err := atomicWriteDeployedFile(filepath.Join(deployedDir, deployedImageDigestName), []byte(lock.ImageDigest+"\n"), 0o600); err != nil {
		return err
	}
	if err := atomicWriteDeployedFile(filepath.Join(deployedDir, deployedAtName), []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		return err
	}
	if err := atomicWriteDeployedFile(filepath.Join(deployedDir, deployedSourceDigestName), []byte(lock.BuildInputDigest+"\n"), 0o600); err != nil {
		return err
	}
	if err := atomicWriteDeployedFile(filepath.Join(deployedDir, deployedLockHashName), []byte(hex.EncodeToString(lockHash[:])+"\n"), 0o600); err != nil {
		return err
	}

	return nil
}

// IsDeployed returns true if the agent has been deployed.
func IsDeployed(homeDir, agentName string) bool {
	if err := validateDeployedAgentInput(homeDir, agentName); err != nil {
		return false
	}
	path := filepath.Join(DeployedAgentPath(homeDir, agentName), deployedLockName)
	if err := rejectSymlinkPath(path, false); err != nil {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func validateDeployedAgentInput(homeDir, agentName string) error {
	if strings.TrimSpace(homeDir) == "" {
		return errors.New("home directory is required")
	}
	if !filepath.IsAbs(homeDir) {
		return fmt.Errorf("home directory must be absolute: %s", homeDir)
	}
	if filepath.Clean(homeDir) != homeDir {
		return fmt.Errorf("home directory must be clean: %s", homeDir)
	}
	if strings.Contains(homeDir, "..") {
		return fmt.Errorf("home directory must not contain '..': %s", homeDir)
	}
	if strings.TrimSpace(agentName) == "" {
		return errors.New("agent name is required")
	}
	if strings.ContainsAny(agentName, "\x00\n\r[]/\\") || strings.Contains(agentName, "..") {
		return fmt.Errorf("invalid agent name %q", agentName)
	}
	if err := validateSecurePath(homeDir, true); err != nil {
		return err
	}
	return nil
}

func ensureDeployedDir(homeDir, agentName string) error {
	stateDir := filepath.Join(homeDir, "state")
	agentsDir := filepath.Join(stateDir, "agents")
	deployedDir := filepath.Join(agentsDir, agentName)
	for _, dir := range []string{stateDir, agentsDir, deployedDir} {
		if err := validateSecurePath(dir, false); err != nil {
			return err
		}
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := rejectSymlinkPath(dir, false); err != nil {
			return err
		}
	}
	return nil
}

func readDeployedLock(deployedDir string) (*AgentLock, error) {
	path := filepath.Join(deployedDir, deployedLockName)
	if err := rejectSymlinkPath(path, false); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deployed agent.lock: %w", err)
	}
	var lock AgentLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse deployed agent.lock: %w", err)
	}
	return &lock, nil
}

func readDeployedTextFile(deployedDir, name string) (string, error) {
	path := filepath.Join(deployedDir, name)
	if err := rejectSymlinkPath(path, false); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read deployed %s: %w", name, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func hashDeployedFile(path string) (string, error) {
	if err := rejectSymlinkPath(path, false); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read deployed file for hash: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func atomicWriteDeployedFile(path string, data []byte, perm os.FileMode) error {
	if err := validateSecurePath(path, false); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := rejectSymlinkPath(dir, false); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := rejectSymlinkPath(tmpPath, false); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("close temp file after symlink rejection: %w", closeErr)
		}
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("close temp file after write failure: %w", closeErr)
		}
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("close temp file after chmod failure: %w", closeErr)
		}
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	removeTemp = false
	return nil
}

func immutableViolation(auditAppender audit.AuditAppender, agentName, field, expected, actual string) error {
	record := audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      audit.EventTypeImmutableViolation,
		DeploymentMode: "local",
		Actor:          "agentpaas",
		Payload: map[string]interface{}{
			"agent_name": agentName,
			"field":      field,
			"expected":   expected,
			"actual":     actual,
		},
	}
	if auditAppender != nil {
		if err := auditAppender.Append(record); err != nil {
			return fmt.Errorf("%w: append immutable violation audit: %w", ErrImmutableViolation, err)
		}
	}
	return fmt.Errorf("%w: %s changed", ErrImmutableViolation, field)
}
