package trigger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

const (
	// DefaultIdempotencyTTL is how long idempotency entries are valid for replay.
	DefaultIdempotencyTTL = 24 * time.Hour
)

// IdempotencyEntry records a previous invoke for replay.
type IdempotencyEntry struct {
	Key         string    `json:"key"`
	RunID       string    `json:"run_id"`
	RequestHash string    `json:"request_hash"`
	CallerID    string    `json:"caller_id"`
	AgentName   string    `json:"agent_name"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// IdempotencyResult is the outcome of checking idempotency.
type IdempotencyResult int

const (
	// IdempotencyNew means no existing entry was found and the caller may proceed.
	IdempotencyNew IdempotencyResult = iota
	// IdempotencyReplayed means a matching previous entry was found.
	IdempotencyReplayed
	// IdempotencyConflict means the key exists with a different request hash.
	IdempotencyConflict
)

// IdempotencyStore is a durable idempotency table backed by a JSON file.
type IdempotencyStore struct {
	mu       sync.Mutex
	filePath string
	audit    audit.AuditAppender
	ttl      time.Duration
	entries  map[string]*IdempotencyEntry
}

// NewIdempotencyStore creates a store backed by the given file path.
func NewIdempotencyStore(filePath string, ttl time.Duration, auditAppender audit.AuditAppender) (*IdempotencyStore, error) {
	if ttl == 0 {
		ttl = DefaultIdempotencyTTL
	}
	if err := validateIdempotencyPath(filePath); err != nil {
		return nil, err
	}
	s := &IdempotencyStore{
		filePath: filePath,
		audit:    auditAppender,
		ttl:      ttl,
		entries:  make(map[string]*IdempotencyEntry),
	}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load idempotency: %w", err)
	}
	s.purgeExpired(time.Now())
	return s, nil
}

// CheckOrReserve checks for an existing entry, or reserves a new one.
func (s *IdempotencyStore) CheckOrReserve(ctx context.Context, key, runID, requestHash, callerID, agentName string) (IdempotencyResult, *IdempotencyEntry, error) {
	if key == "" {
		return IdempotencyNew, nil, nil
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.entries[key]
	if ok && now.Before(existing.ExpiresAt) {
		if existing.RequestHash == requestHash {
			s.auditEvent(ctx, "idempotency_replayed", map[string]interface{}{
				"key":    key,
				"run_id": existing.RunID,
				"caller": callerID,
			})
			return IdempotencyReplayed, existing, nil
		}
		s.auditEvent(ctx, "idempotency_conflict", map[string]interface{}{
			"key":      key,
			"existing": existing.RunID,
			"caller":   callerID,
		})
		return IdempotencyConflict, existing, nil
	}

	entry := &IdempotencyEntry{
		Key:         key,
		RunID:       runID,
		RequestHash: requestHash,
		CallerID:    callerID,
		AgentName:   agentName,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.ttl),
	}
	s.entries[key] = entry

	if err := s.save(); err != nil {
		delete(s.entries, key)
		return IdempotencyNew, nil, fmt.Errorf("persist idempotency: %w", err)
	}

	return IdempotencyNew, entry, nil
}

// CanonicalRequestHash computes the SHA-256 hash over the canonical request fields.
func CanonicalRequestHash(callerID, agentName, lockDigest string, payload []byte, contentType, apiVersion string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "caller=%s\n", callerID)
	_, _ = fmt.Fprintf(h, "agent=%s\n", agentName)
	_, _ = fmt.Fprintf(h, "lock_digest=%s\n", lockDigest)
	_, _ = fmt.Fprintf(h, "payload_len=%d\n", len(payload))
	_, _ = h.Write(payload)
	_, _ = h.Write([]byte("\n"))
	_, _ = fmt.Fprintf(h, "content_type=%s\n", contentType)
	_, _ = fmt.Fprintf(h, "api_version=%s\n", apiVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// PurgeExpired removes entries that have passed their expiry time.
func (s *IdempotencyStore) PurgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpired(time.Now())
}

func (s *IdempotencyStore) purgeExpired(now time.Time) {
	for key, entry := range s.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(s.entries, key)
		}
	}
	_ = s.save()
}

// EntryCount returns the number of active entries.
func (s *IdempotencyStore) EntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *IdempotencyStore) load() error {
	if err := validateIdempotencyPath(s.filePath); err != nil {
		return err
	}
	data, err := os.ReadFile(s.filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var entries []*IdempotencyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	for _, entry := range entries {
		s.entries[entry.Key] = entry
	}
	return nil
}

func (s *IdempotencyStore) save() error {
	entries := make([]*IdempotencyEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, entry)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.filePath)
	if err := validateParentPath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := validateIdempotencyPath(s.filePath); err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := validateIdempotencyPath(tmp); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.filePath)
}

func (s *IdempotencyStore) auditEvent(ctx context.Context, eventType string, payload map[string]interface{}) {
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

func validateIdempotencyPath(filePath string) error {
	if !filepath.IsAbs(filePath) {
		return fmt.Errorf("idempotency path must be absolute")
	}
	clean := filepath.Clean(filePath)
	if clean != filePath {
		return fmt.Errorf("idempotency path must be clean")
	}
	if containsDotDot(clean) {
		return fmt.Errorf("idempotency path must not contain '..'")
	}
	if isIdempotencySystemPath(clean) {
		return fmt.Errorf("idempotency path must not be in a system directory")
	}
	if err := validateParentPath(filepath.Dir(clean)); err != nil {
		return err
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("idempotency path must not be a symlink")
	}
	if info.IsDir() {
		return fmt.Errorf("idempotency path must be a file")
	}
	return nil
}

func validateParentPath(dir string) error {
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("idempotency directory must be absolute")
	}
	if containsDotDot(clean) {
		return fmt.Errorf("idempotency directory must not contain '..'")
	}
	if isIdempotencySystemPath(clean) {
		return fmt.Errorf("idempotency directory must not be a system directory")
	}
	volume := filepath.VolumeName(clean)
	root := string(filepath.Separator)
	current := root
	if volume != "" {
		current = volume + root
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(clean, volume), root)
	if rel == "" {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("idempotency directory component must not be a symlink: %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("idempotency parent component must be a directory: %s", current)
		}
	}
	return nil
}

func containsDotDot(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".." {
			return true
		}
	}
	return false
}

func isIdempotencySystemPath(path string) bool {
	clean := filepath.Clean(path)
	for _, systemDir := range []string{"/etc", "/usr", "/bin"} {
		if clean == systemDir || strings.HasPrefix(clean, systemDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
