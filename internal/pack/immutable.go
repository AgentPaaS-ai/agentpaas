package pack

import (
	"bytes"
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
	if err := verifyDeployedPath(auditAppender, agentName, "deployed directory", deployedDir); err != nil {
		return err
	}

	lockPath := filepath.Join(deployedDir, deployedLockName)
	if err := verifyDeployedPath(auditAppender, agentName, deployedLockName, lockPath); err != nil {
		return err
	}
	imageDigestPath := filepath.Join(deployedDir, deployedImageDigestName)
	if err := verifyDeployedPath(auditAppender, agentName, deployedImageDigestName, imageDigestPath); err != nil {
		return err
	}

	lockContent, err := readDeployedFile(deployedDir, deployedLockName)
	if err != nil {
		return err
	}
	storedLockHash, err := readDeployedTextFile(deployedDir, deployedLockHashName)
	if err != nil {
		return err
	}
	lockHash := sha256.Sum256(lockContent)
	actualLockHash := hex.EncodeToString(lockHash[:])
	if actualLockHash != storedLockHash {
		return immutableViolation(auditAppender, agentName, "agent.lock_hash", storedLockHash, actualLockHash)
	}

	var lock AgentLock
	if err := json.Unmarshal(lockContent, &lock); err != nil {
		return fmt.Errorf("parse deployed agent.lock: %w", err)
	}
	if err := VerifyLockfileSignature(&lock); err != nil {
		return immutableViolation(auditAppender, agentName, "agent.lock_signature", "valid", err.Error())
	}

	imageDigest, err := readDeployedTextFile(deployedDir, deployedImageDigestName)
	if err != nil {
		return err
	}
	sourceDigest, err := readDeployedTextFile(deployedDir, deployedSourceDigestName)
	if err != nil {
		return err
	}
	deployedAtText, err := readDeployedTextFile(deployedDir, deployedAtName)
	if err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, deployedAtText); err != nil {
		return fmt.Errorf("parse deployed_at: %w", err)
	}
	latestLockContent, err := readDeployedFile(deployedDir, deployedLockName)
	if err != nil {
		return err
	}
	if !bytes.Equal(lockContent, latestLockContent) {
		return immutableViolation(auditAppender, agentName, "agent.lock", "stable during verification", "changed during verification")
	}
	if imageDigest != lock.ImageDigest {
		return immutableViolation(auditAppender, agentName, "image.digest", lock.ImageDigest, imageDigest)
	}
	if sourceDigest != lock.BuildInputDigest {
		return immutableViolation(auditAppender, agentName, "source_digest", lock.BuildInputDigest, sourceDigest)
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
	agentsDir, err := ensureDeployedParentDir(homeDir)
	if err != nil {
		return err
	}

	lockContent, err := json.Marshal(lockCanonicalMap(lock, true))
	if err != nil {
		return fmt.Errorf("marshal deployed agent.lock: %w", err)
	}
	lockHash := sha256.Sum256(lockContent)

	stagingDir, err := os.MkdirTemp(agentsDir, "."+agentName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create deployment staging directory: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	if err := rejectSymlinkPath(stagingDir, false); err != nil {
		return err
	}

	files := map[string][]byte{
		deployedLockName:         lockContent,
		deployedImageDigestName:  []byte(lock.ImageDigest + "\n"),
		deployedAtName:           []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n"),
		deployedSourceDigestName: []byte(lock.BuildInputDigest + "\n"),
		deployedLockHashName:     []byte(hex.EncodeToString(lockHash[:]) + "\n"),
	}
	for name, data := range files {
		if err := writeStagedDeployedFile(stagingDir, name, data, 0o600); err != nil {
			return err
		}
	}
	if err := replaceDeployedDir(stagingDir, deployedDir); err != nil {
		return err
	}
	stagingActive = false

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

func ensureDeployedParentDir(homeDir string) (string, error) {
	stateDir := filepath.Join(homeDir, "state")
	agentsDir := filepath.Join(stateDir, "agents")
	for _, dir := range []string{stateDir, agentsDir} {
		if err := validateSecurePath(dir, false); err != nil {
			return "", err
		}
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create %s: %w", dir, err)
		}
		if err := rejectSymlinkPath(dir, false); err != nil {
			return "", err
		}
	}
	return agentsDir, nil
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

func readDeployedFile(deployedDir, name string) ([]byte, error) {
	path := filepath.Join(deployedDir, name)
	if err := rejectSymlinkPath(path, false); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read deployed %s: %w", name, err)
	}
	return data, nil
}

func readDeployedTextFile(deployedDir, name string) (string, error) {
	data, err := readDeployedFile(deployedDir, name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func verifyDeployedPath(auditAppender audit.AuditAppender, agentName, field, path string) error {
	if err := rejectSymlinkPath(path, false); err != nil {
		if strings.Contains(err.Error(), "symlink") {
			return immutableViolation(auditAppender, agentName, field, "not a symlink", err.Error())
		}
		return err
	}
	return nil
}

func writeStagedDeployedFile(dir, name string, data []byte, perm os.FileMode) error {
	path := filepath.Join(dir, name)
	if err := validateSecurePath(path, false); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir, false); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("write staged deployed %s: %w", name, err)
	}
	return nil
}

func replaceDeployedDir(stagingDir, deployedDir string) error {
	if err := validateSecurePath(deployedDir, false); err != nil {
		return err
	}
	if err := rejectSymlinkPath(stagingDir, false); err != nil {
		return err
	}

	info, err := os.Lstat(deployedDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stagingDir, deployedDir); err != nil {
			return fmt.Errorf("rename staged deployment into place: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect deployed directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("deployed path is not a directory: %s", deployedDir)
	}
	if err := rejectSymlinkPath(deployedDir, false); err != nil {
		return err
	}

	backupDir, err := os.MkdirTemp(filepath.Dir(deployedDir), "."+filepath.Base(deployedDir)+".previous-*")
	if err != nil {
		return fmt.Errorf("create deployment backup directory: %w", err)
	}
	if err := os.Remove(backupDir); err != nil {
		return fmt.Errorf("remove empty deployment backup directory: %w", err)
	}
	backupActive := false
	if err := os.Rename(deployedDir, backupDir); err != nil {
		return fmt.Errorf("move existing deployment aside: %w", err)
	}
	backupActive = true
	defer func() {
		if backupActive {
			_ = os.RemoveAll(backupDir)
		}
	}()

	if err := os.Rename(stagingDir, deployedDir); err != nil {
		restoreErr := os.Rename(backupDir, deployedDir)
		backupActive = restoreErr != nil
		if restoreErr != nil {
			return fmt.Errorf("rename staged deployment into place: %w; restore previous deployment: %w", err, restoreErr)
		}
		return fmt.Errorf("rename staged deployment into place: %w", err)
	}
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
