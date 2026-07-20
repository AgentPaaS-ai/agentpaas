package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type DirectLeaseConfig struct {
	Store      SecretStore
	Policy     *policy.Policy
	ActiveRuns []string
	Audit      AuditAppender
	Now        func() time.Time
	LeaseDir   string
	AgentUID   int
}

type DirectLease struct {
	store      SecretStore
	policy     *policy.Policy
	activeRuns map[string]struct{}
	audit      AuditAppender
	now        func() time.Time
	leaseDir   string
	agentUID   int
}

type LeaseHandle struct {
	FilePath string `json:"-"`

	audit        AuditAppender
	now          func() time.Time
	runID        string
	credentialID string
	policyRuleID string
	valid        bool
}

// NewDirectLease creates and returns a new direct lease.
//
// It returns an error if the operation fails or inputs are invalid.
func NewDirectLease(cfg DirectLeaseConfig) (*DirectLease, error) {
	if cfg.Store == nil {
		return nil, errors.New("direct lease requires a secret store")
	}
	if cfg.Policy == nil {
		return nil, errors.New("direct lease requires a policy")
	}
	leaseDir := cfg.LeaseDir
	if leaseDir == "" {
		var err error
		leaseDir, err = os.MkdirTemp("", "agentpaas-leases-*")
		if err != nil {
			return nil, fmt.Errorf("create direct lease directory: %w", err)
		}
	}
	realLeaseDir, err := filepath.EvalSymlinks(leaseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve direct lease directory: %w", err)
	}
	leaseDir = realLeaseDir
	if err := validateLeaseRoot(leaseDir); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	agentUID := cfg.AgentUID
	if agentUID == 0 {
		agentUID = os.Getuid()
	}
	activeRuns := make(map[string]struct{}, len(cfg.ActiveRuns))
	for _, runID := range cfg.ActiveRuns {
		activeRuns[runID] = struct{}{}
	}

	return &DirectLease{
		store:      cfg.Store,
		policy:     cfg.Policy,
		activeRuns: activeRuns,
		audit:      cfg.Audit,
		now:        now,
		leaseDir:   leaseDir,
		agentUID:   agentUID,
	}, nil
}

// DirectLease.Lease leases direct lease.
//
// It returns an error if the operation fails or inputs are invalid.
func (l *DirectLease) Lease(ctx context.Context, runID, credentialID, policyRuleID string) (LeaseHandle, error) {
	if err := l.validateActiveRun(runID); err != nil {
		return LeaseHandle{}, err
	}
	rule, err := l.egressRule(policyRuleID)
	if err != nil {
		return LeaseHandle{}, err
	}
	if rule.Credential != credentialID {
		return LeaseHandle{}, fmt.Errorf("policy rule %s does not reference credential %s", policyRuleID, credentialID)
	}
	credential, err := l.credential(credentialID)
	if err != nil {
		return LeaseHandle{}, err
	}
	if credential.Type == "env_lease" || (credential.Type == "direct_lease" && credential.Mode == "env") {
		return LeaseHandle{}, errors.New("env_lease not supported in P1")
	}
	if credential.Type != "file_lease" {
		return LeaseHandle{}, fmt.Errorf("credential %s is not explicitly marked file_lease", credentialID)
	}
	if strings.TrimSpace(credential.Reason) == "" {
		return LeaseHandle{}, fmt.Errorf("credential %s file_lease requires a policy reason", credentialID)
	}

	value, err := l.store.Get(ctx, credentialID)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return LeaseHandle{}, fmt.Errorf("%w: direct lease credential %s is not set in the secret store", ErrSecretNotFound, credentialID)
		}
		return LeaseHandle{}, fmt.Errorf("load direct lease credential %s: %w", credentialID, err)
	}
	if err := l.store.TouchLastUsed(ctx, credentialID); err != nil {
		return LeaseHandle{}, fmt.Errorf("touch direct lease credential %s: %w", credentialID, err)
	}

	filePath, err := l.createLeaseFile(runID, credentialID, value)
	if err != nil {
		return LeaseHandle{}, err
	}
	handle := LeaseHandle{
		FilePath:     filePath,
		audit:        l.audit,
		now:          l.now,
		runID:        runID,
		credentialID: credentialID,
		policyRuleID: policyRuleID,
		valid:        true,
	}
	if err := handle.auditEvent(audit.EventTypeSecretLeased); err != nil {
		_ = os.Remove(filePath) // best-effort remove
		return LeaseHandle{}, err
	}
	return handle, nil
}

// ReadLease reads lease.
//
// It returns an error if the operation fails or inputs are invalid.
func ReadLease(_ context.Context, handle LeaseHandle) ([]byte, error) {
	if !handle.valid || handle.FilePath == "" {
		return nil, errors.New("not a valid lease handle")
	}
	if err := rejectSymlinkPath(handle.FilePath); err != nil {
		return nil, err
	}
	info, err := os.Lstat(handle.FilePath)
	if err != nil {
		return nil, fmt.Errorf("stat lease file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("lease path is not a regular file")
	}
	data, err := os.ReadFile(handle.FilePath)
	if err != nil {
		return nil, fmt.Errorf("read lease file: %w", err)
	}
	if err := handle.auditEvent(audit.EventTypeSecretRead); err != nil {
		return nil, err
	}
	return data, nil
}

// LeaseHandle.Cleanup cleans up lease handle.
//
// It returns an error if the operation fails or inputs are invalid.
func (h LeaseHandle) Cleanup() error {
	if h.FilePath == "" {
		return nil
	}
	err := os.Remove(h.FilePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove lease file: %w", err)
	}
	return nil
}

// LeaseHandle.String returns the string representation.
func (h LeaseHandle) String() string {
	return "LeaseHandle{FilePath:[REDACTED]}"
}

// LeaseHandle.GoString returns the Go-syntax representation.
func (h LeaseHandle) GoString() string {
	return h.String()
}

// LeaseHandle.Format formats lease handle.
func (h LeaseHandle) Format(s fmt.State, _ rune) { // intentionally ignored (reviewed)
	_, _ = fmt.Fprint(s, h.String()) // best-effort write
}

// LeaseHandle.MarshalJSON marshals the value as JSON.
//
// It returns an error if the operation fails or inputs are invalid.
func (h LeaseHandle) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		FilePath string `json:"file_path"`
	}{
		FilePath: "[REDACTED]",
	})
}

func (h LeaseHandle) auditEvent(eventType string) error {
	if h.audit == nil {
		return nil
	}
	now := h.now
	if now == nil {
		now = time.Now
	}
	return h.audit.Append(audit.AuditRecord{
		Timestamp:      now().UTC().Format(time.RFC3339),
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          "secrets-direct-lease",
		Payload: map[string]interface{}{
			"run_id":           h.runID,
			"policy_rule_id":   h.policyRuleID,
			"credential_id":    h.credentialID,
			"lease_type":       "file",
			"visible_to_agent": true,
		},
	})
}

func (l *DirectLease) createLeaseFile(runID, credentialID string, value []byte) (string, error) {
	runComponent, err := safeLeasePathComponent(runID)
	if err != nil {
		return "", fmt.Errorf("run id: %w", err)
	}
	credentialComponent, err := safeLeasePathComponent(credentialID)
	if err != nil {
		return "", fmt.Errorf("credential id: %w", err)
	}
	runDir := filepath.Join(l.leaseDir, runComponent)
	if err := rejectSymlinkPath(runDir); err != nil {
		return "", err
	}
	if err := os.Mkdir(runDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("create lease run directory: %w", err)
	}
	if err := rejectSymlinkPath(runDir); err != nil {
		return "", err
	}

	filePath := filepath.Join(runDir, credentialComponent)
	if err := rejectSymlinkPath(filePath); err != nil {
		return "", err
	}
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", fmt.Errorf("create lease file: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close

	if _, err := f.Write(value); err != nil {
		_ = os.Remove(filePath) // best-effort remove
		return "", fmt.Errorf("write lease file: %w", err)
	}
	if err := f.Chmod(0o400); err != nil {
		_ = os.Remove(filePath) // best-effort remove
		return "", fmt.Errorf("chmod lease file: %w", err)
	}
	if err := f.Chown(l.agentUID, -1); err != nil {
		_ = os.Remove(filePath) // best-effort remove
		return "", fmt.Errorf("chown lease file: %w", err)
	}
	return filePath, nil
}

func (l *DirectLease) validateActiveRun(runID string) error {
	if runID == "" {
		return errors.New("run id is required")
	}
	if _, ok := l.activeRuns[runID]; !ok {
		return fmt.Errorf("run id %s is not an active run", runID)
	}
	return nil
}

func (l *DirectLease) egressRule(policyRuleID string) (policy.EgressRule, error) {
	index, err := parseRuleID(policyRuleID)
	if err != nil {
		return policy.EgressRule{}, err
	}
	if index < 0 || index >= len(l.policy.Egress) {
		return policy.EgressRule{}, fmt.Errorf("policy rule %s does not exist", policyRuleID)
	}
	return l.policy.Egress[index], nil
}

func (l *DirectLease) credential(id string) (policy.Credential, error) {
	for _, credential := range l.policy.Credentials {
		if credential.ID == id {
			return credential, nil
		}
	}
	return policy.Credential{}, fmt.Errorf("policy rule references undeclared credential %s", id)
}

func validateLeaseRoot(root string) error {
	if !filepath.IsAbs(root) {
		return fmt.Errorf("lease directory %s must be absolute", root)
	}
	if err := rejectSystemPath(root); err != nil {
		return err
	}
	if err := rejectSymlinkPath(root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("stat lease directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("lease directory %s is not a directory", root)
	}
	return nil
}

func rejectSystemPath(path string) error {
	clean := filepath.Clean(path)
	for _, systemDir := range []string{"/etc", "/usr", "/bin"} {
		if clean == systemDir || strings.HasPrefix(clean, systemDir+string(os.PathSeparator)) {
			return fmt.Errorf("lease path %s is under disallowed system directory %s", path, systemDir)
		}
	}
	return nil
}

func rejectSymlinkPath(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("lease path %s must be absolute", path)
	}
	if hasDotDotPathSegment(clean) {
		return fmt.Errorf("lease path %s must not contain dot-dot path segments", path)
	}
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	if rest == "" {
		return nil
	}
	separator := string(os.PathSeparator)
	current := volume
	if strings.HasPrefix(rest, separator) {
		current += separator
		rest = strings.TrimPrefix(rest, separator)
	}
	for _, component := range strings.Split(rest, separator) {
		if component == "" {
			continue
		}
		if current == "" || current == separator {
			current = filepath.Join(current, component)
		} else {
			current = filepath.Join(current, component)
		}
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("lstat lease path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("lease path %s contains symlink component %s", path, current)
		}
	}
	return nil
}

func safeLeasePathComponent(value string) (string, error) {
	if value == "" {
		return "", errors.New("must not be empty")
	}
	if strings.ContainsAny(value, "\r\n\x00/\\") || strings.Contains(value, "..") {
		return "", errors.New("contains unsafe path characters")
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-':
			continue
		default:
			return "", fmt.Errorf("contains unsupported character %q", r)
		}
	}
	return value, nil
}

func hasDotDotPathSegment(path string) bool {
	for _, component := range strings.Split(path, string(os.PathSeparator)) {
		if component == ".." {
			return true
		}
	}
	return false
}
