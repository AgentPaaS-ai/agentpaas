package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"testing"
)

func TestKeychainStore_GuardedIntegration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS")
	}
	if os.Getenv("AGENTPAAS_KEYCHAIN_TESTS") == "" {
		t.Skip("set AGENTPAAS_KEYCHAIN_TESTS=1 to run keychain tests")
	}

	ctx := context.Background()
	service := "ai.agentpaas.secrets.test." + randomSuffix(t)
	store, err := NewKeychainStore(service)
	if err != nil {
		t.Fatalf("NewKeychainStore: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Delete(ctx, "integration_secret")
		_ = store.deleteManifest(ctx)
	})

	if err := store.Set(ctx, "integration_secret", []byte("keychain-value")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.Get(ctx, "integration_secret")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "keychain-value" {
		t.Fatalf("Get value = %q, want keychain-value", got)
	}

	meta, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(meta) != 1 || meta[0].Name != "integration_secret" {
		t.Fatalf("List metadata = %+v, want integration_secret only", meta)
	}

	if err := store.TouchLastUsed(ctx, "integration_secret"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}
	if err := store.Delete(ctx, "integration_secret"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "integration_secret")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrSecretNotFound", err)
	}
}

func TestKeychainStore_NoPlaintextFallbackOnUnsupportedOS(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("unsupported-OS behavior only applies off macOS")
	}

	_, err := NewKeychainStore("ai.agentpaas.secrets.test")
	if !errors.Is(err, ErrKeychainUnavailable) {
		t.Fatalf("NewKeychainStore error = %v, want ErrKeychainUnavailable", err)
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b[:])
}
