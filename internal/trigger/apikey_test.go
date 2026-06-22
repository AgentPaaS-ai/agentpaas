package trigger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

type fakeAudit struct {
	records []audit.AuditRecord
}

func (f *fakeAudit) Append(r audit.AuditRecord) error {
	f.records = append(f.records, r)
	return nil
}

func TestAPIKeyStoreCreateAndValidate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	auditSink := &fakeAudit{}
	store, err := NewAPIKeyStore(filepath.Join(t.TempDir(), "apikeys.json"), auditSink)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}

	rawKey, record, err := store.CreateKey(ctx, []string{"agent-a:invoke"}, "deploy hook", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		t.Fatalf("CreateKey() raw key = %q, want prefix %q", rawKey, apiKeyPrefix)
	}
	if record == nil {
		t.Fatal("CreateKey() record is nil")
	}
	if record.ID == "" {
		t.Fatal("CreateKey() record ID is empty")
	}
	if record.KeyHash == "" {
		t.Fatal("CreateKey() record key hash is empty")
	}
	if record.KeyHash == rawKey {
		t.Fatal("CreateKey() stored raw key in KeyHash")
	}

	stored, err := os.ReadFile(store.filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(stored), rawKey) {
		t.Fatal("persisted key file contains raw API key")
	}

	got, err := store.ValidateKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("ValidateKey() error = %v", err)
	}
	if got.ID != record.ID {
		t.Fatalf("ValidateKey() ID = %q, want %q", got.ID, record.ID)
	}
	if got.LastUsedAt == nil {
		t.Fatal("ValidateKey() did not update LastUsedAt")
	}

	if _, err := store.ValidateKey(ctx, apiKeyPrefix+"00000000000000000000000000000000"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("ValidateKey(wrong) error = %v, want %v", err, ErrInvalidAPIKey)
	}
}

func TestAPIKeyStoreRevoke(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	auditSink := &fakeAudit{}
	store, err := NewAPIKeyStore(filepath.Join(t.TempDir(), "apikeys.json"), auditSink)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}
	rawKey, record, err := store.CreateKey(ctx, []string{"agent-a:invoke"}, "deploy hook", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	if err := store.RevokeKey(ctx, record.ID, "local_user:501"); err != nil {
		t.Fatalf("RevokeKey() error = %v", err)
	}
	if !record.Revoked {
		t.Fatal("RevokeKey() did not mark record revoked")
	}
	if record.RevokedAt == nil {
		t.Fatal("RevokeKey() did not set RevokedAt")
	}
	if _, err := store.ValidateKey(ctx, rawKey); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("ValidateKey(revoked) error = %v, want %v", err, ErrRevokedKey)
	}
	if err := store.RevokeKey(ctx, "key-missing", "local_user:501"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("RevokeKey(missing) error = %v, want %v", err, ErrKeyNotFound)
	}
	if err := store.RevokeKey(ctx, record.ID, "local_user:501"); !errors.Is(err, ErrAlreadyRevoked) {
		t.Fatalf("RevokeKey(already revoked) error = %v, want %v", err, ErrAlreadyRevoked)
	}

	if !auditContains(auditSink.records, "api_key_revoked") {
		t.Fatal("RevokeKey() did not emit api_key_revoked audit event")
	}
}

func TestAPIKeyStoreRotate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewAPIKeyStore(filepath.Join(t.TempDir(), "apikeys.json"), nil)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}
	oldRawKey, oldRecord, err := store.CreateKey(ctx, []string{"agent-a:invoke", "agent-b:*"}, "deploy hook", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	newRawKey, newRecord, err := store.RotateKey(ctx, oldRecord.ID, "local_user:501")
	if err != nil {
		t.Fatalf("RotateKey() error = %v", err)
	}
	if newRawKey == oldRawKey {
		t.Fatal("RotateKey() returned same raw key")
	}
	if !oldRecord.Revoked {
		t.Fatal("RotateKey() did not revoke old key")
	}
	if newRecord.ID == oldRecord.ID {
		t.Fatal("RotateKey() reused old key ID")
	}
	if strings.Join(newRecord.Scopes, ",") != strings.Join(oldRecord.Scopes, ",") {
		t.Fatalf("RotateKey() scopes = %v, want %v", newRecord.Scopes, oldRecord.Scopes)
	}
	if _, err := store.ValidateKey(ctx, oldRawKey); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("ValidateKey(old rotated key) error = %v, want %v", err, ErrRevokedKey)
	}
	if _, err := store.ValidateKey(ctx, newRawKey); err != nil {
		t.Fatalf("ValidateKey(new rotated key) error = %v", err)
	}
}

func TestAPIKeyStoreListKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := NewAPIKeyStore(filepath.Join(t.TempDir(), "apikeys.json"), nil)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}
	rawKey, _, err := store.CreateKey(ctx, []string{"agent-a:invoke"}, "one", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey(one) error = %v", err)
	}
	if _, _, err := store.CreateKey(ctx, []string{"agent-b:invoke"}, "two", "local_user:501"); err != nil {
		t.Fatalf("CreateKey(two) error = %v", err)
	}

	records := store.ListKeys()
	if len(records) != 2 {
		t.Fatalf("ListKeys() len = %d, want 2", len(records))
	}
	for _, record := range records {
		if record.KeyHash == rawKey {
			t.Fatal("ListKeys() exposed raw API key")
		}
	}
}

func TestAPIKeyRecordHasScope(t *testing.T) {
	t.Parallel()

	record := &APIKeyRecord{Scopes: []string{"agent-a:invoke", "agent-b:*", "*"}}
	if !record.HasScope("agent-a:invoke") {
		t.Fatal("HasScope() exact match = false, want true")
	}
	if !record.HasScope("agent-b:cancel") {
		t.Fatal("HasScope() wildcard match = false, want true")
	}
	if !record.HasScope("agent-c:invoke") {
		t.Fatal("HasScope() global wildcard = false, want true")
	}

	restricted := &APIKeyRecord{Scopes: []string{"agent-a:invoke"}}
	if restricted.HasScope("agent-b:invoke") {
		t.Fatal("HasScope() non-matching scope = true, want false")
	}
}

func TestAPIKeyStorePersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	filePath := filepath.Join(t.TempDir(), "apikeys.json")
	store, err := NewAPIKeyStore(filePath, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}
	rawKey, record, err := store.CreateKey(ctx, []string{"agent-a:invoke"}, "deploy hook", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	reloaded, err := NewAPIKeyStore(filePath, nil)
	if err != nil {
		t.Fatalf("NewAPIKeyStore(reload) error = %v", err)
	}
	got, err := reloaded.ValidateKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("ValidateKey(reloaded) error = %v", err)
	}
	if got.ID != record.ID {
		t.Fatalf("ValidateKey(reloaded) ID = %q, want %q", got.ID, record.ID)
	}
}

func TestAPIKeyStoreAuditEvents(t *testing.T) {
	t.Parallel()

	ctx := WithCaller(context.Background(), "local_user:501", AuthMethodNone)
	auditSink := &fakeAudit{}
	store, err := NewAPIKeyStore(filepath.Join(t.TempDir(), "apikeys.json"), auditSink)
	if err != nil {
		t.Fatalf("NewAPIKeyStore() error = %v", err)
	}
	rawKey, record, err := store.CreateKey(ctx, []string{"agent-a:invoke"}, "deploy hook", "local_user:501")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if _, err := store.ValidateKey(ctx, apiKeyPrefix+"00000000000000000000000000000000"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("ValidateKey(wrong) error = %v, want %v", err, ErrInvalidAPIKey)
	}
	if err := store.RevokeKey(ctx, record.ID, "local_user:501"); err != nil {
		t.Fatalf("RevokeKey() error = %v", err)
	}
	if _, err := store.ValidateKey(ctx, rawKey); !errors.Is(err, ErrRevokedKey) {
		t.Fatalf("ValidateKey(revoked) error = %v, want %v", err, ErrRevokedKey)
	}

	for _, eventType := range []string{"api_key_created", "api_key_revoked", "auth_failed"} {
		if !auditContains(auditSink.records, eventType) {
			t.Fatalf("missing audit event %q in %v", eventType, auditSink.records)
		}
	}
	for _, record := range auditSink.records {
		if record.EventType != "" && record.Actor != "local_user:501" {
			t.Fatalf("audit actor = %q, want local_user:501", record.Actor)
		}
	}
}

func auditContains(records []audit.AuditRecord, eventType string) bool {
	for _, record := range records {
		if record.EventType == eventType {
			return true
		}
	}
	return false
}
