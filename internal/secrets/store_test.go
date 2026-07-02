package secrets

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFakeKeyStore_SetGetListTouchDelete(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	value := []byte("top-secret-value")

	if err := store.Set(ctx, "api_key", value); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, "api_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("Get value = %q, want %q", got, value)
	}

	// Mutating the returned bytes must not mutate the stored secret.
	got[0] = 'X'
	gotAgain, err := store.Get(ctx, "api_key")
	if err != nil {
		t.Fatalf("Get again: %v", err)
	}
	if !bytes.Equal(gotAgain, value) {
		t.Fatalf("stored value mutated through returned slice: %q", gotAgain)
	}

	meta, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("List length = %d, want 1", len(meta))
	}
	if meta[0].Name != "api_key" {
		t.Fatalf("List name = %q, want api_key", meta[0].Name)
	}
	if meta[0].CreatedAt.IsZero() || meta[0].UpdatedAt.IsZero() {
		t.Fatalf("List metadata missing timestamps: %+v", meta[0])
	}
	if !meta[0].LastUsedAt.IsZero() {
		t.Fatalf("LastUsedAt before touch = %s, want zero", meta[0].LastUsedAt)
	}

	if err := store.TouchLastUsed(ctx, "api_key"); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}
	meta, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List after touch: %v", err)
	}
	if meta[0].LastUsedAt.IsZero() {
		t.Fatal("LastUsedAt after touch is zero")
	}

	if err := store.Delete(ctx, "api_key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "api_key")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrSecretNotFound", err)
	}
}

func TestFakeKeyStore_RejectsInvalidNames(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	names := []string{
		"",
		"has space",
		"has\ttab",
		"has\nnewline",
		"has\rreturn",
		"has\x00null",
	}
	for _, name := range names {
		err := store.Set(ctx, name, []byte("value"))
		if !errors.Is(err, ErrInvalidSecretName) {
			t.Fatalf("Set(%q) error = %v, want ErrInvalidSecretName", name, err)
		}
	}
}

func TestFakeKeyStore_RejectsOversizeWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	value := bytes.Repeat([]byte("x"), MaxSecretValueSize+1)

	err := store.Set(ctx, "too_large", value)
	if !errors.Is(err, ErrSecretTooLarge) {
		t.Fatalf("Set oversize error = %v, want ErrSecretTooLarge", err)
	}

	_, err = store.Get(ctx, "too_large")
	if !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("Get after oversize Set error = %v, want ErrSecretNotFound", err)
	}
}

func TestFakeKeyStore_CaseSensitiveNames(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()

	if err := store.Set(ctx, "MYKEY", []byte("upper")); err != nil {
		t.Fatalf("Set MYKEY: %v", err)
	}
	if err := store.Set(ctx, "mykey", []byte("lower")); err != nil {
		t.Fatalf("Set mykey: %v", err)
	}

	upper, err := store.Get(ctx, "MYKEY")
	if err != nil {
		t.Fatalf("Get MYKEY: %v", err)
	}
	lower, err := store.Get(ctx, "mykey")
	if err != nil {
		t.Fatalf("Get mykey: %v", err)
	}
	if string(upper) != "upper" || string(lower) != "lower" {
		t.Fatalf("case-sensitive values not preserved: MYKEY=%q mykey=%q", upper, lower)
	}
}

func TestValidateSecretNameRejectsWhitespaceAndControls(t *testing.T) {
	for _, name := range []string{"", "two words", "line\nbreak", "bad\x7fdelete"} {
		if err := ValidateSecretName(name); !errors.Is(err, ErrInvalidSecretName) {
			t.Fatalf("ValidateSecretName(%q) error = %v, want ErrInvalidSecretName", name, err)
		}
	}
	if err := ValidateSecretName("Api_Key-1.prod"); err != nil {
		t.Fatalf("ValidateSecretName valid name: %v", err)
	}
}

func TestListMetadataDoesNotContainSecretValue(t *testing.T) {
	ctx := context.Background()
	store := NewFakeKeyStore()
	secretValue := "never-print-this-secret"

	if err := store.Set(ctx, "metadata_only", []byte(secretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	meta, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(meta) != 1 {
		t.Fatalf("List length = %d, want 1", len(meta))
	}
	if strings.Contains(meta[0].Name, secretValue) {
		t.Fatalf("metadata leaked secret value: %+v", meta[0])
	}
}
