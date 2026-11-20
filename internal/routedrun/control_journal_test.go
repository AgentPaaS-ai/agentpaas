package routedrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestControlJournal opens a ControlJournal rooted under a temp dir for a
// single attempt, generating a fresh per-attempt HMAC key at <stateRoot>/
// runs/<runID>/control-key (0600). The control journal itself lives at
// <stateRoot>/runs/<runID>/<attemptID>/.
func newTestControlJournal(t *testing.T) (*ControlJournal, string, string) {
	t.Helper()
	stateRoot := t.TempDir()
	runID := "run-test-1"
	attemptID := "att-test-1"
	cj, err := NewControlJournal(stateRoot, runID, attemptID)
	if err != nil {
		t.Fatalf("NewControlJournal: %v", err)
	}
	t.Cleanup(func() { _ = cj.Close() })
	return cj, stateRoot, runID
}

// TestControlJournal_DirectoryAndFileModes verifies the per-attempt control
// directory is 0700 and every event file is 0600 (spec line 118-119, 284).
func TestControlJournal_DirectoryAndFileModes(t *testing.T) {
	cj, stateRoot, runID := newTestControlJournal(t)

	// Control directory must be 0700.
	dir := filepath.Join(stateRoot, "runs", runID, "control", "att-test-1")
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("lstat control dir: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("control path %s not a directory", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("control dir mode %#o want 0700", fi.Mode().Perm())
	}

	// Append an event and verify the event file is 0600.
	ev := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     time.Now().UTC(),
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"msg":"accepted"}`,
	}
	if err := cj.Append(ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ef := filepath.Join(dir, "event-0000000001.json")
	efi, err := os.Lstat(ef)
	if err != nil {
		t.Fatalf("lstat event file: %v", err)
	}
	if efi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("event file mode %#o want 0600", efi.Mode().Perm())
	}
	if efi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("event file %s is a symlink", ef)
	}
}

// TestControlJournal_KeyFileMode0600 verifies the per-attempt HMAC key lives
// OUTSIDE the control directory at <stateRoot>/runs/<runID>/control-key with
// mode 0600 (spec line 118-119: Python must not read it).
func TestControlJournal_KeyFileMode0600(t *testing.T) {
	_, stateRoot, runID := newTestControlJournal(t)
	keyPath := filepath.Join(stateRoot, "runs", runID, "control-key")
	fi, err := os.Lstat(keyPath)
	if err != nil {
		t.Fatalf("lstat control-key: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("control-key %s is a symlink", keyPath)
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("control-key %s not a regular file", keyPath)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("control-key mode %#o want 0600", fi.Mode().Perm())
	}
	// Key must be outside the control directory (not readable by worker).
	controlDir := filepath.Join(stateRoot, "runs", runID, "control", "att-test-1")
	if strings.HasPrefix(keyPath, controlDir+string(filepath.Separator)) {
		t.Fatalf("control-key %s must be outside control dir %s", keyPath, controlDir)
	}
	// Key content must be 32 random bytes (256-bit HMAC key), not zero.
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read control-key: %v", err)
	}
	if len(data) != 32 {
		t.Fatalf("control-key length %d want 32", len(data))
	}
	allZero := true
	for _, b := range data {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("control-key is all-zero — must be random")
	}
}

// TestControlJournal_SequenceMonotonicNoGaps verifies sequence numbers are
// monotonic starting at 1 with no gaps (spec: "Sequence numbers: monotonic,
// no gaps").
func TestControlJournal_SequenceMonotonicNoGaps(t *testing.T) {
	cj, _, _ := newTestControlJournal(t)
	now := time.Now().UTC()
	for i := int64(1); i <= 5; i++ {
		ev := InvokeJobEvent{
			SchemaVersion: invokeJobSchemaVersionV1,
			Sequence:      i,
			Timestamp:     now,
			EventKind:     InvokeJobEventProgressRef,
			Payload:       `{"n":` + itoa(i) + `}`,
		}
		if err := cj.Append(ev); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	// Reading all events must return exactly 5, in order, sequences 1..5.
	got, err := cj.Read(1)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d events want 5", len(got))
	}
	for i, ev := range got {
		if ev.Sequence != int64(i+1) {
			t.Fatalf("event %d sequence=%d want %d", i, ev.Sequence, i+1)
		}
	}
	// Read from seq 3 returns events 3..5.
	got3, err := cj.Read(3)
	if err != nil {
		t.Fatalf("Read(3): %v", err)
	}
	if len(got3) != 3 {
		t.Fatalf("Read(3) got %d want 3", len(got3))
	}
	if got3[0].Sequence != 3 {
		t.Fatalf("Read(3) first seq=%d want 3", got3[0].Sequence)
	}

	// A duplicate sequence (gap-free but duplicate) must be rejected.
	dup := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      3,
		Timestamp:     now,
		EventKind:     InvokeJobEventProgressRef,
		Payload:       `{"dup":true}`,
	}
	if err := cj.Append(dup); err == nil {
		t.Fatal("Append duplicate sequence must fail")
	}
	// Out-of-order (gap): sequence 7 when last is 5 must be rejected.
	gap := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      7,
		Timestamp:     now,
		EventKind:     InvokeJobEventProgressRef,
		Payload:       `{"gap":true}`,
	}
	if err := cj.Append(gap); err == nil {
		t.Fatal("Append with gap must fail")
	}
}

// TestControlJournal_OversizedEventRejected verifies events > 64KB are rejected
// (spec line 284: "Bounded sizes: reject writes > 64KB per event").
func TestControlJournal_OversizedEventRejected(t *testing.T) {
	cj, _, _ := newTestControlJournal(t)
	big := strings.Repeat("x", (64*1024)+1)
	ev := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     time.Now().UTC(),
		EventKind:     InvokeJobEventProgressRef,
		Payload:       big,
	}
	if err := cj.Append(ev); err == nil {
		t.Fatal("Append oversized event must fail")
	}
}

// TestControlJournal_SymlinkTraversalRejected verifies that a symlink placed
// inside the control directory cannot be used to write or read outside the
// control root.
func TestControlJournal_SymlinkTraversalRejected(t *testing.T) {
	cj, stateRoot, runID := newTestControlJournal(t)
	controlDir := filepath.Join(stateRoot, "runs", runID, "control", "att-test-1")
	// Place a malicious symlink inside the control dir pointing outside.
	evil := filepath.Join(controlDir, "event-0000000001.json")
	outside := filepath.Join(stateRoot, "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, evil); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Append with seq 1 must refuse to write through the symlink.
	ev := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     time.Now().UTC(),
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"evil":true}`,
	}
	if err := cj.Append(ev); err == nil {
		t.Fatal("Append through symlink must be rejected")
	}
	// The outside secret file must be untouched.
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret" {
		t.Fatalf("outside file corrupted: %q", got)
	}
}

// TestControlJournal_HMACVerificationFailsOnTamper verifies that tampering with
// a written event (modifying the payload after write) causes Read to fail with
// an HMAC verification error.
func TestControlJournal_HMACVerificationFailsOnTamper(t *testing.T) {
	cj, stateRoot, runID := newTestControlJournal(t)
	ev := InvokeJobEvent{
		SchemaVersion: invokeJobSchemaVersionV1,
		Sequence:      1,
		Timestamp:     time.Now().UTC(),
		EventKind:     InvokeJobEventAccepted,
		Payload:       `{"msg":"original"}`,
	}
	if err := cj.Append(ev); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Read must succeed before tamper.
	got, err := cj.Read(1)
	if err != nil {
		t.Fatalf("Read before tamper: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d want 1", len(got))
	}

	// Tamper: rewrite the event file with a changed payload but the same HMAC.
	ef := filepath.Join(stateRoot, "runs", runID, "control", "att-test-1", "event-0000000001.json")
	orig, err := os.ReadFile(ef)
	if err != nil {
		t.Fatal(err)
	}
	var rec InvokeJobEvent
	if err := json.Unmarshal(orig, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Payload = `{"msg":"tampered"}`
	tampered, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(ef, tampered, filePerm); err != nil {
		t.Fatalf("rewrite tampered: %v", err)
	}
	// Read must now fail because the HMAC no longer matches the payload.
	if _, err := cj.Read(1); err == nil {
		t.Fatal("Read after tamper must fail (HMAC mismatch)")
	}
}

// itoa is a tiny helper to avoid importing strconv in the test.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
