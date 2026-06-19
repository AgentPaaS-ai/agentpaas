package identity

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// ADVERSARY TEST SUITE for KeyStore security claims.
// Each test exercises a specific attack vector or abuse case as defined
// in the AgentPaaS Block 3 Task T01 security spine.
// These tests are designed to break the implementation; failing tests
// indicate real security issues. Do NOT weaken assertions to make them pass.

// ---------------------------------------------------------------------------
// Vector 1: Signature forgery
// Sign with key A, claim it's from key B → Verify must return false.
// ---------------------------------------------------------------------------
//
// ADVERSARY BREAK: <none expected — existing contract test covers this>
func TestAdversary_SignatureForgery(t *testing.T) {
	ks := NewFakeKeyStore()
	m1 := testKeyMaterial(t, KeyTypeCA)
	m2 := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("key-a", KeyTypeCA, m1); err != nil {
		t.Fatal(err)
	}
	if err := ks.Create("key-b", KeyTypeCA, m2); err != nil {
		t.Fatal(err)
	}
	digest := []byte("some-digest")
	sig, err := ks.Sign("key-a", digest)
	if err != nil {
		t.Fatal(err)
	}
	// Verify signature from key-a against key-b → must be false
	if ok := ks.Verify("key-b", digest, sig); ok {
		t.Error("BREAK: Verify accepted signature from key-a when verifying against key-b")
	}
}

// ---------------------------------------------------------------------------
// Vector 2: Tampered digest
// Sign digest X, verify against digest Y → must fail.
// ---------------------------------------------------------------------------
func TestAdversary_TamperedDigest(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("key", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	sig, err := ks.Sign("key", []byte("original-digest"))
	if err != nil {
		t.Fatal(err)
	}
	if ok := ks.Verify("key", []byte("tampered-digest"), sig); ok {
		t.Error("BREAK: Verify accepted signature for tampered digest")
	}
}

// ---------------------------------------------------------------------------
// Vector 3: Cross-key-type confusion
// Create a CA key, try to use it where a workload cert is expected (and
// vice versa) → must error, not silently succeed.
// ---------------------------------------------------------------------------
func TestAdversary_CrossKeyTypeConfusion(t *testing.T) {
	ks := NewFakeKeyStore()

	// Workload key used for Sign → must error
	wlMaterial := testKeyMaterial(t, KeyTypeWorkload)
	if err := ks.Create("workload", KeyTypeWorkload, wlMaterial); err != nil {
		t.Fatal(err)
	}
	_, err := ks.Sign("workload", []byte("digest"))
	if err == nil {
		t.Error("BREAK: Sign with workload key succeeded, expected ErrWrongKeyType")
	}

	// Verify with workload key must return false, not panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("BREAK: Verify with workload key panicked: %v", r)
			}
		}()
		if ok := ks.Verify("workload", []byte("digest"), []byte("sig")); ok {
			t.Error("Verify with workload key returned true, expected false")
		}
	}()

	// Signing key loaded and treated as workload cert material
	caMaterial := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("ca-signing", KeyTypeCA, caMaterial); err != nil {
		t.Fatal(err)
	}
	loaded, err := ks.Load("ca-signing")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(loaded.Bytes), "CERTIFICATE") {
		t.Error("Loaded CA key material unexpectedly contains certificate data")
	}
}

// ---------------------------------------------------------------------------
// Vector 4: List() raw-material leak
// Assert List() returns NO raw key bytes, no private key material, no
// derivable prefixes. Marshal every KeyMetadata to JSON and grep for
// private key bytes / D field / X,Y coordinates. Any leak = HIGH break.
// ---------------------------------------------------------------------------
func TestAdversary_ListNoRawMaterialLeak(t *testing.T) {
	ks := NewFakeKeyStore()
	if err := ks.Create("leak-ca", KeyTypeCA, testKeyMaterial(t, KeyTypeCA)); err != nil {
		t.Fatal(err)
	}
	if err := ks.Create("leak-audit", KeyTypeAuditSigning, testKeyMaterial(t, KeyTypeAuditSigning)); err != nil {
		t.Fatal(err)
	}
	if err := ks.Create("leak-pkg", KeyTypePackageIdentity, testKeyMaterial(t, KeyTypePackageIdentity)); err != nil {
		t.Fatal(err)
	}
	if err := ks.Create("leak-wl", KeyTypeWorkload, testKeyMaterial(t, KeyTypeWorkload)); err != nil {
		t.Fatal(err)
	}

	meta, err := ks.List()
	if err != nil {
		t.Fatal(err)
	}

	// Marshal each to JSON and check for key material leaks
	for _, m := range meta {
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		js := string(data)

		// Check for PEM blocks
		if strings.Contains(js, "EC PRIVATE KEY") || strings.Contains(js, "PRIVATE KEY") {
			t.Errorf("BREAK: List entry %q JSON leaks private key PEM: %s", m.ID, js)
		}
		if strings.Contains(js, "BEGIN CERTIFICATE") {
			t.Errorf("BREAK: List entry %q JSON leaks certificate PEM: %s", m.ID, js)
		}

		// Verify only expected JSON fields are present
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		for k := range decoded {
			switch k {
			case "id", "type", "created_at":
				// allowed
			default:
				t.Errorf("BREAK: List entry %q JSON has unexpected field %q: %s", m.ID, k, js)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Vector 5: Empty/nil digest signing
// Sign(keyID, nil) or Sign(keyID, []byte{}) → must error or produce a
// deterministic signature that still fails verification against a different
// key. No panic.
// ---------------------------------------------------------------------------
func TestAdversary_NilDigestSign(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("nil-digest", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BREAK: Sign with nil digest panicked: %v", r)
		}
	}()
	sig, err := ks.Sign("nil-digest", nil)
	if err != nil {
		t.Logf("Sign with nil digest returned error (acceptable): %v", err)
		return
	}
	t.Logf("Sign with nil digest produced signature (len=%d)", len(sig))
	// If it produced a signature, verify against a different digest must fail
	if ok := ks.Verify("nil-digest", []byte("something-else"), sig); ok {
		t.Error("BREAK: Verify accepted nil-digest signature against different digest")
	}
}

func TestAdversary_EmptyDigestSign(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("empty-digest", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BREAK: Sign with empty digest panicked: %v", r)
		}
	}()
	sig, err := ks.Sign("empty-digest", []byte{})
	if err != nil {
		t.Logf("Sign with empty digest returned error (acceptable): %v", err)
		return
	}
	t.Logf("Sign with empty digest produced signature (len=%d)", len(sig))
	// Verify against a non-empty digest must fail
	if ok := ks.Verify("empty-digest", []byte("non-empty"), sig); ok {
		t.Error("BREAK: Verify accepted empty-digest signature against different digest")
	}
}

// ---------------------------------------------------------------------------
// Vector 6: Concurrent Create/Sign/Delete under -race
// Hammer the FakeKeyStore from many goroutines → no data race, no panics,
// no lost updates.
// ---------------------------------------------------------------------------
func TestAdversary_Concurrency(t *testing.T) {
	ks := NewFakeKeyStore()

	// Pre-create materials so we don't call testKeyMaterial inside goroutines
	const numKeys = 10
	materials := make([]KeyMaterial, numKeys)
	for i := 0; i < numKeys; i++ {
		materials[i] = testKeyMaterial(t, KeyTypeCA)
	}

	// Pre-create keys
	for i := 0; i < numKeys; i++ {
		id := KeyID(rune('a' + i))
		if err := ks.Create(id, KeyTypeCA, materials[i]); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan string, 100)

	ops := []func(id KeyID){
		func(id KeyID) {
			sig, err := ks.Sign(id, []byte("concurrent-digest"))
			if err != nil && err != ErrKeyNotFound {
				errCh <- "Sign: " + err.Error()
				return
			}
			if err == nil && len(sig) == 0 {
				errCh <- "Sign returned empty signature"
			}
		},
		func(id KeyID) {
			_ = ks.Verify(id, []byte("concurrent-digest"), []byte("fake-sig"))
		},
		func(id KeyID) {
			_ = ks.Create(id, KeyTypeCA, materials[0])
		},
		func(id KeyID) {
			_ = ks.Delete(id)
		},
		func(id KeyID) {
			_, err := ks.List()
			if err != nil {
				errCh <- "List: " + err.Error()
			}
		},
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := KeyID(rune('a' + n%numKeys))
			op := ops[n%len(ops)]
			op(id)
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []string
	for e := range errCh {
		errs = append(errs, e)
	}
	if len(errs) > 0 {
		t.Errorf("Concurrency errors: %s", strings.Join(errs, "; "))
	}
	t.Log("Concurrency test completed without panic or race")
}

// ---------------------------------------------------------------------------
// Vector 7: Delete-then-Load
// Delete a key, then Load it → must return ErrKeyNotFound, not stale data.
// ---------------------------------------------------------------------------
func TestAdversary_DeleteThenLoad(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("del-load", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	if err := ks.Delete("del-load"); err != nil {
		t.Fatal(err)
	}
	_, err := ks.Load("del-load")
	if err == nil {
		t.Error("BREAK: Load after Delete returned nil error, expected ErrKeyNotFound")
	}
	if err != ErrKeyNotFound {
		t.Errorf("Load after Delete: got %v, want ErrKeyNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Vector 8: Re-Create after delete
// Create same keyID after delete → should succeed (no ghost).
// ---------------------------------------------------------------------------
func TestAdversary_RecreateAfterDelete(t *testing.T) {
	ks := NewFakeKeyStore()
	material1 := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("recreate", KeyTypeCA, material1); err != nil {
		t.Fatal(err)
	}
	if err := ks.Delete("recreate"); err != nil {
		t.Fatal(err)
	}
	// Re-create must succeed (no ghost)
	material2 := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("recreate", KeyTypeCA, material2); err != nil {
		t.Errorf("BREAK: Re-Create after Delete failed: %v", err)
	}
	// Verify new material is stored
	loaded, err := ks.Load("recreate")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Bytes) == 0 {
		t.Error("Loaded key has empty bytes after recreate")
	}
}

// ---------------------------------------------------------------------------
// Vector 9: Sign with deleted key → ErrKeyNotFound.
// ---------------------------------------------------------------------------
func TestAdversary_SignDeletedKey(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("sign-del", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	if err := ks.Delete("sign-del"); err != nil {
		t.Fatal(err)
	}
	_, err := ks.Sign("sign-del", []byte("digest"))
	if err == nil {
		t.Error("BREAK: Sign with deleted key returned nil error, expected ErrKeyNotFound")
	}
	if err != ErrKeyNotFound {
		t.Errorf("Sign with deleted key: got %v, want ErrKeyNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Vector 10: Verify signature against a deleted key → clean error, not
// panic, not false-positive.
// ---------------------------------------------------------------------------
func TestAdversary_VerifyDeletedKey(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("vfy-del", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}
	sig, err := ks.Sign("vfy-del", []byte("digest"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ks.Delete("vfy-del"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BREAK: Verify against deleted key panicked: %v", r)
		}
	}()
	if ok := ks.Verify("vfy-del", []byte("digest"), sig); ok {
		t.Error("BREAK: Verify against deleted key returned true, expected false")
	}
}

// ---------------------------------------------------------------------------
// Vector 11: Large keyID / special-char keyID
// Empty string, very long, unicode, path-traversal-looking ids must be
// rejected or handled safely. Flag if the interface allows arbitrary ids
// without validation.
// ---------------------------------------------------------------------------
func TestAdversary_KeyIDValidation_Empty(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	err := ks.Create("", KeyTypeCA, material)
	if err != nil {
		t.Logf("Empty keyID rejected: %v", err)
	} else {
		t.Log("NOTE: Empty keyID was accepted — interface has no validation (MEDIUM)")
		defer func() { _ = ks.Delete("") }()
	}
	t.Log("MEDIUM FINDING: KeyID type is string with no charset validation — empty, long, unicode, or path-traversal IDs are accepted")
}

func TestAdversary_KeyIDValidation_Long(t *testing.T) {
	ks := NewFakeKeyStore()
	longID := KeyID(strings.Repeat("a", 10000))
	material := testKeyMaterial(t, KeyTypeCA)
	err := ks.Create(longID, KeyTypeCA, material)
	if err != nil {
		t.Logf("Long keyID (10000 chars) rejected: %v", err)
	} else {
		t.Logf("NOTE: Very long keyID (%d chars) was accepted", len(longID))
		// Verify operations still work
		sig, sigErr := ks.Sign(longID, []byte("digest"))
		if sigErr != nil {
			t.Errorf("Sign with long keyID failed: %v", sigErr)
		} else if ok := ks.Verify(longID, []byte("digest"), sig); !ok {
			t.Error("Verify with long keyID failed")
		}
		_ = ks.Delete(longID)
	}
}

func TestAdversary_KeyIDValidation_Unicode(t *testing.T) {
	ks := NewFakeKeyStore()
	unicodeID := KeyID("🔥🦄-emoji-key-日本語")
	material := testKeyMaterial(t, KeyTypeCA)
	err := ks.Create(unicodeID, KeyTypeCA, material)
	if err != nil {
		t.Logf("Unicode keyID rejected: %v", err)
		return
	}
	t.Logf("NOTE: Unicode keyID %q was accepted", unicodeID)
	// Verify full lifecycle works
	sig, err := ks.Sign(unicodeID, []byte("digest"))
	if err != nil {
		t.Errorf("Sign with unicode keyID failed: %v", err)
	} else if ok := ks.Verify(unicodeID, []byte("digest"), sig); !ok {
		t.Error("Verify with unicode keyID failed")
	}
	_, err = ks.Load(unicodeID)
	if err != nil {
		t.Errorf("Load with unicode keyID failed: %v", err)
	}
	_ = ks.Delete(unicodeID)
}

func TestAdversary_KeyIDValidation_PathTraversal(t *testing.T) {
	ks := NewFakeKeyStore()
	pathIDs := []KeyID{
		"../../etc/passwd",
		"../../../etc/shadow",
		"foo/../bar",
		"../keys/ca",
	}
	for _, pid := range pathIDs {
		pid := pid
		material := testKeyMaterial(t, KeyTypeCA)
		err := ks.Create(pid, KeyTypeCA, material)
		if err != nil {
			t.Logf("Path-traversal keyID %q rejected: %v", pid, err)
		} else {
			t.Logf("NOTE: Path-traversal keyID %q was accepted (no filesystem impact for FakeKeyStore, but contract gap)", pid)
			_ = ks.Delete(pid)
		}
	}
	t.Log("MEDIUM FINDING: KeyStore interface allows arbitrary KeyID strings with no validation — suggest charset/format constraints in interface contract")
}

// ---------------------------------------------------------------------------
// Vector 12: Determinism
// Same key + same digest → same signature (ECDSA is non-deterministic by
// default). For T01 fake with testRandReader it is deterministic, which is
// fine for T01 but flag as MEDIUM for T03/T04 audit chains.
// ---------------------------------------------------------------------------
func TestAdversary_Determinism(t *testing.T) {
	ks := NewFakeKeyStore()
	material := testKeyMaterial(t, KeyTypeCA)
	if err := ks.Create("det-key", KeyTypeCA, material); err != nil {
		t.Fatal(err)
	}

	digest := []byte("deterministic-digest")
	sig1, err := ks.Sign("det-key", digest)
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := ks.Sign("det-key", digest)
	if err != nil {
		t.Fatal(err)
	}

	// Due to testRandReader (zero-reader returning all 0x42), signatures
	// should be identical.
	if len(sig1) != len(sig2) {
		t.Log("MEDIUM NOTE: Signatures differ in length — non-deterministic ECDSA")
		return
	}
	match := true
	for i := range sig1 {
		if sig1[i] != sig2[i] {
			match = false
			break
		}
	}
	if match {
		t.Log("MEDIUM NOTE: Signatures ARE deterministic (testRandReader). This is fine for T01 but problematic for T03/T04 audit chains which need reproducible record_hash without relying on a synthetic rand reader.")
	} else {
		t.Log("MEDIUM NOTE: Signatures differ (non-deterministic ECDSA despite testRandReader).")
	}
}

// ---------------------------------------------------------------------------
// Vector 13: Key material exposure in error messages
// Ensure errors don't include raw key bytes.
// ---------------------------------------------------------------------------
func TestAdversary_ErrorMessagesNoKeyMaterial(t *testing.T) {
	ks := NewFakeKeyStore()

	// Create with corrupted material to trigger parse errors
	corruptedMaterial := KeyMaterial{Type: KeyTypeCA, Bytes: []byte("not-a-valid-pem-key")}
	if err := ks.Create("bad-key", KeyTypeCA, corruptedMaterial); err != nil {
		t.Fatal(err)
	}

	_, err := ks.Sign("bad-key", []byte("digest"))
	if err == nil {
		t.Fatal("Expected Sign with corrupted key to fail")
	}
	errMsg := err.Error()
	if strings.Contains(errMsg, "not-a-valid-pem-key") {
		t.Errorf("BREAK: Error message leaks key material bytes: %q", errMsg)
	}

	t.Logf("Error message (acceptable): %q", errMsg)
}