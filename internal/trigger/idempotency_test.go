package trigger

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeIdempotencyAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (f *fakeIdempotencyAudit) Append(record audit.AuditRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

func (f *fakeIdempotencyAudit) Get() []audit.AuditRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	records := make([]audit.AuditRecord, len(f.records))
	copy(records, f.records)
	return records
}

func TestIdempotencyStoreSameKeySamePayloadReplaysRunID(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte(`{"ok":true}`), "application/json", "trigger.v1")

	result, _, err := store.CheckOrReserve(context.Background(), "idem-1", "run-1", hash, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if result != IdempotencyNew {
		t.Fatalf("first result = %v, want %v", result, IdempotencyNew)
	}

	result, entry, err := store.CheckOrReserve(context.Background(), "idem-1", "run-2", hash, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result != IdempotencyReplayed {
		t.Fatalf("second result = %v, want %v", result, IdempotencyReplayed)
	}
	if entry.RunID != "run-1" {
		t.Fatalf("run id = %q, want %q", entry.RunID, "run-1")
	}
}

func TestIdempotencyStoreSameKeyDifferentPayloadConflicts(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash1 := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("one"), "text/plain", "trigger.v1")
	hash2 := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("two"), "text/plain", "trigger.v1")

	result, _, err := store.CheckOrReserve(context.Background(), "idem-1", "run-1", hash1, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if result != IdempotencyNew {
		t.Fatalf("first result = %v, want %v", result, IdempotencyNew)
	}

	result, entry, err := store.CheckOrReserve(context.Background(), "idem-1", "run-2", hash2, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("conflict: %v", err)
	}
	if result != IdempotencyConflict {
		t.Fatalf("second result = %v, want %v", result, IdempotencyConflict)
	}
	if entry.RunID != "run-1" {
		t.Fatalf("conflict run id = %q, want %q", entry.RunID, "run-1")
	}
}

func TestIdempotencyStoreDifferentKeyIsNew(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")

	if result, _, err := store.CheckOrReserve(context.Background(), "idem-1", "run-1", hash, "api_key:1", "agent"); err != nil || result != IdempotencyNew {
		t.Fatalf("first reserve result = %v, err = %v; want new nil", result, err)
	}
	result, _, err := store.CheckOrReserve(context.Background(), "idem-2", "run-2", hash, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("second reserve: %v", err)
	}
	if result != IdempotencyNew {
		t.Fatalf("second result = %v, want %v", result, IdempotencyNew)
	}
}

func TestIdempotencyStoreEmptyKeyAlwaysNew(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")

	for i := 0; i < 2; i++ {
		result, entry, err := store.CheckOrReserve(context.Background(), "", "run-1", hash, "api_key:1", "agent")
		if err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
		if result != IdempotencyNew {
			t.Fatalf("check %d result = %v, want %v", i, result, IdempotencyNew)
		}
		if entry != nil {
			t.Fatalf("check %d entry = %#v, want nil", i, entry)
		}
	}
	if count := store.EntryCount(); count != 0 {
		t.Fatalf("entry count = %d, want 0", count)
	}
}

func TestIdempotencyStoreExpiredEntryTreatedAsNew(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")
	now := time.Now()

	store.mu.Lock()
	store.entries["idem-1"] = &IdempotencyEntry{
		Key:         "idem-1",
		RunID:       "run-expired",
		RequestHash: hash,
		CallerID:    "api_key:1",
		AgentName:   "agent",
		CreatedAt:   now.Add(-25 * time.Hour),
		ExpiresAt:   now.Add(-time.Hour),
	}
	store.mu.Unlock()

	result, entry, err := store.CheckOrReserve(context.Background(), "idem-1", "run-new", hash, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("reserve after expiry: %v", err)
	}
	if result != IdempotencyNew {
		t.Fatalf("result = %v, want %v", result, IdempotencyNew)
	}
	if entry.RunID != "run-new" {
		t.Fatalf("run id = %q, want %q", entry.RunID, "run-new")
	}
}

func TestIdempotencyStoreDaemonRestartPreservesEntries(t *testing.T) {
	filePath := testIdempotencyFile(t)
	store, err := NewIdempotencyStore(filePath, DefaultIdempotencyTTL, nil)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")
	if result, _, err := store.CheckOrReserve(context.Background(), "idem-1", "run-1", hash, "api_key:1", "agent"); err != nil || result != IdempotencyNew {
		t.Fatalf("reserve result = %v, err = %v; want new nil", result, err)
	}

	restarted, err := NewIdempotencyStore(filePath, DefaultIdempotencyTTL, nil)
	if err != nil {
		t.Fatalf("restart store: %v", err)
	}
	result, entry, err := restarted.CheckOrReserve(context.Background(), "idem-1", "run-2", hash, "api_key:1", "agent")
	if err != nil {
		t.Fatalf("replay after restart: %v", err)
	}
	if result != IdempotencyReplayed {
		t.Fatalf("result = %v, want %v", result, IdempotencyReplayed)
	}
	if entry.RunID != "run-1" {
		t.Fatalf("run id = %q, want %q", entry.RunID, "run-1")
	}
}

func TestCanonicalRequestHashDeterministic(t *testing.T) {
	payload := []byte("payload")
	hash1 := CanonicalRequestHash("caller", "agent", "lock", payload, "text/plain", "trigger.v1")
	hash2 := CanonicalRequestHash("caller", "agent", "lock", payload, "text/plain", "trigger.v1")
	if hash1 != hash2 {
		t.Fatalf("hashes differ: %q != %q", hash1, hash2)
	}
}

func TestCanonicalRequestHashDiffersWhenAnyFieldChanges(t *testing.T) {
	base := CanonicalRequestHash("caller", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")
	cases := map[string]string{
		"caller":       CanonicalRequestHash("other", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1"),
		"agent":        CanonicalRequestHash("caller", "other", "lock", []byte("payload"), "text/plain", "trigger.v1"),
		"lock_digest":  CanonicalRequestHash("caller", "agent", "other", []byte("payload"), "text/plain", "trigger.v1"),
		"payload":      CanonicalRequestHash("caller", "agent", "lock", []byte("changed"), "text/plain", "trigger.v1"),
		"content_type": CanonicalRequestHash("caller", "agent", "lock", []byte("payload"), "application/json", "trigger.v1"),
		"api_version":  CanonicalRequestHash("caller", "agent", "lock", []byte("payload"), "text/plain", "trigger.v2"),
	}
	for field, hash := range cases {
		if hash == base {
			t.Fatalf("%s hash = base hash %q, want different", field, base)
		}
	}
}

func TestCanonicalRequestHashDiffersWhenPayloadBytesDiffer(t *testing.T) {
	hash1 := CanonicalRequestHash("caller", "agent", "lock", []byte{0, 1, 2}, "application/octet-stream", "trigger.v1")
	hash2 := CanonicalRequestHash("caller", "agent", "lock", []byte{0, 1, 3}, "application/octet-stream", "trigger.v1")
	if hash1 == hash2 {
		t.Fatalf("hashes matched for different payload bytes: %q", hash1)
	}
}

func TestIdempotencyStoreAuditEvents(t *testing.T) {
	auditLog := &fakeIdempotencyAudit{}
	store := newTestIdempotencyStore(t, auditLog)
	ctx := WithCaller(context.Background(), CallerID("api_key:1"), AuthMethodAPIKey)
	hash1 := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("one"), "text/plain", "trigger.v1")
	hash2 := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("two"), "text/plain", "trigger.v1")

	if result, _, err := store.CheckOrReserve(ctx, "idem-1", "run-1", hash1, "api_key:1", "agent"); err != nil || result != IdempotencyNew {
		t.Fatalf("reserve result = %v, err = %v; want new nil", result, err)
	}
	if result, _, err := store.CheckOrReserve(ctx, "idem-1", "run-2", hash1, "api_key:1", "agent"); err != nil || result != IdempotencyReplayed {
		t.Fatalf("replay result = %v, err = %v; want replayed nil", result, err)
	}
	if result, _, err := store.CheckOrReserve(ctx, "idem-1", "run-3", hash2, "api_key:1", "agent"); err != nil || result != IdempotencyConflict {
		t.Fatalf("conflict result = %v, err = %v; want conflict nil", result, err)
	}

	records := auditLog.Get()
	if len(records) != 2 {
		t.Fatalf("audit record count = %d, want 2", len(records))
	}
	if records[0].EventType != "idempotency_replayed" {
		t.Fatalf("first event = %q, want idempotency_replayed", records[0].EventType)
	}
	if records[1].EventType != "idempotency_conflict" {
		t.Fatalf("second event = %q, want idempotency_conflict", records[1].EventType)
	}
}

func TestTriggerServiceInvokeRejectsPayloadOverMax(t *testing.T) {
	service := NewTriggerService(nil, DefaultMaxPayload)
	req := &triggerv1.InvokeRequest{
		AgentName: "agent",
		Payload:   make([]byte, DefaultMaxPayload+1),
	}

	_, err := service.Invoke(context.Background(), req)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, err = %v; want %v", status.Code(err), err, codes.InvalidArgument)
	}
}

func TestIdempotencyStoreConcurrentCheckOrReserveSameKey(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	hash := CanonicalRequestHash("api_key:1", "agent", "lock", []byte("payload"), "text/plain", "trigger.v1")
	start := make(chan struct{})
	results := make(chan IdempotencyResult, 2)
	errs := make(chan error, 2)

	for _, runID := range []string{"run-1", "run-2"} {
		go func(runID string) {
			<-start
			result, _, err := store.CheckOrReserve(context.Background(), "idem-1", runID, hash, "api_key:1", "agent")
			results <- result
			errs <- err
		}(runID)
	}
	close(start)

	counts := map[IdempotencyResult]int{}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
		counts[<-results]++
	}
	if counts[IdempotencyNew] != 1 {
		t.Fatalf("new count = %d, want 1", counts[IdempotencyNew])
	}
	if counts[IdempotencyReplayed] != 1 {
		t.Fatalf("replayed count = %d, want 1", counts[IdempotencyReplayed])
	}
}

func TestIdempotencyStorePurgeExpiredRemovesExpiredEntries(t *testing.T) {
	store := newTestIdempotencyStore(t, nil)
	now := time.Now()
	store.mu.Lock()
	store.entries["expired"] = &IdempotencyEntry{
		Key:       "expired",
		RunID:     "run-expired",
		ExpiresAt: now.Add(-time.Second),
	}
	store.entries["active"] = &IdempotencyEntry{
		Key:       "active",
		RunID:     "run-active",
		ExpiresAt: now.Add(time.Hour),
	}
	store.mu.Unlock()

	store.PurgeExpired()
	if count := store.EntryCount(); count != 1 {
		t.Fatalf("entry count = %d, want 1", count)
	}
	store.mu.Lock()
	_, expiredOK := store.entries["expired"]
	_, activeOK := store.entries["active"]
	store.mu.Unlock()
	if expiredOK {
		t.Fatal("expired entry still exists")
	}
	if !activeOK {
		t.Fatal("active entry was removed")
	}
}

func newTestIdempotencyStore(t *testing.T, auditAppender audit.AuditAppender) *IdempotencyStore {
	t.Helper()
	store, err := NewIdempotencyStore(testIdempotencyFile(t), DefaultIdempotencyTTL, auditAppender)
	if err != nil {
		t.Fatalf("new idempotency store: %v", err)
	}
	return store
}

func testIdempotencyFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return filepath.Join(resolved, "idempotency.json")
}
