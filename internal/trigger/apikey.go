package trigger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

const (
	apiKeyPrefix    = "agpt_"
	apiKeyRandomLen = 16
)

// APIKeyRecord is the persisted record for an API key. It never contains the
// raw key, only a hash and metadata.
type APIKeyRecord struct {
	ID          string     `json:"id"`
	KeyHash     string     `json:"key_hash"`
	Scopes      []string   `json:"scopes"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   string     `json:"created_by"`
	Revoked     bool       `json:"revoked"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
}

// APIKeyStore manages API key lifecycle with file-based persistence.
type APIKeyStore struct {
	mu       sync.RWMutex
	filePath string
	audit    audit.AuditAppender
	records  map[string]*APIKeyRecord
}

// NewAPIKeyStore creates a store backed by the given file path.
func NewAPIKeyStore(filePath string, auditAppender audit.AuditAppender) (*APIKeyStore, error) {
	canonicalPath, err := canonicalAPIKeyPath(filePath)
	if err != nil {
		return nil, err
	}
	s := &APIKeyStore{
		filePath: canonicalPath,
		audit:    auditAppender,
		records:  make(map[string]*APIKeyRecord),
	}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load api keys: %w", err)
	}
	return s, nil
}

// CreateKey generates a new API key, persists its hash, and returns the raw key once.
func (s *APIKeyStore) CreateKey(ctx context.Context, scopes []string, description, createdBy string) (string, *APIKeyRecord, error) {
	rawKey, keyID, err := newAPIKeyMaterial()
	if err != nil {
		return "", nil, err
	}
	record := &APIKeyRecord{
		ID:          keyID,
		KeyHash:     hashKey(rawKey),
		Scopes:      cloneStrings(scopes),
		Description: description,
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   createdBy,
	}

	s.mu.Lock()
	s.records[keyID] = record
	if err := s.saveLocked(); err != nil {
		delete(s.records, keyID)
		s.mu.Unlock()
		return "", nil, fmt.Errorf("persist key: %w", err)
	}
	s.mu.Unlock()

	s.auditEvent(ctx, "api_key_created", map[string]interface{}{
		"key_id":      keyID,
		"scopes":      cloneStrings(scopes),
		"description": description,
		"created_by":  createdBy,
	})

	return rawKey, record, nil
}

// ValidateKey checks a raw API key against the store.
func (s *APIKeyStore) ValidateKey(ctx context.Context, rawKey string) (*APIKeyRecord, error) {
	if !validAPIKeyFormat(rawKey) {
		s.auditEvent(ctx, "auth_failed", map[string]interface{}{"reason": "malformed"})
		return nil, ErrInvalidAPIKey
	}
	keyHash := hashKey(rawKey)

	var result *APIKeyRecord
	var revoked bool
	s.mu.Lock()
	for _, record := range s.records {
		if subtle.ConstantTimeCompare([]byte(record.KeyHash), []byte(keyHash)) == 1 {
			result = record
			revoked = record.Revoked
		}
	}
	if result == nil {
		s.mu.Unlock()
		s.auditEvent(ctx, "auth_failed", map[string]interface{}{"reason": "key_not_found"})
		return nil, ErrInvalidAPIKey
	}
	if revoked {
		keyID := result.ID
		s.mu.Unlock()
		s.auditEvent(ctx, "auth_failed", map[string]interface{}{
			"key_id": keyID,
			"reason": "revoked",
		})
		return nil, ErrRevokedKey
	}

	now := time.Now().UTC()
	result.LastUsedAt = &now
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("persist last used: %w", err)
	}
	result = cloneRecord(result)
	s.mu.Unlock()
	return result, nil
}

// RevokeKey marks a key as revoked.
func (s *APIKeyStore) RevokeKey(ctx context.Context, keyID, revokedBy string) error {
	s.mu.Lock()
	record, ok := s.records[keyID]
	if !ok {
		s.mu.Unlock()
		return ErrKeyNotFound
	}
	if record.Revoked {
		s.mu.Unlock()
		return ErrAlreadyRevoked
	}

	now := time.Now().UTC()
	record.Revoked = true
	record.RevokedAt = &now
	if err := s.saveLocked(); err != nil {
		record.Revoked = false
		record.RevokedAt = nil
		s.mu.Unlock()
		return fmt.Errorf("persist revocation: %w", err)
	}
	s.mu.Unlock()

	s.auditEvent(ctx, "api_key_revoked", map[string]interface{}{
		"key_id":     keyID,
		"revoked_by": revokedBy,
	})
	return nil
}

// RotateKey revokes the old key and creates a replacement with the same scopes.
func (s *APIKeyStore) RotateKey(ctx context.Context, oldKeyID, rotatedBy string) (string, *APIKeyRecord, error) {
	rawKey, keyID, err := newAPIKeyMaterial()
	if err != nil {
		return "", nil, err
	}

	s.mu.Lock()
	oldRecord, ok := s.records[oldKeyID]
	if !ok {
		s.mu.Unlock()
		return "", nil, ErrKeyNotFound
	}
	if oldRecord.Revoked {
		s.mu.Unlock()
		return "", nil, ErrAlreadyRevoked
	}

	oldRevoked := oldRecord.Revoked
	oldRevokedAt := oldRecord.RevokedAt
	now := time.Now().UTC()
	oldRecord.Revoked = true
	oldRecord.RevokedAt = &now

	newRecord := &APIKeyRecord{
		ID:          keyID,
		KeyHash:     hashKey(rawKey),
		Scopes:      cloneStrings(oldRecord.Scopes),
		Description: oldRecord.Description + " (rotated)",
		CreatedAt:   now,
		CreatedBy:   rotatedBy,
	}
	s.records[keyID] = newRecord
	if err := s.saveLocked(); err != nil {
		oldRecord.Revoked = oldRevoked
		oldRecord.RevokedAt = oldRevokedAt
		delete(s.records, keyID)
		s.mu.Unlock()
		return "", nil, fmt.Errorf("persist rotation: %w", err)
	}
	s.mu.Unlock()

	s.auditEvent(ctx, "api_key_revoked", map[string]interface{}{
		"key_id":     oldKeyID,
		"revoked_by": rotatedBy,
	})
	s.auditEvent(ctx, "api_key_created", map[string]interface{}{
		"key_id":      keyID,
		"scopes":      cloneStrings(newRecord.Scopes),
		"description": newRecord.Description,
		"created_by":  rotatedBy,
	})

	return rawKey, newRecord, nil
}

// ListKeys returns all key records without raw keys.
func (s *APIKeyStore) ListKeys() []*APIKeyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*APIKeyRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, cloneRecord(record))
	}
	return result
}

// HasScope checks if a key record has the given scope.
func (r *APIKeyRecord) HasScope(scope string) bool {
	for _, candidate := range r.Scopes {
		if candidate == scope || candidate == "*" {
			return true
		}
		prefix, ok := strings.CutSuffix(candidate, ":*")
		if ok && strings.HasPrefix(scope, prefix+":") {
			return true
		}
	}
	return false
}

// Authenticate implements Authenticator for APIKeyStore.
func (s *APIKeyStore) Authenticate(ctx context.Context) (CallerID, AuthMethod, error) {
	token := extractBearerToken(ctx)
	if token == "" {
		return "", AuthMethodNone, errors.New("missing API key")
	}
	record, err := s.ValidateKey(ctx, token)
	if err != nil {
		return "", AuthMethodNone, err
	}
	return CallerID("api_key:" + record.ID), AuthMethodAPIKey, nil
}

func (s *APIKeyStore) load() error {
	if err := rejectSymlinkPath(s.filePath, false); err != nil {
		return err
	}
	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var records []*APIKeyRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	for _, record := range records {
		if record != nil {
			s.records[record.ID] = cloneRecord(record)
		}
	}
	return nil
}

func (s *APIKeyStore) saveLocked() error {
	if err := rejectSymlinkPath(s.filePath, false); err != nil {
		return err
	}
	records := make([]*APIKeyRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, cloneRecord(record))
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir, true); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".apikeys-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.filePath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *APIKeyStore) auditEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
	if s.audit == nil {
		return
	}
	caller, _ := CallerFromContext(ctx)
	_ = s.audit.Append(audit.AuditRecord{
		EventType:      eventType,
		DeploymentMode: "local",
		Actor:          string(caller),
		Payload:        payload,
	})
}

var (
	ErrInvalidAPIKey  = errors.New("invalid API key")
	ErrRevokedKey     = errors.New("API key has been revoked")
	ErrKeyNotFound    = errors.New("API key not found")
	ErrAlreadyRevoked = errors.New("API key already revoked")
)

func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}

func newAPIKeyMaterial() (string, string, error) {
	rawBytes := make([]byte, apiKeyRandomLen)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", "", fmt.Errorf("generate random: %w", err)
	}
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return "", "", fmt.Errorf("generate id: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(rawBytes), "key-" + hex.EncodeToString(idBytes), nil
}

func validAPIKeyFormat(rawKey string) bool {
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return false
	}
	keyPart := strings.TrimPrefix(rawKey, apiKeyPrefix)
	if len(keyPart) != apiKeyRandomLen*2 {
		return false
	}
	_, err := hex.DecodeString(keyPart)
	return err == nil
}

func cloneRecord(record *APIKeyRecord) *APIKeyRecord {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Scopes = cloneStrings(record.Scopes)
	if record.RevokedAt != nil {
		revokedAt := *record.RevokedAt
		clone.RevokedAt = &revokedAt
	}
	if record.LastUsedAt != nil {
		lastUsedAt := *record.LastUsedAt
		clone.LastUsedAt = &lastUsedAt
	}
	return &clone
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	clone := make([]string, len(values))
	copy(clone, values)
	return clone
}

func canonicalAPIKeyPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("api key path must be absolute: %s", path)
	}
	cleanPath := filepath.Clean(path)
	if isSystemPath(cleanPath) {
		return "", fmt.Errorf("api key path is not allowed: %s", cleanPath)
	}

	var missing []string
	current := cleanPath
	for {
		if _, err := os.Lstat(current); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("api key path has no existing parent: %s", cleanPath)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}

	resolved, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", err
	}
	for i := len(missing) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, missing[i])
	}
	return resolved, nil
}

func rejectSymlinkPath(path string, mustExist bool) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("api key path must be absolute: %s", path)
	}
	cleanPath := filepath.Clean(path)
	if isSystemPath(cleanPath) {
		return fmt.Errorf("api key path is not allowed: %s", cleanPath)
	}

	volume := filepath.VolumeName(cleanPath)
	remainder := strings.TrimPrefix(cleanPath, volume)
	parts := strings.Split(strings.TrimPrefix(remainder, string(os.PathSeparator)), string(os.PathSeparator))
	current := volume + string(os.PathSeparator)
	for i, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if mustExist || i < len(parts)-1 {
				return err
			}
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("api key path contains symlink: %s", current)
		}
	}
	return nil
}

func isSystemPath(path string) bool {
	for _, systemPath := range []string{"/etc", "/usr", "/bin", "/sbin", "/System"} {
		if path == systemPath || strings.HasPrefix(path, systemPath+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
