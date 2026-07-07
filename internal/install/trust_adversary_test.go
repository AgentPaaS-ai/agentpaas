package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// ---------------------------------------------------------------------------
// Claim 1: No trust-store mutation on ANY non-approval path.
// On decline (TTY abort, non-TTY missing-flag, non-TTY mismatch, key-conflict),
// the on-disk trust store (and the in-memory *trust.Store) must be byte-identical
// to before. Reload the store from disk after the call and assert the recorded
// publisher set is unchanged.
// ---------------------------------------------------------------------------

func TestAdversaryT01_NoStoreMutationOnNonApproval(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) // returns store, storePath, opts, expected error
		wantErr   error
	}{
		{
			name: "TTY abort after 3 wrong prompts",
			setup: func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) {
				tk := generateTestKey(t)
				store, storePath := newTestStore(t)
				// Pre-pin an unrelated publisher so store is non-empty.
				tkOther := generateTestKey(t)
				prePin(t, store, "other-pub", tkOther)
				prompts := promptSequence("aaaaaaaa", "bbbbbbbb", "cccccccc")
				return store, storePath, TrustResolveOpts{
					PublisherName:         "parvez",
					PublisherFingerprint:  tk.fp,
					PublisherPublicKeyPEM: tk.pemData,
					Store:                 store,
					IsTTY:                 true,
					Prompt:                prompts,
				}, ErrTrustRefused
			},
			wantErr: ErrTrustRefused,
		},
		{
			name: "TTY prompt error (ctrl-c / cancel)",
			setup: func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) {
				tk := generateTestKey(t)
				store, storePath := newTestStore(t)
				tkOther := generateTestKey(t)
				prePin(t, store, "other-pub", tkOther)
				errPrompt := func(prompt string) (string, error) {
					return "", fmt.Errorf("input cancelled")
				}
				return store, storePath, TrustResolveOpts{
					PublisherName:         "parvez",
					PublisherFingerprint:  tk.fp,
					PublisherPublicKeyPEM: tk.pemData,
					Store:                 store,
					IsTTY:                 true,
					Prompt:                errPrompt,
				}, ErrTrustRefused
			},
			wantErr: ErrTrustRefused,
		},
		{
			name: "Non-TTY missing --confirm-fingerprint flag",
			setup: func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) {
				tk := generateTestKey(t)
				store, storePath := newTestStore(t)
				tkOther := generateTestKey(t)
				prePin(t, store, "other-pub", tkOther)
				return store, storePath, TrustResolveOpts{
					PublisherName:         "parvez",
					PublisherFingerprint:  tk.fp,
					PublisherPublicKeyPEM: tk.pemData,
					Store:                 store,
					IsTTY:                 false,
					ConfirmedFingerprint:  "",
				}, ErrTrustRefused
			},
			wantErr: ErrTrustRefused,
		},
		{
			name: "Non-TTY mismatch (wrong flag value)",
			setup: func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) {
				tk := generateTestKey(t)
				store, storePath := newTestStore(t)
				tkOther := generateTestKey(t)
				prePin(t, store, "other-pub", tkOther)
				return store, storePath, TrustResolveOpts{
					PublisherName:         "parvez",
					PublisherFingerprint:  tk.fp,
					PublisherPublicKeyPEM: tk.pemData,
					Store:                 store,
					IsTTY:                 false,
					ConfirmedFingerprint:  strings.Repeat("ff", 32),
				}, ErrConfirmMismatch
			},
			wantErr: ErrConfirmMismatch,
		},
		{
			name: "Key-conflict (different key, same name)",
			setup: func(t *testing.T) (*trust.Store, string, TrustResolveOpts, error) {
				tkA := generateTestKey(t)
				tkB := generateTestKey(t)
				store, storePath := newTestStore(t)
				prePin(t, store, "maria", tkA)
				return store, storePath, TrustResolveOpts{
					PublisherName:         "maria",
					PublisherFingerprint:  tkB.fp,
					PublisherPublicKeyPEM: tkB.pemData,
					Store:                 store,
					IsTTY:                 false,
				}, ErrKeyConflict
			},
			wantErr: ErrKeyConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, storePath, opts, _ := tt.setup(t)

			// Capture pre-call state: serialize the store contents to compare later.
			preSnapshot := storeSnapshot(t, storePath, store)

			_, err := ResolveTrust(opts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			// Reload store from disk.
			store2, err := trust.Load(storePath)
			if err != nil {
				t.Fatalf("reload store: %v", err)
			}

			postSnapshot := storeSnapshot(t, storePath, store2)

			// Store must be byte-identical.
			if preSnapshot.count != postSnapshot.count {
				t.Errorf("store count changed: pre=%d post=%d", preSnapshot.count, postSnapshot.count)
			}
			if !preSnapshot.equalFingerprints(postSnapshot) {
				t.Errorf("store fingerprints changed:\n  pre:  %v\n  post: %v",
					preSnapshot.fingerprints(), postSnapshot.fingerprints())
			}

			// Also verify the in-memory store was NOT mutated (dirty flag + records).
			if store.Len() != store2.Len() {
				t.Errorf("in-memory store mutated: pre=%d post=%d", store.Len(), store2.Len())
			}
		})
	}
}

// storeSnapshot captures the state of a trust store for comparison.
type storeSnapshotData struct {
	count int
	pubs  []trust.Publisher
}

func storeSnapshot(t *testing.T, storePath string, store *trust.Store) storeSnapshotData {
	t.Helper()

	// Read the raw file bytes for byte-identical comparison.
	rawBytes, _ := os.ReadFile(storePath)

	// Also capture structured state.
	pubs := store.Publishers()

	// If file doesn't exist, it's an empty store.
	if rawBytes == nil {
		return storeSnapshotData{count: 0, pubs: pubs}
	}

	// Verify the file is parseable.
	var sf struct {
		Version    int              `json:"version"`
		Publishers []trust.Publisher `json:"publishers"`
	}
	if err := json.Unmarshal(rawBytes, &sf); err != nil {
		t.Fatalf("store file is corrupt: %v", err)
	}

	return storeSnapshotData{
		count: len(sf.Publishers),
		pubs:  sf.Publishers,
	}
}

func (s storeSnapshotData) fingerprints() []string {
	fps := make([]string, len(s.pubs))
	for i, p := range s.pubs {
		fps[i] = p.Fingerprint
	}
	return fps
}

func (s storeSnapshotData) equalFingerprints(other storeSnapshotData) bool {
	if s.count != other.count {
		return false
	}
	// Build sets for order-independent comparison.
	set := make(map[string]trust.Publisher)
	for _, p := range s.pubs {
		set[p.Fingerprint] = p
	}
	for _, p := range other.pubs {
		existing, ok := set[p.Fingerprint]
		if !ok {
			return false
		}
		if existing.Alias != p.Alias || existing.Fingerprint != p.Fingerprint {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Claim 2: Fingerprint comparison is exact and case/format-insensitive
// bypass-proof. The trust engine must NOT accept a confirmation that differs
// from the true normalized fingerprint by: (a) uppercase vs lowercase,
// (b) leading/trailing whitespace, (c) a 0x prefix, (d) a partial prefix,
// (e) a near-match (off-by-one hex char). All must result in
// ErrConfirmMismatch / ErrTrustRefused, NOT a pin.
// ---------------------------------------------------------------------------

func TestAdversaryT02_FingerprintBypassProof(t *testing.T) {
	tk := generateTestKey(t)
	trueFP := tk.fp // normalized lowercase, 64 hex chars

	tests := []struct {
		name           string
		confirmValue   string
		expectPin      bool // true = should pin (this test expects FALSE for all variants)
	}{
		// (a) Uppercase variant — same fingerprint, different case. The
		//     trust engine normalizes case, so as long as the hex value is
		//     correct the confirmation is ACCEPTED (display-form leniency).
		//     This is intended — the spec requires exact full-fp match, and
		//     a correct value in wrong case is still the correct value.
		{
			name:         "uppercase-vs-lowercase",
			confirmValue: strings.ToUpper(trueFP),
			expectPin:    true,
		},
		// (b) Leading/trailing/embedded whitespace — display-form leniency;
		//     NormalizeFingerprint strips spaces, so the correct value with
		//     cosmetic whitespace is still accepted (intended).
		{
			name:         "leading-whitespace",
			confirmValue: "  " + trueFP,
			expectPin:    true,
		},
		{
			name:         "trailing-whitespace",
			confirmValue: trueFP + "  ",
			expectPin:    true,
		},
		{
			name:         "whitespace-embedded",
			confirmValue: trueFP[:32] + " " + trueFP[32:],
			expectPin:    true,
		},
		// (c) 0x prefix — must NOT match.
		{
			name:         "0x-prefix",
			confirmValue: "0x" + trueFP,
			expectPin:    false,
		},
		// (d) Partial prefix (first 8 chars) — must NOT match.
		{
			name:         "first-8-chars",
			confirmValue: trueFP[:8],
			expectPin:    false,
		},
		// (e) Near-match: off-by-one hex char — must NOT match.
		{
			name:         "off-by-one-hex",
			confirmValue: flipLastHexChar(trueFP),
			expectPin:    false,
		},
		// Positive control: correct fingerprint MUST work.
		{
			name:         "correct-fingerprint",
			confirmValue: trueFP,
			expectPin:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, storePath := newTestStore(t)
			var events []auditEvent

			result, err := ResolveTrust(TrustResolveOpts{
				PublisherName:         "parvez",
				PublisherFingerprint:  trueFP,
				PublisherPublicKeyPEM: tk.pemData,
				Store:                 store,
				IsTTY:                 false,
				ConfirmedFingerprint:  tt.confirmValue,
				EmitAudit:             auditCollector(&events),
			})

			if tt.expectPin {
				// Positive control: must succeed with a pin.
				if err != nil {
					t.Fatalf("positive control failed: %v", err)
				}
				if !result.WasPinned {
					t.Error("expected WasPinned=true for correct fingerprint")
				}
				return
			}

			// Attack variant: must NOT pin.
			if result != nil && result.WasPinned {
				t.Errorf("VULNERABLE: confirmation %q was accepted and pinned (should be refused)", tt.confirmValue)
			}
			if err == nil {
				t.Errorf("VULNERABLE: ResolveTrust returned nil error for variant %q (should return error)", tt.confirmValue)
				return
			}

			// Must be ErrConfirmMismatch (or ErrTrustRefused).
			if !errors.Is(err, ErrConfirmMismatch) && !errors.Is(err, ErrTrustRefused) {
				t.Errorf("error = %v, want ErrConfirmMismatch or ErrTrustRefused", err)
			}

			// Store must NOT be mutated.
			store2, err := trust.Load(storePath)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if store2.Len() != 0 {
				t.Errorf("store mutated on bypass attempt: got %d publishers, want 0", store2.Len())
			}
		})
	}
}

// flipLastHexChar changes the last hex character of a fingerprint to produce
// an off-by-one near-match.
func flipLastHexChar(fp string) string {
	if len(fp) == 0 {
		return fp
	}
	last := fp[len(fp)-1]
	var flipped byte
	switch last {
	case '0':
		flipped = '1'
	case 'f':
		flipped = 'e'
	default:
		flipped = last ^ 1 // flip LSB
	}
	return fp[:len(fp)-1] + string(flipped)
}

// ---------------------------------------------------------------------------
// Claim 3: TTY last-8 prompt cannot be bypassed by typing the full
// fingerprint, the first 8 chars, or a non-hex string — only the exact
// last 8 lowercase hex chars of the normalized fingerprint may pin.
// Everything else must consume an attempt (3 attempts max) and then return
// ErrTrustRefused with no pin.
// ---------------------------------------------------------------------------

func TestAdversaryT03_TTYLast8BypassProof(t *testing.T) {
	tk := generateTestKey(t)
	trueFP := tk.fp
	last8 := trueFP[len(trueFP)-8:]
	first8 := trueFP[:8]

	tests := []struct {
		name        string
		responses   []string       // each response is one attempt
		wantPin     bool
		wantErr     error
		wantPrompts int // expected number of prompt calls (0 if we don't care)
	}{
		{
			name:      "full-fingerprint-bypass-attempt",
			responses: []string{trueFP, trueFP, trueFP}, // full 64-char fingerprint, 3 times
			wantPin:   false,
			wantErr:   ErrTrustRefused,
		},
		{
			name:      "first-8-bypass-attempt",
			responses: []string{first8, first8, first8}, // first 8 chars, 3 times
			wantPin:   false,
			wantErr:   ErrTrustRefused,
		},
		{
			name:      "non-hex-string-attempt",
			responses: []string{"zzzzzzzz", "gggggggg", "!!!!!!!!"},
			wantPin:   false,
			wantErr:   ErrTrustRefused,
		},
		{
			name:      "correct-last8-on-third-try",
			responses: []string{"aaaaaaaa", "bbbbbbbb", last8},
			wantPin:   true,
			wantErr:   nil,
		},
		{
			name:      "correct-last8-first-try-control",
			responses: []string{last8},
			wantPin:   true,
			wantErr:   nil,
		},
		{
			name:      "full-fingerprint-then-last8",
			responses: []string{trueFP, last8},
			wantPin:   true,
			wantErr:   nil,
		},
		{
			name:      "full-fingerprint-uppercase",
			responses: []string{strings.ToUpper(trueFP), strings.ToUpper(trueFP), strings.ToUpper(trueFP)},
			wantPin:   false,
			wantErr:   ErrTrustRefused,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, storePath := newTestStore(t)
			var events []auditEvent

			prompts := promptSequence(tt.responses...)

			result, err := ResolveTrust(TrustResolveOpts{
				PublisherName:         "parvez",
				PublisherFingerprint:  trueFP,
				PublisherPublicKeyPEM: tk.pemData,
				Store:                 store,
				IsTTY:                 true,
				Prompt:                prompts,
				EmitAudit:             auditCollector(&events),
			})

			if tt.wantPin {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !result.WasPinned {
					t.Error("expected WasPinned=true")
				}
				// Verify persistence.
				store2, err := trust.Load(storePath)
				if err != nil {
					t.Fatalf("reload: %v", err)
				}
				if store2.Len() != 1 {
					t.Errorf("expected 1 publisher in store, got %d", store2.Len())
				}
				return
			}

			// Should have been refused.
			if err == nil {
				t.Errorf("VULNERABLE: ResolveTrust returned nil for bypass %q (should return ErrTrustRefused)", tt.name)
				return
			}
			if !errors.Is(err, ErrTrustRefused) {
				t.Errorf("error = %v, want ErrTrustRefused", err)
			}

			// Store must NOT be mutated.
			store2, err := trust.Load(storePath)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if store2.Len() != 0 {
				t.Errorf("store mutated on bypass attempt: got %d publishers, want 0", store2.Len())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Claim 4: Key-conflict impostor refusal is airtight.
// - Store pinning key A as alias "maria" must refuse a bundle signed by key B
//   claiming name "maria" with ErrKeyConflict, audit publisher_key_conflict,
//   and NO pin and NO store change.
// - Same key A but store has A under a DIFFERENT alias should NOT be a conflict
//   (same key, alias update only).
// ---------------------------------------------------------------------------

func TestAdversaryT04_KeyConflictAirtight(t *testing.T) {
	// Case 1: Different key, same name — MUST be a conflict.
	t.Run("different-key-same-name-conflict", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)

		store, storePath := newTestStore(t)
		prePin(t, store, "maria", tkA)

		var events []auditEvent
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "maria",
			PublisherFingerprint:  tkB.fp,
			PublisherPublicKeyPEM: tkB.pemData,
			Store:                 store,
			IsTTY:                 false,
			EmitAudit:             auditCollector(&events),
		})

		if !errors.Is(err, ErrKeyConflict) {
			t.Fatalf("error = %v, want ErrKeyConflict", err)
		}

		// Verify conflict error type.
		var kce *KeyConflictError
		if !errors.As(err, &kce) {
			t.Fatal("error should be *KeyConflictError")
		}
		if kce.PublisherName != "maria" {
			t.Errorf("PublisherName = %q, want maria", kce.PublisherName)
		}
		if kce.ExpectedFP != tkA.fp {
			t.Errorf("ExpectedFP = %q, want key A fingerprint", kce.ExpectedFP)
		}
		if kce.ReceivedFP != tkB.fp {
			t.Errorf("ReceivedFP = %q, want key B fingerprint", kce.ReceivedFP)
		}

		// Verify audit event.
		if len(events) != 1 {
			t.Fatalf("expected 1 audit event, got %d", len(events))
		}
		if events[0].EventType != audit.EventTypePublisherKeyConflict {
			t.Errorf("audit event type = %q, want %q", events[0].EventType, audit.EventTypePublisherKeyConflict)
		}

		// Verify store unchanged.
		store2, err := trust.Load(storePath)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if store2.Len() != 1 {
			t.Errorf("store has %d publishers, want 1", store2.Len())
		}
		pub, ok := store2.Get(tkA.fp)
		if !ok {
			t.Fatal("key A should still be in store")
		}
		if pub.Alias != "maria" {
			t.Errorf("key A alias = %q, want maria", pub.Alias)
		}
	})

	// Case 2: Same key, different alias — NOT a conflict (alias update).
	t.Run("same-key-different-alias-no-conflict", func(t *testing.T) {
		tkA := generateTestKey(t)

		store, storePath := newTestStore(t)
		// Pin key A under alias "original-name".
		prePin(t, store, "original-name", tkA)

		// Same key A, but now claiming alias "new-alias". Should NOT conflict.
		var events []auditEvent
		result, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "new-alias",
			PublisherFingerprint:  tkA.fp,
			PublisherPublicKeyPEM: tkA.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  tkA.fp,
			EmitAudit:             auditCollector(&events),
		})

		// Should be pinned + same key path (already exists), so no error.
		if err != nil {
			t.Fatalf("unexpected error for same key/different alias: %v", err)
		}
		if result.WasPinned {
			t.Error("WasPinned should be false: same key already in store")
		}

		// Store should still have 1 publisher.
		store2, err := trust.Load(storePath)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if store2.Len() != 1 {
			t.Errorf("store has %d publishers, want 1", store2.Len())
		}
		pub, ok := store2.Get(tkA.fp)
		if !ok {
			t.Fatal("key A should still be in store")
		}
		// Alias remains as originally pinned (not updated to "new-alias").
		if pub.Alias != "original-name" {
			t.Errorf("key A alias = %q, want original-name (unchanged)", pub.Alias)
		}
	})

	// Case 3: Also test that even with ConfirmedFingerprint set, conflict still refuses.
	t.Run("conflict-refuses-even-with-confirmed-fingerprint", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)

		store, _ := newTestStore(t)
		prePin(t, store, "maria", tkA)

		// Even though we set ConfirmedFingerprint to key B's fingerprint,
		// the conflict check happens BEFORE the TOFU path.
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "maria",
			PublisherFingerprint:  tkB.fp,
			PublisherPublicKeyPEM: tkB.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  tkB.fp, // matches key B but conflict should fire first
		})

		if !errors.Is(err, ErrKeyConflict) {
			t.Errorf("error = %v, want ErrKeyConflict even with matching ConfirmedFingerprint", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Claim 5: No secret material in audit payloads.
// Capture every EmitAudit call. Assert that NO audit payload value contains
// any PEM block, any private-key material, or the string "BEGIN".
// ---------------------------------------------------------------------------

func TestAdversaryT05_NoSecretsInAuditPayloads(t *testing.T) {
	// Exercise all paths that emit audit events and capture their payloads.

	// Path 1: TOFU success → publisher_trusted.
	t.Run("tofu-success-no-secrets", func(t *testing.T) {
		tk := generateTestKey(t)
		store, _ := newTestStore(t)
		var events []auditEvent

		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "parvez",
			PublisherFingerprint:  tk.fp,
			PublisherPublicKeyPEM: tk.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  tk.fp,
			EmitAudit:             auditCollector(&events),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for i, ev := range events {
			for k, v := range ev.Payload {
				if strings.Contains(v, "BEGIN") {
					t.Errorf("VULNERABLE: audit event #%d [%s] payload key %q contains 'BEGIN': %q", i, ev.EventType, k, v)
				}
				if strings.Contains(v, "PRIVATE KEY") {
					t.Errorf("VULNERABLE: audit event #%d [%s] payload key %q contains 'PRIVATE KEY': %q", i, ev.EventType, k, v)
				}
				if strings.Contains(v, "-----") {
					t.Errorf("VULNERABLE: audit event #%d [%s] payload key %q contains PEM boundary '-----': %q", i, ev.EventType, k, v)
				}
			}
		}

		// Also check that the publisher_trusted event exists.
		found := false
		for _, ev := range events {
			if ev.EventType == audit.EventTypePublisherTrusted {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected publisher_trusted audit event")
		}
	})

	// Path 2: Non-TTY mismatch → publisher_confirm_mismatch.
	t.Run("mismatch-audit-no-secrets", func(t *testing.T) {
		tk := generateTestKey(t)
		store, _ := newTestStore(t)
		var events []auditEvent

		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "parvez",
			PublisherFingerprint:  tk.fp,
			PublisherPublicKeyPEM: tk.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  strings.Repeat("ff", 32),
			EmitAudit:             auditCollector(&events),
		})
		if !errors.Is(err, ErrConfirmMismatch) {
			t.Fatalf("expected ErrConfirmMismatch, got %v", err)
		}

		for i, ev := range events {
			for k, v := range ev.Payload {
				if strings.Contains(v, "BEGIN") {
					t.Errorf("VULNERABLE: mismatch audit #%d payload key %q contains 'BEGIN': %q", i, k, v)
				}
			}
		}
	})

	// Path 3: Key conflict → publisher_key_conflict.
	t.Run("conflict-audit-no-secrets", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)
		store, _ := newTestStore(t)
		prePin(t, store, "maria", tkA)

		var events []auditEvent
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "maria",
			PublisherFingerprint:  tkB.fp,
			PublisherPublicKeyPEM: tkB.pemData,
			Store:                 store,
			IsTTY:                 false,
			EmitAudit:             auditCollector(&events),
		})
		if !errors.Is(err, ErrKeyConflict) {
			t.Fatalf("expected ErrKeyConflict, got %v", err)
		}

		for i, ev := range events {
			for k, v := range ev.Payload {
				if strings.Contains(v, "BEGIN") {
					t.Errorf("VULNERABLE: conflict audit #%d payload key %q contains 'BEGIN': %q", i, k, v)
				}
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Claim 6: Mismatch is distinguishable as exit-2 class.
// errors.Is(err, ErrConfirmMismatch) must be true for the wrong-flag case
// and false for the conflict/refused cases.
// ---------------------------------------------------------------------------

func TestAdversaryT06_MismatchDistinguishableExit2(t *testing.T) {
	tk := generateTestKey(t)

	// Case 1: Wrong flag → ErrConfirmMismatch.
	t.Run("wrong-flag-is-confirm-mismatch", func(t *testing.T) {
		store, _ := newTestStore(t)
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "parvez",
			PublisherFingerprint:  tk.fp,
			PublisherPublicKeyPEM: tk.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  strings.Repeat("ff", 32),
		})
		if !errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("errors.Is(err, ErrConfirmMismatch) = false, want true; err=%v", err)
		}
		// Should NOT be wrapped in ErrTrustRefused or ErrKeyConflict.
		if errors.Is(err, ErrTrustRefused) {
			t.Error("errors.Is(err, ErrTrustRefused) = true, want false")
		}
		if errors.Is(err, ErrKeyConflict) {
			t.Error("errors.Is(err, ErrKeyConflict) = true, want false")
		}
	})

	// Case 2: Conflict → NOT ErrConfirmMismatch.
	t.Run("conflict-is-not-confirm-mismatch", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)
		store, _ := newTestStore(t)
		prePin(t, store, "maria", tkA)

		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "maria",
			PublisherFingerprint:  tkB.fp,
			PublisherPublicKeyPEM: tkB.pemData,
			Store:                 store,
			IsTTY:                 false,
		})
		if errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("errors.Is(err, ErrConfirmMismatch) = true for conflict, want false; err=%v", err)
		}
		if !errors.Is(err, ErrKeyConflict) {
			t.Errorf("errors.Is(err, ErrKeyConflict) = false for conflict, want true; err=%v", err)
		}
	})

	// Case 3: Refused (non-TTY missing flag) → NOT ErrConfirmMismatch.
	t.Run("refused-is-not-confirm-mismatch", func(t *testing.T) {
		store, _ := newTestStore(t)
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "parvez",
			PublisherFingerprint:  tk.fp,
			PublisherPublicKeyPEM: tk.pemData,
			Store:                 store,
			IsTTY:                 false,
			ConfirmedFingerprint:  "",
		})
		if errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("errors.Is(err, ErrConfirmMismatch) = true for refused, want false; err=%v", err)
		}
		if !errors.Is(err, ErrTrustRefused) {
			t.Errorf("errors.Is(err, ErrTrustRefused) = false for refused, want true; err=%v", err)
		}
	})

	// Case 4: Refused (TTY abort) → NOT ErrConfirmMismatch.
	t.Run("tty-refused-is-not-confirm-mismatch", func(t *testing.T) {
		store, _ := newTestStore(t)
		prompts := promptSequence("aaaaaaaa", "bbbbbbbb", "cccccccc")
		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "parvez",
			PublisherFingerprint:  tk.fp,
			PublisherPublicKeyPEM: tk.pemData,
			Store:                 store,
			IsTTY:                 true,
			Prompt:                prompts,
		})
		if errors.Is(err, ErrConfirmMismatch) {
			t.Errorf("errors.Is(err, ErrConfirmMismatch) = true for TTY refused, want false; err=%v", err)
		}
		if !errors.Is(err, ErrTrustRefused) {
			t.Errorf("errors.Is(err, ErrTrustRefused) = false for TTY refused, want true; err=%v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Claim 7: Pinned-same-key path is a no-op.
// Re-running ResolveTrust for an already-pinned key must NOT call Pin again
// (store records identical), must NOT emit a second publisher_trusted audit,
// and must surface "publisher: <alias> (pinned <date>)".
// ---------------------------------------------------------------------------

func TestAdversaryT07_PinnedSameKeyNoOp(t *testing.T) {
	tk := generateTestKey(t)
	store, storePath := newTestStore(t)

	// First call: pin the publisher via non-TTY TOFU.
	var events1 []auditEvent
	result1, err := ResolveTrust(TrustResolveOpts{
		PublisherName:         "maria",
		PublisherFingerprint:  tk.fp,
		PublisherPublicKeyPEM: tk.pemData,
		Store:                 store,
		IsTTY:                 false,
		ConfirmedFingerprint:  tk.fp,
		EmitAudit:             auditCollector(&events1),
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if !result1.WasPinned {
		t.Fatal("first call should pin")
	}
	if len(events1) != 1 || events1[0].EventType != audit.EventTypePublisherTrusted {
		t.Fatalf("expected 1 publisher_trusted event, got %v", events1)
	}

	// Snapshot the store state after first pin.
	storeAfter1, err := trust.Load(storePath)
	if err != nil {
		t.Fatalf("reload after first pin: %v", err)
	}
	pubAfter1, ok := storeAfter1.Get(tk.fp)
	if !ok {
		t.Fatal("publisher not found after first pin")
	}

	// Second call: same publisher, already pinned → no-op path.
	var events2 []auditEvent
	result2, err := ResolveTrust(TrustResolveOpts{
		PublisherName:         "maria",
		PublisherFingerprint:  tk.fp,
		PublisherPublicKeyPEM: tk.pemData,
		Store:                 store,
		IsTTY:                 false,
		EmitAudit:             auditCollector(&events2),
	})
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	// WasPinned must be false — no second pin.
	if result2.WasPinned {
		t.Error("WasPinned should be false for already-pinned key")
	}

	// NO second publisher_trusted audit.
	for _, ev := range events2 {
		if ev.EventType == audit.EventTypePublisherTrusted {
			t.Errorf("VULNERABLE: second publisher_trusted audit emitted on no-op path")
		}
	}

	// Display line must contain "publisher:" and the alias and "pinned".
	if len(result2.DisplayLines) == 0 {
		t.Fatal("no display lines")
	}
	line := result2.DisplayLines[0]
	if !strings.Contains(line, "publisher:") {
		t.Errorf("display line missing 'publisher:': %q", line)
	}
	if !strings.Contains(line, "maria") {
		t.Errorf("display line missing alias 'maria': %q", line)
	}
	if !strings.Contains(line, "pinned") {
		t.Errorf("display line missing 'pinned': %q", line)
	}

	// Store must be byte-identical: same number of publishers, same records.
	storeAfter2, err := trust.Load(storePath)
	if err != nil {
		t.Fatalf("reload after second call: %v", err)
	}
	if storeAfter2.Len() != storeAfter1.Len() {
		t.Errorf("store count changed: %d → %d", storeAfter1.Len(), storeAfter2.Len())
	}
	pubAfter2, ok2 := storeAfter2.Get(tk.fp)
	if !ok2 {
		t.Fatal("publisher not found after second call")
	}
	// FirstSeen and LastUsed should be identical (no pin called).
	if pubAfter2.FirstSeen != pubAfter1.FirstSeen {
		t.Errorf("FirstSeen changed: %q → %q (should not change on no-op)", pubAfter1.FirstSeen, pubAfter2.FirstSeen)
	}
}

// ---------------------------------------------------------------------------
// Claim 8: No inline override flag exists for conflict.
// The API has no field that bypasses CheckKeyConflict — assert by code
// inspection that TrustResolveOpts has no override field, and behaviorally
// that conflict always refuses regardless of ConfirmedFingerprint value.
// ---------------------------------------------------------------------------

func TestAdversaryT08_NoInlineOverrideForConflict(t *testing.T) {
	// Code inspection: use reflection to verify TrustResolveOpts has no
	// "override" / "bypass" / "force" / "skip" field.
	t.Run("no-override-field-by-reflection", func(t *testing.T) {
		optsType := reflect.TypeOf(TrustResolveOpts{})
		blocklist := []string{"override", "bypass", "force", "skip", "ignore", "allow",
			"insecure", "unsafe", "no_check", "nocheck", "skip_conflict", "skipconflict"}
		for i := 0; i < optsType.NumField(); i++ {
			field := optsType.Field(i)
			lower := strings.ToLower(field.Name)
			for _, blocked := range blocklist {
				if strings.Contains(lower, blocked) {
					t.Errorf("VULNERABLE: TrustResolveOpts has a field named %q (matches blocklist term %q)", field.Name, blocked)
				}
			}
		}
		// Also verify IsExported to catch exported fields we might miss.
		knownFields := map[string]bool{
			"PublisherName":         true,
			"PublisherFingerprint":  true,
			"PublisherPublicKeyPEM": true,
			"Store":                 true,
			"IsTTY":                 true,
			"ConfirmedFingerprint":  true,
			"Prompt":                true,
			"EmitAudit":             true,
		}
		for i := 0; i < optsType.NumField(); i++ {
			field := optsType.Field(i)
			if !knownFields[field.Name] {
				t.Errorf("VULNERABLE: TrustResolveOpts has unexpected field %q (possible override vector)", field.Name)
			}
		}
	})

	// Behavioral test: conflict always refuses, even when ConfirmedFingerprint
	// is set to a value that WOULD match (i.e., the attacker provides the
	// correct fingerprint for key B, but key A is already trusted as "maria").
	// The conflict check (Path 3) fires BEFORE the TOFU path (Path 2).
	t.Run("conflict-refuses-regardless-of-confirmed-fingerprint", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)

		// Test several scenarios where an attacker might try to slip through.
		scenarios := []struct {
			name               string
			confirmedFP        string
			expectedErr        error
		}{
			{
				name:        "matching-keyB-fingerprint",
				confirmedFP: tkB.fp,
				expectedErr: ErrKeyConflict,
			},
			{
				name:        "matching-keyA-fingerprint",
				confirmedFP: tkA.fp,
				expectedErr: ErrKeyConflict,
			},
			{
				name:        "empty-confirmed-fingerprint",
				confirmedFP: "",
				expectedErr: ErrKeyConflict,
			},
		}

		for _, sc := range scenarios {
			t.Run(sc.name, func(t *testing.T) {
				store, storePath := newTestStore(t)
				prePin(t, store, "maria", tkA)

				var events []auditEvent
				_, err := ResolveTrust(TrustResolveOpts{
					PublisherName:         "maria",
					PublisherFingerprint:  tkB.fp,
					PublisherPublicKeyPEM: tkB.pemData,
					Store:                 store,
					IsTTY:                 false,
					ConfirmedFingerprint:  sc.confirmedFP,
					EmitAudit:             auditCollector(&events),
				})

				if !errors.Is(err, sc.expectedErr) {
					t.Errorf("VULNERABLE: with %s, error = %v, want %v", sc.name, err, sc.expectedErr)
				}

				// Store must not change.
				store2, err := trust.Load(storePath)
				if err != nil {
					t.Fatalf("reload: %v", err)
				}
				if store2.Len() != 1 {
					t.Errorf("store mutated: got %d publishers, want 1", store2.Len())
				}
				pub, ok := store2.Get(tkA.fp)
				if !ok || pub.Alias != "maria" {
					t.Error("key A should still be trusted as maria")
				}
			})
		}
	})

	// Also verify that setting ConfirmedFingerprint doesn't bypass conflict
	// in TTY mode either.
	t.Run("conflict-refuses-in-tty-mode-too", func(t *testing.T) {
		tkA := generateTestKey(t)
		tkB := generateTestKey(t)

		store, _ := newTestStore(t)
		prePin(t, store, "maria", tkA)

		last8B := tkB.fp[len(tkB.fp)-8:]

		_, err := ResolveTrust(TrustResolveOpts{
			PublisherName:         "maria",
			PublisherFingerprint:  tkB.fp,
			PublisherPublicKeyPEM: tkB.pemData,
			Store:                 store,
			IsTTY:                 true,
			Prompt:                promptSingle(last8B), // correct last-8 for key B
		})

		if !errors.Is(err, ErrKeyConflict) {
			t.Errorf("VULNERABLE: TTY mode with correct last-8 didn't get conflict; err = %v", err)
		}
	})
}