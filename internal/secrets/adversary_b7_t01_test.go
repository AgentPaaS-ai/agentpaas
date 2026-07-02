package secrets

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestAdversary_B7_T01_ConcurrentAccess exercises FakeKeyStore under concurrent load
// to detect races in Set/Get/Delete/Touch (run with -race).
func TestAdversary_B7_T01_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	const goroutines = 50
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines * 4)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			name := "concurrent-key"
			val := []byte("val-" + string(rune('a'+id%26)))
			for j := 0; j < opsPerGoroutine; j++ {
				_ = store.Set(ctx, name, val)
				_, _ = store.Get(ctx, name)
				_ = store.TouchLastUsed(ctx, name)
				_ = store.Delete(ctx, name)
				_ = store.Set(ctx, name, val)
			}
		}(i)

		go func(id int) {
			defer wg.Done()
			name := "concurrent-key2-" + string(rune('a'+id%10))
			val := bytes.Repeat([]byte("x"), 100)
			for j := 0; j < opsPerGoroutine; j++ {
				_ = store.Set(ctx, name, val)
				_, _ = store.Get(ctx, name)
				_ = store.Delete(ctx, name)
			}
		}(i)

		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				_, _ = store.List(ctx)
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				_ = store.TouchLastUsed(ctx, "concurrent-key")
			}
		}()
	}

	wg.Wait()
	// If no race detected under -race, this confirms safe.
}

// TestAdversary_B7_T01_EmptyStdinAndEOF confirms empty stdin does not crash or store garbage.
func TestAdversary_B7_T01_EmptyStdinAndEOF(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	// Empty value should be accepted (0 bytes <= 64k)
	err := store.Set(ctx, "empty-secret", []byte{})
	if err != nil {
		t.Fatalf("Set empty value: %v", err)
	}
	got, err := store.Get(ctx, "empty-secret")
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty secret length = %d, want 0", len(got))
	}
}

// TestAdversary_B7_T01_Exact64KiBBoundary tests the precise max size limit.
func TestAdversary_B7_T01_Exact64KiBBoundary(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	exact := bytes.Repeat([]byte("a"), MaxSecretValueSize)
	if err := store.Set(ctx, "exact-64k", exact); err != nil {
		t.Fatalf("Set exact 64KiB: %v", err)
	}
	got, err := store.Get(ctx, "exact-64k")
	if err != nil || len(got) != MaxSecretValueSize {
		t.Fatalf("Get exact 64KiB failed or wrong len: %v len=%d", err, len(got))
	}

	oversize := bytes.Repeat([]byte("b"), MaxSecretValueSize+1)
	err = store.Set(ctx, "oversize", oversize)
	if !errors.Is(err, ErrSecretTooLarge) {
		t.Fatalf("Set 64k+1 error = %v, want ErrSecretTooLarge", err)
	}
	_, err = store.Get(ctx, "oversize")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after oversize rejection: %v, want NotFound", err)
	}
}

// TestAdversary_B7_T01_LargeNameAndPathTraversal checks long names and ../ etc.
func TestAdversary_B7_T01_LargeNameAndPathTraversal(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	longName := strings.Repeat("x", 10*1024)
	err := store.Set(ctx, longName, []byte("val"))
	if err != nil {
		t.Logf("long name (10k) rejected (may be ok): %v", err)
	} else {
		t.Logf("long name accepted")
	}

	// Path traversal style names — validate allows (only rejects space/control)
	traversalNames := []string{"../secret", "./secret", "dir/secret", "a\\b", "../../etc/passwd"}
	for _, n := range traversalNames {
		err := ValidateSecretName(n)
		if err != nil {
			t.Fatalf("Validate allowed traversal? %q -> %v", n, err)
		}
		if err := store.Set(ctx, n, []byte("val")); err != nil {
			t.Fatalf("Set traversal name %q: %v", n, err)
		}
	}
}

// TestAdversary_B7_T01_ValueNeverInErrorsOrMetadata confirms no leakage of secret value.
func TestAdversary_B7_T01_ValueNeverInErrorsOrMetadata(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	secret := []byte("ADVERSARY_LEAK_MARKER_secret-value-xyz-123")

	if err := store.Set(ctx, "leak-test", secret); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Force not-found error (name is in error, value must not be)
	_, err := store.Get(ctx, "nonexistent")
	if err == nil || strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("Get notfound error leaked value: %v", err)
	}

	// Delete notfound
	err = store.Delete(ctx, "nonexistent2")
	if err == nil || strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("Delete notfound leaked value: %v", err)
	}

	// List must not contain value
	meta, _ := store.List(ctx)
	for _, m := range meta {
		if strings.Contains(m.Name, string(secret)) {
			t.Fatalf("List metadata name leaked value")
		}
	}
}

// TestAdversary_B7_T01_RmIdempotencyAndGetDoesNotTouchLastUsed confirms Delete behavior and Get semantics.
func TestAdversary_B7_T01_RmIdempotencyAndGetDoesNotTouchLastUsed(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	// rm non-existent errors (current behavior)
	err := store.Delete(ctx, "missing")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Delete missing: %v, want NotFound", err)
	}

	// Get must not update LastUsedAt
	if err := store.Set(ctx, "touch-test", []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	meta, _ := store.List(ctx)
	initial := meta[0].LastUsedAt
	_, _ = store.Get(ctx, "touch-test")
	meta, _ = store.List(ctx)
	if !meta[0].LastUsedAt.Equal(initial) {
		t.Fatalf("Get updated LastUsedAt (should only Touch do it)")
	}
}

// TestAdversary_B7_T01_UnicodeHomoglyphsAndControls confirms validate rejects controls/whitespace variants.
func TestAdversary_B7_T01_UnicodeHomoglyphsAndControls(t *testing.T) {
	// \u00A0 is non-breaking space (IsSpace true), \u200B zero-width, control chars.
	bad := []string{"name\u00A0withnbsp", "zero\u200Bwidth", "ctrl\u0007bell", "name\r\n", ""}
	for _, n := range bad {
		if err := ValidateSecretName(n); err == nil {
			t.Fatalf("ValidateSecretName(%q) accepted bad unicode/control", n)
		}
	}
	// Printable unicode ok
	if err := ValidateSecretName("café-ключ_01"); err != nil {
		t.Fatalf("valid unicode name rejected: %v", err)
	}
}
