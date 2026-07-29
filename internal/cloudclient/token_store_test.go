package cloudclient

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestFakeTokenStore_SetGetDelete(t *testing.T) {
	store := NewFakeTokenStore()
	ctx := context.Background()

	// Get on empty store returns ErrTokenNotFound.
	_, err := store.Get(ctx)
	if !IsTokenNotFoundErr(err) {
		t.Fatalf("Get on empty store: error = %v, want ErrTokenNotFound", err)
	}

	// Set a token.
	if err := store.Set(ctx, "apc_test123"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get returns the token.
	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "apc_test123" {
		t.Fatalf("Get = %q, want apc_test123", got)
	}

	// Overwrite with new token.
	if err := store.Set(ctx, "apc_new456"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	got, err = store.Get(ctx)
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if got != "apc_new456" {
		t.Fatalf("Get after overwrite = %q, want apc_new456", got)
	}

	// Delete.
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get after delete returns ErrTokenNotFound.
	_, err = store.Get(ctx)
	if !IsTokenNotFoundErr(err) {
		t.Fatalf("Get after delete: error = %v, want ErrTokenNotFound", err)
	}

	// Delete is idempotent.
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

func TestFakeTokenStore_Concurrency(t *testing.T) {
	store := NewFakeTokenStore()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = store.Set(ctx, "concurrent")
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			_, _ = store.Get(ctx)
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

func TestKeychainTokenStore_NoPlaintextFallback(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("unsupported-OS behavior only applies off macOS")
	}
	_, err := NewKeychainTokenStore()
	if !IsTokenStoreUnavailableErr(err) {
		t.Fatalf("NewKeychainTokenStore error = %v, want ErrTokenStoreUnavailable", err)
	}
}

func TestKeychainTokenStore_WithEnvVar(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("set AGENTPAAS_KEYCHAIN_TESTS=1 to run keychain tests")
	}

	ctx := context.Background()
	store, err := NewKeychainTokenStore()
	if err != nil {
		t.Fatalf("NewKeychainTokenStore: %v", err)
	}

	// Ensure clean state.
	_ = store.Delete(ctx)

	// Set token.
	if err := store.Set(ctx, "apc_keychain_test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx) })

	// Get token.
	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "apc_keychain_test" {
		t.Fatalf("Get = %q, want apc_keychain_test", got)
	}

	// Delete token.
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get after delete returns error.
	_, err = store.Get(ctx)
	if !IsTokenNotFoundErr(err) {
		t.Fatalf("Get after delete: error = %v, want ErrTokenNotFound", err)
	}

	// Delete is idempotent.
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

func TestIsTokenNotFoundErr(t *testing.T) {
	if IsTokenNotFoundErr(nil) {
		t.Error("nil should not be token not found")
	}
	if !IsTokenNotFoundErr(ErrTokenNotFound) {
		t.Error("ErrTokenNotFound should be detected")
	}
	if !IsTokenNotFoundErr(errors.New("cloud api token not found: details")) {
		t.Error("wrapped ErrTokenNotFound should be detected")
	}
	if IsTokenNotFoundErr(errors.New("some other error")) {
		t.Error("unrelated error should not be detected")
	}
}

func TestIsTokenStoreUnavailableErr(t *testing.T) {
	if IsTokenStoreUnavailableErr(nil) {
		t.Error("nil should not be token store unavailable")
	}
	if !IsTokenStoreUnavailableErr(ErrTokenStoreUnavailable) {
		t.Error("ErrTokenStoreUnavailable should be detected")
	}
}
